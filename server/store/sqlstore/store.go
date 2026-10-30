// Package sqlstore persists tasks in the plugin's own Postgres tables
// (prefix rt_tasks_), reached through the plugin SQL handle.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

var (
	// ErrNotFound is returned for a missing or soft-deleted task or checklist item.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when an external id already belongs to a deleted task.
	ErrConflict = errors.New("conflict")
	// ErrLimit is returned when a task already has the maximum number of checklist items.
	ErrLimit = errors.New("limit reached")
)

func nowMillis() int64 { return time.Now().UnixMilli() }

// Store reads and writes tasks.
type Store struct {
	db *sql.DB
}

// New wraps a database handle. Call Migrate before use.
func New(db *sql.DB) *Store { return &Store{db: db} }

const taskCols = `id, channel_id, title, description, status, priority, due_at, position,
	created_by, created_at, updated_at, completed_at, post_id, source, external_id, is_private`

type rowScanner interface{ Scan(dest ...any) error }

func scanTask(sc rowScanner) (*tasks.Task, error) {
	var (
		t                 tasks.Task
		due, completed    sql.NullInt64
		post, source, ext sql.NullString
	)
	if err := sc.Scan(&t.ID, &t.ChannelID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&due, &t.Position, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &completed, &post, &source, &ext, &t.Private); err != nil {
		return nil, err
	}
	if due.Valid {
		t.DueAt = &due.Int64
	}
	if completed.Valid {
		t.CompletedAt = &completed.Int64
	}
	t.PostID, t.Source, t.ExternalID = post.String, source.String, ext.String
	t.Assignees, t.Labels, t.Checklist = []string{}, []string{}, []tasks.ChecklistItem{}
	return &t, nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// builder accumulates a query and its numbered arguments.
type builder struct {
	sb   strings.Builder
	args []any
}

func (b *builder) arg(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *builder) write(s string) { b.sb.WriteString(s) }

func placeholders(b *builder, vals []string) string {
	ph := make([]string, len(vals))
	for i, v := range vals {
		ph[i] = b.arg(v)
	}
	return strings.Join(ph, ",")
}

// hydrate attaches assignees, labels and checklist items to each task.
func hydrate(ctx context.Context, q queryer, ts []*tasks.Task) error {
	if len(ts) == 0 {
		return nil
	}
	byID := make(map[string]*tasks.Task, len(ts))
	ids := make([]string, 0, len(ts))
	for _, t := range ts {
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}

	run := func(sqlText string, scan func(rows *sql.Rows) error) error {
		b := &builder{}
		b.write(strings.Replace(sqlText, "{ids}", placeholders(b, ids), 1))
		rows, err := q.QueryContext(ctx, b.sb.String(), b.args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			if err = scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}

	if err := run(`SELECT task_id, user_id FROM rt_tasks_assignee WHERE task_id IN ({ids}) ORDER BY user_id`, func(r *sql.Rows) error {
		var tid, uid string
		if err := r.Scan(&tid, &uid); err != nil {
			return err
		}
		byID[tid].Assignees = append(byID[tid].Assignees, uid)
		return nil
	}); err != nil {
		return err
	}
	if err := run(`SELECT task_id, label FROM rt_tasks_label WHERE task_id IN ({ids}) ORDER BY label`, func(r *sql.Rows) error {
		var tid, l string
		if err := r.Scan(&tid, &l); err != nil {
			return err
		}
		byID[tid].Labels = append(byID[tid].Labels, l)
		return nil
	}); err != nil {
		return err
	}
	return run(`SELECT task_id, id, text, done, position FROM rt_tasks_checklist_item WHERE task_id IN ({ids}) ORDER BY position, id`, func(r *sql.Rows) error {
		var tid string
		var it tasks.ChecklistItem
		if err := r.Scan(&tid, &it.ID, &it.Text, &it.Done, &it.Position); err != nil {
			return err
		}
		byID[tid].Checklist = append(byID[tid].Checklist, it)
		return nil
	})
}

func getTask(ctx context.Context, q queryer, id string) (*tasks.Task, error) {
	t, err := scanTask(q.QueryRowContext(ctx, `SELECT `+taskCols+` FROM rt_tasks_task WHERE id = $1 AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = hydrate(ctx, q, []*tasks.Task{t}); err != nil {
		return nil, err
	}
	return t, nil
}

// Get returns one live task with its assignees, labels and checklist.
func (s *Store) Get(ctx context.Context, id string) (*tasks.Task, error) {
	return getTask(ctx, s.db, id)
}

func (s *Store) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// endOfColumn returns a position after every live task in a channel's status column.
func endOfColumn(ctx context.Context, tx *sql.Tx, channelID, status string) (float64, error) {
	var maxPos sql.NullFloat64
	err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM rt_tasks_task WHERE channel_id = $1 AND status = $2 AND deleted_at IS NULL`,
		channelID, status).Scan(&maxPos)
	if err != nil {
		return 0, err
	}
	return maxPos.Float64 + 1000, nil
}

// setTable holds the fixed statements for a child table with one value per
// task, so no table or column name is ever built from a variable.
type setTable struct{ clear, add string }

var (
	assigneeSet = setTable{
		clear: `DELETE FROM rt_tasks_assignee WHERE task_id = $1`,
		add:   `INSERT INTO rt_tasks_assignee (task_id, user_id) VALUES ($1, $2)`,
	}
	labelSet = setTable{
		clear: `DELETE FROM rt_tasks_label WHERE task_id = $1`,
		add:   `INSERT INTO rt_tasks_label (task_id, label) VALUES ($1, $2)`,
	}
)

func replaceSet(ctx context.Context, tx *sql.Tx, set setTable, taskID string, vals []string) error {
	if _, err := tx.ExecContext(ctx, set.clear, taskID); err != nil {
		return err
	}
	for _, v := range vals {
		if _, err := tx.ExecContext(ctx, set.add, taskID, v); err != nil {
			return err
		}
	}
	return nil
}

// Create inserts a task. When in.Source and in.ExternalID are set the call is
// idempotent: an existing live task with that key is returned with created=false.
// A soft-deleted task holding the key yields ErrConflict.
func (s *Store) Create(ctx context.Context, in tasks.CreateInput, createdBy string) (task *tasks.Task, created bool, err error) {
	id := model.NewId()
	now := nowMillis()

	err = s.inTx(ctx, func(tx *sql.Tx) error {
		pos := 0.0
		if in.Position != nil {
			pos = *in.Position
		} else if pos, err = endOfColumn(ctx, tx, in.ChannelID, in.Status); err != nil {
			return err
		}

		var completed any
		if in.Status == tasks.StatusDone {
			completed = now
		}

		res, execErr := tx.ExecContext(ctx, `INSERT INTO rt_tasks_task
			(id, channel_id, title, description, status, priority, due_at, position,
			 created_by, created_at, updated_at, completed_at, post_id, source, external_id, is_private)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (source, external_id) WHERE source IS NOT NULL AND external_id IS NOT NULL DO NOTHING`,
			id, in.ChannelID, in.Title, in.Description, in.Status, in.Priority, in.DueAt, pos,
			createdBy, now, completed, nullIfEmpty(in.PostID), nullIfEmpty(in.Source), nullIfEmpty(in.ExternalID), in.Private != nil && *in.Private)
		if execErr != nil {
			return execErr
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// Idempotent hit: hand back the task that already owns the key.
			var existingID string
			var deleted sql.NullInt64
			if qErr := tx.QueryRowContext(ctx,
				`SELECT id, deleted_at FROM rt_tasks_task WHERE source = $1 AND external_id = $2`,
				in.Source, in.ExternalID).Scan(&existingID, &deleted); qErr != nil {
				return qErr
			}
			if deleted.Valid {
				return ErrConflict
			}
			id = existingID
			return nil
		}

		created = true
		if err = replaceSet(ctx, tx, assigneeSet, id, in.Assignees); err != nil {
			return err
		}
		if err = replaceSet(ctx, tx, labelSet, id, in.Labels); err != nil {
			return err
		}
		for i, text := range in.Checklist {
			if _, err = tx.ExecContext(ctx,
				`INSERT INTO rt_tasks_checklist_item (id, task_id, text, done, position) VALUES ($1,$2,$3,FALSE,$4)`,
				model.NewId(), id, text, i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}

	task, err = s.Get(ctx, id)
	return task, created, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Update applies a patch. Changing status to or from done maintains completed_at,
// and a status change moves the task to the end of its new column.
func (s *Store) Update(ctx context.Context, id string, p tasks.Patch) (*tasks.Task, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		cur, err := lockLive(ctx, tx, id)
		if err != nil {
			return err
		}

		now := nowMillis()
		b := &builder{}
		sets := []string{"updated_at = " + b.arg(now)}
		if p.Title != nil {
			sets = append(sets, "title = "+b.arg(*p.Title))
		}
		if p.Description != nil {
			sets = append(sets, "description = "+b.arg(*p.Description))
		}
		if p.Priority != nil {
			sets = append(sets, "priority = "+b.arg(*p.Priority))
		}
		if p.DueAt.Set {
			sets = append(sets, "due_at = "+b.arg(p.DueAt.Value))
		}
		if p.Private != nil {
			sets = append(sets, "is_private = "+b.arg(*p.Private))
		}
		if p.Status != nil && *p.Status != cur.Status {
			pos, perr := endOfColumn(ctx, tx, cur.ChannelID, *p.Status)
			if perr != nil {
				return perr
			}
			sets = append(sets, "status = "+b.arg(*p.Status), "position = "+b.arg(pos), completedSet(b, *p.Status, now))
		}

		b.write("UPDATE rt_tasks_task SET " + strings.Join(sets, ", ") + " WHERE id = " + b.arg(id))
		if _, err = tx.ExecContext(ctx, b.sb.String(), b.args...); err != nil {
			return err
		}
		if p.Assignees != nil {
			if err = replaceSet(ctx, tx, assigneeSet, id, *p.Assignees); err != nil {
				return err
			}
		}
		if p.Labels != nil {
			return replaceSet(ctx, tx, labelSet, id, *p.Labels)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func completedSet(b *builder, status string, now int64) string {
	if status == tasks.StatusDone {
		return "completed_at = " + b.arg(now)
	}
	return "completed_at = NULL"
}

// lockLive loads a live task row with a row lock so concurrent edits serialise.
func lockLive(ctx context.Context, tx *sql.Tx, id string) (*tasks.Task, error) {
	t, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskCols+` FROM rt_tasks_task WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// Move changes a task's status and ordering. A nil position puts it last in the column.
func (s *Store) Move(ctx context.Context, id, status string, position *float64) (*tasks.Task, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		cur, err := lockLive(ctx, tx, id)
		if err != nil {
			return err
		}
		pos := 0.0
		if position != nil {
			pos = *position
		} else if pos, err = endOfColumn(ctx, tx, cur.ChannelID, status); err != nil {
			return err
		}

		now := nowMillis()
		b := &builder{}
		b.write("UPDATE rt_tasks_task SET status = " + b.arg(status) + ", position = " + b.arg(pos) + ", updated_at = " + b.arg(now))
		if status != cur.Status {
			b.write(", " + completedSet(b, status, now))
		}
		b.write(" WHERE id = " + b.arg(id))
		_, err = tx.ExecContext(ctx, b.sb.String(), b.args...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete soft-deletes a task. Its external id stays reserved.
func (s *Store) Delete(ctx context.Context, id string) error {
	now := nowMillis()
	res, err := s.db.ExecContext(ctx, `UPDATE rt_tasks_task SET deleted_at = $1, updated_at = $1 WHERE id = $2 AND deleted_at IS NULL`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// List returns live tasks matching the filter, ordered for board display.
func (s *Store) List(ctx context.Context, f tasks.Filter) ([]*tasks.Task, error) {
	if len(f.ChannelIDs) == 0 || f.ViewerID == "" {
		return []*tasks.Task{}, nil
	}

	b := &builder{}
	b.write(`SELECT ` + taskCols + ` FROM rt_tasks_task WHERE deleted_at IS NULL AND channel_id IN (` + placeholders(b, f.ChannelIDs) + `)`)
	viewer := b.arg(f.ViewerID)
	b.write(` AND (NOT is_private OR created_by = ` + viewer +
		` OR EXISTS (SELECT 1 FROM rt_tasks_assignee v WHERE v.task_id = rt_tasks_task.id AND v.user_id = ` + viewer + `))`)
	if f.AssigneeID != "" {
		b.write(` AND EXISTS (SELECT 1 FROM rt_tasks_assignee a WHERE a.task_id = rt_tasks_task.id AND a.user_id = ` + b.arg(f.AssigneeID) + `)`)
	}
	if len(f.Statuses) > 0 {
		b.write(` AND status IN (` + placeholders(b, f.Statuses) + `)`)
	}
	if f.Priority != "" {
		b.write(` AND priority = ` + b.arg(f.Priority))
	}
	if f.Label != "" {
		b.write(` AND EXISTS (SELECT 1 FROM rt_tasks_label l WHERE l.task_id = rt_tasks_task.id AND l.label = ` + b.arg(strings.ToLower(f.Label)) + `)`)
	}
	if f.Query != "" {
		p := b.arg("%" + escapeLike(f.Query) + "%")
		b.write(` AND (title ILIKE ` + p + ` ESCAPE '\' OR description ILIKE ` + p + ` ESCAPE '\')`)
	}
	if f.DueBefore != nil {
		b.write(` AND due_at IS NOT NULL AND due_at < ` + b.arg(*f.DueBefore))
	}
	if f.DueAfter != nil {
		b.write(` AND due_at IS NOT NULL AND due_at >= ` + b.arg(*f.DueAfter))
	}

	limit := f.Limit
	if limit <= 0 {
		limit = tasks.DefaultLimit
	}
	if limit > tasks.MaxLimit {
		limit = tasks.MaxLimit
	}
	offset := max(f.Offset, 0)
	b.write(` ORDER BY position ASC, created_at ASC, id ASC LIMIT ` + b.arg(limit) + ` OFFSET ` + b.arg(offset))

	rows, err := s.db.QueryContext(ctx, b.sb.String(), b.args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []*tasks.Task{}
	for rows.Next() {
		t, scanErr := scanTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = hydrate(ctx, s.db, out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddChecklistItem appends an item to a task.
func (s *Store) AddChecklistItem(ctx context.Context, taskID, text string) (*tasks.ChecklistItem, error) {
	var item tasks.ChecklistItem
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := lockLive(ctx, tx, taskID); err != nil {
			return err
		}
		var count int
		var maxPos sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), MAX(position) FROM rt_tasks_checklist_item WHERE task_id = $1`, taskID).Scan(&count, &maxPos); err != nil {
			return err
		}
		if count >= tasks.MaxChecklistItems {
			return ErrLimit
		}
		item = tasks.ChecklistItem{ID: model.NewId(), Text: text, Position: 0}
		if maxPos.Valid {
			item.Position = int(maxPos.Int64) + 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO rt_tasks_checklist_item (id, task_id, text, done, position) VALUES ($1,$2,$3,FALSE,$4)`,
			item.ID, taskID, item.Text, item.Position); err != nil {
			return err
		}
		return touch(ctx, tx, taskID)
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateChecklistItem edits an item's text and/or done flag.
func (s *Store) UpdateChecklistItem(ctx context.Context, taskID, itemID string, text *string, done *bool) (*tasks.ChecklistItem, error) {
	var item tasks.ChecklistItem
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := lockLive(ctx, tx, taskID); err != nil {
			return err
		}
		b := &builder{}
		sets := []string{}
		if text != nil {
			sets = append(sets, "text = "+b.arg(*text))
		}
		if done != nil {
			sets = append(sets, "done = "+b.arg(*done))
		}
		if len(sets) > 0 {
			b.write("UPDATE rt_tasks_checklist_item SET " + strings.Join(sets, ", ") + " WHERE id = " + b.arg(itemID) + " AND task_id = " + b.arg(taskID))
			res, err := tx.ExecContext(ctx, b.sb.String(), b.args...)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return ErrNotFound
			}
		}
		err := tx.QueryRowContext(ctx, `SELECT id, text, done, position FROM rt_tasks_checklist_item WHERE id = $1 AND task_id = $2`, itemID, taskID).
			Scan(&item.ID, &item.Text, &item.Done, &item.Position)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return touch(ctx, tx, taskID)
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// DeleteChecklistItem removes an item.
func (s *Store) DeleteChecklistItem(ctx context.Context, taskID, itemID string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := lockLive(ctx, tx, taskID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM rt_tasks_checklist_item WHERE id = $1 AND task_id = $2`, itemID, taskID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return touch(ctx, tx, taskID)
	})
}

func touch(ctx context.Context, tx *sql.Tx, taskID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE rt_tasks_task SET updated_at = $1 WHERE id = $2`, nowMillis(), taskID)
	return err
}

// UserChannelIDs lists the live channels a user belongs to. It reads Mattermost's
// own channelmembers table, so the integration tests run against a real server
// to catch schema drift.
func (s *Store) UserChannelIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT cm.channelid FROM channelmembers cm JOIN channels c ON c.id = cm.channelid WHERE cm.userid = $1 AND c.deleteat = 0`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
