package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const minTrafficKbps, maxTrafficKbps, maxTrafficOverrides = 32, 1000000, 128

// Rates are decimal kilobits per second, from the wireless client's perspective.
// Null is unlimited. An explicit client override replaces both default directions.
type TrafficLimits struct {
	UploadKbps   *int `json:"uploadKbps"`
	DownloadKbps *int `json:"downloadKbps"`
}
type TrafficQoS struct {
	Priority  string `json:"priority"`
	Mode      string `json:"mode"`
	Mapping   string `json:"mapping"`
	MarkDSCP  bool   `json:"markDSCP"`
	Mark8021p bool   `json:"mark8021p"`
}
type TrafficPolicy struct {
	Bandwidth TrafficLimits            `json:"bandwidth"`
	PerClient TrafficLimits            `json:"perClient"`
	QoS       *TrafficQoS              `json:"qos"` // null preserves native firmware QoS settings
	Clients   map[string]TrafficLimits `json:"clients"`
}

func defaultTrafficPolicy() TrafficPolicy { return TrafficPolicy{Clients: map[string]TrafficLimits{}} }

func normalizeTrafficLimits(l TrafficLimits) (TrafficLimits, error) {
	for _, p := range []**int{&l.UploadKbps, &l.DownloadKbps} {
		if *p == nil {
			continue
		}
		n := **p
		if n < minTrafficKbps || n > maxTrafficKbps {
			return l, fmt.Errorf("Use %d–%d Kbps, or null for unlimited", minTrafficKbps, maxTrafficKbps)
		}
		*p = &n
	}
	return l, nil
}
func trafficMAC(s string) (string, error) {
	m, e := net.ParseMAC(strings.TrimSpace(s))
	if e != nil || len(m) != 6 || m[0]&1 != 0 || bytes.Equal(m, make([]byte, 6)) {
		return "", errors.New("Use a valid unicast Wi-Fi MAC address")
	}
	return strings.ToLower(m.String()), nil
}
func normalizeTrafficPolicy(p TrafficPolicy) (TrafficPolicy, error) {
	var e error
	if p.Bandwidth, e = normalizeTrafficLimits(p.Bandwidth); e != nil {
		return p, e
	}
	if p.PerClient, e = normalizeTrafficLimits(p.PerClient); e != nil {
		return p, e
	}
	if p.QoS != nil {
		q := *p.QoS
		p.QoS = &q
		if !slices.Contains([]string{"voice", "video", "best-effort", "background"}, q.Priority) || !slices.Contains([]string{"ceiling", "fixed"}, q.Mode) || !slices.Contains([]string{"dscp", "8021p", "tos"}, q.Mapping) {
			return p, errors.New("Invalid QoS priority, mode or mapping")
		}
	}
	if len(p.Clients) > maxTrafficOverrides {
		return p, fmt.Errorf("Use at most %d device overrides per network", maxTrafficOverrides)
	}
	clients := map[string]TrafficLimits{}
	for s, l := range p.Clients {
		mac, e := trafficMAC(s)
		if e != nil {
			return p, e
		}
		if _, exists := clients[mac]; exists {
			return p, errors.New("Repeated Wi-Fi MAC address")
		}
		if clients[mac], e = normalizeTrafficLimits(l); e != nil {
			return p, e
		}
	}
	p.Clients = clients
	return p, nil
}
func trafficLimited(l TrafficLimits) bool { return l.UploadKbps != nil || l.DownloadKbps != nil }
func trafficRate(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
func trafficEqual(a, b any) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(aa, bb)
}

type TrafficQueue struct {
	Interface  string `json:"interface"`
	Direction  string `json:"direction"`
	MAC        string `json:"mac,omitempty"`
	LimitKbps  *int   `json:"limitKbps"`
	Bytes      uint64 `json:"bytes"`
	Packets    uint64 `json:"packets"`
	Drops      uint64 `json:"drops"`
	Overlimits uint64 `json:"overlimits"`
}
type TrafficStatus struct {
	Settings  TrafficPolicy  `json:"settings"`
	Supported bool           `json:"supported"`
	Managed   bool           `json:"managed"`
	Applied   bool           `json:"applied"`
	Pending   bool           `json:"pending"`
	QoSStatus string         `json:"qosStatus"`
	Queues    []TrafficQueue `json:"queues"`
	Error     string         `json:"error,omitempty"`
}
type TrafficPolicies struct {
	mu        sync.RWMutex
	path      string
	desired   map[string]TrafficPolicy
	loadErr   error
	supported func() bool
	save      func(string, []byte, os.FileMode) error
	apply     func(context.Context, string, TrafficPolicy, APState) error
	inspect   func(context.Context, string, TrafficPolicy, APState) ([]TrafficQueue, bool, error)
}

func NewTrafficPolicies(path string) *TrafficPolicies {
	t := &TrafficPolicies{path: path, desired: map[string]TrafficPolicy{}, supported: nativeTrafficAvailable, save: atomicNative, apply: applyNativeTraffic, inspect: inspectNativeTraffic}
	raw, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return t
	}
	if e == nil {
		e = json.Unmarshal(raw, &t.desired)
		if e == nil && t.desired == nil {
			e = errors.New("Traffic settings must be a JSON object")
		}
	}
	if e == nil {
		for name, p := range t.desired {
			t.desired[name], e = normalizeTrafficPolicy(p)
			if e != nil {
				break
			}
		}
	}
	t.loadErr = e
	return t
}
func (t *TrafficPolicies) Saved() map[string]TrafficPolicy {
	t.mu.RLock()
	defer t.mu.RUnlock()
	r := map[string]TrafficPolicy{}
	for name, p := range t.desired {
		r[name], _ = normalizeTrafficPolicy(p)
	}
	return r
}
func (t *TrafficPolicies) persist(p map[string]TrafficPolicy) error {
	raw, e := json.Marshal(p)
	if e == nil {
		e = t.save(t.path, raw, 0600)
	}
	if e != nil {
		return e
	}
	t.mu.Lock()
	t.desired = p
	t.mu.Unlock()
	return nil
}
func (t *TrafficPolicies) Status(ctx context.Context, name string, st APState) TrafficStatus {
	r := TrafficStatus{Settings: defaultTrafficPolicy(), Supported: t.supported(), Queues: []TrafficQueue{}, QoSStatus: "firmware-default"}
	if t.loadErr != nil {
		r.Error = "Saved traffic settings are unreadable"
		return r
	}
	if p, ok := t.Saved()[name]; ok {
		r.Settings = p
		r.Managed = true
	} else {
		return r
	}
	if !r.Supported {
		r.Error = "Traffic controls require tested C-460 firmware 18.2.0-32"
		return r
	}
	var e error
	r.Queues, r.Pending, e = t.inspect(ctx, name, r.Settings, st)
	r.Applied = e == nil && !r.Pending
	if e != nil {
		r.Error = e.Error()
	}
	if r.Settings.QoS != nil {
		r.QoSStatus = "configured-no-driver-readback"
		if r.Pending {
			r.QoSStatus = "pending"
		} else if e != nil {
			r.QoSStatus = "error"
		}
	}
	return r
}
func (t *TrafficPolicies) UpdateMany(ctx context.Context, updates map[string]TrafficPolicy, st APState) error {
	if len(updates) == 0 {
		return nil
	}
	if t.loadErr != nil {
		return errors.New("Saved traffic settings are unreadable")
	}
	if !t.supported() {
		return errors.New("Traffic controls are unavailable on this firmware")
	}
	before := t.Saved()
	next := t.Saved()
	for name, p := range updates {
		v, e := normalizeTrafficPolicy(p)
		if e != nil {
			return e
		}
		next[name] = v
	}
	names := make([]string, 0, len(updates))
	for name := range updates {
		names = append(names, name)
	}
	slices.Sort(names)
	changed := []string{}
	rollback := func(cause error) error {
		restore, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var problems []string
		for i := len(changed) - 1; i >= 0; i-- {
			name := changed[i]
			p, ok := before[name]
			if !ok {
				p = defaultTrafficPolicy()
			}
			if e := t.apply(restore, name, p, st); e != nil {
				problems = append(problems, e.Error())
				continue
			}
			if _, _, e := t.inspect(restore, name, p, st); e != nil {
				problems = append(problems, e.Error())
			}
		}
		if len(problems) > 0 {
			return fmt.Errorf("%w; traffic rollback failed: %s", cause, strings.Join(problems, "; "))
		}
		return fmt.Errorf("%w; previous traffic settings restored", cause)
	}
	for _, name := range names {
		changed = append(changed, name)
		if e := t.apply(ctx, name, next[name], st); e != nil {
			return rollback(e)
		}
		if _, _, e := t.inspect(ctx, name, next[name], st); e != nil {
			return rollback(e)
		}
	}
	if e := t.persist(next); e != nil {
		return rollback(e)
	}
	return nil
}
func (t *TrafficPolicies) Reconcile(ctx context.Context, st APState) error {
	if t.loadErr != nil {
		return errors.New("Saved traffic settings are unreadable")
	}
	for name, p := range t.Saved() {
		_, _, e := t.inspect(ctx, name, p, st)
		if e != nil {
			if e = t.apply(ctx, name, p, st); e == nil {
				_, _, e = t.inspect(ctx, name, p, st)
			}
			if e != nil {
				return fmt.Errorf("Restore traffic settings for %s: %w", name, e)
			}
		}
	}
	return nil
}

// Clear the old profile while its runtime mapping still exists, before rename/delete.
// Desired settings remain intact so a failed wireless transaction can restore them.
func (t *TrafficPolicies) BeforeChanges(ctx context.Context, changes []ssidChange, st APState) error {
	for _, c := range changes {
		if c.oldName == "" || c.oldName == c.newName {
			continue
		}
		if _, ok := t.Saved()[c.oldName]; !ok {
			continue
		}
		p := defaultTrafficPolicy()
		if e := t.apply(ctx, c.oldName, p, st); e != nil {
			return e
		}
		if _, _, e := t.inspect(ctx, c.oldName, p, st); e != nil {
			return e
		}
	}
	return nil
}
func invalidateTrafficQoS() { trafficQoSMu.Lock(); clear(trafficQoSApplied); trafficQoSMu.Unlock() }
func (t *TrafficPolicies) Rename(changes []ssidChange) error {
	if t.loadErr != nil {
		return errors.New("Saved traffic settings are unreadable")
	}
	next := t.Saved()
	changed := false
	for _, c := range changes {
		if c.oldName == "" || c.oldName == c.newName {
			continue
		}
		if p, ok := next[c.oldName]; ok {
			delete(next, c.oldName)
			if c.newName != "" {
				next[c.newName] = p
			}
			changed = true
		}
	}
	if changed {
		return t.persist(next)
	}
	return nil
}
func trafficSettingsPath(config string) string {
	return filepath.Join(filepath.Dir(config), "traffic-policies.json")
}
func (a *API) getTraffic(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := a.poller.SSIDConfig(name); !ok {
		fail(w, 404, "Network not found")
		return
	}
	if a.traffic == nil {
		reply(w, 200, TrafficStatus{Settings: defaultTrafficPolicy(), Queues: []TrafficQueue{}})
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	reply(w, 200, a.traffic.Status(ctx, name, a.poller.Snapshot()))
}
func (a *API) updateTraffic(w http.ResponseWriter, r *http.Request) {
	if e := validateCompleteSettings("PUT /api/ssids/{name}/traffic", requestFields(r)); e != nil {
		fail(w, 400, e.Error())
		return
	}
	var p TrafficPolicy
	if !decode(w, r, &p) {
		return
	}
	p, e := normalizeTrafficPolicy(p)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	name := r.PathValue("name")
	if _, ok := a.poller.SSIDConfig(name); !ok {
		fail(w, 404, "Network not found")
		return
	}
	if a.traffic == nil {
		fail(w, 503, "Traffic controls are unavailable")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()
	if e = a.traffic.UpdateMany(ctx, map[string]TrafficPolicy{name: p}, a.poller.Snapshot()); e != nil {
		fail(w, 502, e.Error())
		return
	}
	reply(w, 200, a.traffic.Status(ctx, name, a.poller.Snapshot()))
}
func (a *API) updateClientTraffic(w http.ResponseWriter, r *http.Request) {
	mac, e := trafficMAC(r.PathValue("mac"))
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	var limits TrafficLimits
	if r.Method != http.MethodDelete {
		if e := validateCompleteSettings("PUT /api/ssids/{name}/traffic/clients/{mac}", requestFields(r)); e != nil {
			fail(w, 400, e.Error())
			return
		}
		if !decode(w, r, &limits) {
			return
		}
		if limits, e = normalizeTrafficLimits(limits); e != nil {
			fail(w, 400, e.Error())
			return
		}
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	name := r.PathValue("name")
	if _, ok := a.poller.SSIDConfig(name); !ok {
		fail(w, 404, "Network not found")
		return
	}
	if a.traffic == nil {
		fail(w, 503, "Traffic controls are unavailable")
		return
	}
	p, ok := a.traffic.Saved()[name]
	if !ok {
		p = defaultTrafficPolicy()
	}
	if r.Method == http.MethodDelete {
		delete(p.Clients, mac)
	} else {
		p.Clients[mac] = limits
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
	if e = a.traffic.UpdateMany(ctx, map[string]TrafficPolicy{name: p}, a.poller.Snapshot()); e != nil {
		fail(w, 502, e.Error())
		return
	}
	reply(w, 200, a.traffic.Status(ctx, name, a.poller.Snapshot()))
}
func (a *API) ensureTraffic() error {
	if a.traffic == nil || len(a.traffic.Saved()) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return a.traffic.Reconcile(ctx, a.poller.Snapshot())
}
func (a *API) runTraffic(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			st := a.poller.Snapshot()
			if st.GeneratedAt.IsZero() || st.Error != "" {
				continue
			}
			if a.writeMu.TryLock() {
				if e := a.ensureTraffic(); e != nil {
					log.Printf("traffic settings: %v", e)
				}
				a.writeMu.Unlock()
			}
		}
	}
}
