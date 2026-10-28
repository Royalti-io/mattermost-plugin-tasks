package main

import "github.com/mattermost/mattermost/server/public/model"

// taskChangedEvent is the websocket event name the webapp listens for. Mattermost
// delivers it to clients as "custom_<plugin id>_task_changed".
const taskChangedEvent = "task_changed"

// publishTaskChanged tells every member of the task's channel to refresh. The
// payload carries ids only; clients refetch, so permissions are re-checked by the API.
func (p *Plugin) publishTaskChanged(action, channelID, taskID string) {
	p.client.Frontend.PublishWebSocketEvent(taskChangedEvent, map[string]any{
		"action":     action,
		"channel_id": channelID,
		"task_id":    taskID,
	}, &model.WebsocketBroadcast{ChannelId: channelID})
}
