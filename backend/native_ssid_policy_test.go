package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func policyFixture(t *testing.T) *SSIDPolicies {
	t.Helper()
	p := NewSSIDPolicies(filepath.Join(t.TempDir(), "policies.json"))
	p.apConf = filepath.Join(filepath.Dir(p.path), "ap.conf")
	if err := os.WriteFile(p.apConf, []byte(sampleAPConf), 0600); err != nil {
		t.Fatal(err)
	}
	p.available = func() bool { return true }
	p.apply = func(_ context.Context, before, next []byte, _ []string) error {
		current, _ := os.ReadFile(p.apConf)
		if !bytes.Equal(current, before) {
			return errors.New("concurrent update")
		}
		return os.WriteFile(p.apConf, next, 0600)
	}
	p.verify = func(context.Context, string, SSIDPolicy) error { return nil }
	return p
}

func testPolicy() SSIDPolicy {
	n := 7
	return SSIDPolicy{MACFilter: MACFilter{Mode: "deny", Addresses: []string{"02:00:00:46:00:01"}}, MaxClients: &n}
}

func TestSSIDPolicyValidationAndNativeEncoding(t *testing.T) {
	good := testPolicy()
	normalized, err := normalizeSSIDPolicy(good)
	if err != nil {
		t.Fatal(err)
	}
	fields := policyFields(normalized)
	if fields["MAC_ACL_LIST"] != "AgAARgAB" || fields["ASSOC_LIMIT"] != "7" {
		t.Fatal(fields)
	}
	decoded, err := policyFromFields(fields)
	if err != nil || decoded.MACFilter.Addresses[0] != good.MACFilter.Addresses[0] || *decoded.MaxClients != 7 {
		t.Fatal(decoded, err)
	}
	for _, address := range []string{"bad", "01:00:00:00:00:01", "00:00:00:00:00:00", "02:00:00:00:00:00:00:01"} {
		p := good
		p.MACFilter.Addresses = []string{address}
		if _, err := normalizeSSIDPolicy(p); err == nil {
			t.Fatal("invalid MAC accepted", address)
		}
	}
	for _, limit := range []int{0, -1, 128} {
		p := good
		p.MaxClients = &limit
		if _, err := normalizeSSIDPolicy(p); err == nil {
			t.Fatal("invalid limit accepted", limit)
		}
	}
	for _, p := range []SSIDPolicy{{MACFilter: MACFilter{Mode: "allow"}}, {MACFilter: MACFilter{Mode: "invalid"}}, {MACFilter: MACFilter{Mode: "deny", Addresses: []string{"02:00:00:46:00:01", "02-00-00-46-00-01"}}}} {
		if _, err := normalizeSSIDPolicy(p); err == nil {
			t.Fatal("invalid policy accepted", p)
		}
	}
}

func TestSSIDPolicyOnlyChangesOwnedFieldsAndChecksFullFirmwareDiff(t *testing.T) {
	before := []byte(sampleAPConf)
	next, profiles, err := rewritePolicyFields(before, map[string]map[string]string{"Guest Net": policyFields(testPolicy())})
	if err != nil || strings.Join(profiles, ",") != "222" {
		t.Fatal(profiles, err)
	}
	sections, _ := policySections(next)
	s := sections[1]
	lines := strings.Split(string(next), "\n")
	fullDiff := "[ VAP_START=222 MOD ]\n" + strings.Join(lines[s.start+1:s.end], "\n") + "\n[ VAP_END=222 ]\n"
	if err := checkPolicyDiff(fullDiff, 0, profiles, before, next); err != nil {
		t.Fatal(err)
	}
	fast := "[ VAP_START=222 F_MOD ]\nMAC_ACL_OPERATION=2\nMAC_ACL_LIST=AgAARgAB\n[ VAP_END=222 ]\n"
	if err := checkPolicyDiff(fast, 0, profiles, before, next); err != nil {
		t.Fatal("firmware ACL fast path rejected", err)
	}
	for _, diff := range []string{strings.Replace(fullDiff, "AP_SEC_MODE=6", "AP_SEC_MODE=7", 1), strings.ReplaceAll(fullDiff, "222", "333"), fullDiff + "[ RADIO_START=1 MOD ]\n"} {
		if checkPolicyDiff(diff, 0, profiles, before, next) == nil {
			t.Fatal("unrelated diff accepted")
		}
	}
	if checkPolicyDiff(fullDiff, 1, profiles, before, next) == nil {
		t.Fatal("reboot-required diff accepted")
	}
	if checkPolicyDiff(fullDiff, 0, profiles, before, []byte(strings.Replace(string(next), "X=1", "X=2", 1))) == nil {
		t.Fatal("unrelated candidate accepted")
	}
	restored, _, err := rewritePolicyFields(next, map[string]map[string]string{"Guest Net": {}})
	if err != nil || !bytes.Equal(restored, before) {
		t.Fatal("rollback altered original bytes", err)
	}
	for _, bad := range []string{sampleAPConf + sampleAPConf, strings.Replace(sampleAPConf, "VAP_END=1", "VAP_END=4", 1), strings.Replace(sampleAPConf, "SSID_PROFILE_ID=222", "SSID_PROFILE_ID=111", 1)} {
		if _, _, err := rewritePolicyFields([]byte(bad), map[string]map[string]string{"Office": policyFields(testPolicy())}); err == nil {
			t.Fatal("ambiguous mapping accepted")
		}
	}
}

func TestSSIDPolicyPersistsRegeneratesRenamesAndDeletes(t *testing.T) {
	p := policyFixture(t)
	if err := p.Update(context.Background(), "Office", testPolicy()); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p.path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("public policy store")
	}
	loaded := NewSSIDPolicies(p.path)
	if got := loaded.Saved()["Office"]; got.MACFilter.Mode != "deny" || *got.MaxClients != 7 {
		t.Fatal("restart lost desired settings", got)
	}
	_ = os.WriteFile(p.apConf, []byte(sampleAPConf), 0600)
	if err := p.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p.apConf)
	if !strings.Contains(string(raw), "ASSOC_LIMIT=7") {
		t.Fatal("regeneration lost policy")
	}
	if err := p.Rename([]ssidChange{{oldName: "Office", newName: "Renamed"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Saved()["Renamed"]; !ok {
		t.Fatal("rename lost policy")
	}
	if err := p.Rename([]ssidChange{{oldName: "Renamed"}}); err != nil || len(p.Saved()) != 0 {
		t.Fatal("deletion retained policy", err)
	}
}

func TestSSIDPolicyRollsBackOperatingAndPersistenceFailures(t *testing.T) {
	for _, reason := range []string{"operating", "persistence"} {
		t.Run(reason, func(t *testing.T) {
			p := policyFixture(t)
			before, _ := os.ReadFile(p.apConf)
			if reason == "operating" {
				p.verify = func(_ context.Context, _ string, policy SSIDPolicy) error {
					if policy.MACFilter.Mode == "deny" {
						return errors.New("driver rejected policy")
					}
					return nil
				}
			}
			if reason == "persistence" {
				p.save = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
			}
			err := p.Update(context.Background(), "Office", testPolicy())
			after, _ := os.ReadFile(p.apConf)
			if err == nil || !strings.Contains(err.Error(), "restored") || !bytes.Equal(before, after) || len(p.Saved()) != 0 {
				t.Fatal("rollback failed", err)
			}
		})
	}
}

func TestMetricsReportsOperating320MHzAndCPU(t *testing.T) {
	width, cpu := 320, 12.5
	s := stateResponse{}
	s.Radios = []Radio{{Band: "6", Width: 160, OperatingWidth: &width}}
	s.Device.CPUUsage = &cpu
	out := renderMetrics(s)
	if !strings.Contains(out, "c460_radio_channel_width_mhz{band=\"6\"} 320") || !strings.Contains(out, "c460_cpu_usage_ratio 0.125") {
		t.Fatal("incorrect operating metrics", out)
	}
}

func TestDriverACLReadback(t *testing.T) {
	for _, out := range []string{"", "ath26\tgetmac:", "ath26\tgetmac:02:00:00:46:00:02 02:00:00:46:00:01\n"} {
		macs, err := parseDriverPolicyMACs(out)
		if err != nil || macs == nil {
			t.Fatal("valid kernel list rejected", err)
		}
		if len(macs) > 0 && macs[0] != "02:00:00:46:00:01" {
			t.Fatal("not canonical", macs)
		}
	}
	for _, out := range []string{"unknown driver reply", "ath26\tgetmac:garbage"} {
		if _, err := parseDriverPolicyMACs(out); err == nil {
			t.Fatal("invalid readback accepted")
		}
	}
}

func TestPolicyRestoreChangesAllNetworksInOneNativeCommit(t *testing.T) {
	p := policyFixture(t)
	calls := 0
	apply := p.apply
	p.apply = func(ctx context.Context, before, next []byte, profiles []string) error {
		calls++
		return apply(ctx, before, next, profiles)
	}
	if err := p.UpdateMany(context.Background(), map[string]SSIDPolicy{"Office": testPolicy(), "Guest Net": testPolicy()}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(p.Saved()) != 2 {
		t.Fatal("restore was not grouped", calls)
	}
}

func TestSSIDPolicyRejectsInvalidBodiesBeforeNativeAccess(t *testing.T) {
	a := &API{}
	for _, body := range []string{
		`{}`, `{"macFilter":{"mode":"off"},"maxClients":null}`,
		`{"macFilter":{"mode":"allow","addresses":[]},"maxClients":null}`,
		`{"macFilter":{"mode":"off","addresses":[]},"maxClients":128}`,
		`{"macFilter":{"mode":"off","addresses":[]},"maxClients":null,"unexpected":true}`,
	} {
		w := httptest.NewRecorder()
		a.updateSSIDPolicy(w, httptest.NewRequest(http.MethodPut, "/api/ssids/Office/policy", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal("invalid body reached native configuration", body, w.Code)
		}
	}
}

func TestPolicyAPIRequiresAuthenticationAndRejectsViewerWrites(t *testing.T) {
	a := &API{auth: NewAuth(filepath.Join(t.TempDir(), "auth.json")), changes: NewChangeLog("")}
	mux := http.NewServeMux()
	a.Register(mux)
	for _, path := range []string{"/api/ssids/Office/policy", "/api/v1/ssids/Office/policy"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("anonymous policy access", w.Code)
		}
		session := httptest.NewRecorder()
		if err := a.auth.NewSession(session, "crew", RoleViewer); err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"macFilter":{"mode":"off","addresses":[]},"maxClients":null}`))
		r.AddCookie(session.Result().Cookies()[0])
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("viewer configured policy", w.Code)
		}
	}
}

func TestBackupRejectsUnsupportedAndUnboundPoliciesBeforeWirelessWrite(t *testing.T) {
	req := restoreRequest{Backup: Backup{Format: backupFormat, Version: 1, SSIDPolicies: map[string]SSIDPolicy{"Unknown": testPolicy()}}}
	req.Sections.Wireless = true
	if _, err := (&API{}).planRestore(req); err == nil {
		t.Fatal("unsupported policies accepted")
	}
	a := &API{policies: policyFixture(t)}
	if _, err := a.planRestore(req); err == nil || !strings.Contains(err.Error(), "no network") {
		t.Fatal("unbound backup policy accepted", err)
	}
	req.Backup.SSIDPolicies["Unknown"] = SSIDPolicy{MACFilter: MACFilter{Mode: "allow"}}
	if _, err := a.planRestore(req); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatal("invalid backup policy accepted", err)
	}
}

func TestInactivePolicyIsSavedWithoutStartingWireless(t *testing.T) {
	p := policyFixture(t)
	p.inactive = func(string) bool { return true }
	p.apply = func(context.Context, []byte, []byte, []string) error {
		t.Fatal("inactive network was restarted")
		return nil
	}
	p.verify = func(context.Context, string, SSIDPolicy) error {
		t.Fatal("inactive network was claimed operating")
		return nil
	}
	if err := p.Update(context.Background(), "Office", testPolicy()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p.apConf)
	if string(raw) != sampleAPConf || p.Saved()["Office"].MACFilter.Mode != "deny" {
		t.Fatal("pending desired policy not saved")
	}
}

func TestSSIDPolicyRepairsDriverDriftWithoutNativeConfigurationChange(t *testing.T) {
	p := policyFixture(t)
	if err := p.Update(context.Background(), "Office", testPolicy()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p.apConf)
	drift := true
	p.verify = func(context.Context, string, SSIDPolicy) error {
		if drift {
			return errors.New("driver reset")
		}
		return nil
	}
	apply := p.apply
	p.apply = func(ctx context.Context, old, next []byte, profiles []string) error {
		if !bytes.Equal(old, next) || strings.Join(profiles, ",") != "111" {
			t.Fatal("runtime repair changed native settings")
		}
		drift = false
		return apply(ctx, old, next, profiles)
	}
	if err := p.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p.apConf)
	if drift || !bytes.Equal(before, after) {
		t.Fatal("runtime drift not repaired")
	}
}
