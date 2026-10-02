package main

import (
	"strings"
	"testing"
)

// Output format of "show vlan config" on firmware 18.2.0-32 (addresses changed).
const sampleVLANConfig = `Settings for IPv4:
VLAN for communication with server: Untagged VLAN

VLAN  Type   IP              Mask            Gateway         Status
U     static 10.20.30.40     255.255.255.0   10.20.30.1      Active and Monitored by different AP


IPv4 DNS Settings for Communication VLAN
DNS IP Addresses:
Primary    : 10.20.30.1
Secondary  : 9.9.9.9
Tertiary   :
DNS Prefix : example.lan

Settings for IPv6:
VLAN for communication with server: Untagged VLAN

VLAN  Type   IP                                            Prefix Gateway
U     static

IPv6 DNS Settings for Communication VLAN
DNS IP Addresses:
Primary    :
Secondary  :
Tertiary   :
DNS Prefix :
`

func TestParseVLANConfig(t *testing.T) {
	m := parseVLANConfig(sampleVLANConfig, "untagged")
	if m.Mode != "static" || m.IPv4 != "10.20.30.40" || m.Netmask != "255.255.255.0" || m.Gateway != "10.20.30.1" {
		t.Fatalf("unexpected management: %+v", m)
	}
	if strings.Join(m.DNS, ",") != "10.20.30.1,9.9.9.9" || m.DNSSearch != "example.lan" {
		t.Fatalf("unexpected DNS: %+v", m)
	}
	if !strings.HasPrefix(m.Status, "Active") {
		t.Fatalf("unexpected status %q", m.Status)
	}
}

func TestManagementCommand(t *testing.T) {
	ok := ManagementRequest{Mode: "static", IPv4: "10.20.30.41", Netmask: "255.255.255.0", Gateway: "10.20.30.1", DNS: []string{"10.20.30.1", "9.9.9.9"}, DNSSearch: "example.lan"}
	cmd, err := ok.cliCommand("untagged")
	if err != nil {
		t.Fatal(err)
	}
	want := "force vlan static id U version 4 ip4 10.20.30.41 netmask 255.255.255.0 gw4 10.20.30.1 pdns4 10.20.30.1 sdns4 9.9.9.9 dnsprefix4 example.lan"
	if cmd != want {
		t.Fatalf("got  %q\nwant %q", cmd, want)
	}
	if cmd, _ := (ManagementRequest{Mode: "dhcp"}).cliCommand("99"); cmd != "force vlan dhcp id 99" {
		t.Fatalf("dhcp command %q", cmd)
	}
	bad := []ManagementRequest{
		{Mode: "static", IPv4: "10.20.30.41", Netmask: "255.255.255.0", Gateway: "10.20.31.1", DNS: []string{"1.1.1.1"}}, // gateway outside subnet
		{Mode: "static", IPv4: "10.20.30.0", Netmask: "255.255.255.0", Gateway: "10.20.30.1", DNS: []string{"1.1.1.1"}},  // network address
		{Mode: "static", IPv4: "10.20.30.41", Netmask: "255.0.255.0", Gateway: "10.20.30.1", DNS: []string{"1.1.1.1"}},   // non-contiguous mask
		{Mode: "static", IPv4: "10.20.30.41", Netmask: "255.255.255.0", Gateway: "10.20.30.1"},                           // no DNS
		{Mode: "static", IPv4: "10.20.30.41; reboot", Netmask: "255.255.255.0", Gateway: "10.20.30.1", DNS: []string{"1.1.1.1"}},
		{Mode: "static", IPv4: "10.20.30.41", Netmask: "255.255.255.0", Gateway: "10.20.30.1", DNS: []string{"1.1.1.1"}, DNSSearch: "a b"},
		{Mode: "other"},
	}
	for _, req := range bad {
		if cmd, err := req.cliCommand("untagged"); err == nil {
			t.Errorf("expected rejection for %+v, got %q", req, cmd)
		}
	}
}

func TestParseLLDP(t *testing.T) {
	if got := parseLLDP("lldp.data_valid_bit=0\n"); len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
	got := parseLLDP("lldp.data_valid_bit=1\nlldp.sysname=switch-1\nlldp.portid=Gi1/0/7\n")
	if got["sysname"] != "switch-1" || got["portid"] != "Gi1/0/7" {
		t.Fatalf("unexpected %v", got)
	}
}

func TestManagementVLANValidation(t *testing.T) {
	for _, vlan := range []string{"", "0", "4095", "099", "99; reboot", "99 id U", "-1"} {
		if _, err := (ManagementRequest{Mode: "dhcp"}).cliCommand(vlan); err == nil {
			t.Errorf("unsafe or invalid VLAN %q was accepted", vlan)
		}
	}
	for _, vlan := range []string{"untagged", "1", "99", "4094"} {
		if _, err := (ManagementRequest{Mode: "dhcp"}).cliCommand(vlan); err != nil {
			t.Errorf("valid VLAN %q: %v", vlan, err)
		}
	}
}

func TestParseDHCPManagement(t *testing.T) {
	for _, text := range []string{
		"Settings for IPv4:\n99 dhcp\nSettings for IPv6:\n99 static",
		"Settings for IPv4:\n99 dhcp No IP Address\nSettings for IPv6:\n99 static",
	} {
		m := parseVLANConfig(text, "99")
		if m.Mode != "dhcp" || m.IPv4 != "" || m.Netmask != "" || m.Gateway != "" {
			t.Fatalf("DHCP without a lease: %+v", m)
		}
	}
}
