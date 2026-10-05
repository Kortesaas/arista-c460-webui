package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokensPersistenceRevocationAndHashing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tokens.json")
	s := NewTokenStore(file)
	meta, secret, err := s.create(tokenRequest{Name: "Pi monitor", ExpiresDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), secret) {
		t.Fatal("plaintext token persisted")
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("token file is not private")
	}
	metaJSON, _ := json.Marshal(meta)
	if strings.Contains(string(metaJSON), "hash") || strings.Contains(string(metaJSON), secret) {
		t.Fatal("metadata reveals secret")
	}
	s = NewTokenStore(file)
	principal, ok := s.authenticate(secret)
	if !ok || principal.User != "API: Pi monitor" || len(principal.Scopes) != 1 || principal.Scopes[0] != "monitor" {
		t.Fatalf("wrong persisted principal: %+v", principal)
	}
	if _, ok := s.authenticate(secret + "x"); ok {
		t.Fatal("invalid token accepted")
	}
	if found, err := s.revoke(meta.ID); !found || err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := NewTokenStore(file).authenticate(secret); ok {
		t.Fatal("revoked token still valid after restart")
	}
}

func TestTokenValidationExpiryAndCorruptFile(t *testing.T) {
	s := NewTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	for _, req := range []tokenRequest{{Name: ""}, {Name: "a\x00b"}, {Name: "Pi", Scopes: []string{"admin"}}, {Name: "Pi", ExpiresDays: -1}, {Name: "Pi", ExpiresDays: 3651}} {
		if _, _, err := s.create(req); err == nil {
			t.Fatalf("accepted invalid request: %+v", req)
		}
	}
	_, secret, err := s.create(tokenRequest{Name: "Pi", Scopes: []string{"configure", "configure"}, ExpiresDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.create(tokenRequest{Name: "pi"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	past := time.Now().Add(-time.Second)
	s.tokens[0].ExpiresAt = &past
	if _, ok := s.authenticate(secret); ok {
		t.Fatal("expired token accepted")
	}
	if err := os.WriteFile(s.file, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := NewTokenStore(s.file)
	if _, ok := broken.authenticate(secret); ok {
		t.Fatal("corrupt store accepted token")
	}
	if _, _, err := broken.create(tokenRequest{Name: "new"}); err == nil {
		t.Fatal("corrupt store overwritten")
	}
}

func TestTokenGuardScopesAndCookieCompatibility(t *testing.T) {
	auth := NewAuth(filepath.Join(t.TempDir(), "auth.json"))
	a := &API{auth: auth, changes: NewChangeLog("")}
	for _, tc := range []struct {
		pattern string
		scopes  []string
		allowed bool
	}{
		{"GET /api/clients", nil, true},
		{"PUT /api/management", nil, false},
		{"PUT /api/management", []string{"configure"}, true},
		{"POST /api/reboot", []string{"configure"}, false},
		{"POST /api/reboot", []string{"control"}, true},
		{"GET /api/ssids/{name}/join", nil, false},
		{"GET /api/ssids/{name}/join", []string{"secrets"}, true},
		{"POST /api/restore", []string{"configure", "secrets"}, false},
		{"POST /api/restore", []string{"configure", "control", "secrets"}, true},
		{"POST /api/tokens", apiScopes, false},
		{"GET /api/tokens", apiScopes, false},
		{"POST /api/password", apiScopes, false},
		{"PUT /api/viewer", apiScopes, false},
		{"DELETE /api/ssids/{name}", []string{"configure"}, true},
		{"GET /api/ssids/{name}/policy", nil, true},
		{"PUT /api/ssids/{name}/policy", nil, false},
		{"PUT /api/ssids/{name}/policy", []string{"control"}, false},
		{"PUT /api/ssids/{name}/policy", []string{"configure"}, true},
		{"GET /api/captures", nil, true},
		{"POST /api/captures", []string{"control"}, false},
		{"POST /api/captures", []string{"secrets"}, false},
		{"POST /api/captures", []string{"control", "secrets"}, true},
		{"GET /api/captures/{id}/download", nil, false},
		{"GET /api/captures/{id}/download", []string{"secrets"}, true},
		{"POST /api/captures/{id}/stop", []string{"control", "secrets"}, true},
		{"DELETE /api/captures/{id}", []string{"configure"}, false},
		{"DELETE /api/captures/{id}", []string{"control", "secrets"}, true},
		{"POST /api/support-bundle", nil, true},
	} {
		t.Run(tc.pattern+strings.Join(tc.scopes, "-"), func(t *testing.T) {
			meta, secret, err := auth.tokens.create(tokenRequest{Name: tc.pattern + strings.Join(tc.scopes, "-"), Scopes: tc.scopes})
			if err != nil {
				t.Fatal(err)
			}
			defer auth.tokens.revoke(meta.ID)
			method, path, _ := strings.Cut(tc.pattern, " ")
			r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
			r.Pattern = tc.pattern
			r.Header.Set("Authorization", "Bearer "+secret)
			r.Header.Set("Content-Type", "application/json")
			calls := 0
			w := httptest.NewRecorder()
			a.guard(func(w http.ResponseWriter, r *http.Request) { calls++; reply(w, 200, map[string]bool{"ok": true}) }, method != "GET" || strings.Contains(path, "join") || strings.Contains(path, "tokens")).ServeHTTP(w, r)
			if tc.allowed && (w.Code != 200 || calls != 1) || !tc.allowed && (w.Code != 403 || calls != 0) {
				t.Fatalf("status=%d calls=%d", w.Code, calls)
			}
		})
	}
	w := httptest.NewRecorder()
	if err := auth.NewSession(w, "config", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("DELETE", "/api/ssids/demo", nil)
	r.Pattern = "DELETE /api/ssids/{name}"
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	a.write(func(w http.ResponseWriter, r *http.Request) { reply(w, 200, true) }).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cookie DELETE lost CSRF check")
	}
	r.Header.Set("X-Requested-With", "c460-webui")
	w = httptest.NewRecorder()
	a.write(func(w http.ResponseWriter, r *http.Request) { reply(w, 200, true) }).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("existing browser session broken")
	}
	r.Header.Set("Authorization", "Bearer invalid")
	if _, ok := auth.Session(r); ok {
		t.Fatal("invalid bearer fell back to cookie")
	}
}

func TestVersionedResourcesAndSpecification(t *testing.T) {
	auth := NewAuth(filepath.Join(t.TempDir(), "auth.json"))
	_, secret, err := auth.tokens.create(tokenRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	a := &API{auth: auth, changes: NewChangeLog(""), poller: &Poller{state: APState{GeneratedAt: time.Now(), PollSeconds: 5, Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", SSID: "Test Network", Band: "6"}, {MAC: "11:22:33:44:55:66", SSID: "Other", Band: "5"}}}}}
	mux := http.NewServeMux()
	a.Register(mux)
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := get("/api/v1/clients?ssid=Test+Network&band=6")
	if w.Code != 200 || w.Header().Get("X-API-Version") != "1" || w.Header().Get("X-Poll-Seconds") != "5" || strings.Contains(w.Body.String(), "Other") {
		t.Fatalf("filtered resource: %d %s", w.Code, w.Body.String())
	}
	if get("/api/v1/clients/AA:BB:CC:DD:EE:FF").Code != 200 {
		t.Fatal("versioned MAC path lookup failed")
	}
	if get("/api/v1/clients/not-connected").Code != 404 {
		t.Fatal("missing client not 404")
	}
	w = get("/api/v1/openapi.json")
	var spec struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, methods := range spec.Paths {
		count += len(methods)
	}
	if spec.OpenAPI != "3.0.3" || count != len(a.endpoints) || count < 60 {
		t.Fatalf("incomplete specification: %d endpoints", count)
	}
	if get("/api/v1/docs").Code != 200 {
		t.Fatal("embedded guide unavailable")
	}
	if get("/api/clients?band=6").Code != 200 {
		t.Fatal("legacy route broken")
	}
	if get("/api/v1/tokens").Code != 403 {
		t.Fatal("monitor can list tokens")
	}
	if a.isAdmin(httptest.NewRequest("GET", "/api/snmp", nil)) {
		t.Fatal("anonymous secret access")
	}
	r := httptest.NewRequest("GET", "/api/snmp", nil)
	r.Header.Set("Authorization", "Bearer "+secret)
	if a.isAdmin(r) {
		t.Fatal("monitor secret access")
	}
}

func TestIncompleteVersionedWritesAreRejectedBeforeChangingAP(t *testing.T) {
	auth := NewAuth(filepath.Join(t.TempDir(), "auth.json"))
	_, secret, err := auth.tokens.create(tokenRequest{Name: "writer", Scopes: []string{"configure"}})
	if err != nil {
		t.Fatal(err)
	}
	a := &API{auth: auth, changes: NewChangeLog("")}
	mux := http.NewServeMux()
	a.Register(mux)
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/radios/0", `{"width":40}`},
		{"PUT", "/api/v1/ssids/Test", `{"name":"Test","enabled":false}`},
		{"POST", "/api/v1/batch", `{"changes":[{"kind":"radio","id":0,"radio":{"width":40}}]}`},
		{"PUT", "/api/v1/radios/0/wifi7", `{"width":320,"enabled":null}`},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "complete settings") {
			t.Fatalf("unsafe partial write accepted: %s %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}
