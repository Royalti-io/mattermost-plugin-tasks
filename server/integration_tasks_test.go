package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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

	t.Run("creating from a post replies in its thread", func(t *testing.T) {
		post := th.PostAs(member, chID, "the login page is broken on mobile")
		var task tasks.Task
		code := call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{
			"channel_id": chID, "title": "Fix mobile login", "post_id": post.Id, "notify_thread": true,
		}, &task)
		require.Equal(t, http.StatusCreated, code)
		assert.Equal(t, post.Id, task.PostID)

		thread, _, err := memberClient.GetPostThread(ctx, post.Id, "", false)
		require.NoError(t, err)
		var replies []string
		for _, id := range thread.Order {
			if pp := thread.Posts[id]; pp.Id != post.Id {
				replies = append(replies, pp.Message)
			}
		}
		require.Len(t, replies, 1)
		assert.Contains(t, replies[0], "Fix mobile login")
		assert.Contains(t, replies[0], "@"+member.Username)

		// A post from another channel is refused.
		code = call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{
			"channel_id": chID, "title": "wrong channel", "post_id": th.PostAs(th.AdminUser, secret.Id, "elsewhere").Id,
		}, nil)
		assert.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("task changes are pushed over the websocket to channel members only", func(t *testing.T) {
		wsURL := strings.Replace(th.ServerURL, "http", "ws", 1)
		ws, err := model.NewWebSocketClient4(wsURL, memberClient.AuthToken)
		require.NoError(t, err)
		ws.Listen()
		defer ws.Close()

		// `other` is on the team but not in the channel; they must hear nothing.
		outsider, err := model.NewWebSocketClient4(wsURL, otherClient.AuthToken)
		require.NoError(t, err)
		outsider.Listen()
		defer outsider.Close()

		var task tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{"channel_id": chID, "title": "ws check"}, &task))

		want := "custom_" + testhelper.PluginID() + "_task_changed"
		deadline := time.After(10 * time.Second)
		for {
			select {
			case ev := <-ws.EventChannel:
				if ev == nil || string(ev.EventType()) != want {
					continue
				}
				data := ev.GetData()
				assert.Equal(t, "created", data["action"])
				assert.Equal(t, task.ID, data["task_id"])
				assert.Equal(t, chID, data["channel_id"])

				quiet := time.After(2 * time.Second)
				for {
					select {
					case oe := <-outsider.EventChannel:
						if oe != nil {
							assert.NotEqual(t, want, string(oe.EventType()), "non-members must not receive task events")
						}
					case <-quiet:
						return
					}
				}
			case <-deadline:
				t.Fatal("no task_changed websocket event received")
			}
		}
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

// The web app's assignee picker searches Mattermost's user autocomplete scoped to the
// task's channel. This pins the contract it relies on: channel members come back in
// `users`, everyone else is `out_of_channel`, and a person picked from `users` is accepted.
func TestAssigneePickerContract(t *testing.T) {
	th := testhelper.Setup(t)
	ctx := t.Context()

	teammate := th.CreateUser()
	outsider := th.CreateUser()
	_, _, err := th.AdminClient.AddChannelMember(ctx, th.Channel.Id, teammate.Id)
	require.NoError(t, err)

	// Both new users share the "user-" prefix, so one search shows who is in and who is out.
	res, _, err := th.Client.AutocompleteUsersInChannel(ctx, th.Team.Id, th.Channel.Id, "user-", 10, "")
	require.NoError(t, err)

	var in, out []string
	for _, u := range res.Users {
		in = append(in, u.Id)
	}
	for _, u := range res.OutOfChannel {
		out = append(out, u.Id)
	}
	assert.Contains(t, in, teammate.Id)
	assert.NotContains(t, in, outsider.Id, "non-members are not suggested as assignees")
	assert.Contains(t, out, outsider.Id)

	var task tasks.Task
	require.Equal(t, http.StatusCreated, call(t, th, th.Client, http.MethodPost, "/tasks", map[string]any{"channel_id": th.Channel.Id, "title": "pick me"}, &task))
	assert.Equal(t, http.StatusOK, call(t, th, th.Client, http.MethodPatch, "/tasks/"+task.ID, map[string]any{"assignees": []string{teammate.Id}}, nil), "someone from `users` is accepted")
	assert.Equal(t, http.StatusBadRequest, call(t, th, th.Client, http.MethodPatch, "/tasks/"+task.ID, map[string]any{"assignees": []string{outsider.Id}}, nil), "someone from `out_of_channel` is refused")
}
