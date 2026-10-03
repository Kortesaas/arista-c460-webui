package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/status"
)

type API struct {
	cfg     *Config
	auth    *Auth
	gnmi    *GNMI
	poller  *Poller
	writeMu sync.Mutex // one configuration change at a time

	cli             *CLIInfo
	snmp            *SNMPAgent
	changes         *ChangeLog
	history         *History
	overrides       *WirelessOverrides
	scheduler       Scheduler
	defaultPassword defaultPasswordCheck
	cliTrigger      chan struct{}
	stage           func(ManagementRequest, string) error // nil uses native management configuration files
	wirelessDir     string                                // empty uses the firmware socket directory; overridden only in tests
	networkMu       sync.Mutex
	networkCache    *NetworkSnapshot
	eventMu         sync.Mutex
	eventCache      *WirelessEventLog
	diagnosticMu    sync.Mutex // bounded native diagnostics, independent of configuration writes
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/session", a.session)
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", a.logout)

	// Read access, also for the read-only account.
	mux.Handle("GET /api/state", a.read(a.state))
	mux.Handle("GET /api/network", a.read(a.networkStatus))
	mux.Handle("GET /api/events", a.read(a.wirelessEvents))
	mux.Handle("GET /api/changes", a.read(a.changeLog))
	mux.Handle("GET /api/history", a.read(a.historyHandler))
	mux.Handle("GET /api/clients/{mac}/history", a.read(a.clientHistory))
	mux.Handle("GET /api/ssids/{name}/features", a.read(a.getSSIDFeatures))
	mux.Handle("GET /api/lldp", a.read(a.getLLDP))
	mux.Handle("GET /api/trust", a.read(a.trust))
	mux.Handle("GET /api/snmp", a.read(a.snmpSettings))
	mux.Handle("GET /api/metrics", a.read(a.metricsSettings))
	mux.Handle("GET /api/time", a.read(a.timeSettings))
	mux.Handle("GET /api/wireless-status", a.read(a.wirelessStatus))
	mux.Handle("GET /api/clients/{mac}/details", a.read(a.clientDetails))
	mux.Handle("POST /api/diagnostics", a.read(a.diagnose)) // tests only observe

	// Changes, administrator only; each one is written to the change log.
	mux.Handle("POST /api/password", a.write(a.changePassword))
	mux.Handle("PUT /api/viewer", a.write(a.setViewer))
	mux.Handle("DELETE /api/viewer", a.write(a.removeViewer))
	mux.Handle("PUT /api/refresh", a.write(a.updateRefresh))
	mux.Handle("POST /api/ssids", a.write(a.createSSID))
	mux.Handle("PUT /api/ssids/{name}", a.write(a.updateSSID))
	mux.Handle("DELETE /api/ssids/{name}", a.write(a.deleteSSID))
	mux.Handle("PUT /api/ssids/{name}/features", a.write(a.updateSSIDFeatures))
	mux.Handle("PUT /api/ssids/{name}/schedule", a.write(a.updateSchedule))
	mux.Handle("GET /api/ssids/{name}/join", a.write(a.joinCode)) // reveals the password
	mux.Handle("PUT /api/radios/{id}", a.write(a.updateRadio))
	mux.Handle("POST /api/batch", a.write(a.applyBatch))
	mux.Handle("PUT /api/management", a.write(a.updateManagement))
	mux.Handle("PUT /api/lldp", a.write(a.updateLLDP))
	mux.Handle("PUT /api/time", a.write(a.updateTime))
	mux.Handle("PUT /api/timezone", a.write(a.updateTimeZone))
	mux.Handle("PUT /api/snmp", a.write(a.updateSNMP))
	mux.Handle("PUT /api/metrics", a.write(a.updateMetrics))
	mux.Handle("PUT /api/settings", a.write(a.updateSettings))
	mux.Handle("PUT /api/ssh", a.write(a.updateSSH))
	mux.Handle("POST /api/reboot", a.write(a.reboot))
	mux.Handle("POST /api/locate", a.write(a.locate))
	mux.Handle("DELETE /api/locate", a.write(a.stopLocate))
	mux.Handle("POST /api/backup", a.write(a.createBackup))
	mux.Handle("POST /api/restore", a.write(a.restoreBackup))
	mux.Handle("POST /api/clients/{mac}/reconnect", a.write(a.reconnectClient))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "unknown endpoint") })
}

func (a *API) read(h http.HandlerFunc) http.Handler  { return a.guard(h, false) }
func (a *API) write(h http.HandlerFunc) http.Handler { return a.guard(h, true) }

// guard requires a session; mutating requests must also be JSON, which a
// cross-site form cannot send without a CORS preflight. Administrator-only
// handlers are refused for the read-only account and recorded in the change log.
func (a *API) guard(h http.HandlerFunc, admin bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.auth.Session(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "not signed in")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodDelete {
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				fail(w, http.StatusUnsupportedMediaType, "expected application/json")
				return
			}
		}
		if r.Method == http.MethodDelete && r.Header.Get("X-Requested-With") == "" {
			fail(w, http.StatusForbidden, "missing X-Requested-With header")
			return
		}
		if !admin {
			h(w, r)
			return
		}
		if s.Role != RoleAdmin {
			fail(w, http.StatusForbidden, "This account is read-only.")
			return
		}
		// Describe before running, while the current state is still the old one.
		action := a.describeChange(r, requestFields(r))
		rec := &recorder{ResponseWriter: w}
		h(rec, r)
		entry := ChangeEntry{Time: time.Now(), User: s.User, Address: clientIP(r), Action: action, OK: rec.status < 400}
		if !entry.OK {
			var body struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(rec.body.Bytes(), &body)
			entry.Error = body.Error
		}
		a.changes.Record(entry)
	})
}

// isAdmin reports whether the caller may see secrets such as communities.
func (a *API) isAdmin(r *http.Request) bool {
	s, ok := a.auth.Session(r)
	return ok && s.Role == RoleAdmin
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return false
	}
	return true
}

// ---------------------------------------------------------------- session

func (a *API) session(w http.ResponseWriter, r *http.Request) {
	s, ok := a.auth.Session(r)
	out := map[string]any{"authenticated": ok, "configured": a.auth.Configured(), "username": s.User, "role": s.Role}
	if ok && s.Role == RoleAdmin {
		out["viewer"] = a.auth.ViewerName()
	}
	reply(w, http.StatusOK, out)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	role, err := a.auth.Check(r, body.Username, body.Password)
	if err != nil {
		code := http.StatusUnauthorized
		if errors.Is(err, errTooManyAttempts) {
			code = http.StatusTooManyRequests
		}
		fail(w, code, err.Error())
		return
	}
	if err := a.auth.NewSession(w, body.Username, role); err != nil {
		fail(w, http.StatusInternalServerError, "could not create session")
		return
	}
	reply(w, http.StatusOK, map[string]any{"authenticated": true, "role": role})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	a.auth.EndSession(w, r, false)
	reply(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current  string `json:"current"`
		Username string `json:"username"` // empty keeps the current name
		Next     string `json:"next"`
	}
	if !decode(w, r, &body) {
		return
	}
	user := a.auth.Username()
	if role, err := a.auth.Check(r, user, body.Current); err != nil || role != RoleAdmin {
		if err == nil {
			err = errBadPassword
		}
		fail(w, http.StatusForbidden, "current password: "+err.Error())
		return
	}
	if body.Username != "" {
		user = body.Username
	}
	if err := a.auth.SetCredentials(user, body.Next); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	a.auth.EndSession(w, r, true)
	log.Printf("UI credentials changed from %s", clientIP(r))
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) setViewer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := a.auth.SetViewer(strings.TrimSpace(body.Username), body.Password); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true, "viewer": a.auth.ViewerName()})
}

func (a *API) removeViewer(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.RemoveViewer(); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true, "viewer": ""})
}

type stateResponse struct {
	APState
	Management      Management                `json:"management"`
	Hardware        HardwareInfo              `json:"hardware"`
	ManagementError string                    `json:"managementError,omitempty"`
	Health          []HealthItem              `json:"health"`
	TimeZone        string                    `json:"timeZone"`
	Schedules       map[string]ScheduleStatus `json:"schedules"`
}

func (a *API) state(w http.ResponseWriter, r *http.Request) {
	mgmt, hw, err := a.cli.Snapshot()
	resp := stateResponse{APState: a.snapshot(), Management: mgmt, Hardware: hw, ManagementError: err}
	resp.Health = a.healthChecks(resp)
	resp.TimeZone = a.cfg.Zone().String()
	resp.Schedules = a.scheduleStatuses()
	reply(w, http.StatusOK, resp)
}

func (a *API) refreshCLI() {
	select {
	case a.cliTrigger <- struct{}{}:
	default:
	}
}

// ------------------------------------------------------------------ SSIDs

type ssidRequest struct {
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	Hidden    bool     `json:"hidden"`
	OpMode    string   `json:"opmode"`
	Password  string   `json:"password"` // empty keeps the current one
	Bands     []string `json:"bands"`
	VLAN      *int     `json:"vlan"` // null = untagged on the management bridge
	Isolation bool     `json:"isolation"`
}

var opModes = []string{"WPA3_SAE", opModeMixed, "WPA2_PERSONAL", "ENHANCED_OPEN", "OPEN"}

// needsPassword reports whether a security mode uses a pre-shared password.
func needsPassword(opmode string) bool {
	return opmode == "WPA3_SAE" || opmode == opModeMixed || opmode == "WPA2_PERSONAL"
}

func validSSIDName(name string) error {
	if name == "" || len(name) > 32 {
		return errors.New("SSID name must be 1–32 bytes")
	}
	if !utf8.ValidString(name) || strings.ContainsAny(name, "[]/=\\\"") {
		return errors.New(`SSID name must not contain [ ] / = \ or "`)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("SSID name must not contain control characters")
		}
	}
	return nil
}

// ssidConfig validates a request and merges it into current (nil for a new SSID).
// It returns the config container and the leaves that have to be deleted.
func ssidConfig(req ssidRequest, current map[string]any) (map[string]any, []string, error) {
	if err := validSSIDName(req.Name); err != nil {
		return nil, nil, err
	}
	if !slices.Contains(opModes, req.OpMode) {
		return nil, nil, fmt.Errorf("unsupported security mode %q", req.OpMode)
	}
	freq, ok := bandEnum(req.Bands)
	if !ok {
		return nil, nil, errors.New("select at least one valid band (2.4, 5, 6 GHz)")
	}
	if slices.Contains(req.Bands, "6") && req.OpMode != "WPA3_SAE" && req.OpMode != "ENHANCED_OPEN" {
		if req.OpMode == opModeMixed {
			return nil, nil, errors.New("WPA2/WPA3 mixed works on 2.4 and 5 GHz; 6 GHz requires WPA3 only")
		}
		return nil, nil, errors.New("6 GHz requires WPA3 Personal or Enhanced Open (OWE)")
	}
	// Mixed networks are WPA3 in OpenConfig; the native section is switched
	// to transition mode afterwards (see mixed.go).
	native := req.OpMode
	if native == opModeMixed {
		native = "WPA3_SAE"
	}
	if req.VLAN != nil && (*req.VLAN < 1 || *req.VLAN > 4094) {
		return nil, nil, errors.New("VLAN must be 1–4094, or empty for untagged")
	}
	cfg := map[string]any{}
	for k, v := range current {
		cfg[k] = v
	}
	cfg["name"] = req.Name
	cfg["enabled"] = req.Enabled
	cfg["hidden"] = req.Hidden
	cfg["opmode"] = native
	cfg["operating-frequency"] = freq
	cfg["station-isolation"] = req.Isolation
	cfg["mfp"] = native == "WPA3_SAE" || native == "ENHANCED_OPEN"
	if _, ok := cfg["dva"]; !ok {
		cfg["dva"] = false
	}

	var deletes []string
	existing := str(firstOf(cfg["wpa3-psk"], cfg["wpa2-psk"]))
	password := req.Password
	if password == "" {
		password = existing
	}
	keep := map[string]bool{}
	switch native {
	case "WPA3_SAE":
		keep["wpa3-psk"] = true
	case "WPA2_PERSONAL":
		keep["wpa2-psk"] = true
	}
	if len(keep) > 0 {
		if n := len(password); n < 8 || n > 63 {
			return nil, nil, errors.New("password must be 8–63 characters")
		}
	}
	for _, leaf := range []string{"wpa3-psk", "wpa2-psk"} {
		if keep[leaf] {
			cfg[leaf] = password
		} else if _, had := cfg[leaf]; had {
			delete(cfg, leaf)
			deletes = append(deletes, leaf)
		}
	}
	if req.VLAN != nil {
		cfg["default-vlan"] = *req.VLAN
	} else if _, had := cfg["default-vlan"]; had {
		delete(cfg, "default-vlan")
		deletes = append(deletes, "default-vlan")
	}
	return cfg, deletes, nil
}

func ssidBody(cfg map[string]any) map[string]any {
	return map[string]any{"ssids": map[string]any{"ssid": []any{map[string]any{"name": cfg["name"], "config": cfg}}}}
}

func (a *API) ssidLeaf(name, leaf string) *gpb.Path {
	return a.gnmi.apPath(elem("ssids"), elem("ssid", "name", name), elem("config"), elem(leaf))
}

func (a *API) apply(w http.ResponseWriter, r *http.Request, what string, body map[string]any, deletes []*gpb.Path) {
	if err := a.gnmi.SetAP(r.Context(), body, deletes); err != nil {
		msg := err.Error()
		if s, ok := status.FromError(err); ok {
			msg = s.Message()
		}
		log.Printf("%s failed: %v", what, err)
		fail(w, http.StatusBadGateway, "The access point rejected the change: "+msg)
		return
	}
	log.Printf("%s applied by %s", what, clientIP(r))
	a.poller.Refresh()
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

// ssidChange is one validated SSID operation, ready to be sent alone or in a batch.
type ssidChange struct {
	entry   map[string]any // OpenConfig list entry; nil for a deletion
	deletes []*gpb.Path
	oldName string // "" for a new SSID
	newName string // "" for a deletion
	mixed   bool
}

type httpError struct {
	code int
	msg  string
}

func (e httpError) Error() string { return e.msg }

func (a *API) planCreateSSID(req ssidRequest) (ssidChange, error) {
	if _, exists := a.poller.SSIDConfig(req.Name); exists {
		return ssidChange{}, httpError{http.StatusConflict, fmt.Sprintf("a network named %q already exists", req.Name)}
	}
	if req.Password == "" && needsPassword(req.OpMode) {
		return ssidChange{}, httpError{http.StatusBadRequest, "a password is required"}
	}
	cfg, _, err := ssidConfig(req, nil)
	if err != nil {
		return ssidChange{}, httpError{http.StatusBadRequest, err.Error()}
	}
	return ssidChange{entry: map[string]any{"name": req.Name, "config": cfg}, newName: req.Name, mixed: req.OpMode == opModeMixed}, nil
}

func (a *API) planUpdateSSID(name string, req ssidRequest) (ssidChange, error) {
	current, ok := a.poller.SSIDConfig(name)
	if !ok {
		return ssidChange{}, httpError{http.StatusNotFound, fmt.Sprintf("network %q not found", name)}
	}
	renamed := req.Name != name
	if renamed {
		if _, exists := a.poller.SSIDConfig(req.Name); exists {
			return ssidChange{}, httpError{http.StatusConflict, fmt.Sprintf("a network named %q already exists", req.Name)}
		}
	}
	cfg, leaves, err := ssidConfig(req, current)
	if err != nil {
		return ssidChange{}, httpError{http.StatusBadRequest, err.Error()}
	}
	change := ssidChange{entry: map[string]any{"name": req.Name, "config": cfg}, oldName: name, newName: req.Name, mixed: req.OpMode == opModeMixed}
	if renamed {
		// The name is the list key: replace the entry.
		change.deletes = append(change.deletes, a.gnmi.apPath(elem("ssids"), elem("ssid", "name", name)))
	} else {
		for _, leaf := range leaves {
			change.deletes = append(change.deletes, a.ssidLeaf(name, leaf))
		}
	}
	return change, nil
}

func (a *API) planDeleteSSID(name string) (ssidChange, error) {
	if _, ok := a.poller.SSIDConfig(name); !ok {
		return ssidChange{}, httpError{http.StatusNotFound, fmt.Sprintf("network %q not found", name)}
	}
	return ssidChange{deletes: []*gpb.Path{a.gnmi.apPath(elem("ssids"), elem("ssid", "name", name))}, oldName: name}, nil
}

// applySSID sends one SSID change and records its mixed-mode state.
func (a *API) applySSID(w http.ResponseWriter, r *http.Request, what string, change ssidChange, err error) {
	if err != nil {
		var he httpError
		if errors.As(err, &he) {
			fail(w, he.code, he.msg)
		} else {
			fail(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	body := map[string]any{}
	if change.entry != nil {
		body["ssids"] = map[string]any{"ssid": []any{change.entry}}
	}
	rec := &recorder{ResponseWriter: w}
	a.apply(rec, r, what, body, change.deletes)
	if rec.status < 400 {
		a.recordMixed(change)
	}
}

func (a *API) recordMixed(change ssidChange) {
	if err := a.overrides.Update(change.oldName, change.newName, change.mixed); err != nil {
		log.Printf("mixed mode: could not save: %v", err)
	}
}

func (a *API) createSSID(w http.ResponseWriter, r *http.Request) {
	var req ssidRequest
	if !decode(w, r, &req) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	change, err := a.planCreateSSID(req)
	a.applySSID(w, r, "create SSID "+req.Name, change, err)
}

func (a *API) updateSSID(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req ssidRequest
	if !decode(w, r, &req) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	change, err := a.planUpdateSSID(name, req)
	a.applySSID(w, r, "update SSID "+name, change, err)
	if err == nil && name != req.Name {
		a.renameSchedule(name, req.Name)
	}
}

func (a *API) deleteSSID(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	change, err := a.planDeleteSSID(name)
	a.applySSID(w, r, "delete SSID "+name, change, err)
	if err == nil {
		a.renameSchedule(name, "")
	}
}

// ----------------------------------------------------------------- radios

type radioRequest struct {
	Enabled bool `json:"enabled"`
	Channel int  `json:"channel"`
	Width   int  `json:"width"`
	Power   int  `json:"power"`
	DCA     bool `json:"dca"`
	DTP     bool `json:"dtp"`
}

var widths = map[string][]int{"2.4": {20, 40}, "5": {20, 40, 80, 160}, "6": {20, 40, 80, 160, 320}}

func (a *API) updateRadio(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid radio id")
		return
	}
	var req radioRequest
	if !decode(w, r, &req) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	entry, err := a.radioEntry(id, req)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errRadioNotFound) {
			code = http.StatusNotFound
		}
		fail(w, code, err.Error())
		return
	}
	body := map[string]any{"radios": map[string]any{"radio": []any{entry}}}
	a.apply(w, r, fmt.Sprintf("update radio %d", id), body, nil)
}

var errRadioNotFound = errors.New("radio not found")

// radioEntry validates a radio request against the radio's regulatory channel
// list and band, and returns the OpenConfig list entry to send.
func (a *API) radioEntry(id int, req radioRequest) (map[string]any, error) {
	cfg, freq, ok := a.poller.RadioConfig(id)
	if !ok {
		return nil, errRadioNotFound
	}
	b := band(freq)
	snapshot := a.poller.Snapshot()
	for _, radio := range snapshot.Radios {
		if radio.ID == id && len(radio.AllowedChannels) > 0 && !slices.Contains(radio.AllowedChannels, req.Channel) {
			return nil, fmt.Errorf("channel %d is not allowed on the %s GHz radio", req.Channel, b)
		}
	}
	if !slices.Contains(widths[b], req.Width) {
		return nil, fmt.Errorf("%d MHz is not a valid width for %s GHz", req.Width, b)
	}
	if req.Power < 1 || req.Power > 30 {
		return nil, errors.New("transmit power must be 1–30 dBm")
	}
	cfg["id"] = id
	cfg["operating-frequency"] = freq
	cfg["enabled"] = req.Enabled
	cfg["channel"] = req.Channel
	cfg["channel-width"] = req.Width
	cfg["transmit-power"] = req.Power
	cfg["dca"] = req.DCA
	cfg["dtp"] = req.DTP
	return map[string]any{"id": id, "operating-frequency": freq, "config": cfg}, nil
}

// ------------------------------------------------------- management (CLI)

func (a *API) updateManagement(w http.ResponseWriter, r *http.Request) {
	var req ManagementRequest
	if !decode(w, r, &req) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	current, _, readErr := a.cli.Snapshot()
	comm := current.CommVLAN
	if readErr != "" || current.Mode == "" || comm == "" {
		fail(w, http.StatusServiceUnavailable, "Management settings are not ready. Refresh and try again.")
		return
	}
	if req.CommVLAN != "" {
		comm = req.CommVLAN
	}
	_, err := req.cliCommand(comm)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if managementMatches(req, comm, current) {
		reply(w, http.StatusOK, map[string]any{"ok": true, "changed": false, "rebootRequired": current.PendingBoot})
		return
	}
	stage := a.stage
	if stage == nil {
		stage = func(req ManagementRequest, comm string) error { return stageManagement(r.Context(), req, comm) }
	}
	if err := stage(req, comm); err != nil {
		log.Printf("management save failed: %v", err)
		fail(w, http.StatusInternalServerError, "Could not save management settings: "+err.Error())
		return
	}
	log.Printf("management settings staged by %s for VLAN %s", clientIP(r), comm)
	a.cli.RecordManagement(req, comm)
	a.refreshCLI()
	reply(w, http.StatusOK, map[string]any{"ok": true, "changed": true, "rebootRequired": true})
}

func (a *API) trust(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, runTrustCheck(r.Context()))
}

func (a *API) reboot(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if !decode(w, r, &body) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	check := runTrustCheck(r.Context())
	if !check.OK {
		reply(w, http.StatusConflict, map[string]any{
			"error":    "Reboot refused: the firmware would wipe its writable layer on the next boot.",
			"problems": check.Problems,
		})
		return
	}
	log.Printf("reboot requested by %s", clientIP(r))
	reply(w, http.StatusOK, map[string]bool{"ok": true})
	go func() {
		time.Sleep(time.Second)
		if _, err := runCLI(context.Background(), "force reboot"); err != nil {
			log.Printf("reboot failed: %v", err)
		}
	}()
}

func (a *API) locate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Minutes int `json:"minutes"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Minutes < 1 || body.Minutes > 30 {
		fail(w, http.StatusBadRequest, "minutes must be 1–30")
		return
	}
	if _, err := runCLI(r.Context(), fmt.Sprintf("led blink period %d", body.Minutes)); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) stopLocate(w http.ResponseWriter, r *http.Request) {
	if _, err := runCLI(r.Context(), "no led blink"); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

// -------------------------------------------------------------- settings

func (a *API) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SiteName  string            `json:"siteName"`
		VLANNames map[string]string `json:"vlanNames"`
	}
	if !decode(w, r, &body) {
		return
	}
	site := strings.TrimSpace(body.SiteName)
	if utf8.RuneCountInString(site) > 48 || strings.ContainsFunc(site, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		fail(w, http.StatusBadRequest, "device name: up to 48 printable characters")
		return
	}
	names := map[string]string{}
	for id, name := range body.VLANNames {
		n, err := strconv.Atoi(id)
		name = strings.TrimSpace(name)
		if err != nil || n < 1 || n > 4094 || strconv.Itoa(n) != id {
			fail(w, http.StatusBadRequest, fmt.Sprintf("invalid VLAN id %q", id))
			return
		}
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > 24 {
			fail(w, http.StatusBadRequest, "VLAN names: up to 24 characters")
			return
		}
		names[id] = name
	}
	if err := a.cfg.SetLabels(site, names); err != nil {
		fail(w, http.StatusInternalServerError, "could not save settings: "+err.Error())
		return
	}
	a.poller.Refresh()
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) updateSSH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.apply(w, r, fmt.Sprintf("set SSH server enabled=%t", body.Enabled),
		map[string]any{"system": map[string]any{"ssh-server": map[string]any{"config": map[string]any{"enable": body.Enabled}}}}, nil)
}

func (a *API) updateRefresh(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Seconds int `json:"seconds"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Seconds < 1 || input.Seconds > 60 {
		fail(w, 400, "Refresh interval must be 1–60 seconds")
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if err := a.cfg.SetRefresh(input.Seconds); err != nil {
		fail(w, 500, "Could not save refresh interval")
		return
	}
	a.poller.SetInterval(input.Seconds)
	reply(w, 200, map[string]bool{"ok": true})
}
