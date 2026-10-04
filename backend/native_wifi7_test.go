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
	"time"
)

const nativeWiFi7Fixture = "[ RADIO_v2_START=1 ]\nWIRELESS_BAND=2G\nWIRELESS_PROTOCOL=4\nAP_CHAN_WIDTH=2\n[ RADIO_v2_END=1 ]\n[ RADIO_v2_START=4 ]\nWIRELESS_BAND=6G\nWIRELESS_PROTOCOL=4\nAP_CHAN_WIDTH=4\nOP_CHANNEL=5\nTX_POWER=23\n[ RADIO_v2_END=4 ]\n[ VAP_START=9 ]\nSSID=MGMT\nPSK=preserve-this\nVLAN=0\n[ VAP_END=9 ]\n"

func wifi7Fixture(t *testing.T) *NativeWiFi7 {
	t.Helper()
	dir := t.TempDir()
	n := NewNativeWiFi7(filepath.Join(dir, "wifi7.json"))
	n.apConf = filepath.Join(dir, "ap.conf")
	if err := os.WriteFile(n.apConf, []byte(nativeWiFi7Fixture), 0600); err != nil {
		t.Fatal(err)
	}
	n.quiet = func(context.Context) error { return nil }
	n.supported = func([]byte) bool { return true }
	n.apply = func(_ context.Context, before, after []byte, _ string) error {
		actual, _ := os.ReadFile(n.apConf)
		if !bytes.Equal(actual, before) {
			return errors.New("concurrent write")
		}
		return os.WriteFile(n.apConf, after, 0600)
	}
	n.operating = func(context.Context) (string, int, error) {
		raw, _ := os.ReadFile(n.apConf)
		p, err := find6GHzProfile(raw)
		for width, code := range radioWidthCodes {
			if p.width == code {
				return expected6GHzMode(p.protocol, width), width, err
			}
		}
		return "", 0, errors.New("invalid width")
	}
	return n
}

func TestWiFi7OnlyChangesSelectedRadio(t *testing.T) {
	before := []byte(nativeWiFi7Fixture)
	after, p, err := plan6GHz(before, 5, 320)
	expected := strings.Replace(nativeWiFi7Fixture, "WIRELESS_BAND=6G\nWIRELESS_PROTOCOL=4\nAP_CHAN_WIDTH=4", "WIRELESS_BAND=6G\nWIRELESS_PROTOCOL=5\nAP_CHAN_WIDTH=6", 1)
	if err != nil || p.id != "4" || string(after) != expected {
		t.Fatal("unrelated settings changed", err)
	}
	for _, raw := range []string{nativeWiFi7Fixture + nativeWiFi7Fixture, strings.Replace(nativeWiFi7Fixture, "AP_CHAN_WIDTH=4", "AP_CHAN_WIDTH=5", 1), strings.Replace(nativeWiFi7Fixture, "WIRELESS_PROTOCOL=4\nAP_CHAN_WIDTH=4", "WIRELESS_PROTOCOL=4\nWIRELESS_PROTOCOL=4\nAP_CHAN_WIDTH=4", 1)} {
		if _, _, err := plan6GHz([]byte(raw), 5, 320); err == nil {
			t.Fatal("ambiguous/unsupported profile accepted")
		}
	}
	if _, _, err := plan6GHz(before, 4, 320); err == nil {
		t.Fatal("HE320 accepted")
	}
}

func TestWiFi7DiffRefusesOtherChangesAndReboot(t *testing.T) {
	good := "[ RADIO_START=4 MOD ]\nAP_CHAN_WIDTH=6\nWIRELESS_PROTOCOL=5\n[ RADIO_END=4 ]\n"
	if err := checkRadioDiff(good, 0, "4"); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{strings.Replace(good, "AP_CHAN_WIDTH=6", "SSID=modified", 1), strings.Replace(good, "START=4", "START=3", 1), good + "[ VAP_START=9 MOD ]\n[ VAP_END=9 ]\n"} {
		if err := checkRadioDiff(out, 0, "4"); err == nil {
			t.Fatal("unrelated firmware diff accepted")
		}
	}
	if err := checkRadioDiff(good, 1, "4"); err == nil {
		t.Fatal("reboot accepted")
	}
}

func TestWiFi7PersistsAndRestoresAfterRegeneration(t *testing.T) {
	n := wifi7Fixture(t)
	ctx := context.Background()
	desired := WiFi7Settings{Enabled: true, Width: 320}
	if err := n.Update(ctx, desired, 160); err != nil {
		t.Fatal(err)
	}
	if st := n.Snapshot(); st.Status != "active" || st.OperatingWidth != 320 || st.OperatingMode != "11AEHT320" {
		t.Fatal(st)
	}
	fi, _ := os.Stat(n.path)
	if fi.Mode().Perm() != 0600 {
		t.Fatal("public desired state")
	}
	loaded := NewNativeWiFi7(n.path)
	if loaded.Saved() == nil || *loaded.Saved() != desired {
		t.Fatal("not restored after service restart")
	}
	if err := os.WriteFile(n.apConf, []byte(nativeWiFi7Fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n.Ensure(ctx, 160); err != nil {
		t.Fatal(err)
	}
	if st := n.Snapshot(); st.Status != "active" {
		t.Fatal(st)
	}
	if err := n.Update(ctx, WiFi7Settings{Width: 160}, 160); err != nil {
		t.Fatal(err)
	}
	if st := n.Snapshot(); st.Status != "off" || st.OperatingMode != "11AHE160" {
		t.Fatal(st)
	}
	// Disabled enforcement must leave subsequent ordinary radio changes alone.
	next, _, _ := plan6GHz([]byte(nativeWiFi7Fixture), 4, 80)
	_ = os.WriteFile(n.apConf, next, 0600)
	if err := n.Ensure(ctx, 160); err != nil {
		t.Fatal(err)
	}
	actual, _ := os.ReadFile(n.apConf)
	if !bytes.Equal(actual, next) {
		t.Fatal("disabled override changed ordinary radio configuration")
	}
}

func TestWiFi7FailedApplyAndSaveRollBack(t *testing.T) {
	t.Run("apply", func(t *testing.T) {
		n := wifi7Fixture(t)
		original := n.apply
		count := 0
		n.apply = func(ctx context.Context, b, a []byte, id string) error {
			count++
			if err := original(ctx, b, a, id); err != nil {
				return err
			}
			if count == 1 {
				return errors.New("firmware apply failed")
			}
			return nil
		}
		if err := n.Update(context.Background(), WiFi7Settings{Enabled: true, Width: 320}, 160); err == nil || !strings.Contains(err.Error(), "restored") {
			t.Fatal(err)
		}
		actual, _ := os.ReadFile(n.apConf)
		if string(actual) != nativeWiFi7Fixture || n.Saved() != nil || count != 2 {
			t.Fatal("rollback/persistence failed")
		}
	})
	t.Run("save", func(t *testing.T) {
		n := wifi7Fixture(t)
		n.save = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
		if err := n.Update(context.Background(), WiFi7Settings{Enabled: true, Width: 320}, 160); err == nil || !strings.Contains(err.Error(), "restored") {
			t.Fatal(err)
		}
		actual, _ := os.ReadFile(n.apConf)
		if string(actual) != nativeWiFi7Fixture || n.Saved() != nil {
			t.Fatal("save rollback failed")
		}
	})
	t.Run("driver-rejected", func(t *testing.T) {
		n := wifi7Fixture(t)
		n.operating = func(context.Context) (string, int, error) { return "11AHE160", 160, nil }
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		if err := n.Update(ctx, WiFi7Settings{Enabled: true, Width: 320}, 160); err == nil {
			t.Fatal("unverified radio accepted")
		}
		raw, _ := os.ReadFile(n.apConf)
		if string(raw) != nativeWiFi7Fixture || n.Saved() != nil {
			t.Fatal("runtime failure did not restore profile")
		}
	})
}

func TestWiFi7DoesNotConfuseDesiredAndOperatingState(t *testing.T) {
	n := wifi7Fixture(t)
	raw, _, _ := plan6GHz([]byte(nativeWiFi7Fixture), 5, 320)
	_ = os.WriteFile(n.apConf, raw, 0600)
	n.operating = func(context.Context) (string, int, error) { return "11AHE160", 160, nil }
	if err := n.Update(context.Background(), WiFi7Settings{Enabled: true, Width: 320}, 160); err == nil || n.Saved() != nil {
		t.Fatal("requested mode was mistaken for actual mode")
	}
}

func TestWiFi7EndpointValidationAndAuthorization(t *testing.T) {
	a := radioWidthFixture()
	a.wifi7 = wifi7Fixture(t)
	for _, tc := range []struct{ id, body string }{{"2", `{"enabled":true,"width":255}`}, {"0", `{"enabled":true,"width":320}`}} {
		r := httptest.NewRequest("PUT", "/api/radios/"+tc.id+"/wifi7", strings.NewReader(tc.body))
		r.SetPathValue("id", tc.id)
		w := httptest.NewRecorder()
		a.updateWiFi7(w, r)
		if w.Code != 400 || a.wifi7.Saved() != nil {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	a.auth = NewAuth(filepath.Join(t.TempDir(), "auth.json"))
	a.changes = NewChangeLog("")
	for _, role := range []Role{RoleViewer, RoleAdmin} {
		c := httptest.NewRecorder()
		if err := a.auth.NewSession(c, "test", role); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("PUT", "/api/radios/2/wifi7", strings.NewReader(`{"enabled":true,"width":320}`))
		r.SetPathValue("id", "2")
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(c.Result().Cookies()[0])
		if role == RoleAdmin {
			a.poller.state.Radios[0].Enabled = false
		}
		w := httptest.NewRecorder()
		a.write(a.updateWiFi7).ServeHTTP(w, r)
		want := http.StatusForbidden
		if role == RoleAdmin {
			want = 400
		}
		if w.Code != want || a.wifi7.Saved() != nil {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestWiFi7SnapshotUsesSchemaSafeWidth(t *testing.T) {
	a := radioWidthFixture()
	// gNMI JSON numbers are float64 in real snapshots.
	a.poller.raw["radios"].(map[string]any)["radio"].([]any)[0].(map[string]any)["config"].(map[string]any)["channel-width"] = float64(160)
	a.wifi7 = wifi7Fixture(t)
	if err := a.wifi7.Update(context.Background(), WiFi7Settings{Enabled: true, Width: 320}, 160); err != nil {
		t.Fatal(err)
	}
	a.poller.state.Radios[0].Width = 320
	snapshot := a.snapshot()
	if snapshot.Radios[0].Width != 160 || snapshot.Radios[0].WiFi7.OperatingWidth != 320 {
		t.Fatal("form/backup leaked 320 into OpenConfig")
	}
	if a.poller.state.Radios[0].Width != 320 || a.poller.state.Radios[0].WiFi7 != nil {
		t.Fatal("snapshot mutated poller state")
	}
}

func TestWiFi7BackupValidation(t *testing.T) {
	a := radioWidthFixture()
	a.gnmi = &GNMI{}
	a.wifi7 = wifi7Fixture(t)
	a.wifi7.Observe(context.Background())
	req := restoreRequest{Backup: Backup{Format: backupFormat, Version: 1, WiFi7: &WiFi7Settings{Enabled: true, Width: 320}}}
	req.Sections.Radios = true
	if _, err := a.planRestore(req); err == nil {
		t.Fatal("missing 6 GHz radio accepted")
	}
	req.Backup.Radios = []BackupRadio{{Band: "6", radioRequest: radioRequest{Enabled: true, Channel: 5, Width: 160, Power: 23}}}
	if _, err := a.planRestore(req); err != nil {
		t.Fatal("valid backup rejected", err)
	}
	req.Backup.WiFi7.Width = 255
	if _, err := a.planRestore(req); err == nil {
		t.Fatal("invalid native width accepted")
	}
	req.Backup.WiFi7.Width = 320
	req.Backup.Radios[0].Enabled = false
	if _, err := a.planRestore(req); err == nil {
		t.Fatal("disabled radio accepted")
	}
}
