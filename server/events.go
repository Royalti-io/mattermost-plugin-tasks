package main

import (
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

// taskChangedEvent is the websocket event name the webapp listens for. Mattermost
// delivers it to clients as "custom_<plugin id>_task_changed".
const taskChangedEvent = "task_changed"

func taskChangedPayload(action, channelID, taskID string) map[string]any {
	return map[string]any{
		"action":     action,
		"channel_id": channelID,
		"task_id":    taskID,
	}
}

// publishTaskChanged tells whoever can see the task to refresh: every member of
// its channel, or only the creator and assignees when it is private. The payload
// carries ids only; clients refetch, so permissions are re-checked by the API.
func (p *Plugin) publishTaskChanged(action string, t *tasks.Task) {
	if !t.Private {
		p.publishChannelRefresh(action, t.ChannelID, t.ID)
		return
	}
	for _, uid := range append([]string{t.CreatedBy}, t.Assignees...) {
		p.client.Frontend.PublishWebSocketEvent(taskChangedEvent, taskChangedPayload(action, t.ChannelID, t.ID),
			&model.WebsocketBroadcast{UserId: uid})
	}
}

// publishChannelRefresh asks every member of the channel to refetch the task. It
// is also used when a task changes visibility, so people who lose sight of it drop it.
func (p *Plugin) publishChannelRefresh(action, channelID, taskID string) {
	p.client.Frontend.PublishWebSocketEvent(taskChangedEvent, taskChangedPayload(action, channelID, taskID),
		&model.WebsocketBroadcast{ChannelId: channelID})
}
