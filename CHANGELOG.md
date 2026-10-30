# Changelog

## 0.2.0 - 2026-10-30

- Private tasks. Tick *Private* when creating a task, or later in its details, and only you and the people assigned can see it, even in a shared channel. `/task private on` makes your new tasks private by default. Existing tasks stay shared until their creator marks them private.
- Private tasks are left out of lists, search and the board for everyone else, are never sent to the webhook, and the Tasks bot does not reply in the source thread for them.
- API: tasks have a `private` field (settable on create and, by the creator, on update); new `GET`/`PATCH /prefs` for the "private by default" setting. Adds a migration (`is_private` column); back up before upgrading as usual.

## 0.1.2 - 2026-10-29

- The board now works when `/tasks-board` is opened directly. It used to show an empty channel picker and "Join a channel to see its tasks" until you opened a team first.

## 0.1.1 - 2026-10-29

- The Tasks side panel now scrolls when the list is longer than the window.

## 0.1.0 - 2026-10-29

First public version.

- Channel-scoped tasks: title, description, status, priority, due date, multiple
  assignees (picked from the channel's members), labels, checklist.
- Right-hand panel, Kanban board, create from a message, live updates.
- Reminders, opt-in daily digest, `/task` command.
- REST API, idempotent create for integrations, signed webhooks.
- Completed tasks show their due date instead of an "overdue" count.
- Original plugin icon and README screenshots.
