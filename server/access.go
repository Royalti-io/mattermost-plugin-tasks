package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// uiAllowed reports whether username appears in a comma-separated allowlist.
// An empty allowlist admits nobody, so a missing setting fails closed.
func uiAllowed(allowlist, username string) bool {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return false
	}

	for _, entry := range strings.Split(allowlist, ",") {
		if strings.ToLower(strings.TrimSpace(entry)) == username {
			return true
		}
	}

	return false
}

type accessResponse struct {
	UIEnabled bool `json:"ui_enabled"`
}

// Access tells the webapp whether to register any UI for the calling user.
// The webapp registers nothing when this is false or the request fails.
func (p *Plugin) Access(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")

	resp := accessResponse{}
	if user, err := p.client.User.Get(userID); err == nil {
		resp.UIEnabled = uiAllowed(p.getConfiguration().UIAllowlist, user.Username)
	} else {
		requestLogger(r).WithError(err).Warnf("Failed to load user for access check")
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		requestLogger(r).WithError(err).Errorf("Failed to write response")
	}
}
