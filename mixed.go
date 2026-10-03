package main

// WPA2/WPA3 mixed (transition) mode. The OpenConfig agent only knows WPA3-only
// and WPA2-only, while the firmware's own configuration supports transition
// mode (AP_SEC_MODE=7). Mixed networks are therefore stored as WPA3 in
// OpenConfig, and this enforcer switches their native VAP section to mode 7
// through the firmware's own apply path (handle_ap_conf.sh). The OpenConfig
// agent rewrites the native file after every configuration change, so the
// enforcer re-applies it; if this service stops, the networks fall back to
// WPA3-only, which is the safe direction.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	opModeMixed           = "WPA2_WPA3_PERSONAL"
	wirelessOverridesFile = "/opt/c460-webui/wireless-overrides.json"
	nativeAPConf          = "/opt/ap/ap.conf"
	nativeModeWPA3        = "6"
	nativeModeMixed       = "7"
)

type WirelessOverrides struct {
	mu     sync.Mutex
	path   string
	mixed  []string
	status map[string]string // SSID -> "applied", "pending" or a problem

	apConf      string
	triggerGlob string
	reinitFile  string
	workDir     string
	diff        func(ctx context.Context, current, next string) (string, int, error)
	apply       func(ctx context.Context, next string) error

	lastAttempt  time.Time
	failedConfig [32]byte // native config the last failed attempt started from
}

func NewWirelessOverrides(path string) *WirelessOverrides {
	o := &WirelessOverrides{path: path, status: map[string]string{},
		apConf: nativeAPConf, triggerGlob: "/tmp/trigger/ap-conf.*", reinitFile: "/tmp/reinit_running", workDir: "/tmp/c460-webui-native",
		diff: firmwareConfigDiff, apply: firmwareApplyConfig}
	if raw, err := os.ReadFile(path); err == nil {
		var saved struct {
			Mixed []string `json:"mixed"`
		}
		if json.Unmarshal(raw, &saved) == nil {
			o.mixed = saved.Mixed
		}
	}
	return o
}

func (o *WirelessOverrides) saveLocked() error {
	raw, _ := json.Marshal(map[string]any{"mixed": o.mixed})
	return atomicNative(o.path, raw, 0o600)
}

func (o *WirelessOverrides) IsMixed(ssid string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Contains(o.mixed, ssid)
}

func (o *WirelessOverrides) Status(ssid string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.status[ssid]
}

func (o *WirelessOverrides) HasMixed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.mixed) > 0
}

// Update records that oldName (possibly renamed to newName, or deleted when
// newName is "") is or is not a mixed network.
func (o *WirelessOverrides) Update(oldName, newName string, mixed bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	next := slices.DeleteFunc(slices.Clone(o.mixed), func(n string) bool { return n == oldName || n == newName })
	if mixed && newName != "" {
		next = append(next, newName)
	}
	slices.Sort(next)
	if slices.Equal(next, o.mixed) {
		return nil
	}
	previous := o.mixed
	o.mixed = next
	delete(o.status, oldName)
	if err := o.saveLocked(); err != nil {
		o.mixed = previous
		return err
	}
	return nil
}

// ------------------------------------------------------------ native file

type vapSection struct {
	ssid, mode, profile string
	modeLine, mfpLine   int
}

var sectionHeader = regexp.MustCompile(`^\[ VAP_(START|END)=(\d+) \]$`)

func vapValue(line, key string) (string, bool) {
	v, ok := strings.CutPrefix(line, key+"=")
	if !ok {
		return "", false
	}
	if len(v) >= 2 && (v[0] == '\'' && v[len(v)-1] == '\'' || v[0] == '"' && v[len(v)-1] == '"') {
		v = v[1 : len(v)-1]
	}
	return v, true
}

func parseVAPSections(lines []string) []vapSection {
	var out []vapSection
	var cur *vapSection
	for i, line := range lines {
		if m := sectionHeader.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if m[1] == "START" {
				cur = &vapSection{modeLine: -1, mfpLine: -1}
			} else if cur != nil {
				out = append(out, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			continue
		}
		if v, ok := vapValue(line, "AP_SSID"); ok {
			cur.ssid = v
		} else if v, ok := vapValue(line, "AP_SEC_MODE"); ok {
			cur.mode, cur.modeLine = v, i
		} else if _, ok := vapValue(line, "IEEE80211W_ENABLE"); ok {
			cur.mfpLine = i
		} else if v, ok := vapValue(line, "SSID_PROFILE_ID"); ok {
			cur.profile = v
		}
	}
	return out
}

// planNative returns the new native file and the profile IDs that change.
// wantMixed decides per SSID; only WPA3 (6) and mixed (7) sections are touched.
func planNative(raw []byte, wantMixed func(ssid string) (mixed, managed bool)) ([]byte, []string) {
	lines := strings.Split(string(raw), "\n")
	var changed []string
	for _, vap := range parseVAPSections(lines) {
		mixed, managed := wantMixed(vap.ssid)
		if !managed || vap.modeLine < 0 || vap.mfpLine < 0 || vap.profile == "" {
			continue
		}
		switch {
		case mixed && vap.mode == nativeModeWPA3:
			lines[vap.modeLine], lines[vap.mfpLine] = "AP_SEC_MODE="+nativeModeMixed, "IEEE80211W_ENABLE=1"
		case !mixed && vap.mode == nativeModeMixed:
			lines[vap.modeLine], lines[vap.mfpLine] = "AP_SEC_MODE="+nativeModeWPA3, "IEEE80211W_ENABLE=2"
		default:
			continue
		}
		changed = append(changed, vap.profile)
	}
	return []byte(strings.Join(lines, "\n")), changed
}

var diffHeader = regexp.MustCompile(`^\[ VAP_(?:START=(\d+)(?: (?:DEL|NEW|MOD))?|END=(\d+)) \]$`)

// checkDiff accepts the firmware's diff only when it needs no reboot and
// touches nothing but the VAPs we changed.
func checkDiff(out string, code int, profiles []string) error {
	if code != 0 {
		return fmt.Errorf("the firmware would need a restart for this change (code %d)", code)
	}
	seen := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		m := diffHeader.FindStringSubmatch(line)
		if m == nil || !slices.Contains(profiles, m[1]+m[2]) {
			return errors.New("the firmware reported changes beyond the selected networks")
		}
		seen = true
	}
	if !seen {
		return errors.New("the firmware reported no change")
	}
	return nil
}

// --------------------------------------------------------------- enforcer

// Enforce brings the native configuration in line with the desired mixed
// networks. ocModes maps each SSID to its OpenConfig security mode.
func (o *WirelessOverrides) Enforce(ctx context.Context, ocModes map[string]string) {
	o.mu.Lock()
	mixed := slices.Clone(o.mixed)
	o.mu.Unlock()

	path, err := filepath.EvalSymlinks(o.apConf)
	if err != nil {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	want := func(ssid string) (bool, bool) {
		// Only networks stored as WPA3 in OpenConfig are ours to switch.
		return slices.Contains(mixed, ssid), ocModes[ssid] == "WPA3_SAE"
	}
	next, profiles := planNative(raw, want)
	status := map[string]string{}
	for _, vap := range parseVAPSections(strings.Split(string(raw), "\n")) {
		if slices.Contains(mixed, vap.ssid) {
			if vap.mode == nativeModeMixed {
				status[vap.ssid] = "applied"
			} else {
				status[vap.ssid] = "pending"
			}
		}
	}
	defer func() {
		o.mu.Lock()
		for _, name := range mixed {
			if s, ok := status[name]; ok {
				o.status[name] = s
			} else if ocModes[name] != "" {
				o.status[name] = "pending"
			}
		}
		o.mu.Unlock()
	}()
	if len(profiles) == 0 {
		return
	}
	sum := sha256.Sum256(raw)
	if sum == o.failedConfig || time.Since(o.lastAttempt) < time.Minute || !o.quiet(path) {
		return
	}
	o.lastAttempt = time.Now()
	fail := func(err error) {
		o.failedConfig = sum
		log.Printf("mixed mode: %v", err)
		for _, name := range mixed {
			if status[name] == "pending" {
				status[name] = err.Error()
			}
		}
	}
	if err := os.MkdirAll(o.workDir, 0o700); err != nil {
		fail(err)
		return
	}
	// handle_ap_conf.sh only accepts files whose name contains "ap-conf.".
	candidate := filepath.Join(o.workDir, "ap-conf.c460-webui")
	if err := os.WriteFile(candidate, next, 0o600); err != nil {
		fail(err)
		return
	}
	defer os.Remove(candidate)
	out, code, err := o.diff(ctx, path, candidate)
	if err == nil {
		err = checkDiff(out, code, profiles)
	}
	if err != nil {
		fail(err)
		return
	}
	if current, err := os.ReadFile(path); err != nil || !bytes.Equal(current, raw) {
		return // changed underneath us; try again on the next round
	}
	if err := o.apply(ctx, candidate); err != nil {
		fail(fmt.Errorf("the firmware did not apply the change: %w", err))
		return
	}
	log.Printf("mixed mode: updated native security for profiles %s", strings.Join(profiles, ", "))
	if after, err := os.ReadFile(path); err == nil {
		for _, vap := range parseVAPSections(strings.Split(string(after), "\n")) {
			if slices.Contains(mixed, vap.ssid) {
				status[vap.ssid] = map[bool]string{true: "applied", false: "pending"}[vap.mode == nativeModeMixed]
			}
		}
	}
}

// quiet reports whether the firmware is idle: no configuration waiting to be
// applied, no re-initialisation running and no recent change of the file.
func (o *WirelessOverrides) quiet(path string) bool {
	if pending, _ := filepath.Glob(o.triggerGlob); len(pending) > 0 {
		return false
	}
	if _, err := os.Stat(o.reinitFile); err == nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) > 20*time.Second
}

func vendorShell(ctx context.Context, args ...string) *exec.Cmd {
	argv := append([]string{"-c", `. /etc/profile >/dev/null 2>&1; exec "$@"`, "c460-native"}, args...)
	cmd := exec.CommandContext(ctx, "/bin/sh", argv...)
	cmd.Env = cleanEnv()
	cmd.WaitDelay = time.Second
	return cmd
}

func firmwareConfigDiff(parent context.Context, current, next string) (string, int, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	cmd := vendorShell(ctx, "/sbin/ap_config_diff", current, next, "0")
	out := cappedOutput{limit: 512 * 1024}
	cmd.Stdout = &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out.data), exit.ExitCode(), nil
	}
	return string(out.data), 0, err
}

func firmwareApplyConfig(parent context.Context, next string) error {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	return vendorShell(ctx, "/bin/sh", "/opt/ap/handle_ap_conf.sh", "reinit", next).Run()
}

// RunEnforcer re-checks the native configuration every 15 seconds while
// mixed networks exist, or while a stale mixed section may need reverting.
func (a *API) runEnforcer(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		st := a.poller.Snapshot()
		if st.GeneratedAt.IsZero() || time.Since(st.GeneratedAt) > time.Minute || st.Error != "" {
			continue
		}
		modes := map[string]string{}
		for _, s := range st.SSIDs {
			modes[s.Name] = s.OpMode
		}
		// Never race a configuration change made through this UI.
		if !a.writeMu.TryLock() {
			continue
		}
		a.overrides.Enforce(ctx, modes)
		a.writeMu.Unlock()
	}
}

// snapshot is the poller state with the UI's own overrides applied.
func (a *API) snapshot() APState {
	st := a.poller.Snapshot()
	if a.wifi7 != nil {
		st.Radios = slices.Clone(st.Radios)
		for i, radio := range st.Radios {
			if radio.Band == "6" {
				native := a.wifi7.Snapshot()
				st.Radios[i].WiFi7 = &native
				// Keep form/backup writes within OpenConfig's uint8 schema.
				// The separately verified operating width is displayed by the UI.
				cfg, _, ok := a.poller.RadioConfig(radio.ID)
				if ok {
					st.Radios[i].Width = intv(cfg["channel-width"])
				}
			}
		}
	}
	if a.overrides == nil || !a.overrides.HasMixed() {
		return st
	}
	ssids := slices.Clone(st.SSIDs)
	for i, s := range ssids {
		if s.OpMode == "WPA3_SAE" && a.overrides.IsMixed(s.Name) {
			ssids[i].OpMode = opModeMixed
			ssids[i].MixedStatus = a.overrides.Status(s.Name)
		}
	}
	st.SSIDs = ssids
	return st
}
