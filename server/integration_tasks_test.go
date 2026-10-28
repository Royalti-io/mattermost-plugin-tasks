package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi/testhelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

// call sends a request to the plugin API as the given client and decodes the JSON reply into out (if non-nil).
func call(t *testing.T, th *testhelper.TestHelper, client *model.Client4, method, path string, body, out any) int {
	t.Helper()

	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, th.ServerURL+"/plugins/"+testhelper.PluginID()+"/api/v1"+path, rdr)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+client.AuthToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if out != nil && len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, out), string(raw))
	}
	return resp.StatusCode
}

func loginAs(t *testing.T, th *testhelper.TestHelper, u *model.User) *model.Client4 {
	t.Helper()
	c := model.NewAPIv4Client(th.ServerURL)
	_, _, err := c.Login(t.Context(), u.Username, "Password1!")
	require.NoError(t, err)
	return c
}

type taskList struct {
	Tasks []*tasks.Task `json:"tasks"`
}

// One test, many assertions: each testhelper.Setup resets the whole server (~10s).
func TestTasksAPI(t *testing.T) {
	th := testhelper.Setup(t)
	ctx := t.Context()

	member := th.User
	memberClient := th.Client
	other := th.CreateUser()
	otherClient := loginAs(t, th, other)
	chID := th.Channel.Id

	// A private channel that only the admin belongs to.
	secret := th.CreateChannel(model.ChannelTypePrivate)

	var created tasks.Task
	t.Run("member creates, reads and lists", func(t *testing.T) {
		code := call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{
			"channel_id": chID, "title": "Write docs", "assignees": []string{member.Id},
			"labels": []string{"Docs"}, "checklist": []string{"outline"}, "priority": "high",
		}, &created)
		require.Equal(t, http.StatusCreated, code)
		assert.Equal(t, member.Id, created.CreatedBy)
		assert.Equal(t, []string{"docs"}, created.Labels)

		var got tasks.Task
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodGet, "/tasks/"+created.ID, nil, &got))
		assert.Equal(t, created.ID, got.ID)

		var list taskList
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodGet, "/tasks?channel_id="+chID, nil, &list))
		require.Len(t, list.Tasks, 1)

		list = taskList{}
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodGet, "/tasks?assignee=me", nil, &list))
		require.Len(t, list.Tasks, 1, "cross-channel 'mine' resolves membership from the real channelmembers table")
	})

	t.Run("validation errors", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": " "}, nil))
		assert.Equal(t, http.StatusBadRequest, call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "x", "status": "nope"}, nil))
		assert.Equal(t, http.StatusBadRequest, call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "x", "bogus": 1}, nil), "unknown fields are rejected")
		assert.Equal(t, http.StatusBadRequest, call(t, th, memberClient, http.MethodGet, "/tasks?status=nope", nil, nil))
	})

	t.Run("assignees must be channel members", func(t *testing.T) {
		code := call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{
			"channel_id": chID, "title": "bad assignee", "assignees": []string{other.Id},
		}, nil)
		assert.Equal(t, http.StatusBadRequest, code)

		_, _, err := th.AdminClient.AddChannelMember(ctx, chID, other.Id)
		require.NoError(t, err)
		code = call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{
			"channel_id": chID, "title": "now ok", "assignees": []string{other.Id},
		}, nil)
		assert.Equal(t, http.StatusCreated, code)
		_, err = th.AdminClient.RemoveUserFromChannel(ctx, chID, other.Id)
		require.NoError(t, err)
	})

	t.Run("non-members are kept out of a private channel", func(t *testing.T) {
		var secretTask tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, th.AdminClient, http.MethodPost, "/tasks", map[string]any{"channel_id": secret.Id, "title": "secret plan"}, &secretTask))

		assert.Equal(t, http.StatusForbidden, call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{"channel_id": secret.Id, "title": "sneaky"}, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, memberClient, http.MethodGet, "/tasks/"+secretTask.ID, nil, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, memberClient, http.MethodPatch, "/tasks/"+secretTask.ID, map[string]any{"title": "hijack"}, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, memberClient, http.MethodDelete, "/tasks/"+secretTask.ID, nil, nil))
		assert.Equal(t, http.StatusForbidden, call(t, th, memberClient, http.MethodGet, "/tasks?channel_id="+secret.Id, nil, nil))

		var list taskList
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodGet, "/tasks", nil, &list))
		for _, task := range list.Tasks {
			assert.NotEqual(t, secret.Id, task.ChannelID, "cross-channel list never includes channels the caller is not in")
		}

		var still tasks.Task
		require.Equal(t, http.StatusOK, call(t, th, th.AdminClient, http.MethodGet, "/tasks/"+secretTask.ID, nil, &still))
		assert.Equal(t, "secret plan", still.Title, "the failed hijack changed nothing")
	})

	t.Run("a team member outside an open channel cannot touch its tasks", func(t *testing.T) {
		// Reading open channels is allowed by Mattermost; writing needs membership.
		// Existing tasks answer 404, as if they were missing, so their ids do not leak.
		assert.Equal(t, http.StatusNotFound, call(t, th, otherClient, http.MethodPatch, "/tasks/"+created.ID, map[string]any{"title": "drive-by"}, nil))
		assert.Equal(t, http.StatusForbidden, call(t, th, otherClient, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "drive-by"}, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, otherClient, http.MethodDelete, "/tasks/"+created.ID, nil, nil))
		var got tasks.Task
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodGet, "/tasks/"+created.ID, nil, &got))
		assert.Equal(t, "Write docs", got.Title, "the drive-by edit changed nothing")
	})

	t.Run("idempotent create by source and external_id", func(t *testing.T) {
		body := map[string]any{"channel_id": chID, "title": "from pa", "source": "pa", "external_id": "t-1"}
		var first, second tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, memberClient, http.MethodPost, "/tasks", body, &first))
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodPost, "/tasks", body, &second))
		assert.Equal(t, first.ID, second.ID)
	})

	t.Run("update, move, checklist and delete", func(t *testing.T) {
		p := "/tasks/" + created.ID
		var got tasks.Task
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodPatch, p, map[string]any{"title": "Write docs v2", "due_at": 1_900_000_000_000}, &got))
		assert.Equal(t, "Write docs v2", got.Title)
		require.NotNil(t, got.DueAt)

		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodPatch, p, map[string]any{"due_at": nil}, &got))
		assert.Nil(t, got.DueAt)

		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodPost, p+"/move", map[string]any{"status": "done"}, &got))
		assert.Equal(t, tasks.StatusDone, got.Status)
		assert.NotNil(t, got.CompletedAt)
		assert.Equal(t, http.StatusBadRequest, call(t, th, memberClient, http.MethodPost, p+"/move", map[string]any{"status": "nope"}, nil))

		var item tasks.ChecklistItem
		require.Equal(t, http.StatusCreated, call(t, th, memberClient, http.MethodPost, p+"/checklist", map[string]any{"text": "review"}, &item))
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodPatch, p+"/checklist/"+item.ID, map[string]any{"done": true}, &item))
		assert.True(t, item.Done)
		require.Equal(t, http.StatusNoContent, call(t, th, memberClient, http.MethodDelete, p+"/checklist/"+item.ID, nil, nil))

		require.Equal(t, http.StatusNoContent, call(t, th, memberClient, http.MethodDelete, p, nil, nil))
		assert.Equal(t, http.StatusNotFound, call(t, th, memberClient, http.MethodGet, p, nil, nil))
	})

	t.Run("archived channels are read-only", func(t *testing.T) {
		ch := th.CreateChannel(model.ChannelTypeOpen)
		_, _, err := th.AdminClient.AddChannelMember(ctx, ch.Id, member.Id)
		require.NoError(t, err)
		var task tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{"channel_id": ch.Id, "title": "before archive"}, &task))

		_, err = th.AdminClient.DeleteChannel(ctx, ch.Id)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{"channel_id": ch.Id, "title": "after archive"}, nil))
		assert.Equal(t, http.StatusForbidden, call(t, th, memberClient, http.MethodPatch, "/tasks/"+task.ID, map[string]any{"title": "edit"}, nil))
	})
}
