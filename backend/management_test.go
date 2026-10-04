package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nativeNetworkFixture = "[vlan]\nDEVICE=br0\nBOOTPROTO=static\nIPADDR=10.20.30.40\nNETMASK=255.255.255.0\nGATEWAY=10.20.30.1\nPrimaryDNS=10.20.30.1\nSecondaryDNS=9.9.9.9\nTertiaryDNS=1.1.1.1\nDNSPrefix=old.example\nIPV6ADDR=2001:db8::40\nVENDOR_OPTION=retain\n"
const discoveryFixture = "[Discovery]\n\tcommunication_vlan\t=\t0\n\tprimary_server\t=\t127.0.0.1\n\tserver_discovery_mode\t=\t2\n"

func TestNativeManagementStaging(t *testing.T) {
	for _, mode := range []string{"static", "dhcp"} {
		for _, comm := range []string{"untagged", "99"} {
			t.Run(mode+"/"+comm, func(t *testing.T) {
				dir := t.TempDir()
				disc, pending := filepath.Join(dir, "discovery.conf"), filepath.Join(dir, "pending")
				for name, data := range map[string]string{"ifcfg-br0.factory": nativeNetworkFixture, "ifcfg-br0": nativeNetworkFixture, "discovery.conf": discoveryFixture} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				req := ManagementRequest{Mode: mode, IPv4: "10.20.30.41", Netmask: "255.255.255.0", Gateway: "10.20.30.1", DNS: []string{"10.20.30.1"}}
				if err := writeManagementFiles(dir, disc, pending, []byte("boot-a\n"), req, comm); err != nil {
					t.Fatal(err)
				}
				device, vlan := "br0", "0"
				if comm != "untagged" {
					device, vlan = "br0."+comm, comm
				}
				data, err := os.ReadFile(filepath.Join(dir, "ifcfg-"+device))
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range []string{"DEVICE=" + device, "BOOTPROTO=" + mode, "IPV6ADDR=2001:db8::40", "VENDOR_OPTION=retain"} {
					if !strings.Contains(string(data), value+"\n") {
						t.Errorf("missing %s: %s", value, data)
					}
				}
				if mode == "static" {
					for _, value := range []string{"IPADDR=10.20.30.41", "SecondaryDNS=", "TertiaryDNS=", "DNSPrefix="} {
						if !strings.Contains(string(data), value+"\n") {
							t.Errorf("old DNS not cleared or address not saved: %s", data)
						}
					}
				}
				out, err := os.ReadFile(disc)
				if err != nil {
					t.Fatal(err)
				}
				want := strings.Replace(discoveryFixture, "\t=\t0\n", "\t=\t"+vlan+"\n", 1)
				if string(out) != want {
					t.Fatalf("discovery fields changed unexpectedly: %s", out)
				}
				flag, _ := os.ReadFile(pending)
				if string(flag) != "boot-a\n" {
					t.Fatalf("wrong pending-boot marker: %q", flag)
				}
				info, _ := os.Stat(pending)
				if info.Mode().Perm() != 0o600 {
					t.Fatal("pending marker is not private")
				}
			})
		}
	}
}

func TestManagementStagingRollsBackOnWriteFailure(t *testing.T) {
	for _, comm := range []string{"untagged", "99"} {
		t.Run(comm, func(t *testing.T) {
			dir := t.TempDir()
			for name, data := range map[string]string{"ifcfg-br0.factory": nativeNetworkFixture, "ifcfg-br0": nativeNetworkFixture, "discovery.conf": discoveryFixture} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			req := ManagementRequest{Mode: "static", IPv4: "10.20.30.41", Netmask: "255.255.255.0", Gateway: "10.20.30.1", DNS: []string{"10.20.30.1"}}
			err := writeManagementFiles(dir, filepath.Join(dir, "discovery.conf"), filepath.Join(dir, "missing-dir/pending"), []byte("boot-a"), req, comm)
			if err == nil {
				t.Fatal("expected staging failure")
			}
			for name, want := range map[string]string{"ifcfg-br0": nativeNetworkFixture, "discovery.conf": discoveryFixture} {
				data, _ := os.ReadFile(filepath.Join(dir, name))
				if string(data) != want {
					t.Fatalf("%s not restored: %s", name, data)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "ifcfg-br0.99")); !os.IsNotExist(err) {
				t.Fatal("new inactive VLAN file was not removed")
			}
		})
	}
}
