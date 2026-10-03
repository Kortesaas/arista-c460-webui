// Command c460-webui is a self-contained local management UI for the C-460
// access point. It runs on the AP itself, talks to the AP's local OpenConfig
// (gNMI) agent and serves an embedded single-page app.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed all:web/dist
var webFS embed.FS

var version = "dev"

// Config is read from a JSON file on the AP. Secrets live only there.
type Config struct {
	mu          sync.RWMutex
	path        string
	Listen      string                  `json:"listen"`
	HTTPSListen string                  `json:"httpsListen"` // "" = ":443"; "off" disables HTTPS
	TLSDir      string                  `json:"tlsDir"`
	Hostname    string                  `json:"hostname"` // gNMI access-point key; derived from the eth0 MAC when empty
	PollSeconds int                     `json:"pollSeconds"`
	AuthFile    string                  `json:"authFile"`
	SiteName    string                  `json:"siteName"`  // optional label shown in the UI
	VLANNames   map[string]string       `json:"vlanNames"` // optional, e.g. {"10": "Control"}
	GNMI        GNMIConfig              `json:"gnmi"`
	SNMP        SNMPSettings            `json:"snmp"`
	Metrics     MetricsSettings         `json:"metrics"`
	TimeZone    string                  `json:"timeZone"`  // IANA name, e.g. "Europe/Berlin"; schedules use it
	Schedules   map[string]SSIDSchedule `json:"schedules"` // per SSID
	ChangeLog   string                  `json:"changeLog"` // file; default next to the config
}

type GNMIConfig struct {
	Address    string `json:"address"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	CertFile   string `json:"certFile"`
	ServerName string `json:"serverName"`
	Origin     string `json:"origin"`
}

func loadConfig(path string) (*Config, error) {
	cfg := &Config{
		Listen:      ":80",
		PollSeconds: 5,
		AuthFile:    "/opt/c460-webui/auth.json",
		GNMI: GNMIConfig{
			Address:    "127.0.0.1:8080",
			CertFile:   "/opt/openconfig/cert/agent.crt",
			ServerName: "openconfig.mojonetworks.com",
			Origin:     "openconfig.mojonetworks.com",
		},
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.GNMI.Username == "" || cfg.GNMI.Password == "" {
		return nil, errors.New("gnmi.username and gnmi.password are required")
	}
	cfg.path = path
	if cfg.PollSeconds < 1 {
		cfg.PollSeconds = 1
	}
	if cfg.PollSeconds > 60 {
		cfg.PollSeconds = 60
	}
	if cfg.Hostname == "" {
		mac, err := os.ReadFile("/sys/class/net/eth0/address")
		if err != nil {
			return nil, fmt.Errorf("hostname not configured and eth0 MAC unreadable: %w", err)
		}
		cfg.Hostname = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(string(mac)), ":", "-"))
	}
	return cfg, nil
}

// Labels returns the user-editable display settings.
func (c *Config) Labels() (string, map[string]string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make(map[string]string, len(c.VLANNames))
	for k, v := range c.VLANNames {
		names[k] = v
	}
	return c.SiteName, names
}

// RefreshSeconds reads the shared sampling interval safely.
func (c *Config) RefreshSeconds() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.PollSeconds
}
func (c *Config) saveLocked() error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicNative(c.path, raw, 0600)
}
func (c *Config) SetRefresh(seconds int) error {
	if seconds < 1 || seconds > 60 {
		return errors.New("refresh interval must be 1–60 seconds")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.PollSeconds
	c.PollSeconds = seconds
	if err := c.saveLocked(); err != nil {
		c.PollSeconds = previous
		return err
	}
	return nil
}

// SetLabels updates the display settings and preserves all other configuration.
func (c *Config) SetLabels(site string, names map[string]string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	oldSite, oldNames := c.SiteName, c.VLANNames
	c.SiteName, c.VLANNames = site, names
	if err := c.saveLocked(); err != nil {
		c.SiteName, c.VLANNames = oldSite, oldNames
		return err
	}
	return nil
}

func main() {
	configPath := flag.String("config", "/opt/c460-webui/config.json", "configuration file")
	setPassword := flag.Bool("set-password", false, "read a new UI password from stdin and store its hash")
	username := flag.String("username", "", "login name stored with -set-password (default: keep current, or \""+DefaultUsername+"\")")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	auth := NewAuth(cfg.AuthFile)

	if *setPassword {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			log.Fatalf("read password: %v", err)
		}
		user := *username
		if user == "" {
			user = auth.Username()
		}
		if err := auth.SetCredentials(user, strings.TrimRight(line, "\r\n")); err != nil {
			log.Fatalf("set password: %v", err)
		}
		fmt.Printf("credentials updated for user %q\n", user)
		return
	}

	// Restore locally selected native settings before accepting browser writes.
	if err := restoreTimeSettings(context.Background()); err != nil {
		log.Printf("restore time settings: %v", err)
	}

	gnmi, err := DialGNMI(cfg.GNMI, cfg.Hostname)
	if err != nil {
		log.Fatalf("gnmi: %v", err)
	}
	defer gnmi.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	poller := NewPoller(gnmi, cfg, time.Duration(cfg.PollSeconds)*time.Second)
	cliInfo := &CLIInfo{}
	cliTrigger := make(chan struct{}, 1)
	go cliInfo.Run(ctx, 5*time.Minute, cliTrigger)

	dist, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	snmp := NewSNMPAgent(cfg, poller.Snapshot)
	if err := snmp.Apply(); err != nil {
		log.Printf("%v", err)
	}
	defer snmp.Close()
	changeLogFile := cfg.ChangeLog
	if changeLogFile == "" {
		changeLogFile = filepath.Join(filepath.Dir(*configPath), "changes.json")
	}
	history := NewHistory()
	poller.OnSample(history.Observe)
	api := &API{cfg: cfg, auth: auth, gnmi: gnmi, poller: poller, cli: cliInfo, cliTrigger: cliTrigger, snmp: snmp,
		changes: NewChangeLog(changeLogFile), history: history, overrides: NewWirelessOverrides(filepath.Join(filepath.Dir(*configPath), "wireless-overrides.json"))}
	mux := http.NewServeMux()
	api.Register(mux)
	mux.HandleFunc("GET /metrics", api.metricsEndpoint)
	go api.maintainLLDP(ctx)
	go api.runEnforcer(ctx)
	go api.runScheduler(ctx)
	go poller.Run(ctx)
	mux.Handle("/", spaHandler(dist))

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	var httpsServer *http.Server
	if cfg.HTTPSListen != "off" {
		addr := cfg.HTTPSListen
		if addr == "" {
			addr = ":443"
		}
		dir := cfg.TLSDir
		if dir == "" {
			dir = filepath.Join(filepath.Dir(*configPath), "tls")
		}
		if cert, err := loadOrCreateCertificate(dir, cfg.Hostname); err != nil {
			log.Printf("https disabled: %v", err)
		} else {
			httpsServer = &http.Server{
				Addr:              addr,
				Handler:           server.Handler,
				TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
				ReadHeaderTimeout: server.ReadHeaderTimeout,
				ReadTimeout:       server.ReadTimeout,
				WriteTimeout:      server.WriteTimeout,
				IdleTimeout:       server.IdleTimeout,
			}
			go func() {
				log.Printf("https listening on %s", addr)
				if err := httpsServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Printf("https: %v", err)
				}
			}()
		}
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		if httpsServer != nil {
			_ = httpsServer.Shutdown(shutdown)
		}
	}()
	log.Printf("c460-webui %s listening on %s (access point %s)", version, cfg.Listen, cfg.Hostname)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// spaHandler serves embedded assets and falls back to index.html for client routes.
func spaHandler(dist fs.FS) http.Handler {
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "UI not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
