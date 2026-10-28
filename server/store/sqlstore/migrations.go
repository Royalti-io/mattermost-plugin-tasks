package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
)

// migration is one ordered, append-only schema step. Each statement runs on its
// own Exec because the plugin SQL driver does not accept multi-statement strings.
type migration struct {
	version    int
	statements []string
}

// Never edit a released migration; add a new one. Statements are idempotent so a
// migration that was interrupted, or whose version row was lost, can run again.
var migrations = []migration{
	{
		version: 1,
		statements: []string{
			`CREATE TABLE IF NOT EXISTS rt_tasks_task (
				id          VARCHAR(26)      PRIMARY KEY,
				channel_id  VARCHAR(26)      NOT NULL,
				title       VARCHAR(200)     NOT NULL,
				description TEXT             NOT NULL DEFAULT '',
				status      VARCHAR(32)      NOT NULL DEFAULT 'todo',
				priority    VARCHAR(16)      NOT NULL DEFAULT 'normal',
				due_at      BIGINT           NULL,
				position    DOUBLE PRECISION NOT NULL DEFAULT 0,
				created_by  VARCHAR(26)      NOT NULL,
				created_at  BIGINT           NOT NULL,
				updated_at  BIGINT           NOT NULL,
				completed_at BIGINT          NULL,
				deleted_at  BIGINT           NULL,
				post_id     VARCHAR(26)      NULL,
				source      VARCHAR(64)      NULL,
				external_id VARCHAR(255)     NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_rt_tasks_task_channel_status ON rt_tasks_task (channel_id, status, position) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_rt_tasks_task_due ON rt_tasks_task (due_at) WHERE deleted_at IS NULL AND completed_at IS NULL AND due_at IS NOT NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_rt_tasks_task_source ON rt_tasks_task (source, external_id) WHERE source IS NOT NULL AND external_id IS NOT NULL`,
			`CREATE TABLE IF NOT EXISTS rt_tasks_assignee (
				task_id VARCHAR(26) NOT NULL REFERENCES rt_tasks_task(id) ON DELETE CASCADE,
				user_id VARCHAR(26) NOT NULL,
				PRIMARY KEY (task_id, user_id)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_rt_tasks_assignee_user ON rt_tasks_assignee (user_id)`,
			`CREATE TABLE IF NOT EXISTS rt_tasks_label (
				task_id VARCHAR(26) NOT NULL REFERENCES rt_tasks_task(id) ON DELETE CASCADE,
				label   VARCHAR(40) NOT NULL,
				PRIMARY KEY (task_id, label)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_rt_tasks_label_label ON rt_tasks_label (label)`,
			`CREATE TABLE IF NOT EXISTS rt_tasks_checklist_item (
				id       VARCHAR(26)  PRIMARY KEY,
				task_id  VARCHAR(26)  NOT NULL REFERENCES rt_tasks_task(id) ON DELETE CASCADE,
				text     VARCHAR(500) NOT NULL,
				done     BOOLEAN      NOT NULL DEFAULT FALSE,
				position INTEGER      NOT NULL DEFAULT 0
			)`,
			`CREATE INDEX IF NOT EXISTS idx_rt_tasks_checklist_task ON rt_tasks_checklist_item (task_id, position)`,
		},
	},
	{
		version: 2,
		statements: []string{
			// One row per reminder already sent. due_at is part of the key so moving a
			// task's due date re-arms its reminders.
			`CREATE TABLE IF NOT EXISTS rt_tasks_reminder_state (
				task_id VARCHAR(26) NOT NULL REFERENCES rt_tasks_task(id) ON DELETE CASCADE,
				user_id VARCHAR(26) NOT NULL,
				kind    VARCHAR(16) NOT NULL,
				due_at  BIGINT      NOT NULL,
				sent_at BIGINT      NOT NULL,
				PRIMARY KEY (task_id, user_id, kind, due_at)
			)`,
		},
	},
}

// Migrate applies any pending migrations. It is safe to call from several
// instances at once without a lock: Postgres DDL is transactional, so a racing
// instance either waits for the winner's commit and then fails on "already
// exists", or loses the version insert. In both cases it re-reads the applied
// versions and carries on if another instance got there first. A lock is avoided
// on purpose: a session-level advisory lock outlives a crashed server until
// Postgres notices the dead connection, which would block plugin activation.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err = ensureMigrationsTable(ctx, conn); err != nil {
		return err
	}

	for _, m := range migrations {
		applied, aerr := appliedVersions(ctx, conn)
		if aerr != nil {
			return aerr
		}
		if applied[m.version] {
			continue
		}
		if err = applyMigration(ctx, conn, m); err != nil {
			// Another instance may have applied it between our read and write.
			again, rerr := appliedVersions(ctx, conn)
			if rerr == nil && again[m.version] {
				continue
			}
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
	}
	return nil
}

func ensureMigrationsTable(ctx context.Context, conn *sql.Conn) error {
	const ddl = `CREATE TABLE IF NOT EXISTS rt_tasks_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at BIGINT  NOT NULL
	)`
	// CREATE TABLE IF NOT EXISTS can still lose a race on the catalog; one retry settles it.
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		if _, err2 := conn.ExecContext(ctx, ddl); err2 != nil {
			return fmt.Errorf("create migrations table: %w", err2)
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *sql.Conn) (map[int]bool, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version FROM rt_tasks_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func applyMigration(ctx context.Context, conn *sql.Conn, m migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, stmt := range m.statements {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO rt_tasks_migrations (version, applied_at) VALUES ($1, $2)`, m.version, nowMillis()); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
