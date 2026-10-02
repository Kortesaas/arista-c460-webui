package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const timeSettingsFile = "/opt/c460-webui/time-settings.json"
const sensorConfigFile = "/opt/sensor/sensor.conf"

type TimeSettings struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Managed   bool   `json:"managed"`
	Synced    *bool  `json:"synced"`
	Running   bool   `json:"running"`
}
type TimeInput struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
}

var hostNamePattern = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*\.?$`)

// This firmware splits NTP arguments on spaces; accept hostnames/addresses only.
func validHost(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	if ip := net.ParseIP(value); ip != nil {
		return !ip.IsUnspecified() && !ip.IsMulticast()
	}
	if regexp.MustCompile(`^[0-9.]+$`).MatchString(value) {
		return false
	}
	return hostNamePattern.MatchString(value)
}
func (v TimeInput) validate() error {
	if !validHost(v.Primary) {
		return errors.New("primary NTP server must be a hostname or IP address")
	}
	if v.Secondary != "" && (!validHost(v.Secondary) || strings.EqualFold(v.Primary, v.Secondary)) {
		return errors.New("secondary NTP server must be a different hostname or IP address")
	}
	return nil
}
func nativeField(data []byte, key string) string {
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}
func readTimeSettings() (TimeSettings, error) {
	data, err := os.ReadFile(sensorConfigFile)
	if err != nil {
		return TimeSettings{}, err
	}
	result := TimeSettings{Primary: nativeField(data, "ntpserver"), Secondary: nativeField(data, "secntpserver")}
	if result.Primary == "" && result.Secondary == "" {
		result.Primary = "time.nist.gov"
	}
	if saved, err := os.ReadFile(timeSettingsFile); err == nil {
		var desired TimeInput
		if json.Unmarshal(saved, &desired) == nil {
			result.Managed = desired.Primary == result.Primary && desired.Secondary == result.Secondary
		}
	}
	processes, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, name := range processes {
		raw, _ := os.ReadFile(name)
		if strings.TrimSpace(string(raw)) == "ntpd" {
			result.Running = true
			break
		}
	}
	return result, nil
}

// Encrypt a prepared copy with the vendor's TPM-backed mechanism before changing
// either native file. Keep our desired settings outside the vendor config so a
// subsequent web-service start can restore them if another native writer resets it.
type nativeTimeBackend struct {
	sensor, desired string
	encrypt         func(context.Context, string, string) error
	restart         func(context.Context) error
}

func saveTimeSettings(ctx context.Context, input TimeInput, persist bool) error {
	backend := nativeTimeBackend{sensor: sensorConfigFile, desired: timeSettingsFile, restart: restartNTP,
		encrypt: func(ctx context.Context, plain, enc string) error {
			cmd := exec.CommandContext(ctx, "/bin/sh", "/opt/sensor/scripts/encrypt_secret.sh", "-i", plain, "-o", enc)
			cmd.Env = cleanEnv()
			return cmd.Run()
		}}
	return backend.save(ctx, input, persist)
}
func (backend nativeTimeBackend) save(ctx context.Context, input TimeInput, persist bool) error {
	if err := input.validate(); err != nil {
		return err
	}
	before, err := os.ReadFile(backend.sensor)
	if err != nil {
		return err
	}
	after := nativeValue(nativeValue(before, "ntpserver", input.Primary), "secntpserver", input.Secondary)
	var changes []nativeWrite
	if !bytes.Equal(before, after) {
		sensor, err := prepareNative(backend.sensor, nil)
		if err != nil {
			return err
		}
		sensor.after = after
		encrypted, err := prepareNative(backend.sensor+".enc", nil)
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp(filepath.Dir(backend.desired), ".time-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		plain, enc := filepath.Join(dir, "sensor.conf"), filepath.Join(dir, "sensor.conf.enc")
		if err = os.WriteFile(plain, after, 0o600); err != nil {
			return err
		}
		if err = backend.encrypt(ctx, plain, enc); err != nil {
			return fmt.Errorf("encrypt native time settings: %w", err)
		}
		encrypted.after, err = os.ReadFile(enc)
		if err != nil || len(encrypted.after) == 0 {
			return errors.New("native configuration encryption produced no data")
		}
		encrypted.mode = 0o600
		changes = append(changes, encrypted, sensor)
	}
	if persist {
		saved, err := prepareNative(backend.desired, nil)
		if err != nil {
			return err
		}
		saved.after, err = json.MarshalIndent(input, "", "  ")
		if err != nil {
			return err
		}
		saved.mode = 0o600
		changes = append(changes, saved)
	}
	// Encryption can take a few seconds. Refuse to overwrite another native
	// writer's updates (including OC credentials) while it was in progress.
	current, err := os.ReadFile(backend.sensor)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return errors.New("native configuration changed; retry saving time settings")
	}

	// Roll back both native copies and our desired settings if saving/restarting fails.
	rollback := func(cause error) error {
		var failures []string
		for i := len(changes) - 1; i >= 0; i-- {
			v := changes[i]
			var e error
			if v.existed {
				e = atomicNative(v.path, v.before, v.mode)
			} else {
				e = os.Remove(v.path)
				if os.IsNotExist(e) {
					e = nil
				}
			}
			if e != nil {
				failures = append(failures, e.Error())
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("%w; rollback incomplete: %s", cause, strings.Join(failures, "; "))
		}
		return fmt.Errorf("%w; previous settings restored", cause)
	}
	for _, v := range changes {
		if err = atomicNative(v.path, v.after, v.mode); err != nil {
			return rollback(err)
		}
	}
	// Restart only time synchronisation. Wireless interfaces remain untouched.
	if !bytes.Equal(before, after) {
		if err = backend.restart(ctx); err != nil {
			restored := rollback(err)
			_ = backend.restart(context.Background())
			return restored
		}
	}
	return nil
}
func restartNTP(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/opt/init.d/ntpd.init", "restart")
	cmd.Env = cleanEnv()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restart time synchronisation: %w", err)
	}
	return nil
}
func restoreTimeSettings(parent context.Context) error {
	data, err := os.ReadFile(timeSettingsFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var desired TimeInput
	if err = json.Unmarshal(data, &desired); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	return saveTimeSettings(ctx, desired, false)
}
func (a *API) timeSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := readTimeSettings()
	if err != nil {
		fail(w, 503, "Could not read time settings")
		return
	}
	_, hw, _ := a.cli.Snapshot()
	settings.Synced = hw.NTPSynced
	reply(w, 200, settings)
}
func (a *API) updateTime(w http.ResponseWriter, r *http.Request) {
	var input TimeInput
	if !decode(w, r, &input) {
		return
	}
	input.Primary = strings.TrimSpace(input.Primary)
	input.Secondary = strings.TrimSpace(input.Secondary)
	if err := input.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := saveTimeSettings(ctx, input, true); err != nil {
		log.Printf("time settings: %v", err)
		fail(w, 500, "Could not save time settings: "+err.Error())
		return
	}
	a.refreshCLI()
	reply(w, 200, map[string]bool{"ok": true})
}
