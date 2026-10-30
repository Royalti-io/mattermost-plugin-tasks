# AGENTS.md

Mattermost Tasks plugin: a Go server plugin (`server/`, REST API, Postgres store, reminders, webhooks) and a React/TypeScript webapp plugin (`webapp/`, list panel and Kanban board). Started from the Mattermost plugin starter template. See README.md for the layout and CONTRIBUTING.md for the rules that matter (append-only migrations, channel-based permissions).

## Commands

While iterating, run the narrow command; both `make test` and `make check-style` reinstall Go tools first, and `make test` also rebuilds the bundle and runs every test.

```bash
SKIP_DOCKER_TESTS=1 go test ./server/...       # server unit tests only
go test ./server/tasks -run TestPatchNormalize  # single test
cd webapp && npx jest src/manifest.test.tsx    # single webapp test
go vet ./... && golangci-lint run ./...        # server lint
cd webapp && npm run lint && npm run check-types
make test                                      # full check before pushing
```

Run `make apply` and `cd webapp && npm ci` once first: `npx jest` and `npm run check-types` fail with `Cannot find module 'manifest'` until `webapp/src/manifest.ts` is generated. If `go` or Docker isn't installed, run only the webapp checks and say the server was not compiled or tested.

`make help` lists the remaining targets (deploy, watch, release bumps, debugger attach).

## Architecture

- `plugin.json` is the source of truth for ID, version, and settings. `make apply` generates `server/manifest.go` and `webapp/src/manifest.ts` from it; both are gitignored, so never edit them.
- Project-specific make targets go in `build/custom.mk`.
- Visibility is enforced in two places that must agree: `Task.CanSee` (single-task handlers, via `loadTask`) and the `ViewerID` clause in `sqlstore.List`. A new read path must go through one of them. `tasks.Filter.ViewerID` is mandatory, and an empty value matches nothing. Private tasks are also excluded from webhooks and from channel-wide websocket events. Use `publishTaskChanged`, which picks the recipients, and don't broadcast to the channel directly.
- Server code is in package `main` except the pure task model (`server/tasks`) and the Postgres store (`server/store/sqlstore`).

## Backend tests

- **Integration tests** (`server/integration_test.go`) are the default for anything that goes through the Mattermost server: plugin API calls, hooks, KV persistence, permissions, HTTP routing, activation. `testhelper.Setup(t)` runs Postgres and Mattermost via testcontainers and deploys the bundle from `dist/` (see `go doc github.com/mattermost/mattermost/server/public/pluginapi/testhelper` for fixtures and env vars). Each `Setup` resets the server (~10s), so group related assertions into one test.
- **Plain unit tests** (`server/plugin_test.go`) for pure logic that doesn't touch `p.API`/`p.client`, e.g. drive `ServeHTTP` with `httptest` after `initRouter()`.
- **Mocked-API unit tests** (`server/command/command_test.go`) only for error paths a real server can't easily produce: wrap a `plugintest.API` in `pluginapi.NewClient` and set `api.On(...)` expectations, or use the mockgen mocks in `server/command/mocks` for this plugin's own interfaces. Avoid them otherwise; they encode assumptions about the server rather than testing it.

`server/integration_private_test.go` is the model for a "caller who can't see it gets 404" test, which CONTRIBUTING requires for new endpoints; it reuses `loginAs` and `call` from `integration_tasks_test.go`.

Integration test caveats specific to this repo:
- There's no build tag, so `go test ./...` includes them and fails without Docker. `SKIP_DOCKER_TESTS=1` skips them.
- They test whatever bundle is in `dist/`, which must be linux/amd64 (see the `test` target comment in the Makefile). After a server change, rebuild it before running one by hand:
  ```bash
  make apply server webapp bundle MM_SERVICESETTINGS_ENABLEDEVELOPER=true DEFAULT_GOOS=linux DEFAULT_GOARCH=amd64
  go test ./server -run TestPingEndpoint
  ```
  Drop `webapp` from the target list to skip the web build when only server code changed; `bundle` then needs an existing `webapp/dist`.
