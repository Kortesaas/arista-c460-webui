package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type vendorRunner func(context.Context, string, ...string) (string, error)

// The firmware's cfg80211tool is a shell wrapper without a shebang. Start it
// through the vendor environment; every argument remains a separate argv entry.
func runVendorTool(parent context.Context, tool string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	argv := append([]string{"-c", `. /etc/profile >/dev/null 2>&1; exec "$@"`, "c460-native"}, tool)
	if tool == "/usr/sbin/cfg80211tool" {
		argv = append(argv[:3], "/bin/sh", tool)
	}
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, "/bin/sh", argv...)
	cmd.Env = cleanEnv()
	cmd.WaitDelay = 500 * time.Millisecond
	out := cappedOutput{limit: 128 * 1024}
	var diagnostic cappedOutput
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("native command unavailable: %w", err)
	}
	return strings.TrimSpace(string(out.data)), nil
}

type SSIDFeaturesInput struct {
	RRM  *bool `json:"rrm"`
	Load *bool `json:"load"`
}
type SSIDFeatureStatus struct {
	Interface string `json:"interface"`
	Band      string `json:"band"`
	RRM       *bool  `json:"rrm"`
	Load      *bool  `json:"load"`
	Error     string `json:"error,omitempty"`
}
type SSIDFeatures struct {
	SSIDFeaturesInput
	Settings   map[string]any      `json:"settings"` // every advanced setting; null = firmware default
	Native     map[string]any      `json:"native"`   // what the firmware's own configuration currently has
	Interfaces []SSIDFeatureStatus `json:"interfaces"`
}

func configuredBool(cfg map[string]any, key string) *bool {
	if value, ok := cfg[key].(bool); ok {
		return &value
	}
	return nil
}
func parseDriverBool(text, command string) (*bool, error) {
	pattern := regexp.MustCompile(regexp.QuoteMeta(command) + `\s*:\s*([01])\s*$`)
	for _, line := range strings.Split(text, "\n") {
		if match := pattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			b := match[1] == "1"
			return &b, nil
		}
	}
	return nil, errors.New("driver value unavailable")
}
func readDriverBool(ctx context.Context, run vendorRunner, iface, command string) (*bool, error) {
	if !hostapdInterface.MatchString(iface) {
		return nil, errors.New("invalid wireless interface")
	}
	out, err := run(ctx, "/usr/sbin/cfg80211tool", iface, command)
	if err != nil {
		return nil, err
	}
	return parseDriverBool(out, command)
}
func frequencyBand(freq string) string {
	n, _ := strconv.Atoi(freq)
	if n >= 2400 && n < 2500 {
		return "2.4"
	}
	if n >= 4900 && n < 5925 {
		return "5"
	}
	if n >= 5925 && n <= 7125 {
		return "6"
	}
	return ""
}
func (a *API) getSSIDFeatures(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cfg, ok := a.poller.SSIDConfig(name)
	if !ok {
		fail(w, 404, "Network not found. Refresh the network list.")
		return
	}
	if !a.diagnosticMu.TryLock() {
		fail(w, 429, "Another diagnostic is running. Try again shortly.")
		return
	}
	defer a.diagnosticMu.Unlock()
	result := SSIDFeatures{SSIDFeaturesInput: SSIDFeaturesInput{RRM: configuredBool(cfg, "dot11k"), Load: configuredBool(cfg, "qbss-load")}, Interfaces: []SSIDFeatureStatus{}}
	if entry, ok := a.poller.SSIDEntry(name); ok {
		result.Settings = featureValues(entry, ssidFeatureDefs)
	}
	result.Native = nativeFor(ssidFeatureDefs, a.nativeVAP(name))
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	for _, iface := range hostapdInterfaces(a.wirelessDirectory()) {
		out, err := hostapdCommand(ctx, a.wirelessDirectory(), iface, "STATUS")
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			continue
		}
		props := hostapdProperties(out)
		if props["ssid[0]"] != name {
			continue
		}
		item := SSIDFeatureStatus{Interface: iface, Band: frequencyBand(props["freq"])}
		item.RRM, err = readDriverBool(ctx, runVendorTool, iface, "get_rrm")
		if err != nil {
			item.Error = "Could not read all driver settings"
		}
		item.Load, err = readDriverBool(ctx, runVendorTool, iface, "get_qbssload")
		if err != nil {
			item.Error = "Could not read all driver settings"
		}
		result.Interfaces = append(result.Interfaces, item)
		if ctx.Err() != nil {
			break
		}
	}
	reply(w, 200, result)
}
