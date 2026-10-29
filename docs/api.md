# Tasks plugin API

Everything the web app does goes through this REST API, so scripts, bots and
other systems can do the same.

Base URL: `https://<your-server>/plugins/com.royalti.tasks/api/v1`

## Authentication and permissions

Send either a logged-in session (what the web app does) or a bot or user access
token:

```
Authorization: Bearer <token>
```

Access follows channel membership. There is no separate task permission system.

| Action | Requires |
|---|---|
| Read a task, list a channel's tasks | Permission to read the task's channel |
| Create, edit, move, delete, change a checklist | Permission to post in the channel, and the channel is not archived |
| List without `channel_id` | Returns tasks only from channels the caller belongs to |

A caller who cannot see a task gets `404`, the same as for a task that does not
exist, so task ids do not leak. Creating a task in a channel you cannot post to
returns `403`. Assignees must be members of the task's channel.

Errors are JSON: `{"error": "message"}` with a matching HTTP status
(`400` invalid input, `401` not logged in, `403` not allowed, `404` not found,
`409` conflict).

## The task object

```json
{
  "id": "u1w8r6pbkjnr3dzwk5ejqeuz7a",
  "channel_id": "…",
  "title": "Reconcile Q3",
  "description": "",
  "status": "todo",
  "priority": "normal",
  "due_at": 1893455999000,
  "position": 2000,
  "created_by": "…",
  "created_at": 1761700000000,
  "updated_at": 1761700000000,
  "completed_at": null,
  "post_id": "…",
  "source": "pa-tasks",
  "external_id": "pa-123",
  "assignees": ["…"],
  "labels": ["finance"],
  "checklist": [{"id": "…", "text": "pull statements", "done": false, "position": 0}]
}
```

- Timestamps are Unix milliseconds.
- `status` is one of `backlog`, `todo`, `in_progress`, `blocked`, `done`.
- `priority` is one of `low`, `normal`, `high`, `critical`.
- `position` orders cards within a status column (lower first). It is a float, so
  you can place a task between two others without renumbering.
- `completed_at` is set when the status becomes `done` and cleared when it leaves it.
- `post_id` links the task to the message it came from. `source` and `external_id`
  identify the task in another system.

Limits: title 200 characters, description 10,000, 20 assignees, 20 labels of up
to 40 characters (stored lowercase), 100 checklist items of up to 500 characters.

## Endpoints

### Create: `POST /tasks`

```bash
curl -X POST "$BASE/tasks" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{
    "channel_id": "…",
    "title": "Reconcile Q3",
    "priority": "high",
    "due_at": 1893455999000,
    "assignees": ["<user id>"],
    "labels": ["finance"],
    "checklist": ["pull statements", "match payouts"],
    "source": "pa-tasks",
    "external_id": "pa-123"
  }'
```

Required: `channel_id`, `title`. Optional: `description`, `status` (default `todo`),
`priority` (default `normal`), `due_at`, `position`, `assignees`, `labels`,
`checklist` (list of texts), `post_id`, `source` + `external_id`, `notify_thread`.
Unknown fields are rejected with `400`.

- Returns `201` with the task.
- **Idempotent create.** If you send `source` and `external_id` and a task with
  that pair already exists, the existing task is returned with `200` and nothing
  changes. Retry freely. The pair must be sent together. If the matching task was
  deleted, you get `409`.
- `post_id` must be a post in the same channel. With `"notify_thread": true` the
  Tasks bot replies in that post's thread.

### List: `GET /tasks`

Query parameters, all optional:

| Parameter | Meaning |
|---|---|
| `channel_id` | One channel. Without it, all channels the caller belongs to |
| `assignee` | A user id, or `me` |
| `status` | One or more statuses, comma separated |
| `priority` | One priority |
| `label` | One label (case-insensitive) |
| `q` | Text search in title and description |
| `due_before`, `due_after` | Unix ms bounds on `due_at` (tasks without a due date are excluded) |
| `limit`, `offset` | Paging. Default 100, maximum 500 |

Returns `{"tasks": [...]}` ordered by `position`, then age.

### Read, update, delete

- `GET /tasks/{id}`
- `PATCH /tasks/{id}`: any of `title`, `description`, `status`, `priority`,
  `due_at` (send `null` to clear), `assignees` and `labels` (each replaces the whole
  set). Changing `status` moves the task to the end of its new column.
- `DELETE /tasks/{id}`: soft delete, returns `204`. The row is kept and the
  `source`/`external_id` pair stays reserved.

### Move: `POST /tasks/{id}/move`

```json
{"status": "in_progress", "position": 1500}
```

`position` is optional; without it the task goes to the end of the column.

### Checklist

- `POST /tasks/{id}/checklist` with `{"text": "…"}` returns `201` and the item.
- `PATCH /tasks/{id}/checklist/{item}` with `{"text": "…", "done": true}` (either
  field).
- `DELETE /tasks/{id}/checklist/{item}` returns `204`.

### Health and access

- `GET /ping` returns `{"status":"ok"}` for a logged-in caller.
- `GET /access` returns `{"ui_enabled": true|false}`: whether the web UI is shown to the
  caller (see the *Limit the UI to these usernames* setting). The API itself is not
  affected by that setting.

### Run reminders now: `POST /admin/run-reminders`

System administrators only. Runs the reminder and digest job once and returns
`{"reminders_sent": n, "digests_sent": n}`.

## Setting up an agent or integration

Create one bot per integration, not one shared admin token. A bot only sees the
channels it has been added to.

1. System Console → Integrations → Bot Accounts → Add Bot Account (or
   `POST /api/v4/bots`). Bot accounts must be enabled.
2. Create a token for the bot (the dialog shows it once, or
   `POST /api/v4/users/{bot user id}/tokens`).
3. Add the bot to the team, then to each channel it should work in. It cannot
   read or write anywhere else.
4. Call the API with `Authorization: Bearer <token>`.

Bots can be assigned tasks like any user, so `assignee=me` works for an agent
polling its own queue.

For syncing from another system, store your own id in `source` + `external_id`
on create, and filter your own events out of the webhook by `actor.id` to avoid
loops.

## Webhooks

Set **Webhook URL** and **Webhook secret** in System Console → Plugins → Tasks.
The plugin sends a `POST` for every task change: `task.created`, `task.updated`,
`task.moved`, `task.deleted`. Checklist and assignee changes are `task.updated`.

```json
{
  "id": "event id",
  "type": "task.moved",
  "occurred_at": 1761700000000,
  "actor": {"id": "…", "username": "agent", "is_bot": true},
  "task": { "…the task object after the change…" }
}
```

For `task.deleted`, `task` is the state just before deletion.

Headers: `X-Tasks-Event` (the type), `X-Tasks-Delivery` (unique per event, use it to
de-duplicate), and `X-Tasks-Signature: sha256=<hex>`: the HMAC-SHA256 of the raw
request body using your secret. Verify it before trusting the payload.

```python
import hmac, hashlib

def valid(secret: str, body: bytes, header: str) -> bool:
    expected = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, header)
```

```js
import { createHmac, timingSafeEqual } from "node:crypto";

export function valid(secret, rawBody, header) {
  const expected = "sha256=" + createHmac("sha256", secret).update(rawBody).digest("hex");
  const a = Buffer.from(expected), b = Buffer.from(header || "");
  return a.length === b.length && timingSafeEqual(a, b);
}
```

Delivery is best effort and in order. Respond with any `2xx` quickly. A failing
delivery is retried up to 4 attempts in total (1 s, 4 s, 16 s apart) and then
dropped. The queue holds 1,000 events; beyond that events are dropped and logged.
Treat the webhook as a hint to refetch (`GET /tasks/{id}`) if you cannot afford to
miss one.

## Slash command

`/task add <title>`, `/task list`, `/task digest on|off|status`, `/task help`.

## Reminders

Assignees get a direct message from the Tasks bot when a task is due within 24
hours or newly overdue (up to 7 days past), once per due date. Users can opt in to
a daily digest with `/task digest on`; it arrives between 09:00 and 18:00 in their
own timezone. Admins can switch reminders off with the **Disable reminders**
setting.
