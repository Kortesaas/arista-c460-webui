package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DiagnosticInput struct {
	Tool   string `json:"tool"`
	Target string `json:"target"`
	Port   int    `json:"port,omitempty"`
}
type DiagnosticResult struct {
	Tool       string `json:"tool"`
	Target     string `json:"target"`
	Output     string `json:"output"`
	Success    bool   `json:"success"`
	TimedOut   bool   `json:"timedOut"`
	DurationMS int64  `json:"durationMs"`
	Port       int    `json:"port,omitempty"`
}

func diagnosticArgs(v DiagnosticInput) ([]string, error) {
	if !validHost(v.Target) {
		return nil, errors.New("target must be a hostname or IP address")
	}
	if v.Tool != "tcp" && v.Port != 0 {
		return nil, errors.New("port applies only to a TCP test")
	}
	switch v.Tool {
	case "tcp":
		if v.Port < 1 || v.Port > 65535 {
			return nil, errors.New("TCP port must be 1–65535")
		}
		return nil, nil
	case "ping":
		applet := "ping"
		if ip := net.ParseIP(v.Target); ip != nil && ip.To4() == nil {
			applet = "ping6"
		}
		return []string{applet, "-c", "4", "-W", "2", v.Target}, nil
	case "dns":
		return []string{"nslookup", v.Target}, nil
	case "trace":
		return []string{"traceroute", "-n", "-m", "8", "-q", "1", "-w", "1", v.Target}, nil
	}
	return nil, errors.New("choose ping, dns, trace or tcp")
}

type cappedOutput struct {
	sync.Mutex
	data []byte
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	n := len(p)
	if len(b.data) < 16384 {
		remaining := 16384 - len(b.data)
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (a *API) diagnose(w http.ResponseWriter, r *http.Request) {
	var input DiagnosticInput
	if !decode(w, r, &input) {
		return
	}
	input.Target = strings.TrimSpace(input.Target)
	args, err := diagnosticArgs(input)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !a.diagnosticMu.TryLock() {
		fail(w, 429, "Another diagnostic is running. Try again shortly.")
		return
	}
	defer a.diagnosticMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if input.Tool == "tcp" {
		start := time.Now()
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(input.Target, strconv.Itoa(input.Port)))
		result := DiagnosticResult{Tool: input.Tool, Target: input.Target, Port: input.Port, Success: err == nil, DurationMS: time.Since(start).Milliseconds()}
		if err == nil {
			_ = conn.Close()
			result.Output = fmt.Sprintf("Connected to %s on TCP port %d.", input.Target, input.Port)
		} else {
			result.Output = "Connection failed: " + err.Error()
			var timeout net.Error
			result.TimedOut = errors.As(err, &timeout) && timeout.Timeout()
		}
		reply(w, 200, result)
		return
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, "/bin/busybox", args...)
	cmd.Env = cleanEnv()
	var out cappedOutput
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	result := DiagnosticResult{Tool: input.Tool, Target: input.Target, Output: strings.TrimSpace(string(out.data)), Success: err == nil, TimedOut: ctx.Err() == context.DeadlineExceeded, DurationMS: time.Since(start).Milliseconds()}
	if result.TimedOut {
		result.Output += "\nTest stopped after 15 seconds."
	} else if err != nil && result.Output == "" {
		result.Output = "Could not run diagnostic: " + err.Error()
	}
	reply(w, 200, result)
}

// Speak the hostapd control protocol directly. The firmware's hostapd_cli
// initialises extra vendor services and can abort; the Unix datagram API does not.
var hostapdInterface = regexp.MustCompile(`^ath[0-9]{2,3}$`)

func hostapdCommand(ctx context.Context, dir, iface, command string) (string, error) {
	if !hostapdInterface.MatchString(iface) {
		return "", errors.New("invalid wireless interface")
	}
	temp, err := os.MkdirTemp("", "c460-hostapd-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	conn, err := net.DialUnix("unixgram", &net.UnixAddr{Name: filepath.Join(temp, "s"), Net: "unixgram"}, &net.UnixAddr{Name: filepath.Join(dir, iface), Net: "unixgram"})
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if _, err = conn.Write([]byte(command)); err != nil {
		return "", err
	}
	buf := make([]byte, 32768)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf[:n])), nil
}
func hostapdProperties(text string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			result[k] = v
		}
	}
	return result
}
func hostapdInterfaces(dir string) []string {
	paths, _ := filepath.Glob(filepath.Join(dir, "ath*"))
	names := []string{}
	for _, p := range paths {
		if hostapdInterface.MatchString(filepath.Base(p)) {
			names = append(names, filepath.Base(p))
		}
	}
	sort.Strings(names)
	return names
}

type WirelessStatus struct {
	Interface string            `json:"interface"`
	Values    map[string]string `json:"values"`
	Error     string            `json:"error,omitempty"`
}

var statusFields = []string{"state", "freq", "channel", "beacon_int", "dtim_period", "ieee80211n", "ieee80211ac", "ieee80211ax", "ieee80211be", "max_txpower", "bssid[0]", "ssid[0]", "num_sta[0]", "cac_time_left_seconds"}

func selectProperties(values map[string]string, keys []string) map[string]string {
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := values[k]; ok {
			out[k] = v
		}
	}
	return out
}
func (a *API) wirelessDirectory() string {
	if a.wirelessDir != "" {
		return a.wirelessDir
	}
	return "/var/run/hostapd"
}
func (a *API) wirelessStatus(w http.ResponseWriter, r *http.Request) {
	if !a.diagnosticMu.TryLock() {
		fail(w, 429, "Another diagnostic is running. Try again shortly.")
		return
	}
	defer a.diagnosticMu.Unlock()
	results := []WirelessStatus{}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	for _, iface := range hostapdInterfaces(a.wirelessDirectory()) {
		out, err := hostapdCommand(ctx, a.wirelessDirectory(), iface, "STATUS")
		item := WirelessStatus{Interface: iface, Values: map[string]string{}}
		if err != nil {
			item.Error = "Wireless interface did not respond"
		} else if out == "FAIL" || out == "" {
			item.Error = "Status unavailable"
		} else {
			item.Values = selectProperties(hostapdProperties(out), statusFields)
		}
		results = append(results, item)
		if ctx.Err() != nil {
			break
		}
	}
	reply(w, 200, map[string]any{"interfaces": results, "sampledAt": time.Now().UTC().Format(time.RFC3339)})
}

var macPattern = regexp.MustCompile(`(?i)^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$`)
var clientFields = []string{"flags", "aid", "capability", "listen_interval", "timeout_next", "dot11RSNAStatsSTAAddress", "dot11RSNAStatsVersion", "dot11RSNAStatsSelectedPairwiseCipher", "dot11RSNAStatsTKIPLocalMICFailures", "dot11RSNAStatsTKIPRemoteMICFailures", "dot11RSNAStatsCCMPDecryptErrors", "rx_packets", "tx_packets", "rx_bytes", "tx_bytes", "connected_time", "inactive_msec", "signal", "rx_rate_info", "tx_rate_info", "rx_retry_count", "tx_retry_count", "ht_caps_info", "vht_caps_info", "ext_capab", "he_capab", "eht_capab"}

type NativeClient struct {
	Interface string            `json:"interface"`
	Values    map[string]string `json:"values"`
}

func locateClient(ctx context.Context, dir, mac string) (NativeClient, error) {
	if !macPattern.MatchString(mac) {
		return NativeClient{}, errors.New("invalid client MAC address")
	}
	for _, iface := range hostapdInterfaces(dir) {
		out, err := hostapdCommand(ctx, dir, iface, "STA "+mac)
		if err != nil {
			return NativeClient{}, fmt.Errorf("wireless interface unavailable: %w", err)
		}
		first, _, _ := strings.Cut(out, "\n")
		if strings.EqualFold(first, mac) {
			return NativeClient{Interface: iface, Values: selectProperties(hostapdProperties(out), clientFields)}, nil
		}
	}
	return NativeClient{}, os.ErrNotExist
}
func (a *API) nativeClient(w http.ResponseWriter, r *http.Request, reconnect bool) {
	mac := strings.ToLower(r.PathValue("mac"))
	if !macPattern.MatchString(mac) {
		fail(w, 400, "invalid client MAC address")
		return
	}
	if reconnect {
		var input struct{}
		if !decode(w, r, &input) {
			return
		}
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
	}
	if !a.diagnosticMu.TryLock() {
		fail(w, 429, "Another diagnostic is running. Try again shortly.")
		return
	}
	defer a.diagnosticMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	client, err := locateClient(ctx, a.wirelessDirectory(), mac)
	if os.IsNotExist(err) {
		fail(w, 404, "Client is no longer associated. Refresh the client list.")
		return
	}
	if err != nil {
		fail(w, 503, "Could not read wireless client status")
		return
	}
	if reconnect {
		out, err := hostapdCommand(ctx, a.wirelessDirectory(), client.Interface, "DEAUTHENTICATE "+mac+" reason=2")
		if err != nil || out != "OK" {
			fail(w, 502, "The wireless interface did not accept the reconnect request")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	reply(w, 200, client)
}
func (a *API) clientDetails(w http.ResponseWriter, r *http.Request)   { a.nativeClient(w, r, false) }
func (a *API) reconnectClient(w http.ResponseWriter, r *http.Request) { a.nativeClient(w, r, true) }
