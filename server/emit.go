package main

import (
	"context"
	"encoding/json"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

type eventActor struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsBot    bool   `json:"is_bot"`
}

// taskEvent is the webhook payload. The task is its state after the change; for
// "task.deleted" it is the state just before deletion.
type taskEvent struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	OccurredAt int64       `json:"occurred_at"`
	Actor      eventActor  `json:"actor"`
	Task       *tasks.Task `json:"task"`
}

// buildEvent assembles and encodes a webhook payload.
func buildEvent(action string, actor eventActor, t *tasks.Task) (id, eventType string, body []byte, err error) {
	id = model.NewId()
	eventType = "task." + action
	body, err = json.Marshal(taskEvent{ID: id, Type: eventType, OccurredAt: model.GetMillis(), Actor: actor, Task: t})
	return id, eventType, body, err
}

// emit announces a task change to clients (websocket) and, when configured, to the webhook.
// action is one of created, updated, moved, deleted.
func (p *Plugin) emit(action, actorID string, t *tasks.Task) {
	p.publishTaskChanged(action, t.ChannelID, t.ID)

	if !p.webhooks.Enabled() {
		return
	}
	actor := eventActor{ID: actorID}
	if u, err := p.client.User.Get(actorID); err == nil {
		actor.Username, actor.IsBot = u.Username, u.IsBot
	}
	id, eventType, body, err := buildEvent(action, actor, t)
	if err != nil {
		p.client.Log.Error("Cannot encode webhook event", "error", err.Error())
		return
	}
	p.webhooks.Enqueue(id, eventType, body)
}

// emitByID is emit for callers that do not hold the changed task; it loads the
// task only when a webhook is configured.
func (p *Plugin) emitByID(action, actorID, channelID, taskID string) {
	if !p.webhooks.Enabled() {
		p.publishTaskChanged(action, channelID, taskID)
		return
	}
	t, err := p.tasks.Get(context.Background(), taskID)
	if err != nil {
		p.publishTaskChanged(action, channelID, taskID)
		return
	}
	p.emit(action, actorID, t)
}
