package main

// Prometheus metrics at /metrics, off by default. When enabled, scrapes must
// send the generated token as "Authorization: Bearer <token>".

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type MetricsSettings struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
}

func (c *Config) MetricsSettings() MetricsSettings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Metrics
}

func (c *Config) SetMetrics(m MetricsSettings) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.Metrics
	c.Metrics = m
	if err := c.saveLocked(); err != nil {
		c.Metrics = previous
		return err
	}
	return nil
}

func newToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (a *API) metricsSettings(w http.ResponseWriter, r *http.Request) {
	m := a.cfg.MetricsSettings()
	if !a.isAdmin(r) {
		m.Token = ""
	}
	reply(w, http.StatusOK, m)
}

func (a *API) updateMetrics(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled  bool `json:"enabled"`
		NewToken bool `json:"newToken"`
	}
	if !decode(w, r, &body) {
		return
	}
	m := a.cfg.MetricsSettings()
	m.Enabled = body.Enabled
	if body.NewToken || (m.Enabled && m.Token == "") {
		token, err := newToken()
		if err != nil {
			fail(w, http.StatusInternalServerError, "could not create a token")
			return
		}
		m.Token = token
	}
	if err := a.cfg.SetMetrics(m); err != nil {
		fail(w, http.StatusInternalServerError, "Could not save metrics settings")
		return
	}
	if !a.isAdmin(r) {
		m.Token = ""
	}
	reply(w, http.StatusOK, m)
}

type promWriter struct {
	b    strings.Builder
	seen map[string]bool
}

func promEscape(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

// metric writes one sample; labels are name/value pairs.
func (p *promWriter) metric(name, help, kind string, value float64, labels ...string) {
	if !p.seen[name] {
		p.seen[name] = true
		fmt.Fprintf(&p.b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	}
	p.b.WriteString(name)
	if len(labels) > 0 {
		p.b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				p.b.WriteByte(',')
			}
			fmt.Fprintf(&p.b, `%s="%s"`, labels[i], promEscape(labels[i+1]))
		}
		p.b.WriteByte('}')
	}
	fmt.Fprintf(&p.b, " %g\n", value)
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func renderMetrics(s stateResponse) string {
	p := &promWriter{seen: map[string]bool{}}
	d := s.Device
	p.metric("c460_info", "Access point identity.", "gauge", 1, "hostname", d.Hostname, "name", d.SiteName, "model", d.Model, "firmware", d.Firmware, "ui_version", d.UIVersion)
	p.metric("c460_up", "1 when the configuration agent answers.", "gauge", boolValue(s.Error == ""))
	p.metric("c460_uptime_seconds", "Time since the AP started.", "gauge", d.UptimeSeconds)
	if d.CPUUsage != nil {
		p.metric("c460_cpu_usage_ratio", "CPU busy time, 0 to 1.", "gauge", *d.CPUUsage/100)
	}
	if d.TemperatureC != nil {
		p.metric("c460_temperature_celsius", "Hottest temperature sensor.", "gauge", *d.TemperatureC)
	}
	p.metric("c460_memory_total_bytes", "Total memory.", "gauge", float64(d.MemTotal))
	p.metric("c460_memory_available_bytes", "Available memory.", "gauge", float64(d.MemAvailable))
	p.metric("c460_flash_free_bytes", "Free writable flash.", "gauge", float64(d.StorageFree))

	for _, r := range s.Radios {
		band := r.Band
		p.metric("c460_radio_enabled", "Radio switched on.", "gauge", boolValue(r.Enabled), "band", band)
		p.metric("c460_radio_channel", "Primary channel.", "gauge", float64(r.Channel), "band", band)
		width := r.Width
		if r.OperatingWidth != nil {
			width = *r.OperatingWidth
		}
		p.metric("c460_radio_channel_width_mhz", "Operating channel width.", "gauge", float64(width), "band", band)
		if r.EIRP != nil {
			p.metric("c460_radio_eirp_dbm", "Effective transmit power.", "gauge", *r.EIRP, "band", band)
		}
		if r.Utilization != nil {
			p.metric("c460_radio_utilization_ratio", "Channel utilisation, 0 to 1.", "gauge", *r.Utilization/100, "band", band)
		}
		if r.NoiseFloor != nil {
			p.metric("c460_radio_noise_floor_dbm", "Noise floor.", "gauge", *r.NoiseFloor, "band", band)
		}
		p.metric("c460_radio_neighbors", "Other access points heard.", "gauge", float64(r.Neighbors), "band", band)
	}
	clients := map[[2]string]int{}
	for _, c := range s.Clients {
		clients[[2]string{c.SSID, c.Band}]++
	}
	for _, ssid := range s.SSIDs {
		p.metric("c460_ssid_enabled", "Network broadcasting.", "gauge", boolValue(ssid.Enabled), "ssid", ssid.Name)
		for _, band := range ssid.Bands {
			p.metric("c460_clients", "Connected clients.", "gauge", float64(clients[[2]string{ssid.Name, band}]), "ssid", ssid.Name, "band", band)
		}
	}
	sorted := append([]Client(nil), s.Clients...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].MAC < sorted[j].MAC })
	for _, c := range sorted {
		labels := []string{"mac", c.MAC, "ssid", c.SSID, "band", c.Band, "hostname", c.Hostname}
		if c.RSSI != nil && *c.RSSI != 0 {
			p.metric("c460_client_rssi_dbm", "Client signal strength.", "gauge", *c.RSSI, labels...)
		}
		p.metric("c460_client_receive_bytes_total", "Bytes received from the client.", "counter", c.RxBytes, labels...)
		p.metric("c460_client_transmit_bytes_total", "Bytes sent to the client.", "counter", c.TxBytes, labels...)
	}
	for _, i := range snmpInterfaces(s.Interfaces) {
		port := fmt.Sprint(i.index)
		labels := []string{"port", port, "interface", i.Name, "role", i.Role}
		p.metric("c460_ethernet_up", "Ethernet link up.", "gauge", boolValue(i.Up), labels...)
		p.metric("c460_ethernet_speed_bits_per_second", "Negotiated link speed.", "gauge", float64(i.mbps())*1e6, labels...)
		p.metric("c460_ethernet_receive_bytes_total", "Bytes received.", "counter", i.InOctets, labels...)
		p.metric("c460_ethernet_transmit_bytes_total", "Bytes sent.", "counter", i.OutOctets, labels...)
		p.metric("c460_ethernet_receive_errors_total", "Receive errors.", "counter", i.InErrors, labels...)
		p.metric("c460_ethernet_transmit_errors_total", "Transmit errors.", "counter", i.OutErrors, labels...)
	}
	counts := map[string]int{"danger": 0, "warn": 0, "info": 0}
	for _, h := range s.Health {
		counts[h.Severity]++
	}
	for _, sev := range []string{"danger", "warn", "info"} {
		p.metric("c460_health_issues", "Open health check findings.", "gauge", float64(counts[sev]), "severity", sev)
	}
	return p.b.String()
}

var errMetricsOff = errors.New("metrics are disabled")

func (a *API) metricsEndpoint(w http.ResponseWriter, r *http.Request) {
	m := a.cfg.MetricsSettings()
	if !m.Enabled || m.Token == "" {
		http.Error(w, errMetricsOff.Error(), http.StatusNotFound)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(m.Token)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="c460-webui"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	mgmt, hw, errText := a.cli.Snapshot()
	s := stateResponse{APState: a.snapshot(), Management: mgmt, Hardware: hw, ManagementError: errText}
	s.Health = a.healthChecks(s)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(renderMetrics(s)))
}
