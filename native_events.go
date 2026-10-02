package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type WirelessEvent struct {
	ID        string `json:"id"`
	Time      string `json:"time"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	Tone      string `json:"tone"`
	Interface string `json:"interface"`
	Network   string `json:"network"`
	Client    string `json:"client"`
	Frequency string `json:"frequency"`
}
type WirelessEventLog struct {
	Events      []WirelessEvent `json:"events"`
	SampledAt   time.Time       `json:"sampledAt"`
	ClockSynced *bool           `json:"clockSynced"`
	Error       string          `json:"error,omitempty"`
}

var eventLine = regexp.MustCompile(`^(?:(\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?)\s*:\s*)?(ath[0-9]{2,3}):\s*(AP-STA-CONNECTED|AP-STA-DISCONNECTED|AP-ENABLED|AP-DISABLED|CTRL-EVENT-CHANNEL-SWITCH|CTRL-EVENT-TERMINATING|DFS-RADAR-DETECTED|DFS-CAC-START|DFS-CAC-COMPLETED)(?:\s|$)`)
var eventMAC = regexp.MustCompile(`(?i)^\s*([0-9a-f]{2}(?::[0-9a-f]{2}){5})(?:\s|$)`)
var eventFrequency = regexp.MustCompile(`(?:^|\s)freq=([0-9]{4})(?:\s|$)`)

func parseWirelessEvents(raw string, networks map[string]string) []WirelessEvent {
	kinds := map[string]struct{ title, tone string }{"AP-STA-CONNECTED": {"Client connected", "info"}, "AP-STA-DISCONNECTED": {"Client disconnected", "info"}, "AP-ENABLED": {"Wireless network started", "info"}, "AP-DISABLED": {"Wireless network stopped", "warn"}, "CTRL-EVENT-CHANNEL-SWITCH": {"Channel changed", "info"}, "CTRL-EVENT-TERMINATING": {"Wireless interface stopped", "warn"}, "DFS-RADAR-DETECTED": {"Radar detected", "warn"}, "DFS-CAC-START": {"Checking channel for radar", "info"}, "DFS-CAC-COMPLETED": {"Radar check completed", "info"}}
	result := []WirelessEvent{}
	for _, line := range strings.Split(raw, "\n") {
		m := eventLine.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		field := func(n int) string {
			if m[n*2] < 0 {
				return ""
			}
			return line[m[n*2]:m[n*2+1]]
		}
		kind := field(3)
		tail := line[m[1]:]
		event := WirelessEvent{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(line)))[:20], Time: field(1), Interface: field(2), Kind: kind, Summary: kinds[kind].title, Tone: kinds[kind].tone}
		event.Network = networks[event.Interface]
		if strings.HasPrefix(kind, "AP-STA-") {
			if mac := eventMAC.FindStringSubmatch(" " + tail); mac != nil {
				event.Client = strings.ToUpper(mac[1])
			}
		}
		if freq := eventFrequency.FindStringSubmatch(tail); freq != nil {
			event.Frequency = freq[1]
		}
		result = append(result, event)
	}
	return result
}
func tailEventFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	offset := info.Size() - 256*1024
	if offset < 0 {
		offset = 0
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(f, 256*1024))
	if offset > 0 {
		_, rest, ok := strings.Cut(string(raw), "\n")
		if ok {
			return rest, err
		}
		return "", err
	}
	return string(raw), err
}
func interfaceNetworks(root string, state APState) map[string]string {
	result := map[string]string{}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if !hostapdInterface.MatchString(entry.Name()) {
			continue
		}
		mac, _ := os.ReadFile(filepath.Join(root, entry.Name(), "address"))
		if strings.TrimSpace(string(mac)) == "" {
			continue
		}
		for _, ssid := range state.SSIDs {
			for _, vap := range ssid.BSSIDs {
				if strings.EqualFold(strings.TrimSpace(string(mac)), vap.BSSID) {
					result[entry.Name()] = ssid.Name
				}
			}
		}
	}
	return result
}
func readWirelessEvents(paths []string, networks map[string]string) WirelessEventLog {
	result := WirelessEventLog{Events: []WirelessEvent{}, SampledAt: time.Now()}
	seen := map[string]bool{}
	available := false
	for _, path := range paths {
		raw, err := tailEventFile(path)
		if err != nil {
			continue
		}
		available = true
		for _, event := range parseWirelessEvents(raw, networks) {
			if !seen[event.ID] {
				seen[event.ID] = true
				result.Events = append(result.Events, event)
			}
		}
	}
	if !available {
		result.Error = "Wireless event history is not available yet"
	}
	sort.SliceStable(result.Events, func(i, j int) bool { return result.Events[i].Time > result.Events[j].Time })
	if len(result.Events) > 150 {
		result.Events = result.Events[:150]
	}
	return result
}
func (a *API) wirelessEvents(w http.ResponseWriter, r *http.Request) {
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	if a.eventCache == nil || time.Since(a.eventCache.SampledAt) > 5*time.Second {
		value := readWirelessEvents([]string{"/var/log/hostapd.log", "/var/log/hostapd.log.1"}, interfaceNetworks("/sys/class/net", a.poller.Snapshot()))
		_, hw, _ := a.cli.Snapshot()
		value.ClockSynced = hw.NTPSynced
		a.eventCache = &value
	}
	reply(w, 200, a.eventCache)
}
