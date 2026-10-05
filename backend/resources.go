package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Resource reads use the same cached samples as the UI, never another driver poll.
func (a *API) resource(w http.ResponseWriter, r *http.Request) {
	st := a.snapshot()
	if st.GeneratedAt.IsZero() {
		fail(w, 503, "Waiting for the first AP sample")
		return
	}
	w.Header().Set("X-Sampled-At", st.GeneratedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	w.Header().Set("X-Poll-Seconds", strconv.Itoa(st.PollSeconds))
	name := r.Pattern[strings.LastIndex(r.Pattern, "/")+1:]
	var data any
	switch name {
	case "device":
		data = st.Device
	case "radios":
		data = st.Radios
	case "ssids":
		data = st.SSIDs
	case "clients":
		clients := make([]Client, 0, len(st.Clients))
		for _, c := range st.Clients {
			if q := r.URL.Query().Get("ssid"); q != "" && c.SSID != q {
				continue
			}
			if q := r.URL.Query().Get("band"); q != "" && c.Band != q {
				continue
			}
			clients = append(clients, c)
		}
		data = clients
	case "neighbors":
		data = st.Neighbors
	case "interfaces":
		data = st.Interfaces
	case "management":
		mgmt, _, err := a.cli.Snapshot()
		if err != "" {
			fail(w, 503, err)
			return
		}
		data = mgmt
	case "settings":
		a.cfg.mu.RLock()
		data = map[string]any{"siteName": a.cfg.SiteName, "vlanNames": a.cfg.VLANNames, "timeZone": a.cfg.TimeZone, "pollSeconds": a.cfg.PollSeconds}
		a.cfg.mu.RUnlock()
	case "health":
		mgmt, hw, err := a.cli.Snapshot()
		data = a.healthChecks(stateResponse{APState: st, Management: mgmt, Hardware: hw, ManagementError: err})
	}
	reply(w, 200, map[string]any{"generatedAt": st.GeneratedAt, "pollSeconds": st.PollSeconds, "error": st.Error, "data": data})
}

func (a *API) radioResource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		fail(w, 400, "invalid radio id")
		return
	}
	for _, radio := range a.snapshot().Radios {
		if radio.ID == id {
			reply(w, 200, radio)
			return
		}
	}
	fail(w, 404, "radio not found")
}

func (a *API) ssidResource(w http.ResponseWriter, r *http.Request) {
	for _, ssid := range a.snapshot().SSIDs {
		if ssid.Name == r.PathValue("name") {
			reply(w, 200, ssid)
			return
		}
	}
	fail(w, 404, "network not found")
}

func (a *API) clientResource(w http.ResponseWriter, r *http.Request) {
	for _, client := range a.snapshot().Clients {
		if strings.EqualFold(client.MAC, r.PathValue("mac")) {
			reply(w, 200, client)
			return
		}
	}
	fail(w, 404, "client not found")
}

func (a *API) capabilities(w http.ResponseWriter, r *http.Request) {
	s, _ := a.auth.Session(r)
	permissions := apiScopes
	if s.TokenID != "" {
		permissions = s.Scopes
	} else if s.Role != RoleAdmin {
		permissions = []string{"monitor"}
	}
	reply(w, 200, map[string]any{
		"apiVersion": "1", "softwareVersion": version, "permissions": permissions,
		"authentication": []string{"bearer", "session"}, "openapi": "/api/v1/openapi.json", "documentation": "/api/v1/docs",
		"ssidFeatures": ssidFeatureDefs, "radioFeatures": radioFeatureDefs,
		"securityModes": opModes, "bands": []string{"2.4", "5", "6"}, "radioWidths": widths,
		"wifi7Widths": []int{160, 320}, "maxTokens": 32,
		"ssidPolicy":    map[string]any{"macModes": []string{"off", "allow", "deny"}, "maxMacAddresses": maxPolicyMACs, "minClientsPerBand": 1, "maxClientsPerBand": 127, "backend": "native-legacy", "firmware": "18.2.0-32", "supported": a.policies != nil && a.policies.available()},
		"limits":        map[string]any{"requestBytes": 65536, "diagnosticTimeoutSeconds": 15, "tokenExpiryDays": 3650},
		"packetCapture": map[string]any{"supported": a.captures.supported(), "maxSeconds": captureMaxSeconds, "maxBytes": captureMaxBytes, "maxJobs": captureMaxJobs, "retentionSeconds": int(captureRetention / time.Second), "minSnapLength": 64, "maxSnapLength": 4096, "promiscuous": false, "monitorMode": false},
		"supportBundle": map[string]any{"maxBytes": supportMaxBytes, "format": "zip", "credentialsIncluded": false},
		"traffic":       map[string]any{"supported": a.traffic != nil && a.traffic.supported(), "minKbps": minTrafficKbps, "maxKbps": maxTrafficKbps, "maxClientOverrides": maxTrafficOverrides, "units": "decimal kilobits/second", "scope": "SSID aggregate across bands and MAC client overrides", "ipv4": true, "ipv6": true, "qosPriorities": []string{"voice", "video", "best-effort", "background"}, "qosModes": []string{"fixed", "ceiling"}, "qosOperatingReadback": false},
	})
}
