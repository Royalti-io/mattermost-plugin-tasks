package main

import (
	"net/http"
	"strings"

	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"
)

// userPrefs are per-user settings kept in the plugin KV store.
type userPrefs struct {
	// Digest turns on the daily summary DM.
	Digest bool `json:"digest"`
	// PrivateByDefault makes tasks the user creates private unless they say otherwise.
	PrivateByDefault bool `json:"private_by_default"`
	// LastDigestDate is the user's local date (YYYY-MM-DD) of the last digest, so it is sent once a day.
	LastDigestDate string `json:"last_digest_date"`
}

const prefKeyPrefix = "pref:"

func prefKey(userID string) string { return prefKeyPrefix + userID }

func (p *Plugin) getPrefs(userID string) (userPrefs, error) {
	var prefs userPrefs
	if err := p.client.KV.Get(prefKey(userID), &prefs); err != nil {
		return userPrefs{}, errors.Wrap(err, "read preferences")
	}
	return prefs, nil
}

func (p *Plugin) setPrefs(userID string, prefs userPrefs) error {
	_, err := p.client.KV.Set(prefKey(userID), prefs)
	return errors.Wrap(err, "save preferences")
}

// claimDigestDate records that today's digest is being sent, atomically, and
// returns false if another instance already did. It re-reads the stored value so
// a concurrent change to the digest setting is not overwritten.
func (p *Plugin) claimDigestDate(userID, date string) (bool, error) {
	var old []byte
	if err := p.client.KV.Get(prefKey(userID), &old); err != nil {
		return false, errors.Wrap(err, "read preferences")
	}
	prefs := userPrefs{}
	if len(old) > 0 {
		if err := jsonUnmarshal(old, &prefs); err != nil {
			return false, errors.Wrap(err, "decode preferences")
		}
	}
	if !prefs.Digest || prefs.LastDigestDate == date {
		return false, nil
	}
	prefs.LastDigestDate = date
	saved, err := p.client.KV.Set(prefKey(userID), prefs, pluginapi.SetAtomic(old))
	return saved, errors.Wrap(err, "claim digest date")
}

// releaseDigestDate undoes a claim after a failed send so the next run retries.
func (p *Plugin) releaseDigestDate(userID, date string) {
	prefs, err := p.getPrefs(userID)
	if err != nil || prefs.LastDigestDate != date {
		return
	}
	prefs.LastDigestDate = ""
	_ = p.setPrefs(userID, prefs)
}

// digestUserIDs lists users who have a stored preference record; callers filter on Digest.
func (p *Plugin) digestUserIDs() ([]string, error) {
	const pageSize = 200
	var ids []string
	for page := 0; ; page++ {
		keys, err := p.client.KV.ListKeys(page, pageSize, pluginapi.WithPrefix(prefKeyPrefix))
		if err != nil {
			return nil, errors.Wrap(err, "list preference keys")
		}
		for _, k := range keys {
			ids = append(ids, strings.TrimPrefix(k, prefKeyPrefix))
		}
		if len(keys) < pageSize {
			return ids, nil
		}
	}
}

type prefsResponse struct {
	PrivateByDefault bool `json:"private_by_default"`
}

// GetPrefs returns the caller's settings that the webapp needs.
func (p *Plugin) GetPrefs(w http.ResponseWriter, r *http.Request) {
	prefs, err := p.getPrefs(callerID(r))
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefsResponse{PrivateByDefault: prefs.PrivateByDefault})
}

type prefsPatch struct {
	PrivateByDefault *bool `json:"private_by_default"`
}

// UpdatePrefs changes the caller's settings; absent fields are left alone.
func (p *Plugin) UpdatePrefs(w http.ResponseWriter, r *http.Request) {
	var patch prefsPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	prefs, err := p.getPrefs(callerID(r))
	if err != nil {
		p.storeError(w, r, err)
		return
	}
	if patch.PrivateByDefault != nil {
		prefs.PrivateByDefault = *patch.PrivateByDefault
	}
	if err = p.setPrefs(callerID(r), prefs); err != nil {
		p.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefsResponse{PrivateByDefault: prefs.PrivateByDefault})
}
