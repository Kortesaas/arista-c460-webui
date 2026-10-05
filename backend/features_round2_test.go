package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleAPConf = `[ GLOBAL_START ]
X=1
[ GLOBAL_END ]
[ VAP_START=1 ]
AP_SEC_MODE=6
AP_SSID=Office
IEEE80211W_ENABLE=2
SSID_PROFILE_ID=111
[ VAP_END=1 ]
[ VAP_START=2 ]
AP_SEC_MODE=6
AP_SSID='Guest Net'
IEEE80211W_ENABLE=2
SSID_PROFILE_ID=222
[ VAP_END=2 ]
[ VAP_START=3 ]
AP_SEC_MODE=7
AP_SSID=Old
IEEE80211W_ENABLE=1
SSID_PROFILE_ID=333
[ VAP_END=3 ]
`

func TestPlanNativeSwitchesOnlyManagedSections(t *testing.T) {
	next, changed := planNative([]byte(sampleAPConf), func(ssid string) (bool, bool) {
		switch ssid {
		case "Guest Net":
			return true, true // wanted mixed
		case "Old":
			return false, true // mixed before, now WPA3 again
		}
		return false, true
	})
	if strings.Join(changed, ",") != "222,333" {
		t.Fatalf("changed %v", changed)
	}
	out := string(next)
	if !strings.Contains(out, "AP_SEC_MODE=7\nAP_SSID='Guest Net'\nIEEE80211W_ENABLE=1") {
		t.Fatalf("guest not switched:\n%s", out)
	}
	if !strings.Contains(out, "AP_SEC_MODE=6\nAP_SSID=Old\nIEEE80211W_ENABLE=2") {
		t.Fatalf("old not reverted:\n%s", out)
	}
	if !strings.Contains(out, "AP_SEC_MODE=6\nAP_SSID=Office\nIEEE80211W_ENABLE=2") {
		t.Fatalf("office touched:\n%s", out)
	}
	// Unmanaged sections (not WPA3 in OpenConfig) are never touched.
	_, changed = planNative([]byte(sampleAPConf), func(string) (bool, bool) { return true, false })
	if len(changed) != 0 {
		t.Fatalf("unmanaged changed %v", changed)
	}
}

func TestCheckDiffGate(t *testing.T) {
	ok := "[ VAP_START=222 DEL ]\nAP_SSID=x\n[ VAP_END=222 ]\n[ VAP_START=222 NEW ]\n[ VAP_END=222 ]\n"
	if err := checkDiff(ok, 0, []string{"222"}); err != nil {
		t.Fatal(err)
	}
	if checkDiff(ok, 1, []string{"222"}) == nil {
		t.Fatal("accepted a reboot-required diff")
	}
	if checkDiff(ok+"[ RADIO_START=2 ]\n", 0, []string{"222"}) == nil {
		t.Fatal("accepted a radio change")
	}
	if checkDiff("[ VAP_START=111 DEL ]\n", 0, []string{"222"}) == nil {
		t.Fatal("accepted another network")
	}
	if checkDiff("", 0, []string{"222"}) == nil {
		t.Fatal("accepted an empty diff")
	}
}

func TestEnforceAppliesThroughFirmwarePath(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "ap.conf")
	if err := os.WriteFile(conf, []byte(sampleAPConf), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(conf, old, old)
	o := NewWirelessOverrides(filepath.Join(dir, "overrides.json"))
	o.apConf, o.triggerGlob, o.reinitFile, o.workDir = conf, filepath.Join(dir, "trigger-*"), filepath.Join(dir, "reinit"), filepath.Join(dir, "work")
	o.diff = func(_ context.Context, cur, next string) (string, int, error) {
		return "[ VAP_START=111 DEL ]\n[ VAP_END=111 ]\n[ VAP_START=111 NEW ]\n[ VAP_END=111 ]\n", 0, nil
	}
	applied := 0
	o.apply = func(_ context.Context, next string) error {
		applied++
		raw, _ := os.ReadFile(next)
		return os.WriteFile(conf, raw, 0o600)
	}
	if err := o.Update("", "Office", true); err != nil {
		t.Fatal(err)
	}
	modes := map[string]string{"Office": "WPA3_SAE", "Guest Net": "WPA3_SAE", "Old": "WPA2_PERSONAL"}
	o.Enforce(context.Background(), modes)
	if applied != 1 || o.Status("Office") != "applied" {
		t.Fatalf("applied=%d status=%q", applied, o.Status("Office"))
	}
	// A pending firmware trigger blocks the enforcer.
	if err := o.Update("Office", "Office", false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "trigger-1"), []byte("x"), 0o600)
	o.lastAttempt = time.Time{}
	o.Enforce(context.Background(), modes)
	if applied != 1 {
		t.Fatal("applied while the firmware was busy")
	}
	// A rejected diff is not retried for the same native file.
	_ = os.Remove(filepath.Join(dir, "trigger-1"))
	_ = os.Chtimes(conf, old, old)
	o.diff = func(context.Context, string, string) (string, int, error) { return "", 1, nil }
	o.Enforce(context.Background(), modes)
	o.lastAttempt = time.Time{}
	o.Enforce(context.Background(), modes)
	if applied != 1 {
		t.Fatal("applied a reboot-required change")
	}
}

func TestScheduleWindows(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	s := SSIDSchedule{Enabled: true, Windows: []ScheduleWindow{
		{Days: []int{1, 2, 3, 4, 5}, Start: "08:00", End: "18:00"},
		{Days: []int{5}, Start: "22:00", End: "02:00"}, // Friday night into Saturday
	}}
	at := func(day, hh, mm int) time.Time { return time.Date(2026, 10, 4+day, hh, mm, 0, 0, berlin) } // 2026-10-04 is a Sunday
	cases := []struct {
		t    time.Time
		want bool
	}{
		{at(1, 7, 59), false}, {at(1, 8, 0), true}, {at(1, 17, 59), true}, {at(1, 18, 0), false},
		{at(5, 23, 0), true}, {at(6, 1, 59), true}, {at(6, 2, 0), false}, {at(0, 12, 0), false},
	}
	for _, c := range cases {
		if got := s.activeAt(c.t); got != c.want {
			t.Errorf("%s: %v, want %v", c.t.Format("Mon 15:04"), got, c.want)
		}
	}
	if next := s.nextChange(at(1, 12, 0)); next == nil || !next.Equal(at(1, 18, 0)) {
		t.Fatalf("next change %v", next)
	}
	if (SSIDSchedule{Enabled: true}).validate() == nil || (SSIDSchedule{Windows: []ScheduleWindow{{Days: []int{7}, Start: "08:00", End: "09:00"}}}).validate() == nil {
		t.Fatal("invalid schedule accepted")
	}
}

func TestHistoryBoundedAndRates(t *testing.T) {
	h := NewHistory()
	base := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	rssi := -60.0
	for i := 0; i < 3*60*12; i++ { // three hours of 5 s samples
		st := APState{GeneratedAt: base.Add(time.Duration(i) * 5 * time.Second),
			Interfaces: []Interface{{InOctets: float64(i) * 5000}},
			Clients:    []Client{{MAC: "AA:BB:CC:00:00:01", SSID: "Office", Band: "5", RSSI: &rssi}}}
		h.Observe(st)
	}
	points := h.Points(base.Add(-time.Hour))
	if len(points) != 180 {
		t.Fatalf("%d points", len(points))
	}
	if p := points[10]; p.RxBps == nil || *p.RxBps < 999 || *p.RxBps > 1001 || p.PerSSID["Office"] != 1 {
		t.Fatalf("point %+v", p)
	}
	if n := len(h.Client("aa:bb:cc:00:00:01")); n != clientTrackPoints {
		t.Fatalf("client samples %d", n)
	}
}

func TestJoinPayloadEscaping(t *testing.T) {
	got := joinPayload(`Bar;One`, opModeMixed, `p:a,ss\word`, true)
	if got != `WIFI:T:WPA;S:Bar\;One;P:p\:a\,ss\\word;H:true;;` {
		t.Fatal(got)
	}
	if joinPayload("Open", "ENHANCED_OPEN", "", false) != "WIFI:T:nopass;S:Open;;" {
		t.Fatal("open payload")
	}
}

func TestAuthRoles(t *testing.T) {
	a := NewAuth(filepath.Join(t.TempDir(), "auth.json"))
	if err := a.SetCredentials("config", "admin-secret"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewer("config", "viewer-secret"); err == nil {
		t.Fatal("viewer took the admin name")
	}
	if err := a.SetViewer("viewer", "viewer-secret"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/login", nil)
	if role, err := a.Check(r, "viewer", "viewer-secret"); err != nil || role != RoleViewer {
		t.Fatalf("viewer %v %v", role, err)
	}
	if role, err := a.Check(r, "config", "admin-secret"); err != nil || role != RoleAdmin {
		t.Fatalf("admin %v %v", role, err)
	}
	if _, err := a.Check(r, "viewer", "admin-secret"); !errors.Is(err, errBadPassword) {
		t.Fatal("viewer accepted the admin password")
	}
	// Changing the admin login keeps the viewer.
	if err := a.SetCredentials("administrator", "admin-secret2"); err != nil || a.ViewerName() != "viewer" {
		t.Fatalf("viewer lost: %v %q", err, a.ViewerName())
	}
}

func TestViewerCannotWrite(t *testing.T) {
	dir := t.TempDir()
	auth := NewAuth(filepath.Join(dir, "auth.json"))
	_ = auth.SetCredentials("config", "admin-secret")
	_ = auth.SetViewer("viewer", "viewer-secret")
	api := &API{auth: auth, changes: NewChangeLog(filepath.Join(dir, "changes.json"))}
	called := false
	h := api.write(func(w http.ResponseWriter, r *http.Request) { called = true; reply(w, 200, nil) })

	rec := httptest.NewRecorder()
	if err := auth.NewSession(rec, "viewer", RoleViewer); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("PUT", "/api/refresh", strings.NewReader(`{"seconds":5}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(rec.Result().Cookies()[0])
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusForbidden || called {
		t.Fatalf("viewer write: %d called=%v", out.Code, called)
	}

	rec = httptest.NewRecorder()
	_ = auth.NewSession(rec, "config", RoleAdmin)
	req = httptest.NewRequest("PUT", "/api/refresh", strings.NewReader(`{"seconds":5}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(rec.Result().Cookies()[0])
	req.Pattern = "PUT /api/refresh"
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	entries := api.changes.Entries()
	if out.Code != 200 || !called || len(entries) != 1 || entries[0].User != "config" || entries[0].Action != "Set live updates to every 5 s" {
		t.Fatalf("admin write: %d %+v", out.Code, entries)
	}
	// The log survives a restart and stays bounded.
	for i := 0; i < changeLogLimit+20; i++ {
		api.changes.Record(ChangeEntry{Action: "x"})
	}
	if n := len(NewChangeLog(filepath.Join(dir, "changes.json")).Entries()); n != changeLogLimit {
		t.Fatalf("reloaded %d entries", n)
	}
}

func TestMetricsRender(t *testing.T) {
	rssi := -55.0
	s := stateResponse{APState: APState{
		Device:     Device{Hostname: "AP-1", Model: "C-460"},
		SSIDs:      []SSID{{Name: `Off"ice`, Enabled: true, Bands: []string{"5"}}},
		Clients:    []Client{{MAC: "AA", SSID: `Off"ice`, Band: "5", RSSI: &rssi}},
		Interfaces: []Interface{{Name: "eth0", Up: true, Speed: "1 Gbit/s", Port: 1}},
	}}
	out := renderMetrics(s)
	for _, want := range []string{`c460_clients{ssid="Off\"ice",band="5"} 1`, `c460_client_rssi_dbm{mac="AA",ssid="Off\"ice",band="5",hostname=""} -55`, `c460_ethernet_speed_bits_per_second{port="1",interface="eth0",role=""} 1e+09`, "# TYPE c460_up gauge"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
}
