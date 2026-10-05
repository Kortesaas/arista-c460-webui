package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"
)

const supportMaxBytes = 2 << 20

type SupportInput struct {
	IncludeClients bool `json:"includeClients"`
	IncludeEvents  bool `json:"includeEvents"`
}

type supportManifest struct {
	Format    string       `json:"format"`
	Version   int          `json:"version"`
	CreatedAt time.Time    `json:"createdAt"`
	UIVersion string       `json:"uiVersion"`
	SampledAt time.Time    `json:"sampledAt"`
	Includes  SupportInput `json:"includes"`
	Files     []string     `json:"files"`
	Warnings  []string     `json:"warnings"`
	Excludes  []string     `json:"excludes"`
}

// A typed allowlist, independent of the credential-bearing Config, backup,
// OpenConfig trees and raw native files. It intentionally excludes free-form
// error text, LLDP extension fields, account names and client usernames.
func supportState(st APState, clients bool) APState {
	st.Error = ""
	st.Neighbors = nil
	st.Radios = slices.Clone(st.Radios)
	for i, radio := range st.Radios {
		if radio.WiFi7 != nil {
			wifi7 := *radio.WiFi7
			wifi7.Error = ""
			st.Radios[i].WiFi7 = &wifi7
		}
	}
	st.SSIDs = slices.Clone(st.SSIDs)
	for i := range st.SSIDs {
		st.SSIDs[i].MixedStatus = ""
	}
	st.Clients = slices.Clone(st.Clients)
	if !clients {
		st.Clients = nil
	}
	if len(st.Clients) > 512 {
		st.Clients = st.Clients[:512]
	}
	for i := range st.Clients {
		st.Clients[i].Username = ""
	}
	return st
}

func makeSupportArchive(files map[string]any, manifest supportManifest) ([]byte, error) {
	manifest.Files = make([]string, 0, len(files)+1)
	for name := range files {
		manifest.Files = append(manifest.Files, name)
	}
	manifest.Files = append(manifest.Files, "manifest.json")
	sort.Strings(manifest.Files)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	total := 0
	for _, name := range manifest.Files {
		v := files[name]
		if name == "manifest.json" {
			v = manifest
		}
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		total += len(raw)
		if total > supportMaxBytes {
			return nil, errors.New("support snapshot exceeds the 2 MiB limit")
		}
		entry, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: manifest.CreatedAt})
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(raw); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > supportMaxBytes {
		return nil, errors.New("support archive exceeds the 2 MiB limit")
	}
	return buf.Bytes(), nil
}

func (a *API) supportBundle(w http.ResponseWriter, r *http.Request) {
	var input SupportInput
	if !decode(w, r, &input) {
		return
	}
	if !a.supportMu.TryLock() {
		fail(w, 429, "another support bundle is being prepared")
		return
	}
	defer a.supportMu.Unlock()
	st := a.snapshot()
	if st.GeneratedAt.IsZero() {
		fail(w, 503, "Waiting for the first AP sample")
		return
	}
	manifest := supportManifest{Format: "c460-webui-support", Version: 1, CreatedAt: time.Now().UTC(), UIVersion: version, SampledAt: st.GeneratedAt, Includes: input, Warnings: []string{}, Excludes: []string{"passwords and keys", "API/monitoring tokens and SNMP communities", "accounts and client usernames", "raw configuration, logs and crash dumps", "packet capture contents", "nearby wireless networks"}}
	if st.Error != "" {
		manifest.Warnings = append(manifest.Warnings, "The cached AP sample has an error; some values may be stale.")
	}
	if time.Since(st.GeneratedAt) > 30*time.Second {
		manifest.Warnings = append(manifest.Warnings, "The cached AP sample is more than 30 seconds old.")
	}
	if input.IncludeClients && len(st.Clients) > 512 {
		manifest.Warnings = append(manifest.Warnings, "Client details were limited to the first 512 clients.")
	}
	files := map[string]any{"state.json": supportState(st, input.IncludeClients)}
	mgmt, hw, managementErr := a.cli.Snapshot()
	if managementErr == "" {
		files["management.json"] = mgmt
		files["hardware.json"] = struct {
			Serial      string    `json:"serial"`
			PowerSource string    `json:"powerSource"`
			RadioPower  string    `json:"radioPower"`
			NTPSynced   *bool     `json:"ntpSynced"`
			SampledAt   time.Time `json:"sampledAt"`
		}{hw.Serial, hw.PowerSource, hw.RadioPower, hw.NTPSynced, hw.UpdatedAt}
	} else {
		manifest.Warnings = append(manifest.Warnings, "Management and hardware status could not be read.")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	network := readNetwork(ctx, st)
	if !input.IncludeClients {
		network.Neighbors = nil
	}
	if len(network.Neighbors) > 512 {
		network.Neighbors = network.Neighbors[:512]
		manifest.Warnings = append(manifest.Warnings, "Network neighbours were limited to the first 512 entries.")
	}
	files["network.json"] = network
	if input.IncludeEvents {
		events := readWirelessEvents([]string{"/var/log/hostapd.log", "/var/log/hostapd.log.1"}, interfaceNetworks("/sys/class/net", st))
		events.ClockSynced = hw.NTPSynced
		if !input.IncludeClients {
			for i := range events.Events {
				events.Events[i].Client = ""
			}
		}
		files["events.json"] = events
	}
	if a.diagnosticMu.TryLock() {
		rows := []WirelessStatus{}
		wirelessCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		for _, iface := range hostapdInterfaces(a.wirelessDirectory()) {
			if wirelessCtx.Err() != nil || len(rows) >= 64 {
				manifest.Warnings = append(manifest.Warnings, "Wireless status collection stopped at its time/interface limit.")
				break
			}
			out, err := hostapdCommand(wirelessCtx, a.wirelessDirectory(), iface, "STATUS")
			item := WirelessStatus{Interface: iface, Values: map[string]string{}}
			if err != nil || out == "FAIL" || out == "" {
				item.Error = "Status unavailable"
			} else {
				item.Values = selectProperties(hostapdProperties(out), statusFields)
			}
			rows = append(rows, item)
		}
		stop()
		a.diagnosticMu.Unlock()
		files["wireless-status.json"] = map[string]any{"sampledAt": time.Now().UTC(), "interfaces": rows}
	} else {
		manifest.Warnings = append(manifest.Warnings, "Wireless status was skipped because another diagnostic is running.")
	}
	if ctx.Err() != nil {
		manifest.Warnings = append(manifest.Warnings, "Some diagnostic reads exceeded the collection deadline.")
	}
	raw, err := makeSupportArchive(files, manifest)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="c460-support-%s.zip"`, manifest.CreatedAt.Format("20060102T150405Z")))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
