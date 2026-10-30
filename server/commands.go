package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

const commandTrigger = "task"

const commandHelp = "" +
	"**Tasks**\n" +
	"- `/task add <title>`: add a task to this channel\n" +
	"- `/task list`: your open assigned tasks\n" +
	"- `/task digest on|off|status`: a daily summary DM of what is due\n" +
	"- `/task private on|off|status`: make the tasks you create private by default (only you and the people assigned can see them)\n" +
	"- `/task help`: this message"

func (p *Plugin) registerCommand() error {
	root := model.NewAutocompleteData(commandTrigger, "[add|list|digest|private|help]", "Manage tasks")
	root.AddCommand(model.NewAutocompleteData("add", "<title>", "Add a task to this channel"))
	root.AddCommand(model.NewAutocompleteData("list", "", "List your open assigned tasks"))
	digest := model.NewAutocompleteData("digest", "[on|off|status]", "Daily summary of your due tasks")
	digest.AddStaticListArgument("", true, []model.AutocompleteListItem{
		{Item: "on", HelpText: "Send me a daily summary"},
		{Item: "off", HelpText: "Stop the daily summary"},
		{Item: "status", HelpText: "Show whether it is on"},
	})
	root.AddCommand(digest)
	private := model.NewAutocompleteData("private", "[on|off|status]", "Make your new tasks private by default")
	private.AddStaticListArgument("", true, []model.AutocompleteListItem{
		{Item: "on", HelpText: "New tasks are private unless you share them"},
		{Item: "off", HelpText: "New tasks are visible to the channel"},
		{Item: "status", HelpText: "Show the current setting"},
	})
	root.AddCommand(private)
	root.AddCommand(model.NewAutocompleteData("help", "", "Show command help"))

	return p.client.SlashCommand.Register(&model.Command{
		Trigger:          commandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Manage tasks",
		AutoCompleteHint: "[add|list|digest|private|help]",
		AutocompleteData: root,
	})
}

func ephemeral(text string) *model.CommandResponse {
	return &model.CommandResponse{ResponseType: model.CommandResponseTypeEphemeral, Text: text}
}

// ExecuteCommand handles /task.
func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args.Command), "/"+commandTrigger))
	sub, arg, _ := strings.Cut(rest, " ")
	arg = strings.TrimSpace(arg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch strings.ToLower(sub) {
	case "add":
		return p.commandAdd(ctx, args, arg), nil
	case "list":
		return p.commandList(ctx, args), nil
	case "digest":
		return p.commandDigest(args, arg), nil
	case "private":
		return p.commandPrivate(args, arg), nil
	case "", "help":
		return ephemeral(commandHelp), nil
	default:
		return ephemeral(fmt.Sprintf("Unknown subcommand `%s`.\n\n%s", sub, commandHelp)), nil
	}
}

func (p *Plugin) commandAdd(ctx context.Context, args *model.CommandArgs, title string) *model.CommandResponse {
	in, err := tasks.CreateInput{ChannelID: args.ChannelId, Title: title}.Normalize()
	if err != nil {
		return ephemeral("Could not add the task: " + err.Error() + ".\n\nUsage: `/task add <title>`")
	}
	if !p.canWrite(args.UserId, args.ChannelId) {
		return ephemeral("You cannot create tasks in this channel.")
	}
	prefs, err := p.getPrefs(args.UserId)
	if err != nil {
		p.client.Log.Error("Task command failed", "error", err.Error())
		return ephemeral("Something went wrong reading your settings.")
	}
	in.Private = &prefs.PrivateByDefault
	t, _, err := p.tasks.Create(ctx, in, args.UserId)
	if err != nil {
		p.client.Log.Error("Task command failed", "error", err.Error())
		return ephemeral("Something went wrong adding the task.")
	}
	p.emit("created", args.UserId, t)
	visibility := ""
	if t.Private {
		visibility = " It is private: only you and the people you assign can see it."
	}
	return ephemeral(fmt.Sprintf("Added task **%s**.%s Open the Tasks panel to set a due date or assign it.", escapeMarkdown(t.Title), visibility))
}

func (p *Plugin) commandList(ctx context.Context, args *model.CommandArgs) *model.CommandResponse {
	channelIDs, err := p.tasks.UserChannelIDs(ctx, args.UserId)
	if err != nil {
		p.client.Log.Error("Task command failed", "error", err.Error())
		return ephemeral("Something went wrong loading your tasks.")
	}
	list, err := p.tasks.List(ctx, tasks.Filter{
		ChannelIDs: channelIDs,
		ViewerID:   args.UserId,
		AssigneeID: args.UserId,
		Statuses:   []string{tasks.StatusBacklog, tasks.StatusTodo, tasks.StatusInProgress, tasks.StatusBlocked},
		Limit:      15,
	})
	if err != nil {
		p.client.Log.Error("Task command failed", "error", err.Error())
		return ephemeral("Something went wrong loading your tasks.")
	}
	if len(list) == 0 {
		return ephemeral("You have no open tasks assigned to you.")
	}

	loc := time.UTC
	if u, uerr := p.client.User.Get(args.UserId); uerr == nil {
		loc = userLocation(u)
	}
	var b strings.Builder
	b.WriteString("**Your open tasks**\n")
	for _, t := range list {
		fmt.Fprintf(&b, "- [%s](%s)", escapeMarkdown(t.Title), p.taskLink(t.PostID))
		if t.DueAt != nil {
			fmt.Fprintf(&b, " · due %s", formatDue(*t.DueAt, loc))
		}
		b.WriteString("\n")
	}
	return ephemeral(strings.TrimRight(b.String(), "\n"))
}

func (p *Plugin) commandDigest(args *model.CommandArgs, arg string) *model.CommandResponse {
	prefs, err := p.getPrefs(args.UserId)
	if err != nil {
		p.client.Log.Error("Task command failed", "error", err.Error())
		return ephemeral("Something went wrong reading your settings.")
	}

	switch strings.ToLower(arg) {
	case "on":
		prefs.Digest = true
		prefs.LastDigestDate = ""
		if err = p.setPrefs(args.UserId, prefs); err != nil {
			return ephemeral("Something went wrong saving your settings.")
		}
		return ephemeral(fmt.Sprintf("Daily digest is **on**. You will get a summary of what is due at about %d:00 your time, on days you have something due.", digestHour))
	case "off":
		prefs.Digest = false
		if err = p.setPrefs(args.UserId, prefs); err != nil {
			return ephemeral("Something went wrong saving your settings.")
		}
		return ephemeral("Daily digest is **off**.")
	case "", "status":
		if prefs.Digest {
			return ephemeral("Daily digest is **on**. Turn it off with `/task digest off`.")
		}
		return ephemeral("Daily digest is **off**. Turn it on with `/task digest on`.")
	default:
		return ephemeral("Usage: `/task digest on|off|status`")
	}
}

func (p *Plugin) commandPrivate(args *model.CommandArgs, arg string) *model.CommandResponse {
	prefs, err := p.getPrefs(args.UserId)
	if err != nil {
		p.client.Log.Error("Task command failed", "error", err.Error())
		return ephemeral("Something went wrong reading your settings.")
	}

	switch strings.ToLower(arg) {
	case "on", "off":
		prefs.PrivateByDefault = strings.EqualFold(arg, "on")
		if err = p.setPrefs(args.UserId, prefs); err != nil {
			return ephemeral("Something went wrong saving your settings.")
		}
	case "", "status":
	default:
		return ephemeral("Usage: `/task private on|off|status`")
	}

	if prefs.PrivateByDefault {
		return ephemeral("New tasks are **private** by default: only you and the people assigned can see them. Turn it off with `/task private off`.")
	}
	return ephemeral("New tasks are **shared** with the channel by default. Make them private with `/task private on`.")
}
