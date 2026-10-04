package main

// Health checks: problems worth knowing about, so nobody has to go looking
// for them. Severity "danger" and "warn" colour the status; "info" does not.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type HealthItem struct {
	ID       string `json:"id"`
	Severity string `json:"severity"` // danger, warn, info
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Link     string `json:"link,omitempty"` // UI route with more information
}

const weakSignalDBm = -75

// defaultPasswordCheck caches whether the administrator still uses the
// default password; bcrypt is too slow to run on every state request.
type defaultPasswordCheck struct {
	mu      sync.Mutex
	hash    string
	matches bool
}

func (d *defaultPasswordCheck) uses(auth *Auth) bool {
	f, err := auth.load()
	if err != nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if f.PasswordHash != d.hash {
		d.hash = f.PasswordHash
		d.matches = bcrypt.CompareHashAndPassword([]byte(f.PasswordHash), []byte(DefaultUsername)) == nil
	}
	return d.matches
}

// radarEvents counts radar detections in the cached wireless event log.
func (a *API) radarEvents() int {
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	if a.eventCache == nil || time.Since(a.eventCache.SampledAt) > time.Minute {
		value := readWirelessEvents([]string{"/var/log/hostapd.log", "/var/log/hostapd.log.1"}, interfaceNetworks("/sys/class/net", a.poller.Snapshot()))
		_, hw, _ := a.cli.Snapshot()
		value.ClockSynced = hw.NTPSynced
		a.eventCache = &value
	}
	n := 0
	for _, e := range a.eventCache.Events {
		if e.Kind == "DFS-RADAR-DETECTED" {
			n++
		}
	}
	return n
}

// recentUtilization averages a radio's utilisation over the last minutes.
func (a *API) recentUtilization(band string, fallback *float64) *float64 {
	var sum float64
	n := 0
	for _, p := range a.history.Points(time.Now().Add(-5 * time.Minute)) {
		if v, ok := p.Util[band]; ok {
			sum += v
			n++
		}
	}
	if n == 0 {
		return fallback
	}
	avg := sum / float64(n)
	return &avg
}

func (a *API) healthChecks(s stateResponse) []HealthItem {
	items := []HealthItem{}
	add := func(id, severity, title, detail, link string) {
		items = append(items, HealthItem{ID: id, Severity: severity, Title: title, Detail: detail, Link: link})
	}
	if s.Error != "" {
		add("agent", "danger", "Cannot read the AP configuration", s.Error, "")
		return items
	}
	if a.defaultPassword.uses(a.auth) {
		add("password", "warn", "Default password in use", "Anyone on the network can sign in with config/config. Change it under System.", "/system")
	}
	if s.Device.SiteName == "" {
		add("name", "info", "This AP has no name yet", "A name such as “Stage left” makes it easy to tell several APs apart here, in SNMP and in Prometheus.", "/system")
	}
	if t := s.Device.TemperatureC; t != nil && *t > 75 {
		severity := "warn"
		if *t > 85 {
			severity = "danger"
		}
		add("temperature", severity, fmt.Sprintf("AP is hot (%.0f °C)", *t), "Check ventilation and that the AP is not in direct sun.", "/system")
	}
	if s.Hardware.NTPSynced != nil && !*s.Hardware.NTPSynced {
		add("clock", "warn", "Clock not synchronised", "Event times and schedules depend on the clock. Point the time servers at your router or a reachable NTP server.", "/network")
	}
	// "Low Power Indoor" in the radio power field is the 6 GHz regulatory
	// class, not a power shortage; only the PoE class tells that.
	if src := strings.ToLower(s.Hardware.PowerSource); strings.Contains(src, "802.3af") {
		add("power", "warn", "Running on reduced power", "802.3af PoE does not supply enough for full radio performance. Use 802.3at PoE or better.", "/system")
	}
	up := 0
	for _, iface := range s.Interfaces {
		if iface.Up {
			up++
		}
	}
	if len(s.Interfaces) > 1 && up == 1 {
		add("backup-uplink", "info", "No backup uplink", "Only one Ethernet port has a link. If its cable or switch port fails, the AP goes offline.", "/network")
	}
	for _, r := range s.Radios {
		if !r.Enabled {
			add("radio-off-"+r.Band, "info", fmt.Sprintf("%s GHz radio is off", r.Band), "No networks broadcast on this band.", "/radios")
			continue
		}
		if u := a.recentUtilization(r.Band, r.Utilization); u != nil && *u >= 70 {
			severity := "warn"
			if *u >= 85 {
				severity = "danger"
			}
			add("utilization-"+r.Band, severity, fmt.Sprintf("%s GHz channel %d is busy (%.0f %%)", r.Band, r.Channel, *u), "Clients will be slow. Try a quieter channel or a narrower width.", "/radios")
		}
	}
	if n := a.radarEvents(); n > 0 {
		add("radar", "warn", fmt.Sprintf("Radar detected %d× recently", n), "The 5 GHz radio had to leave a DFS channel. A channel outside 52–144 avoids this.", "/events")
	}
	weak := 0
	for _, c := range s.Clients {
		if c.RSSI != nil && *c.RSSI < weakSignalDBm && *c.RSSI != 0 {
			weak++
		}
	}
	if weak > 0 {
		add("weak-clients", "info", fmt.Sprintf("%d %s with weak signal", weak, map[bool]string{true: "client", false: "clients"}[weak == 1]),
			fmt.Sprintf("Below %d dBm. These devices are far away or behind walls and slow everyone down.", weakSignalDBm), "/clients")
	}
	if s.Management.PendingBoot {
		add("pending-boot", "info", "Management changes waiting for a restart", "The new management network applies when the AP restarts.", "/network")
	}
	for _, ssid := range s.SSIDs {
		if ssid.MixedStatus != "" && ssid.MixedStatus != "applied" && ssid.MixedStatus != "pending" {
			add("mixed-"+ssid.Name, "warn", "WPA2/WPA3 mixed mode not active on “"+ssid.Name+"”", ssid.MixedStatus+". The network still works for WPA3 devices.", "/wireless")
		}
	}
	if s.Device.MemTotal > 0 && float64(s.Device.MemTotal-s.Device.MemAvailable)/float64(s.Device.MemTotal) > 0.9 {
		add("memory", "warn", "Memory almost full", "Less than 10 % of the AP's memory is available.", "/system")
	}
	if s.Device.StorageTotal > 0 && float64(s.Device.StorageTotal-s.Device.StorageFree)/float64(s.Device.StorageTotal) > 0.9 {
		add("flash", "warn", "Flash almost full", "Less than 10 % of the writable flash is free.", "/system")
	}
	return items
}
