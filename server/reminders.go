package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	// The Mattermost image may ship without zoneinfo; embedding it keeps per-user
	// timezones working instead of silently falling back to UTC.
	_ "time/tzdata"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/store/sqlstore"
	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

const (
	reminderInterval = 5 * time.Minute
	// soonWindow is how far ahead "due soon" looks. Due dates are end of day, so this
	// warns from the start of the due day.
	soonWindow = 24 * time.Hour
	// overdueMaxAge stops the first run on an old instance from DMing about ancient tasks;
	// those still appear in the opt-in digest.
	overdueMaxAge = 7 * 24 * time.Hour
	// maxPerMessage bounds one reminder DM; the rest follow on the next run.
	maxPerMessage = 25

	// The digest goes out at the first run on or after digestHour in the user's local time.
	digestHour    = 9
	digestLastHr  = 18
	digestHorizon = 7 * 24 * time.Hour
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// userLocation returns the user's timezone, falling back to UTC.
func userLocation(u *model.User) *time.Location {
	name := ""
	if u.Timezone != nil {
		if u.Timezone["useAutomaticTimezone"] == "true" {
			name = u.Timezone["automaticTimezone"]
		} else {
			name = u.Timezone["manualTimezone"]
		}
	}
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// digestDue reports whether a digest is due at `local` and returns the local date
// to record. It is due from digestHour until digestLastHr, once per local date.
func digestDue(local time.Time, lastDate string) (bool, string) {
	today := local.Format("2006-01-02")
	if lastDate == today {
		return false, today
	}
	h := local.Hour()
	return h >= digestHour && h < digestLastHr, today
}

type reminderItem struct {
	Title       string
	ChannelName string
	Link        string
	DueAt       int64
}

func formatDue(ms int64, loc *time.Location) string {
	return time.UnixMilli(ms).In(loc).Format("Mon Jan 2")
}

func formatItems(b *strings.Builder, items []reminderItem, loc *time.Location, label string) {
	for _, it := range items {
		fmt.Fprintf(b, "- [**%s**](%s)", escapeMarkdown(it.Title), it.Link)
		if it.ChannelName != "" {
			fmt.Fprintf(b, " in ~%s", it.ChannelName)
		}
		fmt.Fprintf(b, " · %s %s\n", label, formatDue(it.DueAt, loc))
	}
}

// escapeMarkdown stops a task title from breaking the link syntax around it.
func escapeMarkdown(s string) string {
	return strings.NewReplacer("[", `\[`, "]", `\]`, "\n", " ").Replace(s)
}

func formatReminders(loc *time.Location, overdue, soon []reminderItem) string {
	var b strings.Builder
	b.WriteString("##### :alarm_clock: Task reminders\n")
	if len(overdue) > 0 {
		b.WriteString("\n**Overdue**\n")
		formatItems(&b, overdue, loc, "was due")
	}
	if len(soon) > 0 {
		b.WriteString("\n**Due soon**\n")
		formatItems(&b, soon, loc, "due")
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatDigest(loc *time.Location, overdue, today, week []reminderItem) string {
	var b strings.Builder
	b.WriteString("##### :spiral_calendar: Your tasks today\n")
	if len(overdue) > 0 {
		b.WriteString("\n**Overdue**\n")
		formatItems(&b, overdue, loc, "was due")
	}
	if len(today) > 0 {
		b.WriteString("\n**Due today**\n")
		formatItems(&b, today, loc, "due")
	}
	if len(week) > 0 {
		b.WriteString("\n**Coming up this week**\n")
		formatItems(&b, week, loc, "due")
	}
	return strings.TrimRight(b.String(), "\n")
}

// classifyDigest splits open tasks into overdue, due today (local) and later this week.
func classifyDigest(now time.Time, ts []*tasks.Task) (overdue, today, week []*tasks.Task) {
	endOfToday := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999_000_000, now.Location()).UnixMilli()
	for _, t := range ts {
		if t.DueAt == nil || t.Status == tasks.StatusDone {
			continue
		}
		switch due := *t.DueAt; {
		case due < now.UnixMilli():
			overdue = append(overdue, t)
		case due <= endOfToday:
			today = append(today, t)
		default:
			week = append(week, t)
		}
	}
	for _, g := range [][]*tasks.Task{overdue, today, week} {
		sort.Slice(g, func(i, j int) bool { return *g[i].DueAt < *g[j].DueAt })
	}
	return overdue, today, week
}

func (p *Plugin) siteURL() string {
	if cfg := p.client.Configuration.GetConfig(); cfg != nil && cfg.ServiceSettings.SiteURL != nil {
		return strings.TrimRight(*cfg.ServiceSettings.SiteURL, "/")
	}
	return ""
}

func (p *Plugin) taskLink(postID string) string {
	if postID != "" {
		return fmt.Sprintf("%s/_redirect/pl/%s", p.siteURL(), postID)
	}
	return p.siteURL() + "/tasks-board"
}

func (p *Plugin) channelName(id string) string {
	ch, err := p.client.Channel.Get(id)
	if err != nil {
		return ""
	}
	switch ch.Type {
	case model.ChannelTypeDirect, model.ChannelTypeGroup:
		return "a direct message"
	}
	if ch.DisplayName != "" {
		return ch.DisplayName
	}
	return ch.Name
}

// canNotify reports whether a user should hear about tasks in a channel: it must be
// live and the user must still belong to it.
func (p *Plugin) canNotify(userID, channelID string) bool {
	ch, err := p.client.Channel.Get(channelID)
	if err != nil || ch.DeleteAt != 0 {
		return false
	}
	_, err = p.client.Channel.GetMember(channelID, userID)
	return err == nil
}

// reachable returns the user if they can receive DMs: it exists, is active and is not a bot.
func (p *Plugin) reachable(userID string) *model.User {
	u, err := p.client.User.Get(userID)
	if err != nil || u.DeleteAt != 0 || u.IsBot {
		return nil
	}
	return u
}

// runReminders DMs each assignee about their tasks that are newly due soon or overdue.
// Each reminder is claimed in the database before it is sent, so it goes out once
// even if two instances run at the same time. It returns the number of DMs sent.
func (p *Plugin) runReminders(ctx context.Context) (int, error) {
	now := time.Now().UnixMilli()
	cands, err := p.tasks.DueForReminder(ctx, now, soonWindow.Milliseconds(), overdueMaxAge.Milliseconds())
	if err != nil {
		return 0, err
	}

	byUser := map[string][]sqlstore.ReminderCandidate{}
	var order []string
	for _, c := range cands {
		if _, seen := byUser[c.UserID]; !seen {
			order = append(order, c.UserID)
		}
		byUser[c.UserID] = append(byUser[c.UserID], c)
	}

	sent := 0
	for _, uid := range order {
		ok, rerr := p.remindUser(ctx, uid, byUser[uid])
		if rerr != nil {
			p.client.Log.Warn("Reminder failed", "user_id", uid, "error", rerr.Error())
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

func (p *Plugin) remindUser(ctx context.Context, userID string, list []sqlstore.ReminderCandidate) (bool, error) {
	user := p.reachable(userID)
	if user == nil {
		return false, nil
	}
	loc := userLocation(user)

	var claimed []sqlstore.ReminderCandidate
	var overdue, soon []reminderItem
	names := map[string]string{}
	for _, c := range list {
		if len(claimed) >= maxPerMessage {
			break
		}
		if !p.canNotify(userID, c.ChannelID) {
			continue
		}
		won, err := p.tasks.ClaimReminder(ctx, c)
		if err != nil {
			return false, err
		}
		if !won {
			continue
		}
		claimed = append(claimed, c)

		name, ok := names[c.ChannelID]
		if !ok {
			name = p.channelName(c.ChannelID)
			names[c.ChannelID] = name
		}
		item := reminderItem{Title: c.Title, ChannelName: name, Link: p.taskLink(c.PostID), DueAt: c.DueAt}
		if c.Kind == sqlstore.KindOverdue {
			overdue = append(overdue, item)
		} else {
			soon = append(soon, item)
		}
	}
	if len(claimed) == 0 {
		return false, nil
	}

	if err := p.client.Post.DM(p.botID, userID, &model.Post{Message: formatReminders(loc, overdue, soon)}); err != nil {
		// Give the claims back so the next run retries instead of silently dropping them.
		for _, c := range claimed {
			if rerr := p.tasks.ReleaseReminder(ctx, c); rerr != nil {
				p.client.Log.Warn("Could not release reminder claim", "task_id", c.TaskID, "error", rerr.Error())
			}
		}
		return false, err
	}
	return true, nil
}

// runDigests sends the daily summary to users who opted in, once per local day.
func (p *Plugin) runDigests(ctx context.Context, now time.Time) (int, error) {
	ids, err := p.digestUserIDs()
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, uid := range ids {
		ok, derr := p.digestUser(ctx, uid, now)
		if derr != nil {
			p.client.Log.Warn("Digest failed", "user_id", uid, "error", derr.Error())
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

func (p *Plugin) digestUser(ctx context.Context, userID string, now time.Time) (bool, error) {
	prefs, err := p.getPrefs(userID)
	if err != nil || !prefs.Digest {
		return false, err
	}
	user := p.reachable(userID)
	if user == nil {
		return false, nil
	}
	loc := userLocation(user)
	local := now.In(loc)
	due, today := digestDue(local, prefs.LastDigestDate)
	if !due {
		return false, nil
	}
	won, err := p.claimDigestDate(userID, today)
	if err != nil || !won {
		return false, err
	}

	msg, err := p.buildDigest(ctx, userID, local, loc)
	if err != nil {
		p.releaseDigestDate(userID, today)
		return false, err
	}
	if msg == "" {
		return false, nil // nothing to report today; the date stays claimed
	}
	if err = p.client.Post.DM(p.botID, userID, &model.Post{Message: msg}); err != nil {
		p.releaseDigestDate(userID, today)
		return false, err
	}
	return true, nil
}

func (p *Plugin) buildDigest(ctx context.Context, userID string, local time.Time, loc *time.Location) (string, error) {
	channelIDs, err := p.tasks.UserChannelIDs(ctx, userID)
	if err != nil {
		return "", err
	}
	horizon := local.Add(digestHorizon).UnixMilli()
	list, err := p.tasks.List(ctx, tasks.Filter{
		ChannelIDs: channelIDs,
		AssigneeID: userID,
		Statuses:   []string{tasks.StatusBacklog, tasks.StatusTodo, tasks.StatusInProgress, tasks.StatusBlocked},
		DueBefore:  &horizon,
		Limit:      200,
	})
	if err != nil {
		return "", err
	}
	overdue, today, week := classifyDigest(local, list)
	if len(overdue)+len(today)+len(week) == 0 {
		return "", nil
	}

	names := map[string]string{}
	toItems := func(ts []*tasks.Task) []reminderItem {
		out := make([]reminderItem, 0, len(ts))
		for _, t := range ts {
			name, ok := names[t.ChannelID]
			if !ok {
				name = p.channelName(t.ChannelID)
				names[t.ChannelID] = name
			}
			out = append(out, reminderItem{Title: t.Title, ChannelName: name, Link: p.taskLink(t.PostID), DueAt: *t.DueAt})
		}
		return out
	}
	return formatDigest(loc, toItems(overdue), toItems(today), toItems(week)), nil
}

// runJob is the scheduled entry point.
func (p *Plugin) runJob() {
	if p.getConfiguration().DisableReminders {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if _, err := p.runReminders(ctx); err != nil {
		p.client.Log.Error("Reminder run failed", "error", err.Error())
	}
	if _, err := p.runDigests(ctx, time.Now()); err != nil {
		p.client.Log.Error("Digest run failed", "error", err.Error())
	}
}
