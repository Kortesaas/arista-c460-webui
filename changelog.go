package main

// Change log: who changed what and when, kept on the AP with a fixed maximum
// number of entries so it can never grow without limit.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const changeLogLimit = 300

type ChangeEntry struct {
	Time    time.Time `json:"time"`
	User    string    `json:"user"`
	Address string    `json:"address"`
	Action  string    `json:"action"`
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
}

type ChangeLog struct {
	mu      sync.Mutex
	path    string // "" keeps the log in memory only
	entries []ChangeEntry
}

func NewChangeLog(path string) *ChangeLog {
	l := &ChangeLog{path: path}
	if path == "" {
		return l
	}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &l.entries); err != nil {
			log.Printf("change log %s unreadable, starting empty: %v", path, err)
			l.entries = nil
		}
	}
	if len(l.entries) > changeLogLimit {
		l.entries = l.entries[len(l.entries)-changeLogLimit:]
	}
	return l
}

func (l *ChangeLog) Record(e ChangeEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	if len(l.entries) > changeLogLimit {
		l.entries = append([]ChangeEntry(nil), l.entries[len(l.entries)-changeLogLimit:]...)
	}
	if l.path == "" {
		return
	}
	raw, err := json.Marshal(l.entries)
	if err == nil {
		err = atomicNative(l.path, raw, 0o600)
	}
	if err != nil {
		log.Printf("change log: %v", err)
	}
}

// Entries returns the newest entries first.
func (l *ChangeLog) Entries() []ChangeEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]ChangeEntry, len(l.entries))
	for i, e := range l.entries {
		out[len(out)-1-i] = e
	}
	return out
}

// recorder captures the status and, for failures, the error message.
type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.status >= 400 && r.body.Len() < 2048 {
		r.body.Write(b)
	}
	return r.ResponseWriter.Write(b)
}

// requestFields peeks at a JSON request body for the fields used in log
// descriptions and puts the body back for the handler.
func requestFields(r *http.Request) map[string]any {
	if r.Body == nil || r.Method == http.MethodGet {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	return fields
}

func quoted(v any) string {
	if s, ok := v.(string); ok && s != "" {
		return "“" + s + "”"
	}
	return ""
}

// describeChange turns a request into a sentence for the change log.
func (a *API) describeChange(r *http.Request, f map[string]any) string {
	name := quoted(r.PathValue("name"))
	switch r.Pattern {
	case "POST /api/password":
		return "Changed the administrator login"
	case "PUT /api/viewer":
		return "Set the read-only account " + quoted(f["username"])
	case "DELETE /api/viewer":
		return "Removed the read-only account"
	case "PUT /api/refresh":
		return fmt.Sprintf("Set live updates to every %v s", f["seconds"])
	case "POST /api/ssids":
		return "Created network " + quoted(f["name"])
	case "PUT /api/ssids/{name}":
		prefix := "Changed network " + name
		if newName := quoted(f["name"]); newName != "" && newName != name {
			prefix = "Renamed network " + name + " to " + newName
		}
		return withDetails(prefix, a.ssidDifferences(r.PathValue("name"), f))
	case "DELETE /api/ssids/{name}":
		return "Deleted network " + name
	case "PUT /api/ssids/{name}/features":
		return "Changed advanced settings of " + name
	case "PUT /api/ssids/{name}/schedule":
		return "Changed the schedule of " + name
	case "GET /api/ssids/{name}/join":
		return "Showed the join code of " + name
	case "PUT /api/radios/{id}":
		id, _ := strconv.Atoi(r.PathValue("id"))
		for _, radio := range a.poller.Snapshot().Radios {
			if radio.ID == id {
				return withDetails("Changed the "+radio.Band+" GHz radio", radioDifferences(radio, f))
			}
		}
		return "Changed radio " + r.PathValue("id")
	case "PUT /api/radios/{id}/wifi7":
		if enabled, _ := f["enabled"].(bool); enabled {
			return fmt.Sprintf("Enabled 6 GHz Wi-Fi 7 at %v MHz", f["width"])
		}
		return "Returned the 6 GHz radio to Wi-Fi 6E"
	case "POST /api/batch":
		if changes, ok := f["changes"].([]any); ok {
			return fmt.Sprintf("Applied %d staged changes", len(changes))
		}
		return "Applied staged changes"
	case "PUT /api/management":
		return "Changed the management network"
	case "PUT /api/lldp":
		return "Changed LLDP timing"
	case "PUT /api/time":
		return "Changed time servers"
	case "PUT /api/timezone":
		return "Set the time zone to " + quoted(f["timeZone"])
	case "PUT /api/snmp":
		return "Changed SNMP settings"
	case "PUT /api/metrics":
		return "Changed Prometheus metrics settings"
	case "PUT /api/settings":
		return "Changed display names"
	case "PUT /api/ssh":
		if on, ok := f["enabled"].(bool); ok && !on {
			return "Disabled SSH"
		}
		return "Enabled SSH"
	case "POST /api/reboot":
		return "Restarted the access point"
	case "POST /api/locate":
		return "Started the locate LED"
	case "DELETE /api/locate":
		return "Stopped the locate LED"
	case "POST /api/backup":
		return "Downloaded a backup"
	case "POST /api/restore":
		if b, ok := f["backup"].(map[string]any); ok {
			if src, ok := b["source"].(map[string]any); ok {
				return "Restored a backup from " + quoted(src["hostname"])
			}
		}
		return "Restored a backup"
	case "POST /api/clients/{mac}/reconnect":
		return "Reconnected client " + r.PathValue("mac")
	}
	return r.Method + " " + r.URL.Path
}

var securityNames = map[string]string{"WPA3_SAE": "WPA3", opModeMixed: "WPA2/WPA3", "WPA2_PERSONAL": "WPA2", "ENHANCED_OPEN": "Enhanced Open", "OPEN": "open"}

func withDetails(action string, details []string) string {
	if len(details) == 0 {
		return action
	}
	return action + ": " + strings.Join(details, ", ")
}

func onOff(b bool) string { return map[bool]string{true: "on", false: "off"}[b] }

func vlanText(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.Itoa(int(x))
	case *int:
		if x != nil {
			return strconv.Itoa(*x)
		}
	}
	return "untagged"
}

// ssidDifferences lists what an SSID update changes, never including the password itself.
func (a *API) ssidDifferences(name string, f map[string]any) []string {
	var cur *SSID
	for _, s := range a.snapshot().SSIDs {
		if s.Name == name {
			s := s
			cur = &s
		}
	}
	if cur == nil || f == nil {
		return nil
	}
	var out []string
	if v, ok := f["opmode"].(string); ok && v != cur.OpMode {
		out = append(out, "security "+securityNames[cur.OpMode]+" → "+securityNames[v])
	}
	if v, ok := f["bands"].([]any); ok {
		var bands []string
		for _, b := range v {
			bands = append(bands, fmt.Sprint(b))
		}
		if strings.Join(bands, "/") != strings.Join(cur.Bands, "/") {
			out = append(out, "bands "+strings.Join(cur.Bands, "/")+" → "+strings.Join(bands, "/")+" GHz")
		}
	}
	if vlan, present := f["vlan"]; present && vlanText(vlan) != vlanText(cur.VLAN) {
		out = append(out, "VLAN "+vlanText(cur.VLAN)+" → "+vlanText(vlan))
	}
	for _, flag := range []struct {
		key, label string
		current    bool
	}{{"enabled", "broadcast", cur.Enabled}, {"hidden", "hidden", cur.Hidden}, {"isolation", "client isolation", cur.Isolation}} {
		if v, ok := f[flag.key].(bool); ok && v != flag.current {
			out = append(out, flag.label+" "+onOff(v))
		}
	}
	if p, ok := f["password"].(string); ok && p != "" {
		out = append(out, "new password")
	}
	return out
}

func radioDifferences(r Radio, f map[string]any) []string {
	var out []string
	num := func(key string) (int, bool) {
		v, ok := f[key].(float64)
		return int(v), ok
	}
	if v, ok := num("channel"); ok && v != r.Channel {
		out = append(out, fmt.Sprintf("channel %d → %d", r.Channel, v))
	}
	if v, ok := num("width"); ok && v != r.Width {
		out = append(out, fmt.Sprintf("width %d → %d MHz", r.Width, v))
	}
	if v, ok := num("power"); ok && v != r.PowerRequested {
		out = append(out, fmt.Sprintf("power %d → %d dBm", r.PowerRequested, v))
	}
	for _, flag := range []struct {
		key, label string
		current    bool
	}{{"enabled", "radio", r.Enabled}, {"dca", "automatic channel", r.DCA}, {"dtp", "automatic power", r.DTP}} {
		if v, ok := f[flag.key].(bool); ok && v != flag.current {
			out = append(out, flag.label+" "+onOff(v))
		}
	}
	return out
}

func (a *API) changeLog(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, map[string]any{"entries": a.changes.Entries(), "limit": changeLogLimit})
}
