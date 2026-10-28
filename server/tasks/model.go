// Package tasks holds the task domain types and validation. It has no
// dependency on Mattermost or SQL so it can be tested in isolation.
package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Fixed status set for v1. The status is stored as a plain string key so custom
// columns can be added later without rewriting task rows.
const (
	StatusBacklog    = "backlog"
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
)

const (
	PriorityLow      = "low"
	PriorityNormal   = "normal"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
)

var (
	Statuses   = []string{StatusBacklog, StatusTodo, StatusInProgress, StatusBlocked, StatusDone}
	Priorities = []string{PriorityLow, PriorityNormal, PriorityHigh, PriorityCritical}
)

// Limits keep one task from growing without bound.
const (
	MaxTitleLen       = 200
	MaxDescriptionLen = 10000
	MaxAssignees      = 20
	MaxLabels         = 20
	MaxLabelLen       = 40
	MaxChecklistItems = 100
	MaxChecklistLen   = 500
	MaxSourceLen      = 64
	MaxExternalIDLen  = 255
)

// ChecklistItem is one subtask inside a task.
type ChecklistItem struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Done     bool   `json:"done"`
	Position int    `json:"position"`
}

// Task is a channel-scoped work item. Timestamps are Unix milliseconds,
// matching Mattermost.
type Task struct {
	ID          string          `json:"id"`
	ChannelID   string          `json:"channel_id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Priority    string          `json:"priority"`
	DueAt       *int64          `json:"due_at"`
	Position    float64         `json:"position"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   int64           `json:"created_at"`
	UpdatedAt   int64           `json:"updated_at"`
	CompletedAt *int64          `json:"completed_at"`
	PostID      string          `json:"post_id,omitempty"`
	Source      string          `json:"source,omitempty"`
	ExternalID  string          `json:"external_id,omitempty"`
	Assignees   []string        `json:"assignees"`
	Labels      []string        `json:"labels"`
	Checklist   []ChecklistItem `json:"checklist"`
}

// CreateInput is what a caller supplies to create a task.
type CreateInput struct {
	ChannelID   string   `json:"channel_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	DueAt       *int64   `json:"due_at"`
	Position    *float64 `json:"position"`
	Assignees   []string `json:"assignees"`
	Labels      []string `json:"labels"`
	Checklist   []string `json:"checklist"`
	PostID      string   `json:"post_id"`
	Source      string   `json:"source"`
	ExternalID  string   `json:"external_id"`
}

// OptionalInt64 distinguishes "field absent" from "field set to null" in a PATCH body.
type OptionalInt64 struct {
	Set   bool
	Value *int64
}

func (o *OptionalInt64) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// Patch holds optional changes. Nil fields are left alone. Assignees and Labels
// replace the whole set when present.
type Patch struct {
	Title       *string       `json:"title"`
	Description *string       `json:"description"`
	Status      *string       `json:"status"`
	Priority    *string       `json:"priority"`
	DueAt       OptionalInt64 `json:"due_at"`
	Assignees   *[]string     `json:"assignees"`
	Labels      *[]string     `json:"labels"`
}

// Filter selects tasks. ChannelIDs is mandatory: callers must scope every list
// to channels the requester can read.
type Filter struct {
	ChannelIDs []string
	AssigneeID string
	Statuses   []string
	Priority   string
	Label      string
	Query      string
	DueBefore  *int64
	DueAfter   *int64
	Limit      int
	Offset     int
}

const (
	DefaultLimit = 100
	MaxLimit     = 500
)

func IsStatus(s string) bool   { return contains(Statuses, s) }
func IsPriority(s string) bool { return contains(Priorities, s) }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func validateTitle(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("title is required")
	}
	if utf8.RuneCountInString(s) > MaxTitleLen {
		return "", fmt.Errorf("title is longer than %d characters", MaxTitleLen)
	}
	return s, nil
}

func validateDescription(s string) error {
	if utf8.RuneCountInString(s) > MaxDescriptionLen {
		return fmt.Errorf("description is longer than %d characters", MaxDescriptionLen)
	}
	return nil
}

// NormalizeUsers trims, drops blanks and duplicates, and enforces the cap.
func NormalizeUsers(ids []string) ([]string, error) {
	out := dedupe(ids)
	if len(out) > MaxAssignees {
		return nil, fmt.Errorf("more than %d assignees", MaxAssignees)
	}
	return out, nil
}

// NormalizeLabels lowercases, trims, drops blanks and duplicates, and enforces the caps.
func NormalizeLabels(labels []string) ([]string, error) {
	lowered := make([]string, 0, len(labels))
	for _, l := range labels {
		lowered = append(lowered, strings.ToLower(l))
	}
	out := dedupe(lowered)
	if len(out) > MaxLabels {
		return nil, fmt.Errorf("more than %d labels", MaxLabels)
	}
	for _, l := range out {
		if utf8.RuneCountInString(l) > MaxLabelLen {
			return nil, fmt.Errorf("label %q is longer than %d characters", l, MaxLabelLen)
		}
	}
	return out, nil
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ValidateChecklistText checks one checklist item's text.
func ValidateChecklistText(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("checklist text is required")
	}
	if utf8.RuneCountInString(s) > MaxChecklistLen {
		return "", fmt.Errorf("checklist text is longer than %d characters", MaxChecklistLen)
	}
	return s, nil
}

// Normalize validates the input and fills defaults. It returns a cleaned copy.
func (in CreateInput) Normalize() (CreateInput, error) {
	var err error
	if in.ChannelID == "" {
		return in, fmt.Errorf("channel_id is required")
	}
	if in.Title, err = validateTitle(in.Title); err != nil {
		return in, err
	}
	if err = validateDescription(in.Description); err != nil {
		return in, err
	}
	if in.Status == "" {
		in.Status = StatusTodo
	}
	if !IsStatus(in.Status) {
		return in, fmt.Errorf("invalid status %q", in.Status)
	}
	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if !IsPriority(in.Priority) {
		return in, fmt.Errorf("invalid priority %q", in.Priority)
	}
	if in.Assignees, err = NormalizeUsers(in.Assignees); err != nil {
		return in, err
	}
	if in.Labels, err = NormalizeLabels(in.Labels); err != nil {
		return in, err
	}
	if len(in.Checklist) > MaxChecklistItems {
		return in, fmt.Errorf("more than %d checklist items", MaxChecklistItems)
	}
	items := make([]string, 0, len(in.Checklist))
	for _, text := range in.Checklist {
		clean, cerr := ValidateChecklistText(text)
		if cerr != nil {
			return in, cerr
		}
		items = append(items, clean)
	}
	in.Checklist = items
	if (in.Source == "") != (in.ExternalID == "") {
		return in, fmt.Errorf("source and external_id must be set together")
	}
	if utf8.RuneCountInString(in.Source) > MaxSourceLen || utf8.RuneCountInString(in.ExternalID) > MaxExternalIDLen {
		return in, fmt.Errorf("source or external_id too long")
	}
	return in, nil
}

// Normalize validates the patch and returns a cleaned copy.
func (p Patch) Normalize() (Patch, error) {
	var err error
	if p.Title != nil {
		t, terr := validateTitle(*p.Title)
		if terr != nil {
			return p, terr
		}
		p.Title = &t
	}
	if p.Description != nil {
		if err = validateDescription(*p.Description); err != nil {
			return p, err
		}
	}
	if p.Status != nil && !IsStatus(*p.Status) {
		return p, fmt.Errorf("invalid status %q", *p.Status)
	}
	if p.Priority != nil && !IsPriority(*p.Priority) {
		return p, fmt.Errorf("invalid priority %q", *p.Priority)
	}
	if p.Assignees != nil {
		a, aerr := NormalizeUsers(*p.Assignees)
		if aerr != nil {
			return p, aerr
		}
		p.Assignees = &a
	}
	if p.Labels != nil {
		l, lerr := NormalizeLabels(*p.Labels)
		if lerr != nil {
			return p, lerr
		}
		p.Labels = &l
	}
	return p, nil
}
