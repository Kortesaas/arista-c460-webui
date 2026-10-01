// Command c460-webui is a self-contained local management UI for the C-460
// access point. It runs on the AP itself, talks to the AP's local OpenConfig
// (gNMI) agent and serves an embedded single-page app.
package main

import (
	"bufio"
	"context"
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
	"strings"
	"syscall"
	"time"
)

//go:embed all:web/dist
var webFS embed.FS

var version = "dev"

// Config is read from a JSON file on the AP. Secrets live only there.
type Config struct {
	Listen      string            `json:"listen"`
	Hostname    string            `json:"hostname"` // gNMI access-point key; derived from the eth0 MAC when empty
	PollSeconds int               `json:"pollSeconds"`
	AuthFile    string            `json:"authFile"`
	SiteName    string            `json:"siteName"`  // optional label shown in the UI
	VLANNames   map[string]string `json:"vlanNames"` // optional, e.g. {"10": "Control"}
	GNMI        GNMIConfig        `json:"gnmi"`
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
	if cfg.PollSeconds < 2 {
		cfg.PollSeconds = 2
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

	gnmi, err := DialGNMI(cfg.GNMI, cfg.Hostname)
	if err != nil {
		log.Fatalf("gnmi: %v", err)
	}
	defer gnmi.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	poller := NewPoller(gnmi, cfg, time.Duration(cfg.PollSeconds)*time.Second)
	go poller.Run(ctx)

	dist, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	api := &API{cfg: cfg, auth: auth, gnmi: gnmi, poller: poller}
	mux := http.NewServeMux()
	api.Register(mux)
	mux.Handle("/", spaHandler(dist))

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
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
