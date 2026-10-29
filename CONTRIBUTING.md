# Contributing

Thanks for helping. Small, focused pull requests with tests are easiest to review.

## Before you open a pull request

```bash
make apply
cd webapp && npm ci && npm run lint && npm run check-types && npx jest && cd ..
SKIP_DOCKER_TESTS=1 go test ./server/...
go test ./server/store/sqlstore     # needs Docker
```

If you change anything that goes through the Mattermost server (API handlers,
permissions, reminders, websocket events, migrations), also run the integration
tests; they are the ones that prove permissions hold on a real server:

```bash
make apply server bundle MM_SERVICESETTINGS_ENABLEDEVELOPER=true DEFAULT_GOOS=linux DEFAULT_GOARCH=amd64
MM_TEST_IMAGE=mattermost/mattermost-team-edition:12.0.1 go test ./server
```

## Guidelines

- **Migrations are append-only.** Never edit a released migration in
  `server/store/sqlstore/migrations.go`; add a new one, and keep statements
  idempotent (`IF NOT EXISTS`).
- **Permissions follow channels.** Every handler that reads or writes a task checks
  the caller's access to the task's channel. A caller who cannot see a task gets `404`.
  New endpoints need a test for the "cannot see it" case.
- **No new database dependency** on Mattermost's own tables beyond what exists
  (`channelmembers`, `channels`); the integration tests cover that schema.
- **Keep the core generic.** Integrations with specific products belong in a separate
  adapter that uses the REST API and webhooks.
- Match the existing style; the webapp uses the Mattermost ESLint config
  (`npm run fix` applies it).

## Reporting bugs

Open an issue with your Mattermost version, plugin version, and what you expected. Do
not post tokens or webhook secrets. For security problems, see [SECURITY.md](SECURITY.md).
