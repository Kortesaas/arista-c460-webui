package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Use the installed vendor IFB redirection path. It covers IPv4 and IPv6,
// shares an SSID cap across bands and matches client Ethernet MAC addresses.
// The script can return success despite failed tc commands: inspect the kernel.
type trafficProfile struct {
	ID         string
	Interfaces []string
	NativeQoS  TrafficQoS
}

var trafficDataInterface = regexp.MustCompile(`^ath[0-9]{2,3}(?:\.[0-9]{1,4})?$`)
var trafficQoSMu sync.Mutex
var trafficQoSApplied = map[string]string{}

func nativeTrafficAvailable() bool {
	if !nativePolicyAvailable() {
		return false
	}
	for _, p := range []string{"/sbin/tc_wrapper.sh", "/bin/tc", "/sys/class/net/ifb0", "/sys/class/net/ifb15"} {
		if _, e := os.Stat(p); e != nil {
			return false
		}
	}
	return true
}
func readTrafficProfile(name string) (trafficProfile, bool, error) {
	p := trafficProfile{}
	vaps, _, e := nativeSections(nativeAPConf)
	if e != nil {
		return p, false, e
	}
	var v map[string]string
	for _, entry := range vaps {
		if entry["AP_SSID"] == name {
			if v != nil {
				return p, false, errors.New("Ambiguous wireless profile")
			}
			v = entry
		}
	}
	if v == nil {
		return p, true, nil
	}
	p.ID = v["SSID_PROFILE_ID"]
	if !policyProfileID.MatchString(p.ID) || len(p.ID) > 10 {
		return p, false, errors.New("Invalid wireless traffic profile ID")
	}
	q := TrafficQoS{Priority: "voice", Mode: "ceiling", Mapping: "dscp"}
	if n, e := strconv.Atoi(v["QOS_SSID_PRIORITY"]); e == nil {
		if n < 0 || n > 3 {
			return p, false, errors.New("Unknown native QoS priority")
		}
		q.Priority = []string{"voice", "video", "best-effort", "background"}[n]
	}
	if v["QOS_PRIORITY_TYPE"] == "1" {
		q.Mode = "fixed"
	}
	if n, e := strconv.Atoi(v["QOS_DOWNSTR_MAP"]); e == nil {
		if n < 0 || n > 2 {
			return p, false, errors.New("Unknown native QoS mapping")
		}
		q.Mapping = []string{"8021p", "dscp", "tos"}[n]
	}
	q.Mark8021p = v["QOS_UPSTR_MARK_802_1p"] == "1"
	q.MarkDSCP = v["QOS_UPSTR_MARK_DSCP_TOS"] == "1"
	if v["QOS_UPSTR_MARK_DSCP_TOS"] == "2" {
		return p, false, errors.New("Native upstream TOS marking requires a separate adapter")
	}
	p.NativeQoS = q
	raw, e := os.ReadFile("/tmp/profile" + p.ID + "/profile.conf")
	if errors.Is(e, os.ErrNotExist) {
		return p, true, nil
	}
	if e != nil {
		return p, false, e
	}
	list := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if s, ok := vapValue(line, "DP_INTF_LIST"); ok {
			if list != "" {
				return p, false, errors.New("Repeated traffic interface mapping")
			}
			list = s
		}
	}
	if list == "" {
		return p, true, nil
	}
	for _, iface := range strings.Split(list, ",") {
		iface = strings.TrimSpace(iface)
		if !trafficDataInterface.MatchString(iface) || slices.Contains(p.Interfaces, iface) {
			return p, false, errors.New("Invalid traffic interface mapping")
		}
		if _, e := os.Stat("/sys/class/net/" + iface); e != nil {
			continue
		}
		p.Interfaces = append(p.Interfaces, iface)
	}
	active := false
	for _, iface := range p.Interfaces {
		base, _, _ := strings.Cut(iface, ".")
		if policyInterfaceUp(base) {
			active = true
		}
	}
	return p, !active, nil
}
func trafficEffectiveClients(name string, p TrafficPolicy, st APState) (map[string]TrafficLimits, error) {
	result := map[string]TrafficLimits{}
	for _, c := range st.Clients {
		if c.SSID != name {
			continue
		}
		mac, e := trafficMAC(c.MAC)
		if e != nil {
			continue
		}
		if trafficLimited(p.PerClient) {
			result[mac] = p.PerClient
		}
	}
	for mac, l := range p.Clients {
		if trafficLimited(l) {
			result[mac] = l
		} else {
			delete(result, mac)
		}
	}
	if len(result) > 512 {
		return nil, errors.New("Too many clients for traffic shaping on this network")
	}
	return result, nil
}
func trafficClassIDs(clients map[string]TrafficLimits) map[string]string {
	keys := make([]string, 0, len(clients))
	for mac := range clients {
		keys = append(keys, mac)
	}
	slices.Sort(keys)
	result := map[string]string{}
	for i, mac := range keys {
		result[mac] = strconv.Itoa(i + 10)
	}
	return result
}
func trafficIFBs(profile string) (string, string, error) {
	raw, e := os.ReadFile("/tmp/used_ifb_numbers")
	if errors.Is(e, os.ErrNotExist) {
		return "", "", nil
	}
	if e != nil {
		return "", "", e
	}
	used := map[int]string{}
	found := -1
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		a, b, ok := strings.Cut(line, ":")
		n, e := strconv.Atoi(a)
		if !ok || e != nil || n < 0 || n > 15 || !policyProfileID.MatchString(b) {
			return "", "", errors.New("Native traffic queue mapping is unreadable")
		}
		if _, ok := used[n]; ok {
			return "", "", errors.New("Native traffic queues have duplicate ownership")
		}
		used[n] = b
		if b == profile {
			if found != -1 {
				return "", "", errors.New("Native traffic profile has duplicate queues")
			}
			found = n
		}
	}
	if found < 0 {
		return "", "", nil
	}
	down, up := fmt.Sprintf("ifb%d", found*2), fmt.Sprintf("ifb%d", found*2+1)
	for _, name := range []string{down, up} {
		if _, e := os.Stat("/sys/class/net/" + name); e != nil {
			return "", "", errors.New("Native traffic queue capacity is exhausted")
		}
	}
	return down, up, nil
}
func trafficFreeIFBPair() error {
	raw, e := os.ReadFile("/tmp/used_ifb_numbers")
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	used := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		a, _, ok := strings.Cut(line, ":")
		n, e := strconv.Atoi(a)
		if !ok || e != nil {
			return errors.New("Native traffic queue mapping is unreadable")
		}
		used[n] = true
	}
	for n := 0; n < 8; n++ {
		if !used[n] {
			return nil
		}
	}
	return errors.New("All eight native SSID traffic queue pairs are in use")
}
func nativeTrafficCommand(ctx context.Context, args ...string) error {
	_, e := runVendorTool(ctx, "/bin/sh", append([]string{"/sbin/tc_wrapper.sh"}, args...)...)
	return e
}
func trafficQoSFlags(q TrafficQoS) int {
	pr := map[string]int{"voice": 3, "video": 2, "best-effort": 0, "background": 1}[q.Priority]
	if q.Mode == "fixed" {
		pr += 4
	}
	pr += 8 * map[string]int{"8021p": 0, "dscp": 1, "tos": 2}[q.Mapping]
	if q.Mark8021p {
		pr += 32
	}
	if q.MarkDSCP {
		pr += 64
	}
	return pr
}
func trafficQoSSignature(p trafficProfile, q *TrafficQoS) string {
	if q == nil {
		return ""
	}
	raw, _ := json.Marshal(q)
	s := string(raw)
	for _, iface := range p.Interfaces {
		base, _, _ := strings.Cut(iface, ".")
		index, e := os.ReadFile("/sys/class/net/" + base + "/ifindex")
		if e != nil {
			return "unavailable"
		}
		s += "|" + base + ":" + strings.TrimSpace(string(index))
	}
	return s
}
func clearNativeTraffic(ctx context.Context, profile trafficProfile) error {
	down, up, e := trafficIFBs(profile.ID)
	if e != nil || down == "" {
		return e
	}
	if e = nativeTrafficCommand(ctx, "clear", "profile", profile.ID, "bss", "up_pri", "0", "down_pri", "0"); e != nil {
		return fmt.Errorf("Clear prior traffic queues: %w", e)
	}
	remaining, _, e := trafficIFBs(profile.ID)
	if e != nil {
		return e
	}
	if remaining != "" {
		return errors.New("Native traffic queue ownership was not released")
	}
	for _, iface := range []string{down, up} {
		raw, e := trafficTC(ctx, "qdisc", iface)
		if e != nil {
			return e
		}
		var rows []nativeTrafficQdisc
		if json.Unmarshal(raw, &rows) != nil {
			return errors.New("Cleared traffic queues are unreadable")
		}
		for _, row := range rows {
			if row.Kind == "tbf" || row.Kind == "htb" {
				return errors.New("Old bandwidth queues were not removed")
			}
		}
	}
	for _, iface := range profile.Interfaces {
		for _, parent := range []string{"1:", "ffff:"} {
			raw, e := trafficTC(ctx, "filter", iface, "parent", parent)
			if e != nil {
				return e
			}
			var rows []struct {
				Options struct {
					Actions []struct {
						Kind   string `json:"kind"`
						Device string `json:"to_dev"`
					} `json:"actions"`
				} `json:"options"`
			}
			if json.Unmarshal(raw, &rows) != nil {
				return errors.New("Cleared traffic redirection is unreadable")
			}
			for _, row := range rows {
				for _, a := range row.Options.Actions {
					if a.Kind == "mirred" && (a.Device == down || a.Device == up) {
						return errors.New("Old traffic redirection was not removed")
					}
				}
			}
		}
	}
	return nil
}
func applyNativeTraffic(ctx context.Context, name string, p TrafficPolicy, st APState) error {
	profile, pending, e := readTrafficProfile(name)
	if e != nil {
		return e
	}
	if pending {
		// Scheduled-off profiles can retain old IFB ownership. Release it before
		// a rename/delete removes the profile mapping, without starting a radio.
		if profile.ID != "" && trafficEqual(p, defaultTrafficPolicy()) {
			return clearNativeTraffic(ctx, profile)
		}
		return nil
	}
	clients, e := trafficEffectiveClients(name, p, st)
	if e != nil {
		return e
	}
	down, _, e := trafficIFBs(profile.ID)
	if e != nil {
		return e
	}
	if down == "" && (trafficLimited(p.Bandwidth) || len(clients) > 0) {
		if e = trafficFreeIFBPair(); e != nil {
			return e
		}
	}
	if down != "" {
		if e = clearNativeTraffic(ctx, profile); e != nil {
			return e
		}
	}
	if trafficLimited(p.Bandwidth) {
		if e = nativeTrafficCommand(ctx, "limit", "profile", profile.ID, "bss", "up", strconv.Itoa(trafficRate(p.Bandwidth.UploadKbps)), "down", strconv.Itoa(trafficRate(p.Bandwidth.DownloadKbps)), "up_pri", "0", "down_pri", "0"); e != nil {
			return e
		}
	}
	ids := trafficClassIDs(clients)
	macs := make([]string, 0, len(clients))
	for mac := range clients {
		macs = append(macs, mac)
	}
	slices.Sort(macs)
	for _, mac := range macs {
		l := clients[mac]
		if e = nativeTrafficCommand(ctx, "limit", "profile", profile.ID, "sta", mac, "classid", ids[mac], "up", strconv.Itoa(trafficRate(l.UploadKbps)), "down", strconv.Itoa(trafficRate(l.DownloadKbps)), "up_pri", "0", "down_pri", "0"); e != nil {
			return e
		}
	}
	q := profile.NativeQoS
	if p.QoS != nil {
		q = *p.QoS
	}
	for _, iface := range profile.Interfaces {
		base, _, _ := strings.Cut(iface, ".")
		if !policyInterfaceUp(base) {
			continue
		}
		if _, e = runVendorTool(ctx, "/sbin/iwpriv", base, "set_qos", strconv.Itoa(trafficQoSFlags(q))); e != nil {
			return fmt.Errorf("Apply QoS: %w", e)
		}
	}
	trafficQoSMu.Lock()
	trafficQoSApplied[profile.ID] = trafficQoSSignature(profile, p.QoS)
	trafficQoSMu.Unlock()
	return nil
}

type nativeTrafficQdisc struct {
	Kind    string `json:"kind"`
	Handle  string `json:"handle"`
	Root    bool   `json:"root"`
	Options struct {
		Rate uint64 `json:"rate"`
	} `json:"options"`
	Bytes      uint64 `json:"bytes"`
	Packets    uint64 `json:"packets"`
	Drops      uint64 `json:"drops"`
	Overlimits uint64 `json:"overlimits"`
}
type nativeTrafficClass struct {
	Class  string `json:"class"`
	Handle string `json:"handle"`
	Parent string `json:"parent"`
	Leaf   string `json:"leaf"`
	Rate   uint64 `json:"rate"`
	Ceil   uint64 `json:"ceil"`
	Stats  struct {
		Bytes      uint64 `json:"bytes"`
		Packets    uint64 `json:"packets"`
		Drops      uint64 `json:"drops"`
		Overlimits uint64 `json:"overlimits"`
	} `json:"stats"`
}

func trafficTC(ctx context.Context, kind, iface string, args ...string) ([]byte, error) {
	out, e := runVendorTool(ctx, "/bin/tc", append([]string{"-j", "-s", kind, "show", "dev", iface}, args...)...)
	return []byte(out), e
}

var trafficMatch = regexp.MustCompile(`"match":\s*\{[^}]+\}`)

func trafficMACMatches(raw []byte, mac, direction string) bool {
	hw, e := net.ParseMAC(mac)
	if e != nil {
		return false
	}
	want := map[int][2]uint64{}
	if direction == "download" {
		want[-16] = [2]uint64{uint64(hw[0])<<8 | uint64(hw[1]), 0xffff}
		want[-12] = [2]uint64{uint64(hw[2])<<24 | uint64(hw[3])<<16 | uint64(hw[4])<<8 | uint64(hw[5]), 0xffffffff}
	} else {
		want[-8] = [2]uint64{uint64(hw[0])<<24 | uint64(hw[1])<<16 | uint64(hw[2])<<8 | uint64(hw[3]), 0xffffffff}
		want[-4] = [2]uint64{uint64(hw[4])<<24 | uint64(hw[5])<<16, 0xffff0000}
	}
	got := map[int][2]uint64{}
	// iproute2 emits duplicate "match" JSON keys for u32's multiple matches.
	// Preserve and validate both rather than trusting a last-key-wins map.
	for _, m := range trafficMatch.FindAll(raw, -1) {
		var x struct {
			Value string `json:"value"`
			Mask  string `json:"mask"`
			Off   int    `json:"off"`
		}
		_, body, _ := strings.Cut(string(m), ":")
		if json.Unmarshal([]byte(body), &x) != nil {
			return false
		}
		v, e1 := strconv.ParseUint(x.Value, 16, 32)
		mask, e2 := strconv.ParseUint(x.Mask, 16, 32)
		if e1 != nil || e2 != nil {
			return false
		}
		if _, exists := got[x.Off]; exists {
			return false
		}
		got[x.Off] = [2]uint64{v, mask}
	}
	return trafficEqual(want, got)
}
func verifyTrafficFilters(ctx context.Context, iface, direction string, clients map[string]TrafficLimits, ids map[string]string) error {
	raw, e := trafficTC(ctx, "filter", iface, "parent", "2:")
	if e != nil {
		return e
	}
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		return errors.New("Operating client filters are unreadable")
	}
	expected := map[string]bool{}
	for mac, l := range clients {
		limit := l.UploadKbps
		if direction == "download" {
			limit = l.DownloadKbps
		}
		if limit == nil {
			continue
		}
		for _, proto := range []string{"ip", "ipv6"} {
			expected[mac+"|"+proto] = false
		}
	}
	for _, r := range rows {
		var row struct {
			Protocol string `json:"protocol"`
			Options  struct {
				FlowID string `json:"flowid"`
			} `json:"options"`
		}
		if json.Unmarshal(r, &row) != nil {
			return errors.New("Operating client filter is unreadable")
		}
		if row.Options.FlowID == "" {
			continue
		}
		matched := false
		for mac := range clients {
			if row.Options.FlowID == "2:"+ids[mac] && trafficMACMatches(r, mac, direction) {
				k := mac + "|" + row.Protocol
				if _, ok := expected[k]; ok {
					expected[k] = true
					matched = true
				}
			}
		}
		if !matched {
			return errors.New("Unexpected native client traffic filter")
		}
	}
	for _, ok := range expected {
		if !ok {
			return errors.New("Operating MAC traffic filters do not match the requested limits")
		}
	}
	return nil
}

func verifyTrafficRedirection(ctx context.Context, profile trafficProfile, down, up string) error {
	for _, iface := range profile.Interfaces {
		for _, parent := range []string{"1:", "ffff:"} {
			target := down
			if parent == "ffff:" {
				target = up
			}
			raw, e := trafficTC(ctx, "filter", iface, "parent", parent)
			if e != nil {
				return e
			}
			var rows []struct {
				Protocol string `json:"protocol"`
				Options  struct {
					Match struct {
						Value string `json:"value"`
						Mask  string `json:"mask"`
						Off   int    `json:"off"`
					} `json:"match"`
					Actions []struct {
						Kind      string `json:"kind"`
						Action    string `json:"mirred_action"`
						Direction string `json:"direction"`
						Device    string `json:"to_dev"`
					} `json:"actions"`
				} `json:"options"`
			}
			if json.Unmarshal(raw, &rows) != nil {
				return errors.New("Operating traffic redirection is unreadable")
			}
			found := map[string]bool{"ip": false, "ipv6": false}
			for _, row := range rows {
				for _, a := range row.Options.Actions {
					if a.Kind == "mirred" && a.Action == "redirect" && a.Direction == "egress" && a.Device == target && row.Options.Match.Value == "0" && row.Options.Match.Mask == "0" && row.Options.Match.Off == 0 {
						if _, ok := found[row.Protocol]; ok {
							found[row.Protocol] = true
						}
					}
				}
			}
			if !found["ip"] || !found["ipv6"] {
				return fmt.Errorf("IPv4/IPv6 traffic redirection is missing on %s", iface)
			}
		}
	}
	return nil
}

// TBF exposes a virtual class 1:1 for its child HTB qdisc, not a client cap.
func trafficClassesExpected(classes []nativeTrafficClass, handles map[string]bool, aggregate bool) bool {
	tbf := 0
	for _, c := range classes {
		if c.Class == "tbf" && c.Handle == "1:1" && c.Parent == "1:" && c.Leaf == "0x2" && aggregate {
			tbf++
			continue
		}
		if !handles[c.Handle] || c.Class != "htb" {
			return false
		}
	}
	return (!aggregate && tbf == 0) || (aggregate && tbf == 1)
}
func inspectNativeTraffic(ctx context.Context, name string, p TrafficPolicy, st APState) ([]TrafficQueue, bool, error) {
	queues := []TrafficQueue{}
	profile, pending, e := readTrafficProfile(name)
	if e != nil || pending {
		return queues, pending, e
	}
	clients, e := trafficEffectiveClients(name, p, st)
	if e != nil {
		return queues, false, e
	}
	down, up, e := trafficIFBs(profile.ID)
	if e != nil {
		return queues, false, e
	}
	needed := trafficLimited(p.Bandwidth) || len(clients) > 0
	if !needed && down != "" {
		return queues, false, errors.New("Old traffic queues are still active")
	}
	if needed && down == "" {
		return queues, false, errors.New("Traffic queues are not active")
	}
	ids := trafficClassIDs(clients)
	if needed {
		if e = verifyTrafficRedirection(ctx, profile, down, up); e != nil {
			return queues, false, e
		}
	}
	if needed {
		for _, direction := range []string{"download", "upload"} {
			iface := down
			limit := p.Bandwidth.DownloadKbps
			if direction == "upload" {
				iface = up
				limit = p.Bandwidth.UploadKbps
			}
			raw, e := trafficTC(ctx, "qdisc", iface)
			if e != nil {
				return queues, false, e
			}
			var qdiscs []nativeTrafficQdisc
			if json.Unmarshal(raw, &qdiscs) != nil {
				return queues, false, errors.New("Operating traffic queue is unreadable")
			}
			found := false
			for _, q := range qdiscs {
				if q.Kind == "tbf" && q.Root {
					if limit == nil || q.Options.Rate*8 != uint64(*limit)*1000 {
						return queues, false, errors.New("Operating SSID bandwidth limit differs from the requested limit")
					}
					found = true
					queues = append(queues, TrafficQueue{Interface: iface, Direction: direction, LimitKbps: limit, Bytes: q.Bytes, Packets: q.Packets, Drops: q.Drops, Overlimits: q.Overlimits})
				}
			}
			if limit != nil && !found {
				return queues, false, errors.New("SSID bandwidth limit is not active")
			}
			raw, e = trafficTC(ctx, "class", iface)
			if e != nil {
				return queues, false, e
			}
			var classes []nativeTrafficClass
			if json.Unmarshal(raw, &classes) != nil {
				return queues, false, errors.New("Operating client queues are unreadable")
			}
			expected := map[string]bool{}
			for mac, l := range clients {
				cap := l.DownloadKbps
				if direction == "upload" {
					cap = l.UploadKbps
				}
				if cap == nil {
					continue
				}
				expected[mac] = false
				for _, c := range classes {
					if c.Handle == "2:"+ids[mac] {
						if c.Class != "htb" || c.Rate*8 != uint64(*cap)*1000 || c.Ceil != c.Rate {
							return queues, false, errors.New("Operating client bandwidth limit differs from the requested limit")
						}
						expected[mac] = true
						queues = append(queues, TrafficQueue{Interface: iface, Direction: direction, MAC: mac, LimitKbps: cap, Bytes: c.Stats.Bytes, Packets: c.Stats.Packets, Drops: c.Stats.Drops, Overlimits: c.Stats.Overlimits})
					}
				}
			}
			expectedHandles := map[string]bool{}
			for mac := range expected {
				expectedHandles["2:"+ids[mac]] = true
			}
			if !trafficClassesExpected(classes, expectedHandles, limit != nil) {
				return queues, false, errors.New("Unexpected native client traffic class")
			}
			for _, ok := range expected {
				if !ok {
					return queues, false, errors.New("Client bandwidth limit is not active")
				}
			}
			if e = verifyTrafficFilters(ctx, iface, direction, clients, ids); e != nil {
				return queues, false, e
			}
		}
	}
	if p.QoS != nil {
		trafficQoSMu.Lock()
		signature := trafficQoSApplied[profile.ID]
		trafficQoSMu.Unlock()
		if signature != trafficQoSSignature(profile, p.QoS) {
			return queues, false, errors.New("QoS needs applying to the current wireless interfaces")
		}
	}
	return queues, false, nil
}
