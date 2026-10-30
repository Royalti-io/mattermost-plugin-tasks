package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

var (
	pgOnce sync.Once
	pgDSN  string
	pgErr  error
	dbSeq  atomic.Int64
)

// newStore returns a migrated Store on a fresh database inside one shared Postgres container.
func newStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("SKIP_DOCKER_TESTS") != "" {
		t.Skip("SKIP_DOCKER_TESTS set")
	}

	pgOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				pgErr = fmt.Errorf("docker not available: %v", r)
			}
		}()
		ctx := context.Background()
		c, err := postgres.Run(ctx, "postgres:16-alpine",
			postgres.WithDatabase("postgres"), postgres.WithUsername("t"), postgres.WithPassword("t"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)))
		if err != nil {
			pgErr = err
			return
		}
		pgDSN, pgErr = c.ConnectionString(ctx, "sslmode=disable")
	})
	require.NoError(t, pgErr)

	admin, err := sql.Open("postgres", pgDSN)
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()

	name := fmt.Sprintf("t%d", dbSeq.Add(1))
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)

	db, err := sql.Open("postgres", replaceDB(pgDSN, name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	s := New(db)
	require.NoError(t, s.Migrate(context.Background()))
	return s
}

// replaceDB swaps the database name in a postgres:// DSN of the form .../postgres?...
func replaceDB(dsn, name string) string {
	const marker = "/postgres?"
	for i := 0; i+len(marker) <= len(dsn); i++ {
		if dsn[i:i+len(marker)] == marker {
			return dsn[:i] + "/" + name + "?" + dsn[i+len(marker):]
		}
	}
	panic("unexpected dsn: " + dsn)
}

func mk(t *testing.T, s *Store, in tasks.CreateInput) *tasks.Task {
	t.Helper()
	in, err := in.Normalize()
	require.NoError(t, err)
	got, created, err := s.Create(context.Background(), in, "creator")
	require.NoError(t, err)
	require.True(t, created)
	return got
}

func ids(ts []*tasks.Task) []string {
	out := make([]string, len(ts))
	for i, x := range ts {
		out[i] = x.ID
	}
	return out
}

func TestMigrateIsIdempotentAndConcurrencySafe(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.Migrate(ctx))

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Migrate(ctx) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}

	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM rt_tasks_migrations`).Scan(&n))
	assert.Equal(t, len(migrations), n)
}

func TestCreateAndGet(t *testing.T) {
	s := newStore(t)
	due := int64(1_900_000_000_000)
	got := mk(t, s, tasks.CreateInput{
		ChannelID: "chan1", Title: "Write plan", Description: "d", Priority: tasks.PriorityHigh,
		DueAt: &due, Assignees: []string{"u2", "u1"}, Labels: []string{"Docs", "docs", "ops"},
		Checklist: []string{"one", "two"}, PostID: "post1",
	})

	assert.Len(t, got.ID, 26)
	assert.Equal(t, tasks.StatusTodo, got.Status)
	assert.Equal(t, "creator", got.CreatedBy)
	assert.Equal(t, []string{"u1", "u2"}, got.Assignees)
	assert.Equal(t, []string{"docs", "ops"}, got.Labels)
	require.Len(t, got.Checklist, 2)
	assert.Equal(t, "one", got.Checklist[0].Text)
	assert.Equal(t, 1, got.Checklist[1].Position)
	assert.Equal(t, "post1", got.PostID)
	require.NotNil(t, got.DueAt)
	assert.Equal(t, due, *got.DueAt)
	assert.Nil(t, got.CompletedAt)

	again, err := s.Get(context.Background(), got.ID)
	require.NoError(t, err)
	assert.Equal(t, got, again)

	_, err = s.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCreateDoneSetsCompletedAt(t *testing.T) {
	s := newStore(t)
	got := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "already done", Status: tasks.StatusDone})
	assert.NotNil(t, got.CompletedAt)
}

func TestCreateIsIdempotentOnSourceAndExternalID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	in, err := tasks.CreateInput{ChannelID: "c", Title: "from pa", Source: "pa", ExternalID: "42"}.Normalize()
	require.NoError(t, err)

	first, created, err := s.Create(ctx, in, "bot")
	require.NoError(t, err)
	assert.True(t, created)

	in.Title = "changed title is ignored on a repeat"
	second, created, err := s.Create(ctx, in, "bot")
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "from pa", second.Title)

	require.NoError(t, s.Delete(ctx, first.ID))
	_, _, err = s.Create(ctx, in, "bot")
	assert.ErrorIs(t, err, ErrConflict)
}

func TestCreateConcurrentSameExternalIDYieldsOneTask(t *testing.T) {
	s := newStore(t)
	in, err := tasks.CreateInput{ChannelID: "c", Title: "race", Source: "pa", ExternalID: "race"}.Normalize()
	require.NoError(t, err)

	var wg sync.WaitGroup
	var createdCount atomic.Int64
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, cerr := s.Create(context.Background(), in, "bot")
			assert.NoError(t, cerr)
			if created {
				createdCount.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(1), createdCount.Load())

	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM rt_tasks_task WHERE source='pa' AND external_id='race'`).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestPositionsAppendToEndOfColumn(t *testing.T) {
	s := newStore(t)
	a := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "a"})
	b := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "b"})
	other := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "other col", Status: tasks.StatusBlocked})
	assert.Less(t, a.Position, b.Position)
	assert.Equal(t, 1000.0, other.Position, "each status column counts separately")
}

func TestListScopingAndFilters(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	soon, later := int64(1000), int64(5000)

	t1 := mk(t, s, tasks.CreateInput{ChannelID: "c1", Title: "Alpha release", Assignees: []string{"ann"}, Labels: []string{"bug"}, DueAt: &soon, Priority: tasks.PriorityHigh})
	t2 := mk(t, s, tasks.CreateInput{ChannelID: "c1", Title: "Beta 100% done", Status: tasks.StatusInProgress, Assignees: []string{"bob"}, DueAt: &later})
	t3 := mk(t, s, tasks.CreateInput{ChannelID: "c2", Title: "Gamma", Assignees: []string{"ann"}})
	t4 := mk(t, s, tasks.CreateInput{ChannelID: "c3", Title: "Hidden channel"})

	list := func(f tasks.Filter) []string {
		if f.ViewerID == "" {
			f.ViewerID = "viewer"
		}
		got, err := s.List(ctx, f)
		require.NoError(t, err)
		return ids(got)
	}

	assert.Empty(t, list(tasks.Filter{}), "no channels means no results")
	got, err := s.List(ctx, tasks.Filter{ChannelIDs: []string{"c1"}})
	require.NoError(t, err)
	assert.Empty(t, got, "no viewer means no results")
	assert.ElementsMatch(t, []string{t1.ID, t2.ID, t3.ID}, list(tasks.Filter{ChannelIDs: []string{"c1", "c2"}}))
	assert.NotContains(t, list(tasks.Filter{ChannelIDs: []string{"c1", "c2"}}), t4.ID)
	assert.ElementsMatch(t, []string{t1.ID, t3.ID}, list(tasks.Filter{ChannelIDs: []string{"c1", "c2"}, AssigneeID: "ann"}))
	assert.Equal(t, []string{t2.ID}, list(tasks.Filter{ChannelIDs: []string{"c1"}, Statuses: []string{tasks.StatusInProgress}}))
	assert.Equal(t, []string{t1.ID}, list(tasks.Filter{ChannelIDs: []string{"c1"}, Label: "BUG"}))
	assert.Equal(t, []string{t1.ID}, list(tasks.Filter{ChannelIDs: []string{"c1"}, Priority: tasks.PriorityHigh}))
	assert.Equal(t, []string{t2.ID}, list(tasks.Filter{ChannelIDs: []string{"c1"}, Query: "100%"}), "% is literal")
	assert.Equal(t, []string{t1.ID}, list(tasks.Filter{ChannelIDs: []string{"c1"}, DueBefore: &later}))
	assert.Equal(t, []string{t2.ID}, list(tasks.Filter{ChannelIDs: []string{"c1"}, DueAfter: &later}))
	assert.Len(t, list(tasks.Filter{ChannelIDs: []string{"c1", "c2"}, Limit: 2}), 2)
	assert.Len(t, list(tasks.Filter{ChannelIDs: []string{"c1", "c2"}, Limit: 2, Offset: 2}), 1)
}

func TestUpdate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	due := int64(1234)
	task := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "old", Assignees: []string{"a"}, Labels: []string{"x"}, DueAt: &due})
	before := task.UpdatedAt

	title, status := "new", tasks.StatusDone
	assignees, labels := []string{"b", "c"}, []string{}
	got, err := s.Update(ctx, task.ID, tasks.Patch{Title: &title, Status: &status, Assignees: &assignees, Labels: &labels, DueAt: tasks.OptionalInt64{Set: true}})
	require.NoError(t, err)
	assert.Equal(t, "new", got.Title)
	assert.Equal(t, tasks.StatusDone, got.Status)
	assert.NotNil(t, got.CompletedAt)
	assert.Equal(t, []string{"b", "c"}, got.Assignees)
	assert.Empty(t, got.Labels)
	assert.Nil(t, got.DueAt, "explicit null clears the due date")
	assert.GreaterOrEqual(t, got.UpdatedAt, before)

	reopen := tasks.StatusTodo
	got, err = s.Update(ctx, task.ID, tasks.Patch{Status: &reopen})
	require.NoError(t, err)
	assert.Nil(t, got.CompletedAt, "leaving done clears completed_at")
	assert.Equal(t, []string{"b", "c"}, got.Assignees, "absent fields are untouched")

	_, err = s.Update(ctx, "missing", tasks.Patch{Title: &title})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMove(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "a"})
	b := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "b"})

	pos := a.Position - 500
	moved, err := s.Move(ctx, b.ID, tasks.StatusTodo, &pos)
	require.NoError(t, err)
	listed, err := s.List(ctx, tasks.Filter{ChannelIDs: []string{"c"}, ViewerID: "viewer"})
	require.NoError(t, err)
	assert.Equal(t, []string{moved.ID, a.ID}, ids(listed), "explicit position reorders within a column")

	done, err := s.Move(ctx, a.ID, tasks.StatusDone, nil)
	require.NoError(t, err)
	assert.Equal(t, tasks.StatusDone, done.Status)
	assert.NotNil(t, done.CompletedAt)
	assert.Equal(t, 1000.0, done.Position)

	_, err = s.Move(ctx, "missing", tasks.StatusTodo, nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeleteIsSoftAndHidesTask(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	task := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "bye"})

	require.NoError(t, s.Delete(ctx, task.ID))
	_, err := s.Get(ctx, task.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	listed, err := s.List(ctx, tasks.Filter{ChannelIDs: []string{"c"}, ViewerID: "viewer"})
	require.NoError(t, err)
	assert.Empty(t, listed)
	assert.ErrorIs(t, s.Delete(ctx, task.ID), ErrNotFound)

	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM rt_tasks_task WHERE id = $1 AND deleted_at IS NOT NULL`, task.ID).Scan(&n))
	assert.Equal(t, 1, n, "the row is kept")

	_, err = s.Update(ctx, task.ID, tasks.Patch{})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestChecklist(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	task := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "t", Checklist: []string{"first"}})

	item, err := s.AddChecklistItem(ctx, task.ID, "second")
	require.NoError(t, err)
	assert.Equal(t, 1, item.Position)

	done, text := true, "second!"
	updated, err := s.UpdateChecklistItem(ctx, task.ID, item.ID, &text, &done)
	require.NoError(t, err)
	assert.True(t, updated.Done)
	assert.Equal(t, "second!", updated.Text)

	_, err = s.UpdateChecklistItem(ctx, task.ID, "nope", &text, nil)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = s.UpdateChecklistItem(ctx, "other-task", item.ID, &text, nil)
	assert.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, s.DeleteChecklistItem(ctx, task.ID, item.ID))
	assert.ErrorIs(t, s.DeleteChecklistItem(ctx, task.ID, item.ID), ErrNotFound)

	got, err := s.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Len(t, got.Checklist, 1)

	for i := 1; i < tasks.MaxChecklistItems; i++ {
		_, err = s.AddChecklistItem(ctx, task.ID, fmt.Sprintf("i%d", i))
		require.NoError(t, err)
	}
	_, err = s.AddChecklistItem(ctx, task.ID, "one too many")
	assert.ErrorIs(t, err, ErrLimit)
}

func TestDueForReminderSelection(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	const hour = int64(60 * 60 * 1000)
	now := nowMillis()
	win, maxAge := 24*hour, 7*24*hour

	at := func(d int64) *int64 { v := now + d; return &v }
	soon := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "soon", DueAt: at(2 * hour), Assignees: []string{"ann", "bob"}})
	late := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "late", DueAt: at(-2 * hour), Assignees: []string{"ann"}, PostID: "p1"})
	mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "far off", DueAt: at(3 * 24 * hour), Assignees: []string{"ann"}})
	mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "ancient", DueAt: at(-30 * 24 * hour), Assignees: []string{"ann"}})
	mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "no due date", Assignees: []string{"ann"}})
	mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "no assignee", DueAt: at(hour)})
	mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "already done", DueAt: at(hour), Assignees: []string{"ann"}, Status: tasks.StatusDone})
	gone := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "deleted", DueAt: at(hour), Assignees: []string{"ann"}})
	require.NoError(t, s.Delete(ctx, gone.ID))

	got, err := s.DueForReminder(ctx, now, win, maxAge)
	require.NoError(t, err)

	type key struct{ task, user, kind string }
	have := map[key]ReminderCandidate{}
	for _, c := range got {
		have[key{c.TaskID, c.UserID, c.Kind}] = c
	}
	assert.Len(t, got, 3, "only soon (x2 assignees) and late qualify")
	assert.Contains(t, have, key{soon.ID, "ann", KindDueSoon})
	assert.Contains(t, have, key{soon.ID, "bob", KindDueSoon})
	assert.Contains(t, have, key{late.ID, "ann", KindOverdue})
	assert.Equal(t, "p1", have[key{late.ID, "ann", KindOverdue}].PostID)
	assert.Equal(t, "c", have[key{soon.ID, "ann", KindDueSoon}].ChannelID)
}

func TestReminderClaimsAreExactlyOnceAndRearmOnNewDueDate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	const hour = int64(60 * 60 * 1000)
	now := nowMillis()
	due := now + hour
	task := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "t", DueAt: &due, Assignees: []string{"ann"}})

	pending := func() int {
		got, err := s.DueForReminder(ctx, now, 24*hour, 7*24*hour)
		require.NoError(t, err)
		return len(got)
	}
	require.Equal(t, 1, pending())

	cand := ReminderCandidate{TaskID: task.ID, UserID: "ann", Kind: KindDueSoon, DueAt: due}
	ok, err := s.ClaimReminder(ctx, cand)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = s.ClaimReminder(ctx, cand)
	require.NoError(t, err)
	assert.False(t, ok, "a second claim loses")
	assert.Equal(t, 0, pending(), "a claimed reminder is no longer offered")

	// Concurrent claims: exactly one winner.
	other := ReminderCandidate{TaskID: task.ID, UserID: "bob", Kind: KindDueSoon, DueAt: due}
	var wg sync.WaitGroup
	var winners atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if won, cerr := s.ClaimReminder(ctx, other); cerr == nil && won {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(1), winners.Load())

	// A failed send releases the claim so it is retried.
	require.NoError(t, s.ReleaseReminder(ctx, cand))
	assert.Equal(t, 1, pending())

	// Moving the due date re-arms the reminder.
	_, err = s.ClaimReminder(ctx, cand)
	require.NoError(t, err)
	require.Equal(t, 0, pending())
	newDue := due + 2*hour
	_, err = s.Update(ctx, task.ID, tasks.Patch{DueAt: tasks.OptionalInt64{Set: true, Value: &newDue}})
	require.NoError(t, err)
	assert.Equal(t, 1, pending(), "a new due date is a new reminder")
}

func TestReminderStateIsRemovedWithItsTask(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	due := nowMillis() + 1000
	task := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "t", DueAt: &due, Assignees: []string{"ann"}})
	_, err := s.ClaimReminder(ctx, ReminderCandidate{TaskID: task.ID, UserID: "ann", Kind: KindDueSoon, DueAt: due})
	require.NoError(t, err)

	_, err = s.db.Exec(`DELETE FROM rt_tasks_task WHERE id = $1`, task.ID)
	require.NoError(t, err)
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM rt_tasks_reminder_state`).Scan(&n))
	assert.Equal(t, 0, n)
}

func TestListHidesPrivateTasks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	yes, no := true, false

	shared := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "shared", Private: &no})
	unset := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "unset"})
	mine := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "mine", Private: &yes}) // created by "creator"
	given := mk(t, s, tasks.CreateInput{ChannelID: "c", Title: "given", Private: &yes, Assignees: []string{"ann"}})

	assert.False(t, shared.Private)
	assert.False(t, unset.Private, "a nil choice stores a shared task")
	assert.True(t, mine.Private)

	list := func(viewer string, f tasks.Filter) []string {
		f.ChannelIDs, f.ViewerID = []string{"c"}, viewer
		got, err := s.List(ctx, f)
		require.NoError(t, err)
		return ids(got)
	}

	assert.ElementsMatch(t, []string{shared.ID, unset.ID, mine.ID, given.ID}, list("creator", tasks.Filter{}))
	assert.ElementsMatch(t, []string{shared.ID, unset.ID, given.ID}, list("ann", tasks.Filter{}), "an assignee sees the task they were given")
	assert.ElementsMatch(t, []string{shared.ID, unset.ID}, list("bystander", tasks.Filter{}))
	assert.Empty(t, list("bystander", tasks.Filter{Query: "mine"}), "search does not match private tasks")
	assert.Equal(t, []string{given.ID}, list("ann", tasks.Filter{AssigneeID: "ann"}))

	// Privacy can be turned on and off with a patch.
	updated, err := s.Update(ctx, shared.ID, tasks.Patch{Private: &yes})
	require.NoError(t, err)
	assert.True(t, updated.Private)
	assert.NotContains(t, list("bystander", tasks.Filter{}), shared.ID)
}
