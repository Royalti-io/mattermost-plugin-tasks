package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi/testhelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

// botClient creates a bot account with a token and returns an API client for it.
func botClient(t *testing.T, th *testhelper.TestHelper, name string) (*model.Bot, *model.Client4) {
	t.Helper()
	ctx := t.Context()

	// The test server ships with bot creation off; production has it on.
	cfg, _, err := th.AdminClient.GetConfig(ctx)
	require.NoError(t, err)
	cfg.ServiceSettings.EnableBotAccountCreation = model.NewPointer(true)
	_, _, err = th.AdminClient.UpdateConfig(ctx, cfg)
	require.NoError(t, err)
	bot, _, err := th.AdminClient.CreateBot(ctx, &model.Bot{Username: name, DisplayName: name})
	require.NoError(t, err)
	tok, _, err := th.AdminClient.CreateUserAccessToken(ctx, bot.UserId, "tasks agent", 0)
	require.NoError(t, err)

	c := model.NewAPIv4Client(th.ServerURL)
	c.SetToken(tok.Token)
	return bot, c
}

func TestAgentBotWorkflow(t *testing.T) {
	th := testhelper.Setup(t)
	ctx := t.Context()

	human := th.User
	chID := th.Channel.Id
	secret := th.CreateChannel(model.ChannelTypePrivate)
	bot, bc := botClient(t, th, "agent-"+model.NewId()[:6])

	t.Run("a bot outside the channel is refused", func(t *testing.T) {
		code := call(t, th, bc, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "nope"}, nil)
		assert.Equal(t, http.StatusForbidden, code)
		assert.Equal(t, http.StatusForbidden, call(t, th, bc, http.MethodGet, "/tasks?channel_id="+chID, nil, nil))
	})

	// A bot, like any user, must join the team before it can be added to a channel.
	_, _, err := th.AdminClient.AddTeamMember(ctx, th.Team.Id, bot.UserId)
	require.NoError(t, err)
	_, _, err = th.AdminClient.AddChannelMember(ctx, chID, bot.UserId)
	require.NoError(t, err)

	var created tasks.Task
	t.Run("a member bot creates idempotently and assigns a human", func(t *testing.T) {
		body := map[string]any{
			"channel_id": chID, "title": "Reconcile Q3", "priority": "high", "assignees": []string{human.Id},
			"labels": []string{"finance"}, "source": "pa-tasks", "external_id": "pa-123", "due_at": ms(48 * time.Hour),
		}
		require.Equal(t, http.StatusCreated, call(t, th, bc, http.MethodPost, "/tasks", body, &created))
		assert.Equal(t, bot.UserId, created.CreatedBy)
		assert.Equal(t, "pa-tasks", created.Source)

		var again tasks.Task
		require.Equal(t, http.StatusOK, call(t, th, bc, http.MethodPost, "/tasks", body, &again), "a retry is a no-op")
		assert.Equal(t, created.ID, again.ID)
	})

	t.Run("the human sees the bot's task and the bot can drive its lifecycle", func(t *testing.T) {
		var mine taskList
		require.Equal(t, http.StatusOK, call(t, th, th.Client, http.MethodGet, "/tasks?assignee=me", nil, &mine))
		require.Len(t, mine.Tasks, 1)
		assert.Equal(t, created.ID, mine.Tasks[0].ID)

		p := "/tasks/" + created.ID
		var got tasks.Task
		require.Equal(t, http.StatusOK, call(t, th, bc, http.MethodPost, p+"/move", map[string]any{"status": "in_progress"}, &got))
		require.Equal(t, http.StatusOK, call(t, th, bc, http.MethodPatch, p, map[string]any{"description": "Started by the agent"}, &got))
		require.Equal(t, http.StatusCreated, call(t, th, bc, http.MethodPost, p+"/checklist", map[string]any{"text": "pull statements"}, nil))
		require.Equal(t, http.StatusOK, call(t, th, bc, http.MethodPost, p+"/move", map[string]any{"status": "done"}, &got))
		assert.NotNil(t, got.CompletedAt)
	})

	t.Run("a bot can be assigned work and filter by it", func(t *testing.T) {
		var assigned tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, th.Client, http.MethodPost, "/tasks", map[string]any{
			"channel_id": chID, "title": "For the agent", "assignees": []string{bot.UserId},
		}, &assigned))

		var list taskList
		require.Equal(t, http.StatusOK, call(t, th, bc, http.MethodGet, "/tasks?assignee=me&status=todo", nil, &list))
		require.Len(t, list.Tasks, 1)
		assert.Equal(t, assigned.ID, list.Tasks[0].ID)
	})

	t.Run("a bot cannot reach a private channel it was not added to", func(t *testing.T) {
		var adminTask tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, th.AdminClient, http.MethodPost, "/tasks", map[string]any{"channel_id": secret.Id, "title": "hidden"}, &adminTask))

		assert.Equal(t, http.StatusNotFound, call(t, th, bc, http.MethodGet, "/tasks/"+adminTask.ID, nil, nil))
		assert.Equal(t, http.StatusForbidden, call(t, th, bc, http.MethodPost, "/tasks", map[string]any{"channel_id": secret.Id, "title": "x"}, nil))
		var list taskList
		require.Equal(t, http.StatusOK, call(t, th, bc, http.MethodGet, "/tasks", nil, &list))
		for _, tk := range list.Tasks {
			assert.NotEqual(t, secret.Id, tk.ChannelID)
		}
	})

	t.Run("a bot cannot run the admin job", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, call(t, th, bc, http.MethodPost, "/admin/run-reminders", nil, nil))
	})

	t.Run("an unreachable webhook never slows or breaks task changes", func(t *testing.T) {
		cfg, _, err := th.AdminClient.GetConfig(ctx)
		require.NoError(t, err)
		if cfg.PluginSettings.Plugins == nil {
			cfg.PluginSettings.Plugins = map[string]map[string]any{}
		}
		cfg.PluginSettings.Plugins[testhelper.PluginID()] = map[string]any{
			"WebhookURL": "http://127.0.0.1:9/hook", "WebhookSecret": "x", "UIAllowlist": "",
		}
		_, _, err = th.AdminClient.UpdateConfig(ctx, cfg)
		require.NoError(t, err)
		time.Sleep(2 * time.Second) // let the plugin pick up the new settings

		start := time.Now()
		for range 5 {
			require.Equal(t, http.StatusCreated, call(t, th, bc, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "with dead webhook"}, nil))
		}
		assert.Less(t, time.Since(start), 5*time.Second, "delivery is asynchronous")
	})
}
