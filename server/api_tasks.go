package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/store/sqlstore"
	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// decodeJSON reads a strict JSON body: unknown fields are rejected so typos surface.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func callerID(r *http.Request) string { return r.Header.Get("Mattermost-User-ID") }

// storeError maps store errors to HTTP responses and logs unexpected ones.
func (p *Plugin) storeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sqlstore.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, sqlstore.ErrConflict):
		writeError(w, http.StatusConflict, "a deleted task already uses this source and external_id")
	case errors.Is(err, sqlstore.ErrLimit):
		writeError(w, http.StatusConflict, "limit reached")
	default:
		requestLogger(r).WithError(err).Errorf("Task store error")
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (p *Plugin) canRead(userID, channelID string) bool {
	return p.client.User.HasPermissionToChannel(userID, channelID, model.PermissionReadChannel)
}

// canWrite requires permission to post and a channel that is not archived.
func (p *Plugin) canWrite(userID, channelID string) bool {
	if !p.client.User.HasPermissionToChannel(userID, channelID, model.PermissionCreatePost) {
		return false
	}
	ch, err := p.client.Channel.Get(channelID)
	return err == nil && ch.DeleteAt == 0
}

// loadTask returns the task if the caller may see it. Callers who cannot see it
// get the same 404 as a missing task so task ids do not leak across channels.
func (p *Plugin) loadTask(w http.ResponseWriter, r *http.Request, write bool) (*tasks.Task, bool) {
	id := mux.Vars(r)["id"]
	if !model.IsValidId(id) {
		writeError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	t, err := p.tasks.Get(r.Context(), id)
	if err != nil {
		p.storeError(w, r, err)
		return nil, false
	}
	if !p.canRead(callerID(r), t.ChannelID) {
		writeError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	if write && !p.canWrite(callerID(r), t.ChannelID) {
		writeError(w, http.StatusForbidden, "you cannot change tasks in this channel")
		return nil, false
	}
	return t, true
}

// checkAssignees requires every assignee to be a member of the channel, so a
// task is never assigned to someone who cannot see it.
func (p *Plugin) checkAssignees(channelID string, assignees []string) error {
	for _, uid := range assignees {
		if !model.IsValidId(uid) {
			return fmt.Errorf("invalid assignee id %q", uid)
		}
		if _, err := p.client.Channel.GetMember(channelID, uid); err != nil {
			return fmt.Errorf("assignee %s is not a member of the channel", uid)
		}
	}
	return nil
}

func (p *Plugin) CreateTask(w http.ResponseWriter, r *http.Request) {
	var in tasks.CreateInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in, err := in.Normalize()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !model.IsValidId(in.ChannelID) || !p.canWrite(callerID(r), in.ChannelID) {
		writeError(w, http.StatusForbidden, "you cannot create tasks in this channel")
		return
	}
	if err = p.checkAssignees(in.ChannelID, in.Assignees); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.PostID != "" {
		post, perr := p.client.Post.GetPost(in.PostID)
		if perr != nil || post.ChannelId != in.ChannelID {
			writeError(w, http.StatusBadRequest, "post_id does not belong to this channel")
			return
		}
	}

	t, created, err := p.tasks.Create(r.Context(), in, callerID(r))
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	status := http.StatusCreated
	if created {
		p.emit("created", callerID(r), t)
		if in.NotifyThread && in.PostID != "" {
			p.replyTaskCreated(r, in.PostID, t)
		}
	} else {
		status = http.StatusOK
	}
	writeJSON(w, status, t)
}

func (p *Plugin) GetTask(w http.ResponseWriter, r *http.Request) {
	if t, ok := p.loadTask(w, r, false); ok {
		writeJSON(w, http.StatusOK, t)
	}
}

func (p *Plugin) UpdateTask(w http.ResponseWriter, r *http.Request) {
	t, ok := p.loadTask(w, r, true)
	if !ok {
		return
	}
	var patch tasks.Patch
	if err := decodeJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	patch, err := patch.Normalize()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if patch.Assignees != nil {
		if err = p.checkAssignees(t.ChannelID, *patch.Assignees); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	updated, err := p.tasks.Update(r.Context(), t.ID, patch)
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	p.emit("updated", callerID(r), updated)
	writeJSON(w, http.StatusOK, updated)
}

type moveRequest struct {
	Status   string   `json:"status"`
	Position *float64 `json:"position"`
}

func (p *Plugin) MoveTask(w http.ResponseWriter, r *http.Request) {
	t, ok := p.loadTask(w, r, true)
	if !ok {
		return
	}
	var req moveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !tasks.IsStatus(req.Status) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid status %q", req.Status))
		return
	}
	moved, err := p.tasks.Move(r.Context(), t.ID, req.Status, req.Position)
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	p.emit("moved", callerID(r), moved)
	writeJSON(w, http.StatusOK, moved)
}

func (p *Plugin) DeleteTask(w http.ResponseWriter, r *http.Request) {
	t, ok := p.loadTask(w, r, true)
	if !ok {
		return
	}
	if err := p.tasks.Delete(r.Context(), t.ID); err != nil {
		p.storeError(w, r, err)
		return
	}
	p.emit("deleted", callerID(r), t)
	w.WriteHeader(http.StatusNoContent)
}

type listResponse struct {
	Tasks []*tasks.Task `json:"tasks"`
}

// ListTasks returns tasks from one channel (channel_id) or, without it, from every
// channel the caller belongs to.
func (p *Plugin) ListTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	uid := callerID(r)

	f := tasks.Filter{
		Priority: q.Get("priority"),
		Label:    q.Get("label"),
		Query:    strings.TrimSpace(q.Get("q")),
	}
	if f.Priority != "" && !tasks.IsPriority(f.Priority) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid priority %q", f.Priority))
		return
	}
	if s := q.Get("status"); s != "" {
		for _, st := range strings.Split(s, ",") {
			if !tasks.IsStatus(st) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid status %q", st))
				return
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	switch a := q.Get("assignee"); a {
	case "":
	case "me":
		f.AssigneeID = uid
	default:
		f.AssigneeID = a
	}
	for name, dst := range map[string]**int64{"due_before": &f.DueBefore, "due_after": &f.DueAfter} {
		if v := q.Get(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, name+" must be Unix milliseconds")
				return
			}
			*dst = &n
		}
	}
	for name, dst := range map[string]*int{"limit": &f.Limit, "offset": &f.Offset} {
		if v := q.Get(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, name+" must be a non-negative integer")
				return
			}
			*dst = n
		}
	}

	if ch := q.Get("channel_id"); ch != "" {
		if !model.IsValidId(ch) || !p.canRead(uid, ch) {
			writeError(w, http.StatusForbidden, "you cannot read this channel")
			return
		}
		f.ChannelIDs = []string{ch}
	} else {
		ids, err := p.tasks.UserChannelIDs(r.Context(), uid)
		if err != nil {
			p.storeError(w, r, err)
			return
		}
		f.ChannelIDs = ids
	}

	list, err := p.tasks.List(r.Context(), f)
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Tasks: list})
}

type checklistCreate struct {
	Text string `json:"text"`
}

type checklistPatch struct {
	Text *string `json:"text"`
	Done *bool   `json:"done"`
}

func (p *Plugin) AddChecklistItem(w http.ResponseWriter, r *http.Request) {
	t, ok := p.loadTask(w, r, true)
	if !ok {
		return
	}
	var req checklistCreate
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	text, err := tasks.ValidateChecklistText(req.Text)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := p.tasks.AddChecklistItem(r.Context(), t.ID, text)
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	p.emitByID("updated", callerID(r), t.ChannelID, t.ID)
	writeJSON(w, http.StatusCreated, item)
}

func (p *Plugin) UpdateChecklistItem(w http.ResponseWriter, r *http.Request) {
	t, ok := p.loadTask(w, r, true)
	if !ok {
		return
	}
	var req checklistPatch
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Text != nil {
		text, err := tasks.ValidateChecklistText(*req.Text)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.Text = &text
	}
	item, err := p.tasks.UpdateChecklistItem(r.Context(), t.ID, mux.Vars(r)["item"], req.Text, req.Done)
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	p.emitByID("updated", callerID(r), t.ChannelID, t.ID)
	writeJSON(w, http.StatusOK, item)
}

func (p *Plugin) DeleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	t, ok := p.loadTask(w, r, true)
	if !ok {
		return
	}
	if err := p.tasks.DeleteChecklistItem(r.Context(), t.ID, mux.Vars(r)["item"]); err != nil {
		p.storeError(w, r, err)
		return
	}
	p.emitByID("updated", callerID(r), t.ChannelID, t.ID)
	w.WriteHeader(http.StatusNoContent)
}

// replyTaskCreated leaves a note in the source post's thread so people see that
// the message became a task. Failure is logged, never returned: the task exists.
func (p *Plugin) replyTaskCreated(r *http.Request, postID string, t *tasks.Task) {
	log := requestLogger(r)
	post, err := p.client.Post.GetPost(postID)
	if err != nil {
		log.WithError(err).Warnf("Cannot load source post for task reply")
		return
	}
	root := post.RootId
	if root == "" {
		root = post.Id
	}
	username := callerID(r)
	if u, uerr := p.client.User.Get(callerID(r)); uerr == nil {
		username = u.Username
	}
	reply := &model.Post{
		ChannelId: t.ChannelID,
		RootId:    root,
		UserId:    p.botID,
		Message:   fmt.Sprintf(":white_check_mark: @%s created a task from this message: **%s**", username, t.Title),
	}
	if err = p.client.Post.CreatePost(reply); err != nil {
		log.WithError(err).Warnf("Cannot post task reply")
	}
}

type runRemindersResponse struct {
	Reminders int `json:"reminders_sent"`
	Digests   int `json:"digests_sent"`
}

// RunReminders runs the reminder and digest job once, for system admins. It is the
// manual trigger for operations and for integration tests.
func (p *Plugin) RunReminders(w http.ResponseWriter, r *http.Request) {
	if !p.client.User.HasPermissionTo(callerID(r), model.PermissionManageSystem) {
		writeError(w, http.StatusForbidden, "system administrators only")
		return
	}
	ctx := r.Context()
	reminders, err := p.runReminders(ctx)
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	digests, err := p.runDigests(ctx, time.Now())
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, runRemindersResponse{Reminders: reminders, Digests: digests})
}
