package main

import (
	"net/http"
	"testing"

	"github.com/mattermost/mattermost/server/public/pluginapi/testhelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

// A private task is visible only to its creator and assignees, even to other
// members of the same channel.
func TestPrivateTasks(t *testing.T) {
	th := testhelper.Setup(t)
	ctx := t.Context()

	ownerClient := th.Client
	other := th.CreateUser()
	otherClient := loginAs(t, th, other)
	chID := th.Channel.Id

	_, _, err := th.AdminClient.AddChannelMember(ctx, chID, other.Id)
	require.NoError(t, err)

	var secret tasks.Task
	require.Equal(t, http.StatusCreated, call(t, th, ownerClient, http.MethodPost, "/tasks", map[string]any{
		"channel_id": chID, "title": "Salary review", "private": true,
		"source": "test", "external_id": "salary",
	}, &secret))
	require.True(t, secret.Private)

	// Control: a shared task in the same channel stays visible.
	var shared tasks.Task
	require.Equal(t, http.StatusCreated, call(t, th, ownerClient, http.MethodPost, "/tasks", map[string]any{
		"channel_id": chID, "title": "Team lunch", "private": false,
	}, &shared))
	require.False(t, shared.Private)

	t.Run("other channel members cannot see it", func(t *testing.T) {
		id := "/tasks/" + secret.ID
		assert.Equal(t, http.StatusNotFound, call(t, th, otherClient, http.MethodGet, id, nil, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, otherClient, http.MethodPatch, id, map[string]any{"title": "x"}, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, otherClient, http.MethodPost, id+"/move", map[string]any{"status": "done"}, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, otherClient, http.MethodPost, id+"/checklist", map[string]any{"text": "x"}, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, otherClient, http.MethodDelete, id, nil, nil))

		for _, path := range []string{"/tasks?channel_id=" + chID, "/tasks"} {
			var list taskList
			require.Equal(t, http.StatusOK, call(t, th, otherClient, http.MethodGet, path, nil, &list), path)
			require.Len(t, list.Tasks, 1, path)
			assert.Equal(t, shared.ID, list.Tasks[0].ID, path)
		}

		var list taskList
		require.Equal(t, http.StatusOK, call(t, th, otherClient, http.MethodGet, "/tasks?q=Salary", nil, &list))
		assert.Empty(t, list.Tasks, "search does not match private tasks either")

		list = taskList{}
		require.Equal(t, http.StatusOK, call(t, th, ownerClient, http.MethodGet, "/tasks?channel_id="+chID, nil, &list))
		assert.Len(t, list.Tasks, 2, "the creator sees both")
	})

	t.Run("a repeated source key does not reveal the task", func(t *testing.T) {
		code := call(t, th, otherClient, http.MethodPost, "/tasks", map[string]any{
			"channel_id": chID, "title": "Salary review", "source": "test", "external_id": "salary",
		}, nil)
		assert.Equal(t, http.StatusConflict, code)
	})

	t.Run("assigning someone lets them see and edit it", func(t *testing.T) {
		require.Equal(t, http.StatusOK, call(t, th, ownerClient, http.MethodPatch, "/tasks/"+secret.ID,
			map[string]any{"assignees": []string{other.Id}}, nil))

		var got tasks.Task
		require.Equal(t, http.StatusOK, call(t, th, otherClient, http.MethodGet, "/tasks/"+secret.ID, nil, &got))
		assert.True(t, got.Private)
		assert.Equal(t, http.StatusOK, call(t, th, otherClient, http.MethodPatch, "/tasks/"+secret.ID, map[string]any{"priority": "high"}, nil))

		var list taskList
		require.Equal(t, http.StatusOK, call(t, th, otherClient, http.MethodGet, "/tasks?assignee=me", nil, &list))
		require.Len(t, list.Tasks, 1)
		assert.Equal(t, secret.ID, list.Tasks[0].ID)
	})

	t.Run("only the creator changes privacy", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, call(t, th, otherClient, http.MethodPatch, "/tasks/"+secret.ID, map[string]any{"private": false}, nil))

		var got tasks.Task
		require.Equal(t, http.StatusOK, call(t, th, ownerClient, http.MethodPatch, "/tasks/"+secret.ID, map[string]any{"assignees": []string{}}, &got))
		assert.Equal(t, http.StatusNotFound, call(t, th, otherClient, http.MethodGet, "/tasks/"+secret.ID, nil, nil), "unassigning hides it again")

		require.Equal(t, http.StatusOK, call(t, th, ownerClient, http.MethodPatch, "/tasks/"+secret.ID, map[string]any{"private": false}, &got))
		assert.False(t, got.Private)
		assert.Equal(t, http.StatusOK, call(t, th, otherClient, http.MethodGet, "/tasks/"+secret.ID, nil, nil), "now shared with the channel")
	})

	t.Run("private by default", func(t *testing.T) {
		var prefs prefsResponse
		require.Equal(t, http.StatusOK, call(t, th, ownerClient, http.MethodGet, "/prefs", nil, &prefs))
		assert.False(t, prefs.PrivateByDefault)

		require.Equal(t, http.StatusOK, call(t, th, ownerClient, http.MethodPatch, "/prefs", map[string]any{"private_by_default": true}, &prefs))
		assert.True(t, prefs.PrivateByDefault)

		var task tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, ownerClient, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "default"}, &task))
		assert.True(t, task.Private, "no explicit choice uses the saved default")

		require.Equal(t, http.StatusCreated, call(t, th, ownerClient, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "explicit", "private": false}, &task))
		assert.False(t, task.Private, "an explicit choice wins")

		require.Equal(t, http.StatusCreated, call(t, th, otherClient, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "theirs"}, &task))
		assert.False(t, task.Private, "the default is per user")
	})
}
