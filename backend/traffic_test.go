package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func trafficInt(n int) *int { return &n }
func trafficFixture(t *testing.T) (*TrafficPolicies, map[string]TrafficPolicy) {
	t.Helper()
	p := NewTrafficPolicies(filepath.Join(t.TempDir(), "traffic.json"))
	p.supported = func() bool { return true }
	operating := map[string]TrafficPolicy{}
	p.apply = func(_ context.Context, n string, v TrafficPolicy, _ APState) error { operating[n] = v; return nil }
	p.inspect = func(_ context.Context, n string, v TrafficPolicy, _ APState) ([]TrafficQueue, bool, error) {
		if !trafficEqual(v, operating[n]) {
			return nil, false, errors.New("kernel mismatch")
		}
		return []TrafficQueue{}, false, nil
	}
	return p, operating
}
func trafficTestPolicy() TrafficPolicy {
	p := defaultTrafficPolicy()
	p.Bandwidth.DownloadKbps = trafficInt(4000)
	p.Clients["02:00:00:46:00:01"] = TrafficLimits{UploadKbps: trafficInt(1000)}
	return p
}
func TestTrafficValidationAndOwnership(t *testing.T) {
	p := trafficTestPolicy()
	p.Clients = map[string]TrafficLimits{"02-00-00-46-00-01": {UploadKbps: trafficInt(32)}}
	p.QoS = &TrafficQoS{Priority: "voice", Mode: "fixed", Mapping: "dscp", MarkDSCP: true}
	n, e := normalizeTrafficPolicy(p)
	if e != nil {
		t.Fatal(e)
	}
	*p.Bandwidth.DownloadKbps = 7
	*p.Clients["02-00-00-46-00-01"].UploadKbps = 3
	p.QoS.Mode = "invalid"
	if *n.Bandwidth.DownloadKbps != 4000 || *n.Clients["02:00:00:46:00:01"].UploadKbps != 32 || n.QoS.Mode != "fixed" {
		t.Fatal("normalization retained caller-owned values")
	}
	for _, rate := range []int{-1, 0, 31, 1000001} {
		p := trafficTestPolicy()
		p.Bandwidth.UploadKbps = &rate
		if _, e := normalizeTrafficPolicy(p); e == nil {
			t.Fatal("invalid rate accepted", rate)
		}
	}
	for _, mac := range []string{"garbage", "01:00:00:00:00:01", "00:00:00:00:00:00", "02:00:00:00:00:00:00:01"} {
		if _, e := trafficMAC(mac); e == nil {
			t.Fatal("invalid MAC", mac)
		}
	}
	p = trafficTestPolicy()
	p.Clients["02-00-00-46-00-01"] = TrafficLimits{}
	if _, e := normalizeTrafficPolicy(p); e == nil {
		t.Fatal("duplicate accepted")
	}
	for _, q := range []TrafficQoS{{Priority: "control", Mode: "fixed", Mapping: "dscp"}, {Priority: "voice", Mode: "invalid", Mapping: "dscp"}, {Priority: "voice", Mode: "ceiling", Mapping: "invalid"}} {
		p.QoS = &q
		if _, e := normalizeTrafficPolicy(p); e == nil {
			t.Fatal("invalid QoS accepted")
		}
	}
}
func TestTrafficClientDefaultsOverrideBothDirections(t *testing.T) {
	p := defaultTrafficPolicy()
	p.PerClient = TrafficLimits{UploadKbps: trafficInt(1000), DownloadKbps: trafficInt(4000)}
	p.Clients["02:00:00:00:00:01"] = TrafficLimits{}
	p.Clients["02:00:00:00:00:02"] = TrafficLimits{DownloadKbps: trafficInt(2000)}
	p.Clients["02:00:00:00:00:04"] = TrafficLimits{UploadKbps: trafficInt(500)}
	st := APState{Clients: []Client{{SSID: "Lab", MAC: "02:00:00:00:00:01"}, {SSID: "Lab", MAC: "02:00:00:00:00:02"}, {SSID: "Lab", MAC: "02:00:00:00:00:03"}, {SSID: "Other", MAC: "02:00:00:00:00:05"}}}
	c, e := trafficEffectiveClients("Lab", p, st)
	if e != nil || len(c) != 3 || c["02:00:00:00:00:02"].UploadKbps != nil || *c["02:00:00:00:00:03"].UploadKbps != 1000 {
		t.Fatal(c, e)
	}
	if _, ok := c["02:00:00:00:00:01"]; ok {
		t.Fatal("unlimited override inherited default")
	}
	if _, ok := c["02:00:00:00:00:05"]; ok {
		t.Fatal("cross-SSID shaping")
	}
}
func TestTrafficMACFilterReadbackPreservesEveryMatch(t *testing.T) {
	up := []byte(`{"match":{"value":"02000046","mask":"ffffffff","off":-8},"match":{"value":"00010000","mask":"ffff0000","off":-4}}`)
	down := []byte(`{"match":{"value":"00000200","mask":"0000ffff","off":-16},"match":{"value":"00460001","mask":"ffffffff","off":-12}}`)
	if !trafficMACMatches(up, "02:00:00:46:00:01", "upload") || !trafficMACMatches(down, "02:00:00:46:00:01", "download") {
		t.Fatal("native duplicate match keys rejected")
	}
	for _, bad := range [][]byte{[]byte(strings.Replace(string(up), "02000046", "03000046", 1)), []byte(strings.Replace(string(up), "ffffffff", "0000ffff", 1)), []byte(`{"match":{"value":"00010000","mask":"ffff0000","off":-4}}`), down} {
		if trafficMACMatches(bad, "02:00:00:46:00:01", "upload") {
			t.Fatal("incomplete or wrong filter claimed enforced")
		}
	}
}
func TestTrafficPersistenceRollbackAndReconciliation(t *testing.T) {
	for _, phase := range []string{"apply", "verify", "persist"} {
		t.Run(phase, func(t *testing.T) {
			p, operating := trafficFixture(t)
			old := defaultTrafficPolicy()
			old.Bandwidth.DownloadKbps = trafficInt(8000)
			if e := p.UpdateMany(context.Background(), map[string]TrafficPolicy{"Lab": old}, APState{}); e != nil {
				t.Fatal(e)
			}
			apply, inspect := p.apply, p.inspect
			switch phase {
			case "apply":
				p.apply = func(ctx context.Context, n string, v TrafficPolicy, st APState) error {
					apply(ctx, n, v, st)
					if trafficEqual(v, trafficTestPolicy()) {
						return errors.New("partial native failure")
					}
					return nil
				}
			case "verify":
				p.inspect = func(ctx context.Context, n string, v TrafficPolicy, st APState) ([]TrafficQueue, bool, error) {
					if trafficEqual(v, trafficTestPolicy()) {
						return nil, false, errors.New("IPv6 filter missing")
					}
					return inspect(ctx, n, v, st)
				}
			case "persist":
				p.save = func(string, []byte, os.FileMode) error { return errors.New("flash full") }
			}
			e := p.UpdateMany(context.Background(), map[string]TrafficPolicy{"Lab": trafficTestPolicy()}, APState{})
			if e == nil || !strings.Contains(e.Error(), "restored") || !trafficEqual(p.Saved()["Lab"], old) || !trafficEqual(operating["Lab"], old) {
				t.Fatal("rollback failed", e)
			}
		})
	}
	p, operating := trafficFixture(t)
	if e := p.UpdateMany(context.Background(), map[string]TrafficPolicy{"Lab": trafficTestPolicy()}, APState{}); e != nil {
		t.Fatal(e)
	}
	reloaded := NewTrafficPolicies(p.path)
	if !trafficEqual(reloaded.Saved(), p.Saved()) {
		t.Fatal("not persisted")
	}
	info, _ := os.Stat(p.path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions", info.Mode())
	}
	copy := p.Saved()
	*copy["Lab"].Bandwidth.DownloadKbps = 35
	if *p.Saved()["Lab"].Bandwidth.DownloadKbps != 4000 {
		t.Fatal("Saved exposed mutable state")
	}
	operating["Lab"] = defaultTrafficPolicy()
	if e := p.Reconcile(context.Background(), APState{}); e != nil || !trafficEqual(operating["Lab"], p.Saved()["Lab"]) {
		t.Fatal("driver reset not repaired", e)
	}
	if e := p.BeforeChanges(context.Background(), []ssidChange{{oldName: "Lab", newName: "Renamed"}}, APState{}); e != nil || !trafficEqual(operating["Lab"], defaultTrafficPolicy()) {
		t.Fatal("old queue not cleared", e)
	}
	if e := p.Rename([]ssidChange{{oldName: "Lab", newName: "Renamed"}}); e != nil || len(p.Saved()) != 1 || !trafficEqual(p.Saved()["Renamed"], trafficTestPolicy()) {
		t.Fatal("rename", e)
	}
	if e := p.Rename([]ssidChange{{oldName: "Renamed", newName: ""}}); e != nil || len(p.Saved()) != 0 {
		t.Fatal("delete", e)
	}
}
func TestTrafficPendingAndCorruptSettings(t *testing.T) {
	p, _ := trafficFixture(t)
	p.inspect = func(context.Context, string, TrafficPolicy, APState) ([]TrafficQueue, bool, error) {
		return []TrafficQueue{}, true, nil
	}
	if e := p.UpdateMany(context.Background(), map[string]TrafficPolicy{"Lab": trafficTestPolicy()}, APState{}); e != nil {
		t.Fatal(e)
	}
	s := p.Status(context.Background(), "Lab", APState{})
	if s.Applied || !s.Pending || !s.Managed {
		t.Fatal("pending claimed applied", s)
	}
	if e := os.WriteFile(p.path, []byte("broken"), 0600); e != nil {
		t.Fatal(e)
	}
	bad := NewTrafficPolicies(p.path)
	if e := bad.UpdateMany(context.Background(), map[string]TrafficPolicy{"Lab": trafficTestPolicy()}, APState{}); e == nil {
		t.Fatal("corrupt settings overwritten")
	}
}
func TestTrafficAPIRequiresCompleteSettingsAndPermissions(t *testing.T) {
	a := &API{}
	for _, body := range []string{`{}`, `{"bandwidth":{},"perClient":{},"qos":null,"clients":{}}`, `{"bandwidth":{"uploadKbps":null,"downloadKbps":31},"perClient":{"uploadKbps":null,"downloadKbps":null},"qos":null,"clients":{}}`} {
		w := httptest.NewRecorder()
		a.updateTraffic(w, httptest.NewRequest("PUT", "/api/ssids/Lab/traffic", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal("invalid input reached hardware", w.Code)
		}
	}
	a = &API{auth: NewAuth(filepath.Join(t.TempDir(), "auth.json")), changes: NewChangeLog("")}
	mux := http.NewServeMux()
	a.Register(mux)
	for _, path := range []string{"/api/ssids/Lab/traffic", "/api/v1/ssids/Lab/traffic"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
		session := httptest.NewRecorder()
		a.auth.NewSession(session, "crew", RoleViewer)
		r := httptest.NewRequest("PUT", path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(session.Result().Cookies()[0])
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("viewer write", w.Code)
		}
	}
	if !trafficEqual(tokenPermissions("PUT /api/ssids/{name}/traffic"), []string{"configure"}) || !trafficEqual(tokenPermissions("GET /api/ssids/{name}/traffic"), []string{"monitor"}) {
		t.Fatal("wrong traffic scopes")
	}
	schema := requestSchema("PUT /api/ssids/{name}/traffic")
	raw, _ := json.Marshal(schema)
	if !strings.Contains(string(raw), `"maximum":1000000`) || !strings.Contains(string(raw), `"maxProperties":128`) {
		t.Fatal("limits absent from OpenAPI")
	}
	if trafficQoSFlags(TrafficQoS{Priority: "voice", Mode: "fixed", Mapping: "dscp", MarkDSCP: true}) != 79 || trafficQoSFlags(TrafficQoS{Priority: "voice", Mode: "ceiling", Mapping: "dscp"}) != 11 {
		t.Fatal("wrong driver QoS flags")
	}
}
func TestTrafficBackupValidationBeforeAnyHardwareChange(t *testing.T) {
	p, _ := trafficFixture(t)
	r := restoreRequest{Backup: Backup{Format: backupFormat, Version: 1, TrafficPolicies: map[string]TrafficPolicy{"Unknown": trafficTestPolicy()}}}
	r.Sections.Wireless = true
	if _, e := (&API{}).planRestore(r); e == nil {
		t.Fatal("unsupported restore accepted")
	}
	if _, e := (&API{traffic: p}).planRestore(r); e == nil || !strings.Contains(e.Error(), "missing network") {
		t.Fatal("unbound policy accepted", e)
	}
}

func TestTrafficVirtualTBFClassAndUnexpectedClientCaps(t *testing.T) {
	rows := []nativeTrafficClass{{Class: "tbf", Handle: "1:1", Parent: "1:", Leaf: "0x2"}, {Class: "htb", Handle: "2:10"}}
	expected := map[string]bool{"2:10": true}
	if !trafficClassesExpected(rows, expected, true) {
		t.Fatal("native virtual TBF class rejected")
	}
	if trafficClassesExpected(rows, expected, false) {
		t.Fatal("unexpected aggregate accepted")
	}
	rows = append(rows, nativeTrafficClass{Class: "htb", Handle: "2:1"})
	if trafficClassesExpected(rows, expected, true) {
		t.Fatal("unrequested default client cap accepted")
	}
}
