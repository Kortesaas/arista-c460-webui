package main

// Staged changes: several SSID and radio edits sent to the AP in one
// transaction, so Wi-Fi restarts once instead of after every edit.

import (
	"errors"
	"fmt"
	"net/http"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
)

const batchLimit = 32

type batchChange struct {
	Kind  string        `json:"kind"` // ssid-create, ssid-update, ssid-delete, radio
	Name  string        `json:"name,omitempty"`
	ID    *int          `json:"id,omitempty"`
	SSID  *ssidRequest  `json:"ssid,omitempty"`
	Radio *radioRequest `json:"radio,omitempty"`
}

type batchPlan struct {
	body    map[string]any
	deletes []*gpb.Path
	ssids   []ssidChange
}

func (a *API) planBatch(changes []batchChange) (*batchPlan, error) {
	if len(changes) == 0 {
		return nil, httpError{http.StatusBadRequest, "no changes to apply"}
	}
	if len(changes) > batchLimit {
		return nil, httpError{http.StatusBadRequest, fmt.Sprintf("at most %d changes at once", batchLimit)}
	}
	plan := &batchPlan{body: map[string]any{}}
	var ssidEntries, radioEntries []any
	names := map[string]bool{}
	radios := map[int]bool{}
	claim := func(names_ ...string) error {
		for _, n := range names_ {
			if n == "" {
				continue
			}
			if names[n] {
				return httpError{http.StatusBadRequest, fmt.Sprintf("network %q is changed twice; combine the changes", n)}
			}
			names[n] = true
		}
		return nil
	}
	for i, c := range changes {
		var change ssidChange
		var err error
		switch c.Kind {
		case "ssid-create":
			if c.SSID == nil {
				return nil, httpError{http.StatusBadRequest, fmt.Sprintf("change %d has no network settings", i+1)}
			}
			change, err = a.planCreateSSID(*c.SSID)
		case "ssid-update":
			if c.SSID == nil {
				return nil, httpError{http.StatusBadRequest, fmt.Sprintf("change %d has no network settings", i+1)}
			}
			change, err = a.planUpdateSSID(c.Name, *c.SSID)
		case "ssid-delete":
			change, err = a.planDeleteSSID(c.Name)
		case "radio":
			if c.ID == nil || c.Radio == nil || radios[*c.ID] {
				return nil, httpError{http.StatusBadRequest, fmt.Sprintf("change %d: invalid or repeated radio", i+1)}
			}
			radios[*c.ID] = true
			entry, err := a.radioEntry(*c.ID, *c.Radio)
			if err != nil {
				return nil, httpError{http.StatusBadRequest, err.Error()}
			}
			radioEntries = append(radioEntries, entry)
			continue
		default:
			return nil, httpError{http.StatusBadRequest, fmt.Sprintf("change %d: unknown kind %q", i+1, c.Kind)}
		}
		if err != nil {
			return nil, err
		}
		if err := claim(change.oldName, change.newName); err != nil {
			return nil, err
		}
		if change.entry != nil {
			ssidEntries = append(ssidEntries, change.entry)
		}
		plan.deletes = append(plan.deletes, change.deletes...)
		plan.ssids = append(plan.ssids, change)
	}
	if len(ssidEntries) > 0 {
		plan.body["ssids"] = map[string]any{"ssid": ssidEntries}
	}
	if len(radioEntries) > 0 {
		plan.body["radios"] = map[string]any{"radio": radioEntries}
	}
	return plan, nil
}

func (a *API) applyBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Changes []batchChange `json:"changes"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	plan, err := a.planBatch(req.Changes)
	if err != nil {
		var he httpError
		if errors.As(err, &he) {
			fail(w, he.code, he.msg)
		} else {
			fail(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	rec := &recorder{ResponseWriter: w}
	a.applyChanges(rec, r, fmt.Sprintf("%d staged changes", len(req.Changes)), plan.body, plan.deletes, plan.ssids)
	if rec.status >= 400 {
		return
	}
	for _, change := range plan.ssids {
		if change.oldName != "" && change.oldName != change.newName {
			a.renameSchedule(change.oldName, change.newName)
		}
	}
}
