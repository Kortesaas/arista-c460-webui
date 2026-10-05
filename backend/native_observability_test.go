package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshStorageAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	fixtureFile(t, path, `{"hostname":"test-ap","pollSeconds":5,"siteName":"Test AP","vlanNames":{"10":"Management"},"gnmi":{"username":"api-user","password":"fixture-secret"}}`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	p := &Poller{interval: 5 * time.Second, state: APState{PollSeconds: 5}, trigger: make(chan struct{}, 1)}
	a := &API{cfg: cfg, poller: p}
	for _, body := range []string{`{}`, `{"seconds":0}`, `{"seconds":61}`, `{"seconds":1.5}`, `{"seconds":1,"other":true}`} {
		w := httptest.NewRecorder()
		a.updateRefresh(w, httptest.NewRequest("PUT", "/api/refresh", strings.NewReader(body)))
		if w.Code != 400 || cfg.RefreshSeconds() != 5 {
			t.Fatal("invalid update accepted", body, w.Code)
		}
	}
	w := httptest.NewRecorder()
	a.updateRefresh(w, httptest.NewRequest("PUT", "/api/refresh", strings.NewReader(`{"seconds":1}`)))
	loaded, err := loadConfig(path)
	if err != nil || w.Code != 200 || loaded.RefreshSeconds() != 1 || p.interval != time.Second || p.Snapshot().PollSeconds != 1 {
		t.Fatal("update not stored/applied", w.Code, err)
	}
	if loaded.GNMI != cfg.GNMI || loaded.SiteName != "Test AP" || loaded.VLANNames["10"] != "Management" {
		t.Fatal("unrelated config changed")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("secret config permissions changed")
	}
	cfg.path = filepath.Join(t.TempDir(), "missing", "config.json")
	w = httptest.NewRecorder()
	a.updateRefresh(w, httptest.NewRequest("PUT", "/api/refresh", strings.NewReader(`{"seconds":2}`)))
	if w.Code != 500 || cfg.RefreshSeconds() != 1 || p.interval != time.Second {
		t.Fatal("failed save changed runtime interval")
	}
	if err = cfg.SetLabels("Lost", map[string]string{}); err == nil || cfg.SiteName != "Test AP" || cfg.VLANNames["10"] != "Management" {
		t.Fatal("failed label save changed config")
	}
}

func TestConcurrentRefreshSettings(t *testing.T) {
	cfg := &Config{path: filepath.Join(t.TempDir(), "config.json"), PollSeconds: 5}
	p := &Poller{trigger: make(chan struct{}, 1)}
	var wg sync.WaitGroup
	for n := 1; n <= 10; n++ {
		wg.Add(1)
		go func(seconds int) {
			defer wg.Done()
			if err := cfg.SetRefresh(seconds); err != nil {
				t.Error(err)
			}
			p.SetInterval(seconds)
			_ = cfg.RefreshSeconds()
			_ = p.Snapshot()
		}(n)
	}
	wg.Wait()
}

func TestObservabilityRequiresSession(t *testing.T) {
	a := &API{auth: NewAuth(filepath.Join(t.TempDir(), "auth.json"))}
	mux := http.NewServeMux()
	a.Register(mux)
	for _, route := range []struct{ method, path string }{{"PUT", "/api/refresh"}, {"GET", "/api/network"}, {"GET", "/api/events"}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{"seconds":1}`)))
		if w.Code != 401 {
			t.Fatal(route, w.Code)
		}
	}
}

func TestEthernetUsesIndependentKernelCounters(t *testing.T) {
	root := t.TempDir()
	for path, raw := range map[string]string{"eth0/carrier": "1", "eth0/speed": "2500", "eth0/duplex": "full", "eth0/statistics/rx_bytes": "12345", "eth1/carrier": "0", "eth1/speed": "-1", "eth1/duplex": "unknown", "eth1/statistics/rx_bytes": "0", "eth1/statistics/tx_errors": "3"} {
		fixtureFile(t, filepath.Join(root, path), raw)
	}
	interfaces := []Interface{{Name: "eth0", InOctets: 99}, {Name: "eth1", Up: true, InOctets: 99, Speed: "old"}, {Name: "ath00", Up: true, InOctets: 99}}
	supplementEthernet(interfaces, root)
	if !interfaces[0].Up || interfaces[0].Speed != "2.5 Gbit/s" || interfaces[0].Duplex != "full" || interfaces[0].InOctets != 12345 || interfaces[1].Up || interfaces[1].InOctets != 0 || interfaces[1].OutErrors != 3 || interfaces[2].InOctets != 99 {
		t.Fatal(interfaces)
	}
	before := append([]Interface(nil), interfaces...)
	supplementEthernet(interfaces, t.TempDir())
	if !reflect.DeepEqual(interfaces, before) {
		t.Fatal("missing kernel data discarded previous values")
	}
}

func TestBridgeMembershipAndNetworkMapping(t *testing.T) {
	vlans := parseVLANs("VLAN Dev name | VLAN ID | Device\neth0.10 | 10 | eth0\nath01.10 | 10 | ath01\ninvalid | 4095 | eth0\neth0.20 | 20 | eth0")
	if len(vlans) != 3 {
		t.Fatal(vlans)
	}
	for _, tc := range []struct {
		members []string
		mode    string
		id      int
	}{{[]string{"eth0", "ath03"}, "native", 0}, {[]string{"eth0.10", "ath01.10"}, "tagged", 10}, {[]string{"eth0.10", "eth0.20"}, "mixed", 0}, {[]string{"eth0", "eth0.10"}, "mixed", 0}, {nil, "unknown", 0}} {
		id, mode := bridgeVLAN(tc.members, vlans)
		if mode != tc.mode || (tc.id == 0 && id != nil) || (tc.id > 0 && (id == nil || *id != tc.id)) {
			t.Fatal(tc, id, mode)
		}
	}
	root := t.TempDir()
	for _, path := range []string{"br0.10/bridge", "br0.10/brif/eth0.10", "br0.10/brif/ath01.10"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	fixtureFile(t, filepath.Join(root, "br0.10/flags"), "0x1003")
	fixtureFile(t, filepath.Join(root, "ath01/address"), "aa:bb:cc:dd:ee:ff\n")
	state := APState{SSIDs: []SSID{{Name: "Office", BSSIDs: []BSSID{{BSSID: "AA:BB:CC:DD:EE:FF"}}}}}
	bridges := collectBridges(root, vlans, state, map[string][]string{"br0.10": {"192.168.10.40/24"}})
	if len(bridges) != 1 || bridges[0].VLAN == nil || *bridges[0].VLAN != 10 || !bridges[0].Up || !reflect.DeepEqual(bridges[0].Networks, []string{"Office"}) || len(bridges[0].Addresses) != 1 {
		t.Fatal(bridges)
	}
	if interfaceNetworks(root, state)["ath01"] != "Office" {
		t.Fatal("event interface mapping failed")
	}
}

func TestWirelessEventsAllowlistAndBoundedHistory(t *testing.T) {
	raw := "2026.10.03 00:16:21.036365: ath23: AP-STA-CONNECTED aa:bb:cc:dd:ee:ff password=do-not-export\n" +
		"2026.10.03 00:16:22.036365: ath23: CTRL-EVENT-CHANNEL-SWITCH freq=5975 password=do-not-export\n" +
		"2026.10.03 00:16:23: ath23: WPA: password=do-not-export\n" +
		"2026.10.03 00:16:24: ath23: AP-STA-CONNECTED-SECRET password=do-not-export\n" +
		"ath23: DFS-RADAR-DETECTED freq=5500\n"
	events := parseWirelessEvents(raw, map[string]string{"ath23": "Guest"})
	encoded, _ := json.Marshal(events)
	if len(events) != 3 || events[0].Client != "AA:BB:CC:DD:EE:FF" || events[0].Network != "Guest" || events[1].Frequency != "5975" || events[2].Tone != "warn" || strings.Contains(string(encoded), "do-not-export") {
		t.Fatal(string(encoded))
	}
	path := filepath.Join(t.TempDir(), "hostapd.log")
	fixtureFile(t, path, strings.Repeat("x", 300000)+"\n"+raw)
	tail, err := tailEventFile(path)
	if err != nil || len(tail) > 256*1024 || !strings.HasPrefix(tail, "2026.") {
		t.Fatal("invalid bounded tail", err)
	}
	var lines strings.Builder
	for i := 0; i < 180; i++ {
		fmt.Fprintf(&lines, "2026.10.03 00:%02d:%02d: ath23: AP-ENABLED\n", i/60, i%60)
	}
	fixtureFile(t, path, lines.String())
	rotated := path + ".1"
	fixtureFile(t, rotated, lines.String())
	log := readWirelessEvents([]string{path, rotated}, nil)
	if len(log.Events) != 150 || log.Events[0].Time != "2026.10.03 00:02:59" || log.Error != "" {
		t.Fatal("history order/deduplication/limit incorrect")
	}
	if readWirelessEvents([]string{path + "missing"}, nil).Error == "" {
		t.Fatal("unavailable logs shown as empty valid history")
	}
	fixtureFile(t, path, strings.Repeat("x", 300000))
	tail, err = tailEventFile(path)
	if err != nil || tail != "" {
		t.Fatal("returned incomplete log line")
	}
}
