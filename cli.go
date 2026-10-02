package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Settings that this firmware does not expose through OpenConfig (management
// IP, VLAN, LEDs, reboot) are handled by the vendor CLI (/sbin/cli).

// runCLI runs one vendor CLI command. The command is passed through the
// environment, never interpolated into the shell line.
func runCLI(parent context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `. /etc/profile >/dev/null 2>&1; exec /sbin/cli -c "$C460_CLI_CMD"`)
	cmd.Env = append(cleanEnv(), "C460_CLI_CMD="+command)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("%s: %w: %s", command, err, text)
	}
	if strings.HasPrefix(text, "Error") || strings.Contains(text, "Invalid") {
		return text, fmt.Errorf("%s: %s", command, text)
	}
	return text, nil
}

// cleanEnv drops LD_PRELOAD: procd's environment preloads a library that is
// missing on this firmware, and the loader warning would pollute the output.
func cleanEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LD_PRELOAD=") {
			env = append(env, e)
		}
	}
	return env
}

// ------------------------------------------------------------- management

type Management struct {
	CommVLAN    string   `json:"commVlan"` // "untagged" or a VLAN id
	Mode        string   `json:"mode"`     // "static" or "dhcp"
	IPv4        string   `json:"ipv4"`
	Netmask     string   `json:"netmask"`
	Gateway     string   `json:"gateway"`
	DNS         []string `json:"dns"`
	DNSSearch   string   `json:"dnsSearch"`
	Status      string   `json:"status"`
	PendingBoot bool     `json:"pendingReboot"` // saved settings differ from what is running
}

var (
	vlanRow    = regexp.MustCompile(`^(U|\d{1,4})\s+(static|dhcp)\s*(\S*)\s*(\S*)\s*(\S*)\s*(.*)$`)
	dnsLine    = regexp.MustCompile(`^(Primary|Secondary|Tertiary)\s*:\s*(\S*)`)
	prefixLine = regexp.MustCompile(`^DNS Prefix\s*:\s*(\S*)`)
)

// parseVLANConfig reads the IPv4 part of "show vlan config".
func parseVLANConfig(text, comm string) Management {
	m := Management{CommVLAN: comm, DNS: []string{}}
	ipv4 := text
	if i := strings.Index(text, "Settings for IPv6"); i >= 0 {
		ipv4 = text[:i]
	}
	want := "U"
	if comm != "untagged" {
		want = comm
	}
	sc := bufio.NewScanner(strings.NewReader(ipv4))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if r := vlanRow.FindStringSubmatch(line); r != nil && strings.EqualFold(r[1], want) {
			m.Mode, m.IPv4, m.Netmask, m.Gateway, m.Status = r[2], r[3], r[4], r[5], strings.TrimSpace(r[6])
			if m.Mode == "dhcp" {
				// For DHCP the columns hold the leased values, if any.
				if net.ParseIP(m.IPv4) == nil {
					m.IPv4, m.Netmask, m.Gateway = "", "", ""
				}
			}
		} else if d := dnsLine.FindStringSubmatch(line); d != nil && d[2] != "" {
			m.DNS = append(m.DNS, d[2])
		} else if p := prefixLine.FindStringSubmatch(line); p != nil {
			m.DNSSearch = p[1]
		}
	}
	return m
}

// ManagementRequest is the desired management configuration of the communication VLAN.
type ManagementRequest struct {
	CommVLAN  string   `json:"commVlan,omitempty"` // empty preserves the current communication VLAN
	Mode      string   `json:"mode"`
	IPv4      string   `json:"ipv4"`
	Netmask   string   `json:"netmask"`
	Gateway   string   `json:"gateway"`
	DNS       []string `json:"dns"`
	DNSSearch string   `json:"dnsSearch"`
}

var hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)

func ipv4Of(s string) net.IP {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return nil
	}
	return ip.To4()
}

// cliCommand validates a request and returns the vendor CLI command for it.
func (req ManagementRequest) cliCommand(commVLAN string) (string, error) {
	id, err := communicationVLANID(commVLAN)
	if err != nil {
		return "", err
	}
	if req.Mode == "dhcp" {
		return "force vlan dhcp id " + id, nil
	}
	if req.Mode != "static" {
		return "", errors.New("mode must be static or dhcp")
	}
	ip, mask, gw := ipv4Of(req.IPv4), ipv4Of(req.Netmask), ipv4Of(req.Gateway)
	if ip == nil {
		return "", errors.New("invalid IPv4 address")
	}
	if mask == nil {
		return "", errors.New("invalid subnet mask")
	}
	ones, bits := net.IPMask(mask).Size()
	if bits == 0 || ones < 8 || ones > 30 {
		return "", errors.New("subnet mask must be contiguous, between /8 and /30")
	}
	subnet := &net.IPNet{IP: ip.Mask(net.IPMask(mask)), Mask: net.IPMask(mask)}
	broadcast := make(net.IP, 4)
	for i := range broadcast {
		broadcast[i] = subnet.IP[i] | ^mask[i]
	}
	if ip.Equal(subnet.IP) || ip.Equal(broadcast) || ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() {
		return "", errors.New("the address cannot be the network, broadcast, loopback or multicast address")
	}
	if gw == nil || !subnet.Contains(gw) || gw.Equal(ip) || gw.Equal(subnet.IP) || gw.Equal(broadcast) {
		return "", fmt.Errorf("the gateway must be another host address inside %s", subnet)
	}
	if len(req.DNS) == 0 {
		return "", errors.New("at least one DNS server is required")
	}
	if len(req.DNS) > 3 {
		return "", errors.New("at most three DNS servers")
	}
	cmd := fmt.Sprintf("force vlan static id %s version 4 ip4 %s netmask %s gw4 %s", id, ip, mask, gw)
	for i, d := range req.DNS {
		dns := ipv4Of(d)
		if dns == nil {
			return "", fmt.Errorf("invalid DNS server %q", d)
		}
		cmd += fmt.Sprintf(" %s %s", []string{"pdns4", "sdns4", "tdns4"}[i], dns)
	}
	if s := strings.TrimSpace(req.DNSSearch); s != "" {
		if !hostnameRe.MatchString(s) || len(s) > 253 {
			return "", errors.New("invalid DNS search domain")
		}
		cmd += " dnsprefix4 " + s
	}
	return cmd, nil
}

func communicationVLANID(vlan string) (string, error) {
	if vlan == "untagged" {
		return "U", nil
	}
	id, err := strconv.Atoi(vlan)
	if err != nil || id < 1 || id > 4094 || strconv.Itoa(id) != vlan {
		return "", errors.New("management VLAN must be untagged or an integer from 1 to 4094")
	}
	return vlan, nil
}

// --------------------------------------------------------------- hardware

type HardwareInfo struct {
	Serial      string            `json:"serial"`
	PowerSource string            `json:"powerSource"`
	RadioPower  string            `json:"radioPower"`
	NTPSynced   *bool             `json:"ntpSynced"`
	LLDP        map[string]string `json:"lldp"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

func field(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseLLDP turns "lldp.key=value" lines into a map; it is empty without a valid neighbour.
func parseLLDP(text string) map[string]string {
	out := map[string]string{}
	valid := false
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		k = strings.TrimPrefix(k, "lldp.")
		if k == "data_valid_bit" {
			valid = v == "1"
			continue
		}
		if v != "" {
			out[k] = v
		}
	}
	if !valid {
		return map[string]string{}
	}
	return out
}

// CLIInfo caches management and hardware data; the CLI is slow and logs every call.
type CLIInfo struct {
	mu         sync.RWMutex
	management Management
	hardware   HardwareInfo
	running    string // management IP the AP booted with
	err        string
}

func (c *CLIInfo) Refresh(ctx context.Context) {
	comm := "untagged"
	commOut, commErr := runCLI(ctx, "show vlan communication")
	if commErr == nil {
		out := commOut
		if v := strings.TrimSpace(strings.TrimPrefix(out, "Communication VLAN:")); v != "" && !strings.EqualFold(v, "untagged") {
			comm = v
		}
	}
	vlanOut, vlanErr := runCLI(ctx, "show vlan config")
	mgmt := parseVLANConfig(vlanOut, comm)
	if commErr != nil {
		vlanErr = commErr
	} else if _, err := communicationVLANID(comm); err != nil {
		vlanErr = err
	} else if mgmt.Mode != "static" && mgmt.Mode != "dhcp" {
		vlanErr = errors.New("cannot read the management VLAN's address configuration")
	}
	hw := HardwareInfo{LLDP: map[string]string{}, UpdatedAt: time.Now()}
	if out, err := runCLI(ctx, "show device info"); err == nil {
		hw.Serial = field(out, "Serial Number")
		hw.RadioPower = field(out, "Radio Power")
	}
	if out, err := runCLI(ctx, "show power source"); err == nil {
		hw.PowerSource = field(out, "Power source")
	}
	if out, err := runCLI(ctx, "time"); err == nil {
		if v := field(out, "NTP synchronized"); v != "" {
			synced := strings.EqualFold(v, "yes")
			hw.NTPSynced = &synced
		}
	}
	if out, err := runCLI(ctx, "show lldp neighbor info"); err == nil {
		hw.LLDP = parseLLDP(out)
	}
	running, prefix := managementIPv4()
	runningMask := net.IP(net.CIDRMask(prefix, 32)).String()
	runningGateway := defaultGateway()

	c.mu.Lock()
	defer c.mu.Unlock()
	if vlanErr != nil {
		c.err = vlanErr.Error()
	} else {
		c.err = ""
		mgmt.PendingBoot = mgmt.Mode == "static" && mgmt.IPv4 != "" && running != "" &&
			(mgmt.IPv4 != running || mgmt.Netmask != runningMask || mgmt.Gateway != runningGateway)
		c.management = mgmt
	}
	c.hardware = hw
	c.running = running
}

func (c *CLIInfo) Snapshot() (Management, HardwareInfo, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.management, c.hardware, c.err
}

// Run refreshes at start and every interval; trigger forces a refresh.
func (c *CLIInfo) Run(ctx context.Context, interval time.Duration, trigger <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		c.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-trigger:
		}
	}
}
