package sqlstore

import (
	"context"
	"database/sql"
)

// Reminder kinds.
const (
	KindDueSoon = "due_soon"
	KindOverdue = "overdue"
)

// ReminderCandidate is one assignee who has not yet been told about one task.
type ReminderCandidate struct {
	TaskID    string
	ChannelID string
	Title     string
	DueAt     int64
	PostID    string
	UserID    string
	Kind      string
}

// maxReminderCandidates bounds one job run so a backlog cannot flood anyone.
const maxReminderCandidates = 1000

// DueForReminder lists open, assigned tasks that are due within `soonWindow`
// milliseconds of now, or overdue by no more than `overdueMaxAge`, for which the
// assignee has not been reminded of that kind for the task's current due date.
func (s *Store) DueForReminder(ctx context.Context, now, soonWindow, overdueMaxAge int64) ([]ReminderCandidate, error) {
	const kindExpr = `CASE WHEN t.due_at < $1 THEN 'overdue' ELSE 'due_soon' END`
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.channel_id, t.title, t.due_at, t.post_id, a.user_id, `+kindExpr+`
		FROM rt_tasks_task t
		JOIN rt_tasks_assignee a ON a.task_id = t.id
		WHERE t.deleted_at IS NULL AND t.completed_at IS NULL AND t.status <> 'done'
		  AND t.due_at IS NOT NULL AND t.due_at <= $2 AND t.due_at >= $3
		  AND NOT EXISTS (
		    SELECT 1 FROM rt_tasks_reminder_state r
		    WHERE r.task_id = t.id AND r.user_id = a.user_id AND r.due_at = t.due_at AND r.kind = `+kindExpr+`
		  )
		ORDER BY t.due_at, t.id, a.user_id
		LIMIT $4`, now, now+soonWindow, now-overdueMaxAge, maxReminderCandidates)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []ReminderCandidate{}
	for rows.Next() {
		var c ReminderCandidate
		var post sql.NullString
		if err = rows.Scan(&c.TaskID, &c.ChannelID, &c.Title, &c.DueAt, &post, &c.UserID, &c.Kind); err != nil {
			return nil, err
		}
		c.PostID = post.String
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClaimReminder records that a reminder is being sent. It returns false when that
// reminder was already claimed, so concurrent job runs never send one twice.
func (s *Store) ClaimReminder(ctx context.Context, c ReminderCandidate) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO rt_tasks_reminder_state (task_id, user_id, kind, due_at, sent_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING`, c.TaskID, c.UserID, c.Kind, c.DueAt, nowMillis())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ReleaseReminder undoes a claim after a failed send so the next run retries it.
func (s *Store) ReleaseReminder(ctx context.Context, c ReminderCandidate) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM rt_tasks_reminder_state
		WHERE task_id = $1 AND user_id = $2 AND kind = $3 AND due_at = $4`, c.TaskID, c.UserID, c.Kind, c.DueAt)
	return err
}
