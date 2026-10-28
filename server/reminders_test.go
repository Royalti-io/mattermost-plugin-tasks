package main

import (
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

func TestDigestDue(t *testing.T) {
	loc := time.FixedZone("x", 0)
	at := func(h int) time.Time { return time.Date(2026, 10, 29, h, 30, 0, 0, loc) }

	due, today := digestDue(at(9), "")
	assert.True(t, due)
	assert.Equal(t, "2026-10-29", today)

	due, _ = digestDue(at(8), "")
	assert.False(t, due, "before the digest hour")

	due, _ = digestDue(at(17), "")
	assert.True(t, due, "a run that was delayed still sends the same day")

	due, _ = digestDue(at(18), "")
	assert.False(t, due, "too late in the day to be useful")

	due, _ = digestDue(at(10), "2026-10-29")
	assert.False(t, due, "already sent today")

	due, _ = digestDue(at(10), "2026-10-28")
	assert.True(t, due, "yesterday's digest does not block today's")
}

func TestUserLocation(t *testing.T) {
	assert.Equal(t, time.UTC, userLocation(&model.User{}))

	auto := &model.User{Timezone: model.StringMap{"useAutomaticTimezone": "true", "automaticTimezone": "Africa/Lagos", "manualTimezone": "Asia/Tokyo"}}
	assert.Equal(t, "Africa/Lagos", userLocation(auto).String())

	manual := &model.User{Timezone: model.StringMap{"useAutomaticTimezone": "false", "automaticTimezone": "Africa/Lagos", "manualTimezone": "Asia/Tokyo"}}
	assert.Equal(t, "Asia/Tokyo", userLocation(manual).String())

	bad := &model.User{Timezone: model.StringMap{"useAutomaticTimezone": "true", "automaticTimezone": "Not/AZone"}}
	assert.Equal(t, time.UTC, userLocation(bad), "an unknown zone falls back to UTC")
}

func ptr(v int64) *int64 { return &v }

func TestClassifyDigest(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, loc)
	endToday := time.Date(2026, 10, 29, 23, 59, 59, 0, loc).UnixMilli()

	late := &tasks.Task{ID: "late", Status: tasks.StatusTodo, DueAt: ptr(now.Add(-48 * time.Hour).UnixMilli())}
	earlier := &tasks.Task{ID: "earlier", Status: tasks.StatusBlocked, DueAt: ptr(now.Add(-72 * time.Hour).UnixMilli())}
	todayT := &tasks.Task{ID: "today", Status: tasks.StatusTodo, DueAt: ptr(endToday)}
	tomorrow := &tasks.Task{ID: "tomorrow", Status: tasks.StatusTodo, DueAt: ptr(endToday + 1000)}
	done := &tasks.Task{ID: "done", Status: tasks.StatusDone, DueAt: ptr(endToday)}
	undated := &tasks.Task{ID: "undated", Status: tasks.StatusTodo}

	overdue, today, week := classifyDigest(now, []*tasks.Task{tomorrow, late, todayT, done, undated, earlier})
	ids := func(ts []*tasks.Task) []string {
		out := []string{}
		for _, x := range ts {
			out = append(out, x.ID)
		}
		return out
	}
	assert.Equal(t, []string{"earlier", "late"}, ids(overdue), "oldest overdue first")
	assert.Equal(t, []string{"today"}, ids(today), "end of the local day still counts as today")
	assert.Equal(t, []string{"tomorrow"}, ids(week))
}

func TestFormatReminders(t *testing.T) {
	loc, err := time.LoadLocation("Africa/Lagos")
	require.NoError(t, err)
	due := time.Date(2026, 10, 28, 23, 59, 59, 0, loc).UnixMilli()

	msg := formatReminders(loc,
		[]reminderItem{{Title: "Pay [vendor]", ChannelName: "~Ops", Link: "https://mm/x", DueAt: due}},
		[]reminderItem{{Title: "Ship", Link: "https://mm/tasks-board", DueAt: due}},
	)

	assert.Contains(t, msg, "Task reminders")
	assert.Contains(t, msg, "**Overdue**")
	assert.Contains(t, msg, "**Due soon**")
	assert.Contains(t, msg, `[**Pay \[vendor\]**](https://mm/x) in ~Ops · was due Wed Oct 28`)
	assert.Contains(t, msg, "[**Ship**](https://mm/tasks-board) · due Wed Oct 28")
	assert.False(t, strings.HasSuffix(msg, "\n"))

	onlySoon := formatReminders(loc, nil, []reminderItem{{Title: "A", Link: "l", DueAt: due}})
	assert.NotContains(t, onlySoon, "Overdue")
}

func TestFormatDigestSections(t *testing.T) {
	loc := time.UTC
	due := time.Date(2026, 10, 29, 12, 0, 0, 0, loc).UnixMilli()
	item := reminderItem{Title: "T", Link: "l", DueAt: due}

	msg := formatDigest(loc, []reminderItem{item}, []reminderItem{item}, []reminderItem{item})
	for _, h := range []string{"**Overdue**", "**Due today**", "**Coming up this week**"} {
		assert.Contains(t, msg, h)
	}
	assert.NotContains(t, formatDigest(loc, nil, []reminderItem{item}, nil), "Overdue")
}

func TestEscapeMarkdown(t *testing.T) {
	assert.Equal(t, `a \[b\] c d`, escapeMarkdown("a [b] c\nd"))
}
