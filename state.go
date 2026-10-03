package main

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------- UI model

type APState struct {
	GeneratedAt time.Time         `json:"generatedAt"`
	PollSeconds int               `json:"pollSeconds"`
	Error       string            `json:"error,omitempty"`
	Device      Device            `json:"device"`
	Radios      []Radio           `json:"radios"`
	SSIDs       []SSID            `json:"ssids"`
	Clients     []Client          `json:"clients"`
	Neighbors   []Neighbor        `json:"neighbors"`
	Interfaces  []Interface       `json:"interfaces"`
	VLANNames   map[string]string `json:"vlanNames"`
}

type Radio struct {
	ID              int      `json:"id"`
	Band            string   `json:"band"`
	Frequency       string   `json:"frequency"`
	Enabled         bool     `json:"enabled"`
	Channel         int      `json:"channel"`
	Width           int      `json:"width"`
	PowerRequested  int      `json:"powerRequested"`
	EIRP            *float64 `json:"eirp"`
	MaxEIRP         *float64 `json:"maxEirp"`
	MaxTxPower      *float64 `json:"maxTxPower"`
	AllowedChannels []int    `json:"allowedChannels"`
	Utilization     *float64 `json:"utilization"`
	RxUtilization   *float64 `json:"rxUtilization"`
	TxUtilization   *float64 `json:"txUtilization"`
	NoiseFloor      *float64 `json:"noiseFloor"`
	DCA             bool     `json:"dca"`
	DTP             bool     `json:"dtp"`
	Scanning        bool     `json:"scanning"`
	BaseMAC         string   `json:"baseMac"`
	Clients         int      `json:"clients"`
	BSSIDs          int      `json:"bssids"`
	Neighbors       int      `json:"neighbors"`
}

type BSSID struct {
	BSSID   string `json:"bssid"`
	RadioID int    `json:"radioId"`
	Band    string `json:"band"`
	Clients int    `json:"clients"`
}

type SSID struct {
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Hidden      bool     `json:"hidden"`
	OpMode      string   `json:"opmode"`
	Bands       []string `json:"bands"`
	VLAN        *int     `json:"vlan"`
	Isolation   bool     `json:"isolation"`
	MFP         bool     `json:"mfp"`
	HasPassword bool     `json:"hasPassword"`
	MixedStatus string   `json:"mixedStatus,omitempty"` // WPA2/WPA3 mixed: "applied", "pending" or a problem
	BSSIDs      []BSSID  `json:"bssids"`
	Clients     int      `json:"clients"`
	RxBytes     float64  `json:"rxBytes"`
	TxBytes     float64  `json:"txBytes"`
}

type Client struct {
	MAC            string     `json:"mac"`
	SSID           string     `json:"ssid"`
	Band           string     `json:"band"`
	VLAN           *int       `json:"vlan"`
	IPv4           string     `json:"ipv4"`
	IPv4Source     string     `json:"ipv4Source,omitempty"`
	IPv6           []string   `json:"ipv6"`
	Hostname       string     `json:"hostname"`
	OS             string     `json:"os"`
	Username       string     `json:"username"`
	State          string     `json:"state"`
	Mode           string     `json:"mode"`
	RSSI           *float64   `json:"rssi"`
	SNR            *float64   `json:"snr"`
	TxRate         *float64   `json:"txRate"`
	RxRate         *float64   `json:"rxRate"`
	Streams        *float64   `json:"streams"`
	ConnectedSince *time.Time `json:"connectedSince"`
	RxBytes        float64    `json:"rxBytes"`
	TxBytes        float64    `json:"txBytes"`
	Retries        float64    `json:"retries"`
}

type Neighbor struct {
	RadioID        int        `json:"radioId"`
	Band           string     `json:"band"`
	BSSID          string     `json:"bssid"`
	SSID           string     `json:"ssid"`
	Channel        int        `json:"channel"`
	PrimaryChannel int        `json:"primaryChannel"`
	RSSI           *float64   `json:"rssi"`
	OpMode         string     `json:"opmode"`
	LastSeen       *time.Time `json:"lastSeen"`
}

type Interface struct {
	Name       string  `json:"name"`
	Up         bool    `json:"up"`
	Speed      string  `json:"speed"`
	Duplex     string  `json:"duplex"`
	MAC        string  `json:"mac"`
	InOctets   float64 `json:"inOctets"`
	OutOctets  float64 `json:"outOctets"`
	InErrors   float64 `json:"inErrors"`
	OutErrors  float64 `json:"outErrors"`
	InDiscard  float64 `json:"inDiscards"`
	OutDiscard float64 `json:"outDiscards"`
	// Port is the physical socket number (ETH 1/2, "LAN1/2" in the vendor CLI).
	// The firmware swaps eth0/eth1 so that eth0 is always the active uplink.
	Port int    `json:"port"`
	Role string `json:"role"` // "uplink" or "backup"
}

// ------------------------------------------------------------------ poller

type Poller struct {
	gnmi     *GNMI
	cfg      *Config
	interval time.Duration
	trigger  chan struct{}

	mu    sync.RWMutex
	raw   map[string]any // normalized access-point tree, including secrets; never sent to clients
	state APState

	observers []func(APState) // called after each successful sample
}

// OnSample registers f to receive every successful sample; register before Run.
func (p *Poller) OnSample(f func(APState)) { p.observers = append(p.observers, f) }

func NewPoller(g *GNMI, cfg *Config, interval time.Duration) *Poller {
	return &Poller{gnmi: g, cfg: cfg, interval: interval, trigger: make(chan struct{}, 1),
		state: APState{PollSeconds: cfg.PollSeconds, VLANNames: map[string]string{}}}
}

func (p *Poller) Run(ctx context.Context) {
	for ctx.Err() == nil {
		started := time.Now()
		p.poll(ctx)
		p.mu.RLock()
		interval := p.interval
		p.mu.RUnlock()
		// Schedule from the start of the read, with no overlap or catch-up burst.
		delay := interval - time.Since(started)
		if delay < 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-p.trigger:
			timer.Stop()
		}
	}
}
func (p *Poller) SetInterval(seconds int) {
	p.mu.Lock()
	p.interval = time.Duration(seconds) * time.Second
	p.state.PollSeconds = seconds
	p.mu.Unlock()
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}

// Refresh asks for an immediate poll, and another one shortly after, because
// the AP applies configuration asynchronously.
func (p *Poller) Refresh() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
	time.AfterFunc(4*time.Second, func() {
		select {
		case p.trigger <- struct{}{}:
		default:
		}
	})
}

func (p *Poller) poll(ctx context.Context) {
	if st, ok := p.sample(ctx); ok {
		for _, f := range p.observers {
			f(st)
		}
	}
}

func (p *Poller) sample(ctx context.Context) (APState, bool) {
	device := readDevice(p.cfg)
	raw, err := p.gnmi.GetAP(ctx)
	var tree map[string]any
	if err == nil {
		var decoded any
		if err = json.Unmarshal(raw, &decoded); err == nil {
			tree, _ = normalize(decoded).(map[string]any)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		log.Printf("poll: %v", err)
		p.state.Error = "Cannot read the access point configuration: " + err.Error()
		p.state.Device = device
		return APState{}, false
	}
	p.raw = tree
	st := build(tree)
	supplementClientAddresses(st.Clients, readARPAddresses())
	supplementEthernet(st.Interfaces, "/sys/class/net")
	annotatePorts(st.Interfaces, "/sys/class/net")
	st.Device = device
	st.Device.Hostname = p.gnmi.host
	if v, ok := dig(tree, "system", "ssh-server", "config", "enable").(bool); ok {
		st.Device.SSHEnabled = v
	}
	st.GeneratedAt = time.Now()
	st.PollSeconds = p.cfg.RefreshSeconds()
	_, st.VLANNames = p.cfg.Labels()
	p.state = st
	return st, true
}

func (p *Poller) Snapshot() APState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// SSIDConfig returns a copy of the current config container of an SSID.
func (p *Poller) SSIDConfig(name string) (map[string]any, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, s := range items(p.raw, "ssids", "ssid") {
		if str(dig(s, "name")) == name {
			cfg, _ := dig(s, "config").(map[string]any)
			return clone(cfg), true
		}
	}
	return nil, false
}

// RadioConfig returns a copy of the config container and the band identity of a radio.
func (p *Poller) RadioConfig(id int) (map[string]any, string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, r := range items(p.raw, "radios", "radio") {
		if rid, ok := num(dig(r, "id")); ok && int(rid) == id {
			cfg, _ := dig(r, "config").(map[string]any)
			return clone(cfg), ident(str(dig(r, "operating-frequency"))), true
		}
	}
	return nil, "", false
}

// ------------------------------------------------------------- conversion

func build(tree map[string]any) APState {
	st := APState{Radios: []Radio{}, SSIDs: []SSID{}, Clients: []Client{}, Neighbors: []Neighbor{}, Interfaces: []Interface{}}
	radioBand := map[int]string{}
	radioClients := map[int]int{}
	radioBSSIDs := map[int]int{}

	for _, r := range items(tree, "radios", "radio") {
		id := intv(dig(r, "id"))
		radioBand[id] = band(ident(str(dig(r, "operating-frequency"))))
	}

	for _, s := range items(tree, "ssids", "ssid") {
		cfg := dig(s, "config")
		name := str(dig(s, "name"))
		ssid := SSID{
			Name:        name,
			Enabled:     boolv(dig(cfg, "enabled")),
			Hidden:      boolv(dig(cfg, "hidden")),
			OpMode:      ident(str(dig(cfg, "opmode"))),
			Bands:       bandsOf(ident(str(dig(cfg, "operating-frequency")))),
			Isolation:   boolv(dig(cfg, "station-isolation")),
			MFP:         boolv(dig(cfg, "mfp")),
			HasPassword: str(dig(cfg, "wpa3-psk")) != "" || str(dig(cfg, "wpa2-psk")) != "",
			BSSIDs:      []BSSID{},
		}
		if v, ok := num(dig(cfg, "default-vlan")); ok {
			vlan := int(v)
			ssid.VLAN = &vlan
		}
		for _, b := range items(s, "bssids", "bssid") {
			rid := intv(dig(b, "radio-id"))
			n := intv(dig(b, "state", "num-associated-clients"))
			ssid.BSSIDs = append(ssid.BSSIDs, BSSID{BSSID: str(dig(b, "bssid")), RadioID: rid, Band: radioBand[rid], Clients: n})
			radioBSSIDs[rid]++
			ssid.RxBytes += numz(dig(b, "state", "counters", "rx-bytes-data"))
			ssid.TxBytes += numz(dig(b, "state", "counters", "tx-bytes-data"))
		}
		sort.Slice(ssid.BSSIDs, func(i, j int) bool { return ssid.BSSIDs[i].RadioID < ssid.BSSIDs[j].RadioID })
		for _, c := range items(s, "clients", "client") {
			cl := buildClient(c, name, ssid.VLAN)
			for rid, b := range radioBand {
				if b == cl.Band && cl.Band != "" {
					radioClients[rid]++
				}
			}
			st.Clients = append(st.Clients, cl)
			ssid.Clients++
		}
		st.SSIDs = append(st.SSIDs, ssid)
	}
	sort.Slice(st.SSIDs, func(i, j int) bool { return st.SSIDs[i].Name < st.SSIDs[j].Name })
	sort.Slice(st.Clients, func(i, j int) bool {
		return st.Clients[i].SSID+st.Clients[i].MAC < st.Clients[j].SSID+st.Clients[j].MAC
	})

	for _, r := range items(tree, "radios", "radio") {
		cfg, state := dig(r, "config"), dig(r, "state")
		id := intv(dig(r, "id"))
		freq := ident(str(dig(r, "operating-frequency")))
		radio := Radio{
			ID: id, Band: band(freq), Frequency: freq,
			Enabled:         boolv(dig(cfg, "enabled")),
			Channel:         intv(firstOf(dig(state, "channel"), dig(cfg, "channel"))),
			Width:           intv(firstOf(dig(state, "channel-width"), dig(cfg, "channel-width"))),
			PowerRequested:  intv(dig(cfg, "transmit-power")),
			EIRP:            ptr(dig(state, "transmit-eirp")),
			MaxEIRP:         ptr(dig(state, "allowed-max-eirp")),
			MaxTxPower:      ptr(dig(state, "allowed-max-txpower")),
			AllowedChannels: ints(firstOf(dig(state, "allowed-regulatory-channels"), dig(state, "supported-channels"))),
			Utilization:     ptr(dig(state, "total-channel-utilization")),
			RxUtilization:   ptr(dig(state, "rx-dot11-channel-utilization")),
			TxUtilization:   ptr(dig(state, "tx-dot11-channel-utilization")),
			NoiseFloor:      ptr(dig(state, "counters", "noise-floor")),
			DCA:             boolv(dig(cfg, "dca")),
			DTP:             boolv(dig(cfg, "dtp")),
			Scanning:        boolv(dig(cfg, "scanning")),
			BaseMAC:         str(dig(state, "base-radio-mac")),
			Clients:         radioClients[id],
			BSSIDs:          radioBSSIDs[id],
		}
		for _, n := range items(r, "neighbors", "neighbor") {
			ns := dig(n, "state")
			nb := Neighbor{
				RadioID: id, Band: radio.Band,
				BSSID:          str(firstOf(dig(n, "bssid"), dig(ns, "bssid"))),
				SSID:           str(dig(ns, "ssid")),
				Channel:        intv(dig(ns, "channel")),
				PrimaryChannel: intv(dig(ns, "primary-channel")),
				RSSI:           ptr(dig(ns, "rssi")),
				OpMode:         ident(str(dig(ns, "opmode"))),
				LastSeen:       nanoTime(dig(ns, "last-seen")),
			}
			st.Neighbors = append(st.Neighbors, nb)
			radio.Neighbors++
		}
		st.Radios = append(st.Radios, radio)
	}
	sort.Slice(st.Radios, func(i, j int) bool { return bandOrder(st.Radios[i].Band) < bandOrder(st.Radios[j].Band) })
	sort.Slice(st.Neighbors, func(i, j int) bool {
		a, b := st.Neighbors[i].RSSI, st.Neighbors[j].RSSI
		if a == nil || b == nil {
			return b == nil && a != nil
		}
		return *a > *b
	})

	for _, it := range items(tree, "interfaces", "interface") {
		state := dig(it, "state")
		eth := dig(it, "ethernet", "state")
		st.Interfaces = append(st.Interfaces, Interface{
			Name:       str(dig(it, "name")),
			Up:         strings.EqualFold(str(dig(state, "oper-status")), "UP"),
			Speed:      speed(ident(str(dig(eth, "negotiated-port-speed")))),
			Duplex:     str(dig(eth, "negotiated-duplex-mode")),
			MAC:        strings.ToUpper(str(dig(eth, "hw-mac-address"))),
			InOctets:   numz(dig(state, "counters", "in-octets")),
			OutOctets:  numz(dig(state, "counters", "out-octets")),
			InErrors:   numz(dig(state, "counters", "in-errors")),
			OutErrors:  numz(dig(state, "counters", "out-errors")),
			InDiscard:  numz(dig(state, "counters", "in-discards")),
			OutDiscard: numz(dig(state, "counters", "out-discards")),
		})
	}
	sort.Slice(st.Interfaces, func(i, j int) bool { return st.Interfaces[i].Name < st.Interfaces[j].Name })
	return st
}

func buildClient(c any, ssid string, vlan *int) Client {
	conn := dig(c, "client-connection", "state")
	rf := dig(c, "client-rf", "state")
	counters := dig(c, "state", "counters")
	cl := Client{
		MAC:      strings.ToUpper(str(firstOf(dig(c, "mac"), dig(c, "state", "mac")))),
		SSID:     ssid,
		VLAN:     vlan,
		IPv4:     str(dig(conn, "ipv4-address")),
		IPv6:     strs(dig(conn, "ipv6-addresses")),
		Hostname: str(dig(conn, "hostname")),
		OS:       str(dig(conn, "operating-system")),
		Username: str(dig(conn, "username")),
		State:    ident(str(dig(conn, "client-state"))),
		Mode:     ident(str(dig(rf, "connection-mode"))),
		RSSI:     ptr(dig(rf, "rssi")),
		SNR:      ptr(dig(rf, "snr")),
		TxRate:   ptr(firstOf(dig(rf, "tx-phy-rate"), dig(rf, "phy-rate"))),
		RxRate:   ptr(dig(rf, "rx-phy-rate")),
		Streams:  ptr(dig(rf, "ss")),
		RxBytes:  numz(dig(counters, "rx-bytes")),
		TxBytes:  numz(dig(counters, "tx-bytes")),
		Retries:  numz(dig(counters, "tx-retries")) + numz(dig(counters, "rx-retries")),
	}
	switch intv(dig(rf, "frequency")) {
	case 2:
		cl.Band = "2.4"
	case 5:
		cl.Band = "5"
	case 6:
		cl.Band = "6"
	}
	if t := connectionTime(dig(conn, "connection-time")); t != nil {
		cl.ConnectedSince = t
	}
	return cl
}

// ----------------------------------------------------------------- helpers

// normalize strips YANG module prefixes from object keys ("openconfig-x:foo" -> "foo").
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if i := strings.LastIndexByte(k, ':'); i >= 0 {
				k = k[i+1:]
			}
			out[k] = normalize(val)
		}
		return out
	case []any:
		for i := range t {
			t[i] = normalize(t[i])
		}
		return t
	}
	return v
}

func clone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func dig(v any, path ...string) any {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

func items(v any, container, list string) []any {
	l, _ := dig(v, container, list).([]any)
	return l
}

func firstOf(vs ...any) any {
	for _, v := range vs {
		if v != nil {
			return v
		}
	}
	return nil
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

func strs(v any) []string {
	out := []string{}
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s := str(x); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func num(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	}
	return 0, false
}

func numz(v any) float64 { f, _ := num(v); return f }
func intv(v any) int     { f, _ := num(v); return int(f) }

func ptr(v any) *float64 {
	if f, ok := num(v); ok {
		return &f
	}
	return nil
}

func ints(v any) []int {
	out := []int{}
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if f, ok := num(x); ok {
				out = append(out, int(f))
			}
		}
	}
	sort.Ints(out)
	return out
}

func boolv(v any) bool { b, _ := v.(bool); return b }

func ident(s string) string {
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func band(freq string) string {
	switch freq {
	case "FREQ_2GHZ", "FREQ_2_4_GHZ":
		return "2.4"
	case "FREQ_5GHZ":
		return "5"
	case "FREQ_6GHZ":
		return "6"
	}
	return ""
}

func bandOrder(b string) int {
	return map[string]int{"2.4": 0, "5": 1, "6": 2}[b]
}

// bandsOf turns FREQ_2_5_6_GHZ into ["2.4","5","6"].
func bandsOf(freq string) []string {
	out := []string{}
	for _, part := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(freq, "FREQ_"), "GHZ"), "_") {
		switch part {
		case "2":
			out = append(out, "2.4")
		case "5", "6":
			out = append(out, part)
		}
	}
	return out
}

// bandEnum is the inverse of bandsOf.
func bandEnum(bands []string) (string, bool) {
	has := map[string]bool{}
	for _, b := range bands {
		has[b] = true
	}
	var parts []string
	for _, b := range []string{"2.4", "5", "6"} {
		if has[b] {
			parts = append(parts, strings.TrimSuffix(b, ".4"))
		}
	}
	if len(parts) == 0 || len(parts) != len(has) {
		return "", false
	}
	if len(parts) == 1 {
		return "FREQ_" + parts[0] + "GHZ", true // FREQ_5GHZ
	}
	return "FREQ_" + strings.Join(parts, "_") + "_GHZ", true // FREQ_2_5_GHZ
}

func speed(s string) string {
	s = strings.TrimPrefix(s, "SPEED_")
	return strings.Replace(strings.Replace(s, "GB", " Gbit/s", 1), "MB", " Mbit/s", 1)
}

// nanoTime converts a nanosecond Unix timestamp (as sent by this agent) to a time.
func nanoTime(v any) *time.Time {
	f, ok := num(v)
	if !ok || f <= 0 {
		return nil
	}
	t := time.Unix(0, int64(f))
	return &t
}

// connectionTime accepts either a Unix timestamp in ns/s or a duration in seconds.
func connectionTime(v any) *time.Time {
	f, ok := num(v)
	if !ok || f <= 0 {
		return nil
	}
	switch {
	case f > 1e17:
		t := time.Unix(0, int64(f))
		return &t
	case f > 1e9:
		t := time.Unix(int64(f), 0)
		return &t
	default:
		t := time.Now().Add(-time.Duration(f) * time.Second)
		return &t
	}
}
