package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUIAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowlist string
		username  string
		want      bool
	}{
		{"empty allowlist shows the UI to everyone", "", "ada", true},
		{"blank allowlist is the same as empty", "  ", "ada", true},
		{"blank username never allowed", "ada", "", false},
		{"exact match", "ada", "ada", true},
		{"case-insensitive", "Ada", "aDa", true},
		{"trims spaces around entries", " ada , roy ", "roy", true},
		{"not listed", "ada,roy", "eve", false},
		{"no partial matches", "adam", "ada", false},
		{"only empty entries still restricts", ",,", "ada", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, uiAllowed(tt.allowlist, tt.username))
		})
	}
}
