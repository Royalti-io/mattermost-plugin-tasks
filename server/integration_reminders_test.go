package main

import (
	"fmt"
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

// dmPosts returns the messages the tasks bot has sent a user, oldest first.
func dmPosts(t *testing.T, th *testhelper.TestHelper, client *model.Client4, user *model.User) []string {
	t.Helper()
	ctx := t.Context()
	bot, _, err := th.AdminClient.GetUserByUsername(ctx, "tasks", "")
	require.NoError(t, err)
	ch, _, err := client.CreateDirectChannel(ctx, user.Id, bot.Id)
	require.NoError(t, err)
	list, _, err := client.GetPostsForChannel(ctx, ch.Id, 0, 100, "", false, false)
	require.NoError(t, err)

	var out []string
	for i := len(list.Order) - 1; i >= 0; i-- {
		if p := list.Posts[list.Order[i]]; p.UserId == bot.Id {
			out = append(out, p.Message)
		}
	}
	return out
}

type runResult struct {
	Reminders int `json:"reminders_sent"`
	Digests   int `json:"digests_sent"`
}

func runJobNow(t *testing.T, th *testhelper.TestHelper) runResult {
	t.Helper()
	var res runResult
	require.Equal(t, http.StatusOK, call(t, th, th.AdminClient, http.MethodPost, "/admin/run-reminders", nil, &res))
	return res
}

func ms(d time.Duration) int64 { return time.Now().Add(d).UnixMilli() }

// zoneWhereHourIs returns an IANA zone in which the current local hour is `hour`.
func zoneWhereHourIs(hour int) string {
	n := (hour - time.Now().UTC().Hour() + 24) % 24
	if n > 14 {
		n -= 24
	}
	switch {
	case n == 0:
		return "Etc/GMT"
	case n > 0:
		return fmt.Sprintf("Etc/GMT-%d", n) // Etc/GMT-3 is UTC+3
	default:
		return fmt.Sprintf("Etc/GMT+%d", -n)
	}
}

func TestRemindersAndDigest(t *testing.T) {
	th := testhelper.Setup(t)
	ctx := t.Context()

	member := th.User
	memberClient := th.Client
	chID := th.Channel.Id

	create := func(body map[string]any) tasks.Task {
		t.Helper()
		body["channel_id"] = chID
		var task tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, memberClient, http.MethodPost, "/tasks", body, &task))
		return task
	}

	t.Run("only administrators can run the job by hand", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, call(t, th, memberClient, http.MethodPost, "/admin/run-reminders", nil, nil))
		assert.Equal(t, http.StatusOK, call(t, th, th.AdminClient, http.MethodPost, "/admin/run-reminders", nil, nil))
	})

	t.Run("due-soon and overdue tasks arrive in one grouped DM, once", func(t *testing.T) {
		create(map[string]any{"title": "Send invoice", "assignees": []string{member.Id}, "due_at": ms(2 * time.Hour)})
		create(map[string]any{"title": "File taxes", "assignees": []string{member.Id}, "due_at": ms(-3 * time.Hour)})

		// None of these may produce a reminder.
		create(map[string]any{"title": "Far future", "assignees": []string{member.Id}, "due_at": ms(5 * 24 * time.Hour)})
		create(map[string]any{"title": "No due date", "assignees": []string{member.Id}})
		create(map[string]any{"title": "Nobody owns this", "due_at": ms(time.Hour)})
		create(map[string]any{"title": "Already finished", "assignees": []string{member.Id}, "due_at": ms(time.Hour), "status": "done"})
		create(map[string]any{"title": "Ancient", "assignees": []string{member.Id}, "due_at": ms(-30 * 24 * time.Hour)})

		res := runJobNow(t, th)
		assert.Equal(t, 1, res.Reminders, "one DM for the user, not one per task")

		msgs := dmPosts(t, th, memberClient, member)
		require.Len(t, msgs, 1)
		assert.Contains(t, msgs[0], "Task reminders")
		assert.Contains(t, msgs[0], "Send invoice")
		assert.Contains(t, msgs[0], "File taxes")
		assert.Contains(t, msgs[0], "**Overdue**")
		assert.Contains(t, msgs[0], "**Due soon**")
		for _, absent := range []string{"Far future", "No due date", "Nobody owns this", "Already finished", "Ancient"} {
			assert.NotContains(t, msgs[0], absent)
		}

		assert.Equal(t, 0, runJobNow(t, th).Reminders, "a second run sends nothing")
		assert.Len(t, dmPosts(t, th, memberClient, member), 1)
	})

	t.Run("changing the due date re-arms the reminder", func(t *testing.T) {
		task := create(map[string]any{"title": "Moves around", "assignees": []string{member.Id}, "due_at": ms(3 * time.Hour)})
		require.Equal(t, 1, runJobNow(t, th).Reminders)

		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodPatch, "/tasks/"+task.ID, map[string]any{"due_at": ms(4 * time.Hour)}, nil))
		assert.Equal(t, 1, runJobNow(t, th).Reminders)
		assert.Equal(t, 0, runJobNow(t, th).Reminders)
	})

	t.Run("completing a task before the run cancels its reminder", func(t *testing.T) {
		task := create(map[string]any{"title": "Done in time", "assignees": []string{member.Id}, "due_at": ms(5 * time.Hour)})
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodPost, "/tasks/"+task.ID+"/move", map[string]any{"status": "done"}, nil))
		assert.Equal(t, 0, runJobNow(t, th).Reminders)
	})

	t.Run("someone who left the channel is not reminded", func(t *testing.T) {
		other := th.CreateUser()
		_, _, err := th.AdminClient.AddChannelMember(ctx, chID, other.Id)
		require.NoError(t, err)
		create(map[string]any{"title": "Was theirs", "assignees": []string{other.Id}, "due_at": ms(time.Hour)})
		_, err = th.AdminClient.RemoveUserFromChannel(ctx, chID, other.Id)
		require.NoError(t, err)

		assert.Equal(t, 0, runJobNow(t, th).Reminders)
		assert.Empty(t, dmPosts(t, th, loginAs(t, th, other), other))
	})

	t.Run("/task add, list and help", func(t *testing.T) {
		resp, _, err := memberClient.ExecuteCommand(ctx, chID, "/task add Buy milk")
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "Added task")

		var list taskList
		require.Equal(t, http.StatusOK, call(t, th, memberClient, http.MethodGet, "/tasks?channel_id="+chID+"&q=milk", nil, &list))
		require.Len(t, list.Tasks, 1)
		assert.Equal(t, "Buy milk", list.Tasks[0].Title)
		assert.Equal(t, member.Id, list.Tasks[0].CreatedBy)

		resp, _, err = memberClient.ExecuteCommand(ctx, chID, "/task add")
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "title is required")

		resp, _, err = memberClient.ExecuteCommand(ctx, chID, "/task list")
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "Send invoice")

		resp, _, err = memberClient.ExecuteCommand(ctx, chID, "/task help")
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "/task digest")

		resp, _, err = memberClient.ExecuteCommand(ctx, chID, "/task nonsense")
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "Unknown subcommand")
	})

	t.Run("/task add respects channel permissions", func(t *testing.T) {
		secret := th.CreateChannel(model.ChannelTypePrivate)
		resp, _, err := memberClient.ExecuteCommand(ctx, secret.Id, "/task add sneaky")
		// The server may refuse the command outright for a non-member; either way no task is made.
		if err == nil {
			assert.NotContains(t, resp.Text, "Added task")
		}
		var list taskList
		require.Equal(t, http.StatusOK, call(t, th, th.AdminClient, http.MethodGet, "/tasks?channel_id="+secret.Id, nil, &list))
		assert.Empty(t, list.Tasks)
	})

	t.Run("the daily digest is opt-in, local-time aware and sent once a day", func(t *testing.T) {
		// Put the user where it is midday, so the digest window is open.
		_, _, err := th.AdminClient.PatchUser(ctx, member.Id, &model.UserPatch{Timezone: model.StringMap{
			"useAutomaticTimezone": "false", "manualTimezone": zoneWhereHourIs(12),
		}})
		require.NoError(t, err)

		before := runJobNow(t, th)
		assert.Equal(t, 0, before.Digests, "no digest until the user opts in")

		resp, _, err := memberClient.ExecuteCommand(ctx, chID, "/task digest status")
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "off")
		resp, _, err = memberClient.ExecuteCommand(ctx, chID, "/task digest on")
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "on")

		beforeCount := len(dmPosts(t, th, memberClient, member))
		assert.Equal(t, 1, runJobNow(t, th).Digests)

		msgs := dmPosts(t, th, memberClient, member)
		require.Len(t, msgs, beforeCount+1)
		digest := msgs[len(msgs)-1]
		assert.Contains(t, digest, "Your tasks today")
		assert.Contains(t, digest, "File taxes", "overdue work appears in the digest, even beyond the reminder age cap rules")
		assert.Contains(t, digest, "Send invoice")
		assert.False(t, strings.Contains(digest, "Already finished"))

		assert.Equal(t, 0, runJobNow(t, th).Digests, "once per local day")

		resp, _, err = memberClient.ExecuteCommand(ctx, chID, "/task digest off")
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "off")
	})

	t.Run("the digest waits for the morning in the user's own timezone", func(t *testing.T) {
		other := th.CreateUser()
		otherClient := loginAs(t, th, other)
		_, _, err := th.AdminClient.AddChannelMember(ctx, chID, other.Id)
		require.NoError(t, err)
		_, _, err = th.AdminClient.PatchUser(ctx, other.Id, &model.UserPatch{Timezone: model.StringMap{
			"useAutomaticTimezone": "false", "manualTimezone": zoneWhereHourIs(3), // 03:xx: before the digest hour
		}})
		require.NoError(t, err)
		var task tasks.Task
		require.Equal(t, http.StatusCreated, call(t, th, memberClient, http.MethodPost, "/tasks", map[string]any{
			"channel_id": chID, "title": "Overnight", "assignees": []string{other.Id}, "due_at": ms(30 * time.Hour),
		}, &task))
		_, _, err = otherClient.ExecuteCommand(ctx, chID, "/task digest on")
		require.NoError(t, err)

		assert.Equal(t, 0, runJobNow(t, th).Digests)
	})
}
