package main

// Native SSID admission policies. The legacy vendor path applies MAC ACLs
// and maxsta; the newer radio-manager handlers are empty on this firmware.
import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

const maxPolicyMACs = 128

type MACFilter struct {
	Mode      string   `json:"mode"` // off, allow, deny
	Addresses []string `json:"addresses"`
}

type SSIDPolicy struct {
	MACFilter  MACFilter `json:"macFilter"`
	MaxClients *int      `json:"maxClients"` // per BSS/band; null = firmware default (127)
}

func defaultSSIDPolicy() SSIDPolicy {
	return SSIDPolicy{MACFilter: MACFilter{Mode: "off", Addresses: []string{}}}
}

func normalizeSSIDPolicy(p SSIDPolicy) (SSIDPolicy, error) {
	if !slices.Contains([]string{"off", "allow", "deny"}, p.MACFilter.Mode) {
		return p, errors.New("MAC filtering mode must be off, allow or deny")
	}
	if p.MaxClients != nil && (*p.MaxClients < 1 || *p.MaxClients > 127) {
		return p, errors.New("Client limit per band must be 1–127 or null for the firmware default")
	}
	if len(p.MACFilter.Addresses) > maxPolicyMACs {
		return p, fmt.Errorf("Use at most %d MAC addresses", maxPolicyMACs)
	}
	addresses := []string{}
	for _, text := range p.MACFilter.Addresses {
		mac, err := net.ParseMAC(strings.TrimSpace(text))
		if err != nil || len(mac) != 6 || mac[0]&1 != 0 || bytes.Equal(mac, make([]byte, 6)) {
			return p, fmt.Errorf("Invalid unicast Wi-Fi MAC address %q", text)
		}
		address := strings.ToLower(mac.String())
		if slices.Contains(addresses, address) {
			return p, fmt.Errorf("Repeated MAC address %s", address)
		}
		addresses = append(addresses, address)
	}
	if p.MACFilter.Mode == "allow" && len(addresses) == 0 {
		return p, errors.New("An allow list needs at least one MAC address")
	}
	slices.Sort(addresses)
	p.MACFilter.Addresses = addresses
	if p.MaxClients != nil {
		n := *p.MaxClients
		p.MaxClients = &n
	}
	return p, nil
}

var policyKeys = []string{"MAC_ACL_ENABLED", "MAC_ACL_OPERATION", "MAC_ACL_LIST", "ENABLE_LIMIT_ON_ASSOC", "ASSOC_LIMIT"}

func policyFields(p SSIDPolicy) map[string]string {
	action, enabled := "0", "0"
	if p.MACFilter.Mode == "allow" {
		action, enabled = "1", "1"
	} else if p.MACFilter.Mode == "deny" {
		action, enabled = "2", "1"
	}
	encoded := make([]string, 0, len(p.MACFilter.Addresses))
	for _, address := range p.MACFilter.Addresses {
		mac, _ := net.ParseMAC(address) // normalized before rendering
		encoded = append(encoded, base64.StdEncoding.EncodeToString(mac))
	}
	limit, limited := "127", "0"
	if p.MaxClients != nil {
		limit, limited = strconv.Itoa(*p.MaxClients), "1"
	}
	return map[string]string{"MAC_ACL_ENABLED": enabled, "MAC_ACL_OPERATION": action,
		"MAC_ACL_LIST": strings.Join(encoded, ","), "ENABLE_LIMIT_ON_ASSOC": limited, "ASSOC_LIMIT": limit}
}

func policyFromFields(fields map[string]string) (SSIDPolicy, error) {
	p := defaultSSIDPolicy()
	if fields["MAC_ACL_ENABLED"] == "1" {
		switch fields["MAC_ACL_OPERATION"] {
		case "1":
			p.MACFilter.Mode = "allow"
		case "2":
			p.MACFilter.Mode = "deny"
		default:
			return p, errors.New("Native MAC filtering action is unknown")
		}
	}
	if fields["MAC_ACL_LIST"] != "" {
		for _, encoded := range strings.Split(fields["MAC_ACL_LIST"], ",") {
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || len(raw) != 6 {
				return p, errors.New("Native MAC address list is unreadable")
			}
			p.MACFilter.Addresses = append(p.MACFilter.Addresses, net.HardwareAddr(raw).String())
		}
	}
	if fields["ENABLE_LIMIT_ON_ASSOC"] == "1" {
		n, err := strconv.Atoi(fields["ASSOC_LIMIT"])
		if err != nil {
			return p, errors.New("Native client limit is unreadable")
		}
		p.MaxClients = &n
	}
	return normalizeSSIDPolicy(p)
}

type policySection struct {
	name, profile, id string
	start, end        int
	fields            map[string]string
}

func policySections(raw []byte) ([]policySection, error) {
	var sections []policySection
	var current *policySection
	seen := map[string]bool{}
	profiles := map[string]bool{}
	for i, line := range strings.Split(string(raw), "\n") {
		if m := sectionHeader.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if m[1] == "START" {
				if current != nil {
					return nil, errors.New("Nested native SSID section")
				}
				current = &policySection{id: m[2], start: i, fields: map[string]string{}}
			} else if current != nil {
				current.end = i
				if current.id != m[2] || current.name == "" || !policyProfileID.MatchString(current.profile) || seen[current.name] || profiles[current.profile] {
					return nil, errors.New("Ambiguous native SSID/profile mapping")
				}
				seen[current.name] = true
				profiles[current.profile] = true
				sections = append(sections, *current)
				current = nil
			}
			continue
		}
		if current == nil {
			continue
		}
		if v, ok := vapValue(line, "AP_SSID"); ok {
			if current.name != "" {
				return nil, errors.New("Repeated native SSID name")
			}
			current.name = v
		}
		if v, ok := vapValue(line, "SSID_PROFILE_ID"); ok {
			if current.profile != "" {
				return nil, errors.New("Repeated native profile ID")
			}
			current.profile = v
		}
		for _, key := range policyKeys {
			if v, ok := vapValue(line, key); ok {
				if _, exists := current.fields[key]; exists {
					return nil, fmt.Errorf("Repeated native policy field %s", key)
				}
				current.fields[key] = v
			}
		}
	}
	if current != nil {
		return nil, errors.New("Incomplete native SSID section")
	}
	return sections, nil
}

var policyProfileID = regexp.MustCompile(`^[0-9]+$`)

// Only the five owned admission fields change. All unknown fields and secrets
// retain their original bytes. Missing rollback fields are removed, not defaulted.
func rewritePolicyFields(raw []byte, fields map[string]map[string]string) ([]byte, []string, error) {
	sections, err := policySections(raw)
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(string(raw), "\n")
	var profiles []string
	for i := len(sections) - 1; i >= 0; i-- {
		section := sections[i]
		wanted, managed := fields[section.name]
		if !managed {
			continue
		}
		equal := len(wanted) == len(section.fields)
		for key, value := range wanted {
			if v, ok := section.fields[key]; !ok || v != value {
				equal = false
			}
		}
		if equal {
			continue
		}
		body := []string{}
		for _, line := range lines[section.start+1 : section.end] {
			key, _, _ := strings.Cut(line, "=")
			if !slices.Contains(policyKeys, key) {
				body = append(body, line)
			}
		}
		for _, key := range policyKeys {
			if value, ok := wanted[key]; ok {
				body = append(body, key+"="+value)
			}
		}
		next := append(slices.Clone(lines[:section.start+1]), body...)
		lines = append(next, lines[section.end:]...)
		profiles = append(profiles, section.profile)
	}
	slices.Sort(profiles)
	return []byte(strings.Join(lines, "\n")), profiles, nil
}

var policyDiffHeader = regexp.MustCompile(`^\[ VAP_(?:START=(\d+)(?: (?:MOD|F_MOD))?|END=(\d+)) \]$`)

func checkPolicyDiff(out string, code int, profiles []string, before, next []byte) error {
	if code != 0 {
		return fmt.Errorf("The firmware would need a restart for this change (code %d)", code)
	}
	sections, err := policySections(next)
	if err != nil {
		return err
	}
	wanted, rawFields := map[string]map[string]string{}, map[string]map[string]string{}
	lines := strings.Split(string(next), "\n")
	for _, section := range sections {
		if !slices.Contains(profiles, section.profile) {
			continue
		}
		wanted[section.name] = section.fields
		rawFields[section.profile] = map[string]string{}
		for _, line := range lines[section.start+1 : section.end] {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				rawFields[section.profile][key] = value
			}
		}
	}
	// The firmware emits full MOD sections. Prove the candidate changes only
	// our fields, then compare every emitted value with the exact candidate.
	expected, changed, err := rewritePolicyFields(before, wanted)
	if err != nil || !bytes.Equal(expected, next) || !slices.Equal(changed, profiles) {
		return errors.New("Client access candidate includes unrelated changes")
	}
	profile := ""
	seen := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			m := policyDiffHeader.FindStringSubmatch(line)
			if m == nil || !slices.Contains(profiles, m[1]+m[2]) {
				return errors.New("The firmware reported changes beyond the selected networks")
			}
			profile = m[1]
			seen = true
			continue
		}
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		actual, exists := rawFields[profile][key]
		if !ok || !exists || actual != value {
			return errors.New("Firmware diff differs from the client access candidate")
		}
	}
	if !seen {
		return errors.New("The firmware reported no client access change")
	}
	return nil
}

type SSIDPolicies struct {
	mu           sync.RWMutex
	path, apConf string
	desired      map[string]SSIDPolicy
	loadErr      error
	apply        func(context.Context, []byte, []byte, []string) error
	verify       func(context.Context, string, SSIDPolicy) error
	save         func(string, []byte, os.FileMode) error
	available    func() bool
	inactive     func(string) bool
}

func NewSSIDPolicies(path string) *SSIDPolicies {
	p := &SSIDPolicies{path: path, apConf: nativeAPConf, desired: map[string]SSIDPolicy{}, apply: stageNativePolicy, verify: verifyPolicyOperating, save: atomicNative, available: nativePolicyAvailable, inactive: nativePolicyInactive}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p
	}
	if err == nil {
		err = json.Unmarshal(raw, &p.desired)
		if err == nil && p.desired == nil {
			err = errors.New("Client access settings must be a JSON object")
		}
	}
	if err == nil {
		for name, policy := range p.desired {
			p.desired[name], err = normalizeSSIDPolicy(policy)
			if err != nil {
				break
			}
		}
	}
	p.loadErr = err
	return p
}

func nativePolicyAvailable() bool {
	tag, err := os.ReadFile("/opt/reltag")
	if err != nil || !strings.Contains(string(tag), "18.2.0-32") {
		return false
	}
	banner, err := os.ReadFile("/opt/banner")
	if err != nil || !bytes.Contains(banner, []byte("C-460")) {
		return false
	}
	for _, path := range []string{"/opt/ap/handle_mac_filter.sh", "/etc/ath/configVAP", "/sbin/iwpriv", "/opt/sensor/scripts/encrypt_secret.sh", nativeAPConf + ".enc"} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func (p *SSIDPolicies) Saved() map[string]SSIDPolicy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	copy := map[string]SSIDPolicy{}
	for name, value := range p.desired {
		copy[name], _ = normalizeSSIDPolicy(value)
	}
	return copy
}

func (p *SSIDPolicies) persist(desired map[string]SSIDPolicy) error {
	raw, err := json.Marshal(desired)
	if err == nil {
		err = p.save(p.path, raw, 0600)
	}
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.desired = desired
	p.mu.Unlock()
	return nil
}

func (p *SSIDPolicies) Rename(changes []ssidChange) error {
	if p.loadErr != nil {
		return errors.New("Saved client access settings are unreadable")
	}
	desired, changed := p.Saved(), false
	for _, change := range changes {
		if change.oldName == "" || change.oldName == change.newName {
			continue
		}
		if policy, exists := desired[change.oldName]; exists {
			delete(desired, change.oldName)
			if change.newName != "" {
				desired[change.newName] = policy
			}
			changed = true
		}
	}
	if changed {
		return p.persist(desired)
	}
	return nil
}

func (p *SSIDPolicies) reconcile(ctx context.Context, desired map[string]SSIDPolicy) error {
	if p.loadErr != nil {
		return errors.New("Saved client access settings are unreadable")
	}
	if len(desired) == 0 {
		return nil
	}
	if !p.available() {
		return errors.New("Native client access controls require tested C-460 firmware 18.2.0-32")
	}
	raw, err := os.ReadFile(p.apConf)
	if err != nil {
		return err
	}
	fields := map[string]map[string]string{}
	for name, policy := range desired {
		if p.inactive != nil && p.inactive(name) {
			continue
		}
		fields[name] = policyFields(policy)
	}
	next, profiles, err := rewritePolicyFields(raw, fields)
	if err != nil {
		return err
	}
	sections, err := policySections(next)
	if err != nil {
		return err
	}
	// Runtime state can reset without changing ap.conf. Repair those profiles
	// through the same live setters, including after an incomplete rollback.
	for _, section := range sections {
		policy, managed := desired[section.name]
		if !managed || slices.Contains(profiles, section.profile) || p.inactive != nil && p.inactive(section.name) {
			continue
		}
		if err := p.verify(ctx, section.name, policy); err != nil {
			profiles = append(profiles, section.profile)
		}
	}
	if len(profiles) == 0 {
		return nil
	}
	slices.Sort(profiles)
	if err = p.apply(ctx, raw, next, profiles); err != nil {
		return err
	}
	for _, section := range sections {
		if slices.Contains(profiles, section.profile) {
			if err := p.verify(ctx, section.name, desired[section.name]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *SSIDPolicies) Reconcile(ctx context.Context) error { return p.reconcile(ctx, p.Saved()) }

// Commit only after independent operating readback. Failures restore the old
// owned fields through the same guarded apply route, preserving other writers.
func (p *SSIDPolicies) Update(ctx context.Context, name string, policy SSIDPolicy) error {
	return p.UpdateMany(ctx, map[string]SSIDPolicy{name: policy})
}

func (p *SSIDPolicies) UpdateMany(ctx context.Context, updates map[string]SSIDPolicy) error {
	if len(updates) == 0 {
		return nil
	}
	desired := p.Saved()
	for name, policy := range updates {
		value, err := normalizeSSIDPolicy(policy)
		if err != nil {
			return err
		}
		desired[name] = value
	}
	if p.loadErr != nil {
		return errors.New("Saved client access settings are unreadable")
	}
	if !p.available() {
		return errors.New("Native client access controls are unavailable on this firmware")
	}
	before, err := os.ReadFile(p.apConf)
	if err != nil {
		return err
	}
	sections, err := policySections(before)
	if err != nil {
		return err
	}
	oldFields := map[string]map[string]string{}
	for _, section := range sections {
		if _, changed := updates[section.name]; changed {
			oldFields[section.name] = section.fields
		}
	}
	if len(oldFields) != len(updates) {
		return errors.New("The wireless profile is not ready; refresh and retry")
	}
	err = p.reconcile(ctx, desired)
	if err == nil {
		for name := range updates {
			if p.inactive != nil && p.inactive(name) {
				continue
			}
			if err = p.verify(ctx, name, desired[name]); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = p.persist(desired)
	}
	if err == nil {
		return nil
	}
	rollback, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	current, restoreErr := os.ReadFile(p.apConf)
	if restoreErr == nil {
		var restored []byte
		var profiles []string
		restored, profiles, restoreErr = rewritePolicyFields(current, oldFields)
		if restoreErr == nil && len(profiles) > 0 {
			restoreErr = p.apply(rollback, current, restored, profiles)
		}
		for name, fields := range oldFields {
			if p.inactive != nil && p.inactive(name) {
				continue
			}
			if previous, decodeErr := policyFromFields(fields); restoreErr == nil && decodeErr == nil {
				restoreErr = p.verify(rollback, name, previous)
			}
		}
	}
	if restoreErr != nil {
		return fmt.Errorf("%w; restoring the previous client access settings failed: %v", err, restoreErr)
	}
	return fmt.Errorf("%w; previous native client access settings restored", err)
}

func stageNativePolicy(ctx context.Context, before, next []byte, profiles []string) error {
	if err := waitNativeQuiet(ctx); err != nil {
		return err
	}
	current, err := os.ReadFile(nativeAPConf)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return errors.New("AP configuration changed while planning; retry")
	}
	f, err := os.CreateTemp("/tmp", "c460-policy-candidate-")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	_, err = f.Write(next)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(before, next) {
		out, code, err := firmwareConfigDiff(ctx, nativeAPConf, path)
		if err != nil {
			return err
		}
		changed := []string{}
		sections, err := policySections(next)
		if err != nil {
			return err
		}
		for _, section := range sections {
			if !slices.Contains(profiles, section.profile) {
				continue
			}
			_, ids, err := rewritePolicyFields(before, map[string]map[string]string{section.name: section.fields})
			if err != nil {
				return err
			}
			changed = append(changed, ids...)
		}
		slices.Sort(changed)
		if err = checkPolicyDiff(out, code, changed, before, next); err != nil {
			return err
		}
	}
	current, err = os.ReadFile(nativeAPConf)
	if err != nil || !bytes.Equal(current, before) {
		return errors.New("AP configuration changed before application; retry")
	}
	return applyLivePolicy(ctx, before, next, profiles, path)
}

type PolicyInterface struct {
	Interface  string   `json:"interface"`
	Band       string   `json:"band"`
	MACMode    *string  `json:"macMode"`
	MaxClients *int     `json:"maxClients"`
	Addresses  []string `json:"addresses"` // null = unavailable; empty = kernel list empty
	Error      string   `json:"error,omitempty"`
}
type SSIDPolicyStatus struct {
	Settings   SSIDPolicy        `json:"settings"`
	Managed    bool              `json:"managed"`
	Supported  bool              `json:"supported"`
	Interfaces []PolicyInterface `json:"interfaces"`
	Error      string            `json:"error,omitempty"`
}

var policyDriverValue = regexp.MustCompile(`:\s*([0-9]+)\s*$`)

func parseDriverPolicyMACs(out string) ([]string, error) {
	addresses := []string{}
	if strings.TrimSpace(out) == "" {
		return addresses, nil
	}
	_, list, ok := strings.Cut(out, "getmac:")
	if !ok {
		return nil, errors.New("Operating MAC address list unavailable")
	}
	for _, value := range strings.Fields(list) {
		mac, err := net.ParseMAC(value)
		if err != nil || len(mac) != 6 {
			return nil, errors.New("Operating MAC address list is unreadable")
		}
		addresses = append(addresses, strings.ToLower(mac.String()))
	}
	slices.Sort(addresses)
	return addresses, nil
}

func driverPolicyInt(ctx context.Context, iface, getter string) (*int, error) {
	out, err := runVendorTool(ctx, "/sbin/iwpriv", iface, getter)
	if err != nil {
		return nil, err
	}
	match := policyDriverValue.FindStringSubmatch(out)
	if match == nil {
		return nil, errors.New("Operating client access value unavailable")
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func readPolicyInterfaces(ctx context.Context, name string) []PolicyInterface {
	result := []PolicyInterface{}
	expected := map[string]bool{}
	if raw, err := os.ReadFile(nativeAPConf); err == nil {
		if sections, err := policySections(raw); err == nil {
			for _, section := range sections {
				if section.name != name {
					continue
				}
				if profile, err := os.ReadFile("/tmp/profile" + section.profile + "/profile.conf"); err == nil {
					for _, line := range strings.Split(string(profile), "\n") {
						if list, ok := vapValue(line, "VAP_LIST"); ok {
							for _, iface := range strings.Split(list, ",") {
								iface, _, _ = strings.Cut(strings.TrimSpace(iface), ".")
								if validPolicyInterface.MatchString(iface) && policyInterfaceUp(iface) {
									expected[iface] = true
								}
							}
						}
					}
				}
			}
		}
	}
	for _, iface := range hostapdInterfaces("/var/run/hostapd") {
		if !policyInterfaceUp(iface) {
			continue
		}
		out, err := hostapdCommand(ctx, "/var/run/hostapd", iface, "STATUS")
		if err != nil && !expected[iface] {
			continue
		}
		props := hostapdProperties(out)
		if props["ssid[0]"] != name && !expected[iface] {
			continue
		}
		item := PolicyInterface{Interface: iface, Band: frequencyBand(props["freq"])}
		if err != nil || props["ssid[0]"] != name {
			item.Error = "Operating network identity readback unavailable"
		}
		mode, modeErr := driverPolicyInt(ctx, iface, "get_maccmd")
		if modeErr == nil && *mode >= 0 && *mode <= 2 {
			value := []string{"off", "allow", "deny"}[*mode]
			item.MACMode = &value
		} else {
			item.Error = "MAC filtering readback unavailable"
		}
		item.MaxClients, err = driverPolicyInt(ctx, iface, "get_maxsta")
		if err != nil {
			item.Error = "Client access readback incomplete"
		}
		if out, err := runVendorTool(ctx, "/sbin/iwpriv", iface, "getmac"); err == nil {
			item.Addresses, err = parseDriverPolicyMACs(out)
			if err != nil {
				item.Error = err.Error()
			}
		} else {
			item.Error = "MAC address list readback unavailable"
		}
		result = append(result, item)
		delete(expected, iface)
	}
	for iface := range expected {
		result = append(result, PolicyInterface{Interface: iface, Error: "Operating network readback unavailable"})
	}
	slices.SortFunc(result, func(a, b PolicyInterface) int { return strings.Compare(a.Interface, b.Interface) })
	return result
}

var validPolicyInterface = regexp.MustCompile(`^ath[0-9]+$`)

func policyInterfaceUp(iface string) bool {
	raw, err := os.ReadFile("/sys/class/net/" + iface + "/flags")
	if err != nil {
		return true
	} // Unknown must produce a readback error, not appear inactive.
	flags, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 0, 32)
	return err != nil || flags&1 != 0 // IFF_UP; disabled SSIDs leave stale profile/VAP entries.
}

// Applying a native MOD to a stopped VAP can bring it up. Keep the desired
// policy pending while every mapped interface is down; reconcile when enabled.
func nativePolicyInactive(name string) bool {
	raw, err := os.ReadFile(nativeAPConf)
	if err != nil {
		return false
	}
	sections, err := policySections(raw)
	if err != nil {
		return false
	}
	for _, section := range sections {
		if section.name != name {
			continue
		}
		profile, err := os.ReadFile("/tmp/profile" + section.profile + "/profile.conf")
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(profile), "\n") {
			if list, ok := vapValue(line, "VAP_LIST"); ok && list != "" {
				for _, iface := range strings.Split(list, ",") {
					iface, _, _ = strings.Cut(strings.TrimSpace(iface), ".")
					if !validPolicyInterface.MatchString(iface) || policyInterfaceUp(iface) {
						return false
					}
				}
				return true
			}
		}
	}
	return false
}

func verifyPolicyOperating(ctx context.Context, name string, p SSIDPolicy) error {
	interfaces := readPolicyInterfaces(ctx, name)
	// Disabled/scheduled-off profiles have no BSS; native persistence is checked
	// separately and the UI must show that operating verification is pending.
	if len(interfaces) == 0 {
		return nil
	}
	limit := 127
	if p.MaxClients != nil {
		limit = *p.MaxClients
	}
	for _, iface := range interfaces {
		if iface.Error != "" || iface.MACMode == nil || iface.MaxClients == nil || *iface.MACMode != p.MACFilter.Mode || *iface.MaxClients != limit {
			return fmt.Errorf("Operating client access settings did not match on %s", iface.Interface)
		}
		wanted := p.MACFilter.Addresses
		if p.MACFilter.Mode == "off" {
			wanted = []string{}
		}
		if iface.Addresses == nil || !slices.Equal(iface.Addresses, wanted) {
			return fmt.Errorf("Operating MAC address list did not match on %s", iface.Interface)
		}
	}
	return nil
}

func (a *API) policyStatus(ctx context.Context, name string) SSIDPolicyStatus {
	status := SSIDPolicyStatus{Settings: defaultSSIDPolicy(), Interfaces: []PolicyInterface{}}
	if a.policies == nil {
		status.Error = "Client access controls are unavailable"
		return status
	}
	status.Supported = a.policies.available()
	if a.policies.loadErr != nil {
		status.Error = "Saved client access settings are unreadable"
		return status
	}
	if policy, ok := a.policies.Saved()[name]; ok {
		status.Settings = policy
		status.Managed = true
	} else if raw, err := os.ReadFile(a.policies.apConf); err == nil {
		if sections, err := policySections(raw); err == nil {
			for _, section := range sections {
				if section.name == name {
					status.Settings, err = policyFromFields(section.fields)
					if err != nil {
						status.Error = err.Error()
					}
				}
			}
		} else {
			status.Error = err.Error()
		}
	}
	status.Interfaces = readPolicyInterfaces(ctx, name)
	return status
}

func (a *API) getSSIDPolicy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := a.poller.SSIDConfig(name); !ok {
		fail(w, 404, "Network not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	reply(w, 200, a.policyStatus(ctx, name))
}

func (a *API) updateSSIDPolicy(w http.ResponseWriter, r *http.Request) {
	var input SSIDPolicy
	if !decode(w, r, &input) {
		return
	}
	if input.MACFilter.Addresses == nil {
		fail(w, 400, "Send a MAC addresses array; use an empty array when no addresses are listed")
		return
	}
	policy, err := normalizeSSIDPolicy(input)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	name := r.PathValue("name")
	if _, ok := a.poller.SSIDConfig(name); !ok {
		fail(w, 404, "Network not found")
		return
	}
	if a.policies == nil {
		fail(w, 503, "Client access controls are unavailable")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(4 * time.Minute))
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer cancel()
	if err := a.policies.Update(ctx, name, policy); err != nil {
		fail(w, 502, err.Error())
		return
	}
	reply(w, 200, a.policyStatus(ctx, name))
}

func (a *API) ensureSSIDPolicies() error {
	if a.policies == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return a.policies.Reconcile(ctx)
}

// filepath is also used by the constructor's caller; keep state next to the
// configuration so installations with a custom config directory remain portable.
func policySettingsPath(config string) string {
	return filepath.Join(filepath.Dir(config), "ssid-policies.json")
}
