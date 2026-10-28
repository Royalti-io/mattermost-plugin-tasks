package tasks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateInputNormalizeDefaults(t *testing.T) {
	in, err := CreateInput{ChannelID: "c1", Title: "  Ship it  "}.Normalize()
	require.NoError(t, err)
	assert.Equal(t, "Ship it", in.Title)
	assert.Equal(t, StatusTodo, in.Status)
	assert.Equal(t, PriorityNormal, in.Priority)
}

func TestCreateInputNormalizeErrors(t *testing.T) {
	tests := []struct {
		name string
		in   CreateInput
	}{
		{"no channel", CreateInput{Title: "x"}},
		{"blank title", CreateInput{ChannelID: "c", Title: "   "}},
		{"long title", CreateInput{ChannelID: "c", Title: strings.Repeat("a", MaxTitleLen+1)}},
		{"bad status", CreateInput{ChannelID: "c", Title: "x", Status: "nope"}},
		{"bad priority", CreateInput{ChannelID: "c", Title: "x", Priority: "urgent"}},
		{"source without external id", CreateInput{ChannelID: "c", Title: "x", Source: "pa"}},
		{"external id without source", CreateInput{ChannelID: "c", Title: "x", ExternalID: "1"}},
		{"blank checklist item", CreateInput{ChannelID: "c", Title: "x", Checklist: []string{" "}}},
		{"long description", CreateInput{ChannelID: "c", Title: "x", Description: strings.Repeat("a", MaxDescriptionLen+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.in.Normalize()
			assert.Error(t, err)
		})
	}
}

func TestNormalizeLabelsAndUsers(t *testing.T) {
	labels, err := NormalizeLabels([]string{" Bug ", "bug", "", "Urgent"})
	require.NoError(t, err)
	assert.Equal(t, []string{"bug", "urgent"}, labels)

	_, err = NormalizeLabels([]string{strings.Repeat("a", MaxLabelLen+1)})
	assert.Error(t, err)

	users, err := NormalizeUsers([]string{"u1", " u1 ", "", "u2"})
	require.NoError(t, err)
	assert.Equal(t, []string{"u1", "u2"}, users)

	many := make([]string, MaxAssignees+1)
	for i := range many {
		many[i] = string(rune('a' + i))
	}
	_, err = NormalizeUsers(many)
	assert.Error(t, err)
}

func TestPatchDueAtDistinguishesAbsentFromNull(t *testing.T) {
	var absent Patch
	require.NoError(t, json.Unmarshal([]byte(`{"title":"x"}`), &absent))
	assert.False(t, absent.DueAt.Set)

	var cleared Patch
	require.NoError(t, json.Unmarshal([]byte(`{"due_at":null}`), &cleared))
	assert.True(t, cleared.DueAt.Set)
	assert.Nil(t, cleared.DueAt.Value)

	var set Patch
	require.NoError(t, json.Unmarshal([]byte(`{"due_at":1700000000000}`), &set))
	assert.True(t, set.DueAt.Set)
	require.NotNil(t, set.DueAt.Value)
	assert.Equal(t, int64(1700000000000), *set.DueAt.Value)
}

func TestPatchNormalize(t *testing.T) {
	bad := "nope"
	_, err := Patch{Status: &bad}.Normalize()
	assert.Error(t, err)

	blank := " "
	_, err = Patch{Title: &blank}.Normalize()
	assert.Error(t, err)

	title := " ok "
	p, err := Patch{Title: &title}.Normalize()
	require.NoError(t, err)
	assert.Equal(t, "ok", *p.Title)
}
