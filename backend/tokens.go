package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var apiScopes = []string{"monitor", "configure", "control", "secrets"}

type APIToken struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type storedToken struct {
	APIToken
	Hash string `json:"hash"`
}

type TokenStore struct {
	mu      sync.Mutex
	file    string
	tokens  []storedToken
	loadErr error
}

func NewTokenStore(file string) *TokenStore {
	s := &TokenStore{file: file}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return s
	}
	if err == nil {
		err = json.Unmarshal(raw, &s.tokens)
	}
	if err == nil {
		for _, t := range s.tokens {
			hash, e := hex.DecodeString(t.Hash)
			if e != nil || len(hash) != sha256.Size || t.ID == "" || len(t.Scopes) == 0 {
				err = errors.New("invalid token record")
				break
			}
			for _, scope := range t.Scopes {
				if !slices.Contains(apiScopes, scope) {
					err = errors.New("invalid token scope")
				}
			}
		}
	}
	s.loadErr = err
	return s
}

func (s *TokenStore) list() ([]APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, errors.New("API token file could not be loaded")
	}
	out := make([]APIToken, 0, len(s.tokens))
	for _, t := range s.tokens {
		out = append(out, t.APIToken)
	}
	return out, nil
}

func (s *TokenStore) save(tokens []storedToken) error {
	raw, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	if err = atomicNative(s.file, raw, 0o600); err != nil {
		return err
	}
	s.tokens = tokens
	return nil
}

type tokenRequest struct {
	Name        string   `json:"name"`
	Scopes      []string `json:"scopes"`
	ExpiresDays int      `json:"expiresDays"` // 0 = no expiry; otherwise 1–3650
}

func (s *TokenStore) create(req tokenRequest) (APIToken, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return APIToken{}, "", errors.New("API token file could not be loaded")
	}
	if len(s.tokens) >= 32 {
		return APIToken{}, "", errors.New("revoke an existing token first (maximum 32)")
	}
	name := strings.TrimSpace(req.Name)
	if len(name) < 1 || len(name) > 64 || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return APIToken{}, "", errors.New("name must be 1–64 characters without control characters")
	}
	if req.ExpiresDays < 0 || req.ExpiresDays > 3650 {
		return APIToken{}, "", errors.New("expiresDays must be 0–3650")
	}
	scopes := []string{"monitor"}
	for _, scope := range req.Scopes {
		if !slices.Contains(apiScopes, scope) {
			return APIToken{}, "", errors.New("unknown permission: " + scope)
		}
		if !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	for _, t := range s.tokens {
		if strings.EqualFold(t.Name, name) {
			return APIToken{}, "", errors.New("a token already uses this name")
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return APIToken{}, "", err
	}
	secret := "c460_" + hex.EncodeToString(buf)
	hash := sha256.Sum256([]byte(secret))
	now := time.Now().UTC()
	t := APIToken{ID: hex.EncodeToString(buf[:8]), Name: name, Scopes: scopes, CreatedAt: now}
	if req.ExpiresDays != 0 {
		expiry := now.Add(time.Duration(req.ExpiresDays) * 24 * time.Hour)
		t.ExpiresAt = &expiry
	}
	next := append(append([]storedToken{}, s.tokens...), storedToken{APIToken: t, Hash: hex.EncodeToString(hash[:])})
	if err := s.save(next); err != nil {
		return APIToken{}, "", err
	}
	return t, secret, nil
}

func (s *TokenStore) revoke(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return false, errors.New("API token file could not be loaded")
	}
	next := make([]storedToken, 0, len(s.tokens))
	for _, t := range s.tokens {
		if t.ID != id {
			next = append(next, t)
		}
	}
	if len(next) == len(s.tokens) {
		return false, nil
	}
	return true, s.save(next)
}

func (s *TokenStore) authenticate(secret string) (Session, bool) {
	if len(secret) != 69 || !strings.HasPrefix(secret, "c460_") {
		return Session{}, false
	}
	hash := sha256.Sum256([]byte(secret))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return Session{}, false
	}
	for _, t := range s.tokens {
		want, _ := hex.DecodeString(t.Hash)
		if subtle.ConstantTimeCompare(hash[:], want) != 1 {
			continue
		}
		if t.ExpiresAt != nil && !time.Now().Before(*t.ExpiresAt) {
			return Session{}, false
		}
		return Session{User: "API: " + t.Name, Role: RoleAdmin, TokenID: t.ID, Scopes: t.Scopes}, true
	}
	return Session{}, false
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := a.auth.tokens.list()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	reply(w, 200, map[string]any{"tokens": tokens, "scopes": apiScopes})
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if !decode(w, r, &req) {
		return
	}
	t, secret, err := a.auth.tokens.create(req)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	reply(w, 201, map[string]any{"token": secret, "access": t})
}

func (a *API) revokeToken(w http.ResponseWriter, r *http.Request) {
	found, err := a.auth.tokens.revoke(r.PathValue("id"))
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if !found {
		fail(w, 404, "token not found")
		return
	}
	reply(w, 200, map[string]bool{"ok": true})
}

// Token permissions are explicit. Tokens cannot manage accounts or other tokens.
func tokenPermissions(pattern string) []string {
	switch pattern {
	case "POST /api/password", "PUT /api/viewer", "DELETE /api/viewer", "GET /api/tokens", "POST /api/tokens", "DELETE /api/tokens/{id}":
		return []string{"browser-admin"}
	case "GET /api/ssids/{name}/join", "POST /api/backup":
		return []string{"secrets"}
	case "POST /api/restore":
		return []string{"configure", "control", "secrets"}
	case "POST /api/reboot", "PUT /api/ssh", "POST /api/locate", "DELETE /api/locate", "POST /api/clients/{mac}/reconnect":
		return []string{"control"}
	case "POST /api/diagnostics":
		return []string{"monitor"}
	}
	if strings.HasPrefix(pattern, "GET ") {
		return []string{"monitor"}
	}
	return []string{"configure"}
}
