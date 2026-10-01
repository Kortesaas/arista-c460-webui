package main

import (
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
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/session", a.session)
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", a.logout)
	mux.Handle("POST /api/password", a.protect(a.changePassword))
	mux.Handle("GET /api/state", a.protect(a.state))
	mux.Handle("POST /api/ssids", a.protect(a.createSSID))
	mux.Handle("PUT /api/ssids/{name}", a.protect(a.updateSSID))
	mux.Handle("DELETE /api/ssids/{name}", a.protect(a.deleteSSID))
	mux.Handle("PUT /api/radios/{id}", a.protect(a.updateRadio))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "unknown endpoint") })
}

// protect requires a session; mutating requests must also be JSON, which a
// cross-site form cannot send without a CORS preflight.
func (a *API) protect(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.auth.Valid(r) {
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
		h(w, r)
	})
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
	reply(w, http.StatusOK, map[string]any{"authenticated": a.auth.Valid(r), "configured": a.auth.Configured(), "username": a.sessionUser(r)})
}

// sessionUser exposes the login name only to signed-in callers.
func (a *API) sessionUser(r *http.Request) string {
	if a.auth.Valid(r) {
		return a.auth.Username()
	}
	return ""
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := a.auth.Check(r, body.Username, body.Password); err != nil {
		code := http.StatusUnauthorized
		if errors.Is(err, errTooManyAttempts) {
			code = http.StatusTooManyRequests
		}
		fail(w, code, err.Error())
		return
	}
	if err := a.auth.NewSession(w); err != nil {
		fail(w, http.StatusInternalServerError, "could not create session")
		return
	}
	reply(w, http.StatusOK, map[string]bool{"authenticated": true})
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
	if err := a.auth.Check(r, user, body.Current); err != nil {
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

func (a *API) state(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, a.poller.Snapshot())
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

var opModes = []string{"WPA3_SAE", "WPA2_PERSONAL", "ENHANCED_OPEN", "OPEN"}

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
		return nil, nil, errors.New("6 GHz requires WPA3 Personal or Enhanced Open (OWE)")
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
	cfg["opmode"] = req.OpMode
	cfg["operating-frequency"] = freq
	cfg["station-isolation"] = req.Isolation
	cfg["mfp"] = req.OpMode == "WPA3_SAE" || req.OpMode == "ENHANCED_OPEN"
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
	switch req.OpMode {
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

func (a *API) createSSID(w http.ResponseWriter, r *http.Request) {
	var req ssidRequest
	if !decode(w, r, &req) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if _, exists := a.poller.SSIDConfig(req.Name); exists {
		fail(w, http.StatusConflict, "an SSID with this name already exists")
		return
	}
	if req.Password == "" && (req.OpMode == "WPA3_SAE" || req.OpMode == "WPA2_PERSONAL") {
		fail(w, http.StatusBadRequest, "a password is required")
		return
	}
	cfg, _, err := ssidConfig(req, nil)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	a.apply(w, r, "create SSID "+req.Name, ssidBody(cfg), nil)
}

func (a *API) updateSSID(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req ssidRequest
	if !decode(w, r, &req) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	current, ok := a.poller.SSIDConfig(name)
	if !ok {
		fail(w, http.StatusNotFound, "SSID not found")
		return
	}
	renamed := req.Name != name
	if renamed {
		if _, exists := a.poller.SSIDConfig(req.Name); exists {
			fail(w, http.StatusConflict, "an SSID with this name already exists")
			return
		}
	}
	cfg, leaves, err := ssidConfig(req, current)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	var deletes []*gpb.Path
	if renamed {
		// The name is the list key: replace the entry.
		deletes = append(deletes, a.gnmi.apPath(elem("ssids"), elem("ssid", "name", name)))
	} else {
		for _, leaf := range leaves {
			deletes = append(deletes, a.ssidLeaf(name, leaf))
		}
	}
	a.apply(w, r, "update SSID "+name, ssidBody(cfg), deletes)
}

func (a *API) deleteSSID(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if _, ok := a.poller.SSIDConfig(name); !ok {
		fail(w, http.StatusNotFound, "SSID not found")
		return
	}
	a.apply(w, r, "delete SSID "+name, map[string]any{}, []*gpb.Path{a.gnmi.apPath(elem("ssids"), elem("ssid", "name", name))})
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
	cfg, freq, ok := a.poller.RadioConfig(id)
	if !ok {
		fail(w, http.StatusNotFound, "radio not found")
		return
	}
	b := band(freq)
	var radio *Radio
	snapshot := a.poller.Snapshot()
	for i := range snapshot.Radios {
		if snapshot.Radios[i].ID == id {
			radio = &snapshot.Radios[i]
		}
	}
	if radio != nil && len(radio.AllowedChannels) > 0 && !slices.Contains(radio.AllowedChannels, req.Channel) {
		fail(w, http.StatusBadRequest, fmt.Sprintf("channel %d is not allowed on this radio", req.Channel))
		return
	}
	if !slices.Contains(widths[b], req.Width) {
		fail(w, http.StatusBadRequest, fmt.Sprintf("%d MHz is not a valid width for %s GHz", req.Width, b))
		return
	}
	if req.Power < 1 || req.Power > 30 {
		fail(w, http.StatusBadRequest, "transmit power must be 1–30 dBm")
		return
	}
	cfg["id"] = id
	cfg["operating-frequency"] = freq
	cfg["enabled"] = req.Enabled
	cfg["channel"] = req.Channel
	cfg["channel-width"] = req.Width
	cfg["transmit-power"] = req.Power
	cfg["dca"] = req.DCA
	cfg["dtp"] = req.DTP
	body := map[string]any{"radios": map[string]any{"radio": []any{map[string]any{"id": id, "operating-frequency": freq, "config": cfg}}}}
	a.apply(w, r, fmt.Sprintf("update radio %d", id), body, nil)
}
