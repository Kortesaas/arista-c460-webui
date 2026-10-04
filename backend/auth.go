package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie   = "c460_session"
	sessionLifetime = 12 * time.Hour
	maxFailures     = 5
	failureWindow   = 5 * time.Minute
)

// Auth holds the administrator and an optional read-only account (bcrypt)
// and in-memory sessions.
type Auth struct {
	mu       sync.Mutex
	file     string
	sessions map[string]Session
	failures map[string][]time.Time
}

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

type Session struct {
	User    string
	Role    Role
	expires time.Time
}

type authAccount struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
}

type authFile struct {
	Username     string       `json:"username"`
	PasswordHash string       `json:"passwordHash"`
	Viewer       *authAccount `json:"viewer,omitempty"`
}

// DefaultUsername matches the AP's own CLI account name.
const DefaultUsername = "config"

func NewAuth(file string) *Auth {
	return &Auth{file: file, sessions: map[string]Session{}, failures: map[string][]time.Time{}}
}

func (a *Auth) load() (authFile, error) {
	var f authFile
	raw, err := os.ReadFile(a.file)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, err
	}
	if f.PasswordHash == "" {
		return f, errors.New("no password set")
	}
	if f.Username == "" {
		f.Username = DefaultUsername
	}
	return f, nil
}

func (a *Auth) Configured() bool {
	_, err := a.load()
	return err == nil
}

// Username returns the configured login name (DefaultUsername when unset).
func (a *Auth) Username() string {
	if f, err := a.load(); err == nil {
		return f.Username
	}
	return DefaultUsername
}

func validUsername(name string) error {
	if len(name) < 1 || len(name) > 32 {
		return errors.New("username must be 1–32 characters")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return errors.New("username may only contain letters, digits, '.', '_' and '-'")
		}
	}
	return nil
}

func hashPassword(username, password string) (string, error) {
	if err := validUsername(username); err != nil {
		return "", err
	}
	if len(password) < 6 {
		return "", errors.New("password must be at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func (a *Auth) save(f authFile) error {
	raw, _ := json.Marshal(f)
	if err := os.MkdirAll(filepath.Dir(a.file), 0o700); err != nil {
		return err
	}
	tmp := a.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.file)
}

// SetCredentials stores the administrator's username and bcrypt password
// hash; a read-only account is kept.
func (a *Auth) SetCredentials(username, password string) error {
	hash, err := hashPassword(username, password)
	if err != nil {
		return err
	}
	f, _ := a.load()
	if f.Viewer != nil && strings.EqualFold(f.Viewer.Username, username) {
		return errors.New("the read-only account already uses this username")
	}
	f.Username, f.PasswordHash = username, hash
	return a.save(f)
}

// ViewerName returns the read-only account's username, or "" without one.
func (a *Auth) ViewerName() string {
	if f, err := a.load(); err == nil && f.Viewer != nil {
		return f.Viewer.Username
	}
	return ""
}

// SetViewer creates or replaces the read-only account and signs out its sessions.
func (a *Auth) SetViewer(username, password string) error {
	f, err := a.load()
	if err != nil {
		return errNotConfigured
	}
	if strings.EqualFold(f.Username, username) {
		return errors.New("the administrator already uses this username")
	}
	hash, err := hashPassword(username, password)
	if err != nil {
		return err
	}
	f.Viewer = &authAccount{Username: username, PasswordHash: hash}
	if err := a.save(f); err != nil {
		return err
	}
	a.endRole(RoleViewer)
	return nil
}

// RemoveViewer deletes the read-only account and signs out its sessions.
func (a *Auth) RemoveViewer() error {
	f, err := a.load()
	if err != nil {
		return errNotConfigured
	}
	f.Viewer = nil
	if err := a.save(f); err != nil {
		return err
	}
	a.endRole(RoleViewer)
	return nil
}

func (a *Auth) endRole(role Role) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for token, s := range a.sessions {
		if s.Role == role {
			delete(a.sessions, token)
		}
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Check verifies username and password, rate limiting failures per client
// address, and returns the role of the matching account.
func (a *Auth) Check(r *http.Request, username, password string) (Role, error) {
	ip := clientIP(r)
	a.mu.Lock()
	now := time.Now()
	recent := a.failures[ip][:0]
	for _, t := range a.failures[ip] {
		if now.Sub(t) < failureWindow {
			recent = append(recent, t)
		}
	}
	a.failures[ip] = recent
	locked := len(recent) >= maxFailures
	a.mu.Unlock()
	if locked {
		return "", errTooManyAttempts
	}
	f, err := a.load()
	if err != nil {
		return "", errNotConfigured
	}
	account, role := authAccount{Username: f.Username, PasswordHash: f.PasswordHash}, RoleAdmin
	if f.Viewer != nil && subtle.ConstantTimeCompare([]byte(username), []byte(f.Viewer.Username)) == 1 {
		account, role = *f.Viewer, RoleViewer
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(account.Username)) == 1
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil || !userOK {
		a.mu.Lock()
		a.failures[ip] = append(a.failures[ip], now)
		a.mu.Unlock()
		time.Sleep(400 * time.Millisecond)
		return "", errBadPassword
	}
	a.mu.Lock()
	delete(a.failures, ip)
	a.mu.Unlock()
	return role, nil
}

var (
	errTooManyAttempts = errors.New("too many failed attempts, try again in a few minutes")
	errNotConfigured   = errors.New("no UI password configured on this access point")
	errBadPassword     = errors.New("wrong username or password")
)

func (a *Auth) NewSession(w http.ResponseWriter, user string, role Role) error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	token := hex.EncodeToString(buf)
	a.mu.Lock()
	now := time.Now()
	for t, s := range a.sessions {
		if now.After(s.expires) {
			delete(a.sessions, t)
		}
	}
	a.sessions[token] = Session{User: user, Role: role, expires: now.Add(sessionLifetime)}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLifetime.Seconds()),
	})
	return nil
}

// Session returns the caller's session, if it is valid.
func (a *Auth) Session(r *http.Request) (Session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return Session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if !ok || time.Now().After(s.expires) {
		delete(a.sessions, c.Value)
		return Session{}, false
	}
	return s, true
}

func (a *Auth) Valid(r *http.Request) bool {
	_, ok := a.Session(r)
	return ok
}

// EndSession removes the caller's session; with others=true every other session is dropped instead.
func (a *Auth) EndSession(w http.ResponseWriter, r *http.Request, others bool) {
	c, _ := r.Cookie(sessionCookie)
	a.mu.Lock()
	defer a.mu.Unlock()
	if others {
		for t := range a.sessions {
			if c == nil || t != c.Value {
				delete(a.sessions, t)
			}
		}
		return
	}
	if c != nil {
		delete(a.sessions, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
