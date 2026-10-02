package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSSIDFeaturePreservation(t *testing.T) {
	original := map[string]any{"name": "test", "wpa3-psk": "private", "default-vlan": float64(99), "dot11k": true, "qbss-load": true, "dot11v": map[string]any{"bss-transition": true}}
	off := false
	cfg, deletes := featuresConfig(SSIDFeaturesInput{RRM: nil, Load: &off}, original)
	if len(deletes) != 1 || deletes[0] != "dot11k" || cfg["qbss-load"] != false || cfg["wpa3-psk"] != "private" || cfg["default-vlan"] != float64(99) || !reflect.DeepEqual(cfg["dot11v"], original["dot11v"]) {
		t.Fatal("lost unrelated configuration", cfg, deletes)
	}
	if original["dot11k"] != true || original["qbss-load"] != true {
		t.Fatal("mutated snapshot")
	}
	if configuredBool(map[string]any{}, "qbss-load") != nil {
		t.Fatal("missing feature is not a false value")
	}
	for _, text := range []string{"get_rrm:1", "ath0 get_rrm: 1\n"} {
		b, e := parseDriverBool(text, "get_rrm")
		if e != nil || b == nil || !*b {
			t.Fatal(text, e)
		}
	}
	for _, text := range []string{"get_rrm: 2", "get_wnm: 1", "unsupported"} {
		if _, e := parseDriverBool(text, "get_rrm"); e == nil {
			t.Fatal("accepted unrelated driver output", text)
		}
	}
	for _, body := range []string{`{}`, `{"rrm":true}`, `{"rrm":"true","load":null}`, `{"rrm":null,"load":0}`, `{"rrm":false,"load":false,"shell":"reboot"}`} {
		w := httptest.NewRecorder()
		(&API{}).updateSSIDFeatures(w, httptest.NewRequest("PUT", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal("invalid request accepted", body, w.Code)
		}
	}
}
func TestLLDPParsing(t *testing.T) {
	timing, e := parseLLDPTiming(`{"configuration":{"config":{"tx-delay":"30","tx-hold":"4"}}}`)
	if e != nil || timing != (LLDPTiming{30, 4}) {
		t.Fatal(timing, e)
	}
	if _, e = parseLLDPTiming(`{"configuration":{}}`); e == nil {
		t.Fatal("accepted unavailable timing")
	}
	peer := `{"eth0":{"age":"0 day, 00:00:04","chassis":{"switch":{"id":{"value":"aa:bb:cc:dd:ee:ff"},"descr":"Dell","mgmt-ip":["192.168.99.10","fe80::1"]}},"port":{"id":{"value":"Gi1/0/1"},"descr":"Trunk","ttl":"120"}}}`
	for _, raw := range []string{`{"lldp":{"interface":` + peer + `}}`, `{"lldp":{"interface":[` + peer + `]}}`} {
		neighbors, e := parseLLDPNeighbors(raw)
		if e != nil || len(neighbors) != 1 || neighbors[0].Name != "switch" || neighbors[0].PortID != "Gi1/0/1" || len(neighbors[0].Addresses) != 2 || neighbors[0].TTL != "120" {
			t.Fatal(neighbors, e)
		}
	}
	empty, e := parseLLDPNeighbors(`{"lldp":{}}`)
	if e != nil || empty == nil || len(empty) != 0 {
		t.Fatal(empty, e)
	}
	for _, s := range []LLDPTiming{{4, 4}, {3601, 4}, {30, 1}, {30, 11}} {
		if s.validate() == nil {
			t.Fatal("invalid timing", s)
		}
	}
}
func TestLLDPTransaction(t *testing.T) {
	for _, scenario := range []string{"success", "partial apply", "storage failure"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "desired.json")
			if scenario == "storage failure" {
				path = filepath.Join(t.TempDir(), "missing", "desired.json")
			}
			before := LLDPTiming{30, 4}
			current := before
			failed := false
			run := func(_ context.Context, _ string, args ...string) (string, error) {
				if args[0] == "-f" {
					return fmt.Sprintf(`{"configuration":{"config":{"tx-delay":"%d","tx-hold":"%d"}}}`, current.Interval, current.Hold), nil
				}
				n, _ := strconv.Atoi(args[3])
				if args[2] == "tx-interval" {
					current.Interval = n
				} else {
					if scenario == "partial apply" && !failed {
						failed = true
						return "", errors.New("daemon refused hold")
					}
					current.Hold = n
				}
				return "", nil
			}
			err := (lldpBackend{path: path, run: run}).save(context.Background(), LLDPTiming{31, 5})
			saved, e := readSavedLLDP(path)
			if scenario == "success" {
				if err != nil || e != nil || saved == nil || *saved != current || current != (LLDPTiming{31, 5}) {
					t.Fatal(current, saved, err, e)
				}
			} else {
				if err == nil || current != before || saved != nil {
					t.Fatal("failed update not rolled back", current, saved, err)
				}
			}
		})
	}
}
func TestTCPDiagnostic(t *testing.T) {
	for _, input := range []DiagnosticInput{{Tool: "tcp", Target: "127.0.0.1", Port: 0}, {Tool: "tcp", Target: "127.0.0.1", Port: 65536}, {Tool: "tcp", Target: "x;reboot", Port: 80}, {Tool: "ping", Target: "127.0.0.1", Port: 80}} {
		if _, e := diagnosticArgs(input); e == nil {
			t.Fatal("accepted", input)
		}
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	for _, open := range []bool{true, false} {
		if !open {
			listener.Close()
		}
		w := httptest.NewRecorder()
		(&API{}).diagnose(w, httptest.NewRequest("POST", "/", strings.NewReader(fmt.Sprintf(`{"tool":"tcp","target":"127.0.0.1","port":%d}`, port))))
		var result DiagnosticResult
		json.Unmarshal(w.Body.Bytes(), &result)
		if w.Code != 200 || result.Success != open || result.Port != port {
			t.Fatal(w.Code, result)
		}
	}
}
func TestNewFeatureAuthentication(t *testing.T) {
	a := &API{auth: NewAuth(filepath.Join(t.TempDir(), "auth.json"))}
	mux := http.NewServeMux()
	a.Register(mux)
	for _, route := range []struct{ method, path string }{{"GET", "/api/lldp"}, {"PUT", "/api/lldp"}, {"GET", "/api/ssids/test/features"}, {"PUT", "/api/ssids/test/features"}, {"POST", "/api/diagnostics"}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
		if w.Code != 401 {
			t.Fatal(route, w.Code)
		}
	}
}
