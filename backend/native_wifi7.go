package main

// The firmware's OpenConfig width leaf is uint8. Native radio profiles use
// enum 6 for 320 MHz; apply them through the vendor trigger/config manager.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type WiFi7Settings struct {
	Enabled bool `json:"enabled"`
	Width   int  `json:"width"`
}

func (s WiFi7Settings) validate() error {
	if s.Width != 160 && s.Width != 320 {
		return errors.New("Wi-Fi 7 width must be 160 or 320 MHz")
	}
	return nil
}

type WiFi7State struct {
	Supported      bool           `json:"supported"`
	Saved          *WiFi7Settings `json:"saved"`
	OperatingMode  string         `json:"operatingMode"`
	OperatingWidth int            `json:"operatingWidth"`
	Status         string         `json:"status"`
	Error          string         `json:"error,omitempty"`
}

type nativeRadioProfile struct {
	id              string
	start, end      int
	protocol, width int
}

var nativeRadioHeader = regexp.MustCompile(`^\[ RADIO_v2_(START|END)=(\d+) \]$`)
var radioWidthCodes = map[int]int{20: 1, 40: 2, 80: 3, 160: 4, 320: 6}

func find6GHzProfile(raw []byte) (nativeRadioProfile, error) {
	lines := strings.Split(string(raw), "\n")
	var found []nativeRadioProfile
	var cur nativeRadioProfile
	band := ""
	active := false
	for i, line := range lines {
		if m := nativeRadioHeader.FindStringSubmatch(line); m != nil {
			if m[1] == "START" {
				cur, band, active = nativeRadioProfile{id: m[2], start: i}, "", true
			} else if active && m[2] == cur.id {
				cur.end = i
				if band == "6G" {
					found = append(found, cur)
				}
				active = false
			}
			continue
		}
		if !active {
			continue
		}
		if v, ok := vapValue(line, "WIRELESS_BAND"); ok {
			band = v
		}
		if v, ok := vapValue(line, "WIRELESS_PROTOCOL"); ok {
			cur.protocol, _ = strconv.Atoi(v)
		}
		if v, ok := vapValue(line, "AP_CHAN_WIDTH"); ok {
			cur.width, _ = strconv.Atoi(v)
		}
	}
	if len(found) != 1 || (found[0].protocol != 4 && found[0].protocol != 5) || !slices.Contains([]int{1, 2, 3, 4, 6}, found[0].width) {
		return nativeRadioProfile{}, errors.New("A unique native 6 GHz radio profile could not be identified")
	}
	return found[0], nil
}

func plan6GHz(raw []byte, protocol, width int) ([]byte, nativeRadioProfile, error) {
	p, err := find6GHzProfile(raw)
	if err != nil {
		return nil, p, err
	}
	if (protocol != 4 && protocol != 5) || radioWidthCodes[width] == 0 || (protocol == 4 && width == 320) {
		return nil, p, errors.New("invalid native radio mode")
	}
	lines := strings.Split(string(raw), "\n")
	for key, value := range map[string]int{"WIRELESS_PROTOCOL": protocol, "AP_CHAN_WIDTH": radioWidthCodes[width]} {
		count := 0
		for i := p.start + 1; i < p.end; i++ {
			if _, ok := vapValue(lines[i], key); ok {
				lines[i] = key + "=" + strconv.Itoa(value)
				count++
			}
		}
		if count != 1 {
			return nil, p, errors.New("unexpected native radio field count")
		}
	}
	return []byte(strings.Join(lines, "\n")), p, nil
}

func checkRadioDiff(out string, code int, id string) error {
	if code != 0 {
		return fmt.Errorf("The firmware refused a radio-only change without reboot (code %d)", code)
	}
	var headers []string
	allowed := []string{"ACS_CHAN_LIST", "AP_CHAN_WIDTH", "AUTO_CHANSEL_PERIOD", "DCS_ENABLED", "OP_CHANNEL", "SECOND_OP_CHANNEL", "WIRELESS_BAND", "WIRELESS_PROTOCOL"}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			headers = append(headers, line)
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok || !slices.Contains(allowed, key) {
			return errors.New("The firmware reported changes beyond the selected radio")
		}
	}
	if !slices.Equal(headers, []string{"[ RADIO_START=" + id + " MOD ]", "[ RADIO_END=" + id + " ]"}) {
		return errors.New("The firmware reported changes beyond the selected radio")
	}
	return nil
}

type NativeWiFi7 struct {
	mu           sync.RWMutex
	path, apConf string
	desired      *WiFi7Settings
	state        WiFi7State
	lastFailure  time.Time
	supported    func([]byte) bool
	apply        func(context.Context, []byte, []byte, string) error
	operating    func(context.Context) (string, int, error)
	save         func(string, []byte, os.FileMode) error
	quiet        func(context.Context) error
}

func NewNativeWiFi7(path string) *NativeWiFi7 {
	n := &NativeWiFi7{path: path, apConf: nativeAPConf, supported: nativeWiFi7Supported, apply: stageNativeRadio, operating: read6GHzOperating, save: atomicNative, quiet: waitNativeQuiet}
	if raw, err := os.ReadFile(path); err == nil {
		var s WiFi7Settings
		if json.Unmarshal(raw, &s) == nil && s.validate() == nil {
			n.desired = &s
		}
	}
	return n
}

func (n *NativeWiFi7) Saved() *WiFi7Settings {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.desired == nil {
		return nil
	}
	s := *n.desired
	return &s
}

func (n *NativeWiFi7) Snapshot() WiFi7State {
	n.mu.RLock()
	defer n.mu.RUnlock()
	st := n.state
	if n.desired != nil {
		s := *n.desired
		st.Saved = &s
	}
	return st
}

func (n *NativeWiFi7) Observe(ctx context.Context) {
	raw, err := os.ReadFile(n.apConf)
	st := WiFi7State{Status: "off"}
	if err == nil {
		st.Supported = n.supported(raw)
	}
	if st.Supported {
		st.OperatingMode, st.OperatingWidth, err = n.operating(ctx)
		if err != nil {
			st.Error = "Operating radio information is unavailable"
		}
	}
	saved := n.Saved()
	if saved != nil && saved.Enabled {
		st.Status = "pending"
		if strings.HasPrefix(st.OperatingMode, "11AEHT") && st.OperatingWidth == saved.Width {
			st.Status = "active"
		}
	}
	n.mu.Lock()
	if n.state.Error != "" && !n.lastFailure.IsZero() {
		st.Error = n.state.Error
		st.Status = "error"
	}
	n.state = st
	n.mu.Unlock()
}

func targetWiFi7(s WiFi7Settings, fallback int) (int, int) {
	if s.Enabled {
		return 5, s.Width
	}
	return 4, fallback
}

// Caller holds API.writeMu. Completion/rollback uses its own deadline so a
// phone disconnecting during its radio restart cannot abandon the operation.
func (n *NativeWiFi7) switchMode(ctx context.Context, s WiFi7Settings, fallback int) error {
	if n.quiet != nil {
		if err := n.quiet(ctx); err != nil {
			return err
		}
	}
	raw, err := os.ReadFile(n.apConf)
	if err != nil {
		return err
	}
	if !n.supported(raw) {
		return errors.New("Native Wi-Fi 7 is unavailable on this AP")
	}
	proto, width := targetWiFi7(s, fallback)
	next, p, err := plan6GHz(raw, proto, width)
	if err != nil {
		return err
	}
	if bytes.Equal(raw, next) {
		mode, w, err := n.operating(ctx)
		if err == nil && w == width && mode == expected6GHzMode(proto, width) {
			return nil
		}
		return errors.New("The saved radio configuration does not match its operating state; retry when the radio is enabled")
	}
	if err = n.apply(ctx, raw, next, p.id); err == nil {
		err = n.waitOperating(ctx, expected6GHzMode(proto, width), width)
	}
	if err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		current, readErr := os.ReadFile(n.apConf)
		oldWidth := 0
		for mhz, code := range radioWidthCodes {
			if code == p.width {
				oldWidth = mhz
			}
		}
		if readErr == nil {
			restore, rp, planErr := plan6GHz(current, p.protocol, oldWidth)
			if planErr == nil {
				if !bytes.Equal(current, restore) {
					readErr = n.apply(rollbackCtx, current, restore, rp.id)
				}
				if readErr == nil {
					readErr = n.waitOperating(rollbackCtx, expected6GHzMode(p.protocol, oldWidth), oldWidth)
				}
			} else if planErr != nil {
				readErr = planErr
			}
		}
		if readErr != nil {
			return fmt.Errorf("%w; radio rollback failed: %v", err, readErr)
		}
		return fmt.Errorf("%w; previous radio settings restored", err)
	}
	return nil
}

func expected6GHzMode(protocol, width int) string {
	p := "HE"
	if protocol == 5 {
		p = "EHT"
	}
	return "11A" + p + strconv.Itoa(width)
}

func pauseNative(ctx context.Context) error {
	t := time.NewTimer(time.Second)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (n *NativeWiFi7) waitOperating(ctx context.Context, mode string, width int) error {
	for {
		m, w, err := n.operating(ctx)
		if err == nil && m == mode && w == width {
			return nil
		}
		if err = pauseNative(ctx); err != nil {
			return errors.New("The radio did not reach the requested operating mode")
		}
	}
}

func (n *NativeWiFi7) Update(ctx context.Context, s WiFi7Settings, fallback int) error {
	if err := s.validate(); err != nil {
		return err
	}
	prior := n.Saved()
	if err := n.switchMode(ctx, s, fallback); err != nil {
		return err
	}
	raw, _ := json.Marshal(s)
	if err := n.save(n.path, raw, 0600); err != nil {
		restore := WiFi7Settings{Width: 160}
		if prior != nil {
			restore = *prior
		}
		rc, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if re := n.switchMode(rc, restore, fallback); re != nil {
			return fmt.Errorf("saving failed: %v; rollback failed: %v", err, re)
		}
		return fmt.Errorf("saving failed: %v; previous radio settings restored", err)
	}
	n.mu.Lock()
	n.desired = &s
	n.lastFailure = time.Time{}
	n.state.Error = ""
	n.mu.Unlock()
	n.Observe(ctx)
	return nil
}

func (n *NativeWiFi7) Ensure(ctx context.Context, fallback int) error {
	s := n.Saved()
	if s == nil || !s.Enabled {
		return nil
	}
	err := n.switchMode(ctx, *s, fallback)
	n.mu.Lock()
	if err != nil {
		n.state.Error = err.Error()
		n.state.Status = "error"
		n.lastFailure = time.Now()
	} else {
		n.state.Error = ""
		n.lastFailure = time.Time{}
	}
	n.mu.Unlock()
	n.Observe(ctx)
	return err
}

// Called with the AP write lock after every OpenConfig transaction. The
// independent deadline also covers an HTTP client losing Wi-Fi mid-save.
func (a *API) ensureWiFi7() error {
	if a.wifi7 == nil || a.wifi7.Saved() == nil {
		return nil
	}
	fallback, on := a.wifi7Fallback()
	if !on {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return a.wifi7.Ensure(ctx, fallback)
}

func nativeWiFi7Supported(raw []byte) bool {
	if _, err := find6GHzProfile(raw); err != nil {
		return false
	}
	cap, err := os.ReadFile("/opt/ap/ap_capability.conf")
	if err != nil {
		return false
	}
	env, _ := os.ReadFile("/opt/sensor/env")
	return bytes.Contains(cap, []byte("WIFI_2_MAX_PROTOCOL=5\n")) && bytes.Contains(cap, []byte("WIFI_2_MAX_CHAN_WIDTH=6\n")) && bytes.Contains(env, []byte("CONFIG_MANAGER_ENABLED=TRUE\n"))
}

func waitNativeQuiet(ctx context.Context) error {
	for {
		pending, _ := filepath.Glob("/tmp/trigger/ap-conf.*")
		_, err := os.Stat("/tmp/reinit_running")
		if len(pending) == 0 && os.IsNotExist(err) {
			return nil
		}
		if err = pauseNative(ctx); err != nil {
			return err
		}
	}
}

func stageNativeRadio(ctx context.Context, before, next []byte, id string) error {
	if err := waitNativeQuiet(ctx); err != nil {
		return err
	}
	current, err := os.ReadFile(nativeAPConf)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, current) {
		return errors.New("AP configuration changed while planning; retry")
	}
	f, err := os.CreateTemp("/tmp", "c460-radio-candidate-")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err = f.Write(next); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	diff, code, err := firmwareConfigDiff(ctx, nativeAPConf, path)
	if err != nil {
		return err
	}
	if err = checkRadioDiff(diff, code, id); err != nil {
		return err
	}
	current, err = os.ReadFile(nativeAPConf)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, current) {
		return errors.New("AP configuration changed while planning; retry")
	}
	if err = os.Rename(path, fmt.Sprintf("/tmp/trigger/ap-conf.webui-radio.%d", time.Now().UnixNano())); err != nil {
		return err
	}
	// The trigger file remains present until the vendor manager finishes.
	if err = waitNativeQuiet(ctx); err != nil {
		return err
	}
	current, err = os.ReadFile(nativeAPConf)
	if err != nil {
		return err
	}
	p, err := find6GHzProfile(current)
	if err != nil {
		return err
	}
	wanted, _ := find6GHzProfile(next)
	if p.protocol != wanted.protocol || p.width != wanted.width {
		return errors.New("The firmware did not apply the radio configuration")
	}
	return nil
}

var safeNativeVAP = regexp.MustCompile(`^ath\d{2,3}$`)
var iwWidthPattern = regexp.MustCompile(`width: (\d+) MHz`)
var iwFreqPattern = regexp.MustCompile(`channel \d+ \((\d+) MHz\)`)

func read6GHzOperating(ctx context.Context) (string, int, error) {
	files, _ := filepath.Glob("/tmp/radio*/radio.conf")
	var prefix string
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if bytes.Contains(raw, []byte("WIRELESS_BAND=6G\n")) {
			for _, line := range strings.Split(string(raw), "\n") {
				if v, ok := vapValue(line, "RADIO_INDEX"); ok {
					if _, err := strconv.Atoi(v); err == nil {
						prefix = "ath" + v
					}
				}
			}
		}
	}
	if prefix == "" {
		return "", 0, errors.New("6 GHz radio not found")
	}
	entries, _ := os.ReadDir("/var/run/hostapd")
	for _, e := range entries {
		iface := e.Name()
		if !safeNativeVAP.MatchString(iface) || !strings.HasPrefix(iface, prefix) {
			continue
		}
		mode, err := runVendorTool(ctx, "/bin/sh", "/usr/sbin/cfg80211tool", iface, "get_mode")
		if err != nil {
			continue
		}
		info, err := runVendorTool(ctx, "iw", "dev", iface, "info")
		if err != nil {
			continue
		}
		wm, fm := iwWidthPattern.FindStringSubmatch(info), iwFreqPattern.FindStringSubmatch(info)
		if len(wm) != 2 || len(fm) != 2 {
			continue
		}
		freq, _ := strconv.Atoi(fm[1])
		if freq < 5955 || freq > 7115 {
			continue
		}
		_, m, ok := strings.Cut(strings.TrimSpace(mode), ":")
		if !ok {
			continue
		}
		width, _ := strconv.Atoi(wm[1])
		return strings.TrimSpace(m), width, nil
	}
	return "", 0, errors.New("6 GHz radio is not operating")
}

func (a *API) wifi7Fallback() (int, bool) {
	for _, r := range a.poller.Snapshot().Radios {
		if r.Band == "6" {
			cfg, _, ok := a.poller.RadioConfig(r.ID)
			return intv(cfg["channel-width"]), ok && r.Enabled
		}
	}
	return 0, false
}

func (a *API) updateWiFi7(w http.ResponseWriter, r *http.Request) {
	// Include the independent rollback deadline in the response window.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(4 * time.Minute))
	var s WiFi7Settings
	if !decode(w, r, &s) {
		return
	}
	if err := s.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	cfg, freq, ok := a.poller.RadioConfig(id)
	if err != nil || !ok || band(freq) != "6" {
		fail(w, 400, "Wi-Fi 7 native controls are available for the 6 GHz radio only")
		return
	}
	if a.wifi7 == nil {
		fail(w, 503, "Native radio controls are unavailable")
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if _, enabled := a.wifi7Fallback(); !enabled {
		fail(w, 400, "Enable the 6 GHz radio before changing its Wi-Fi mode")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer cancel()
	if err = a.wifi7.Update(ctx, s, intv(cfg["channel-width"])); err != nil {
		fail(w, 502, err.Error())
		return
	}
	a.poller.Refresh()
	reply(w, 200, a.wifi7.Snapshot())
}

func (a *API) runNativeWiFi7(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		if a.writeMu.TryLock() {
			probe, cancel := context.WithTimeout(ctx, 5*time.Second)
			a.wifi7.Observe(probe)
			cancel()
			width, on := a.wifi7Fallback()
			a.wifi7.mu.RLock()
			retry := time.Since(a.wifi7.lastFailure) > time.Minute
			a.wifi7.mu.RUnlock()
			if on && retry {
				work, cancel := context.WithTimeout(ctx, 90*time.Second)
				if err := a.wifi7.Ensure(work, width); err != nil {
					log.Printf("native Wi-Fi 7: %v", err)
				}
				cancel()
			}
			a.writeMu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
