package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func supportContents(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if json.Unmarshal(files[f.Name], &v) != nil {
			t.Fatal("invalid JSON in support archive", f.Name)
		}
	}
	return files
}

func TestSupportAllowlistAndClientOptIn(t *testing.T) {
	st := APState{GeneratedAt: time.Now(), Error: "password=raw-error-secret", Device: Device{Hostname: "test-ap"},
		Radios:    []Radio{{Band: "6", WiFi7: &WiFi7State{OperatingWidth: 320, Error: "key=driver-secret"}}},
		SSIDs:     []SSID{{Name: "Office", MixedStatus: "raw-secret-status", HasPassword: true}},
		Clients:   []Client{{MAC: "02:00:00:00:00:01", IPv4: "192.0.2.1", Username: "account-secret", Hostname: "client-host"}},
		Neighbors: []Neighbor{{SSID: "private-neighbour"}},
	}
	for _, include := range []bool{false, true} {
		clean := supportState(st, include)
		raw, err := makeSupportArchive(map[string]any{"state.json": clean}, supportManifest{Format: "c460-webui-support", Version: 1, CreatedAt: time.Now(), Includes: SupportInput{IncludeClients: include}})
		if err != nil {
			t.Fatal(err)
		}
		files := supportContents(t, raw)
		text := string(files["state.json"])
		for _, secret := range []string{"raw-error-secret", "driver-secret", "raw-secret-status", "account-secret", "private-neighbour"} {
			if strings.Contains(text, secret) {
				t.Fatal("allowlist leaked", secret)
			}
		}
		if strings.Contains(text, "02:00:00:00:00:01") != include || !strings.Contains(text, `"operatingWidth": 320`) {
			t.Fatal("opt-in or operating state lost", text)
		}
		var manifest supportManifest
		_ = json.Unmarshal(files["manifest.json"], &manifest)
		if len(manifest.Files) != 2 || manifest.Includes.IncludeClients != include {
			t.Fatal("archive manifest does not describe content")
		}
	}
	if st.Error == "" || st.Radios[0].WiFi7.Error == "" || st.Clients[0].Username == "" {
		t.Fatal("support redaction mutated live snapshot")
	}
}

func TestSupportArchiveBoundAndClientLimit(t *testing.T) {
	if _, err := makeSupportArchive(map[string]any{"large.json": strings.Repeat("x", supportMaxBytes)}, supportManifest{}); err == nil {
		t.Fatal("oversized archive accepted")
	}
	st := APState{Clients: make([]Client, 600)}
	if len(supportState(st, true).Clients) != 512 || len(st.Clients) != 600 {
		t.Fatal("client collection cap failed")
	}
}

func TestSupportBundleHTTPViewerAndNoConfigSecrets(t *testing.T) {
	a := &API{auth: NewAuth(filepath.Join(t.TempDir(), "auth.json")), cfg: &Config{GNMI: GNMIConfig{Password: "gnmi-secret"}, Metrics: MetricsSettings{Token: "metrics-secret"}}, poller: &Poller{state: APState{GeneratedAt: time.Now().Add(-time.Minute), Error: "password=raw-error-secret", Device: Device{Hostname: "fixture-ap"}, Clients: []Client{{MAC: "02:00:00:00:00:01", Username: "client-secret"}}}}, cli: &CLIInfo{}, wirelessDir: t.TempDir()}
	mux := http.NewServeMux()
	a.Register(mux)
	r := httptest.NewRequest("POST", "/api/support-bundle", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated bundle", w.Code)
	}
	session := httptest.NewRecorder()
	_ = a.auth.NewSession(session, "observer", RoleViewer)
	r = httptest.NewRequest("POST", "/api/v1/support-bundle", strings.NewReader(`{"includeClients":false,"includeEvents":false}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(session.Result().Cookies()[0])
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	files := supportContents(t, w.Body.Bytes())
	for name, raw := range files {
		for _, secret := range []string{"gnmi-secret", "metrics-secret", "client-secret", "raw-error-secret", "02:00:00:00:00:01"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatal("bundle leaked", name, secret)
			}
		}
	}
	if files["events.json"] != nil || files["network.json"] == nil || files["wireless-status.json"] == nil {
		t.Fatal("bundle selection incorrect")
	}
	var manifest supportManifest
	_ = json.Unmarshal(files["manifest.json"], &manifest)
	if len(manifest.Warnings) < 2 || len(manifest.Excludes) < 5 {
		t.Fatal("missing provenance/redaction contract", manifest)
	}
}

func TestCaptureAndBundleOpenAPIContracts(t *testing.T) {
	a := &API{}
	a.Register(http.NewServeMux())
	paths := a.specification()["paths"].(map[string]any)
	for path, media := range map[string]string{"/api/v1/support-bundle": "application/zip", "/api/v1/captures/{id}/download": "application/vnd.tcpdump.pcap"} {
		method := "get"
		if path == "/api/v1/support-bundle" {
			method = "post"
		}
		op := paths[path].(map[string]any)[method].(map[string]any)
		content := op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
		if content[media] == nil {
			t.Fatal("wrong binary media type", path)
		}
	}
	op := paths["/api/v1/captures"].(map[string]any)["post"].(map[string]any)
	if op["responses"].(map[string]any)["202"] == nil {
		t.Fatal("capture acceptance status not documented")
	}
	schema := requestSchema("POST /api/captures")
	p := schema["properties"].(map[string]any)
	if p["seconds"].(map[string]any)["maximum"] != captureMaxSeconds || p["maxBytes"].(map[string]any)["maximum"] != captureMaxBytes {
		t.Fatal("documented cap differs from runtime")
	}
}
