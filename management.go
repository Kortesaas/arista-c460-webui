package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const networkScriptsDir = "/opt/sysconfig/network-scripts"
const discoveryFile = "/opt/sensor/discovery.conf"
const managementPendingFile = "/opt/c460-webui/management-pending"
const bootIDFile = "/proc/sys/kernel/random/boot_id"

// The firmware's force-vlan CLI commands reboot immediately, even for a no-op.
// Stage the same native files instead, preserving IPv6 and discovery settings.
// The vendor's boot pipeline consumes these files after an explicit restart.
func stageManagement(ctx context.Context, req ManagementRequest, comm string) error {
	id, err := communicationVLANID(comm)
	if err != nil {
		return err
	}
	device := "br0"
	if id != "U" {
		device += "." + id
	}
	lock := "/tmp/ifcfg-" + device + ".LOCK"
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		if err = os.Mkdir(lock, 0o700); err == nil {
			break
		}
		if !os.IsExist(err) {
			return fmt.Errorf("management lock: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("management settings are busy; try again")
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer os.Remove(lock)
	bootID, err := os.ReadFile(bootIDFile)
	if err != nil {
		return err
	}
	return writeManagementFiles(networkScriptsDir, discoveryFile, managementPendingFile, bootID, req, comm)
}

func managementMatches(req ManagementRequest, comm string, current Management) bool {
	if comm != current.CommVLAN || req.Mode != current.Mode {
		return false
	}
	if req.Mode == "dhcp" {
		return true
	}
	if strings.TrimSpace(req.IPv4) != current.IPv4 || strings.TrimSpace(req.Netmask) != current.Netmask || strings.TrimSpace(req.Gateway) != current.Gateway || strings.TrimSpace(req.DNSSearch) != current.DNSSearch || len(req.DNS) != len(current.DNS) {
		return false
	}
	for i := range req.DNS {
		if strings.TrimSpace(req.DNS[i]) != current.DNS[i] {
			return false
		}
	}
	return true
}

func managementPending() bool {
	pending, err := os.ReadFile(managementPendingFile)
	if err != nil {
		return false
	}
	boot, err := os.ReadFile(bootIDFile)
	return err == nil && strings.TrimSpace(string(pending)) == strings.TrimSpace(string(boot))
}

// Replace only the requested key, retaining other native configuration fields.
func nativeValue(data []byte, key, value string) []byte {
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		name, previous, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(name) == key {
			spacing := previous[:len(previous)-len(strings.TrimLeft(previous, " \t"))]
			lines[i] = name + "=" + spacing + value
			found = true
		}
	}
	if found {
		return []byte(strings.Join(lines, "\n"))
	}
	result := bytes.Clone(data)
	if len(result) > 0 && result[len(result)-1] != '\n' {
		result = append(result, '\n')
	}
	return append(result, []byte(key+"="+value+"\n")...)
}

type nativeWrite struct {
	path          string
	before, after []byte
	mode          os.FileMode
	existed       bool
}

func prepareNative(path string, fallback []byte) (nativeWrite, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nativeWrite{path: path, after: fallback, mode: 0o644}, nil
	}
	if err != nil {
		return nativeWrite{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nativeWrite{}, err
	}
	return nativeWrite{path: path, before: data, after: data, mode: info.Mode().Perm(), existed: true}, nil
}

func atomicNative(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".c460-management-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func writeManagementFiles(dir, discovery, pending string, bootID []byte, req ManagementRequest, comm string) error {
	if _, err := req.cliCommand(comm); err != nil {
		return err
	}
	device, vlan := "br0", "0"
	if comm != "untagged" {
		device, vlan = "br0."+comm, comm
	}
	template, err := os.ReadFile(filepath.Join(dir, "ifcfg-br0.factory"))
	if err != nil {
		return err
	}
	network, err := prepareNative(filepath.Join(dir, "ifcfg-"+device), template)
	if err != nil {
		return err
	}
	network.after = nativeValue(network.after, "DEVICE", device)
	network.after = nativeValue(network.after, "BOOTPROTO", req.Mode)
	if req.Mode == "static" {
		values := map[string]string{"IPADDR": ipv4Of(req.IPv4).String(), "NETMASK": ipv4Of(req.Netmask).String(), "GATEWAY": ipv4Of(req.Gateway).String(), "DNSPrefix": strings.TrimSpace(req.DNSSearch)}
		for i, key := range []string{"PrimaryDNS", "SecondaryDNS", "TertiaryDNS"} {
			values[key] = ""
			if i < len(req.DNS) {
				values[key] = ipv4Of(req.DNS[i]).String()
			}
		}
		for _, key := range []string{"IPADDR", "NETMASK", "GATEWAY", "PrimaryDNS", "SecondaryDNS", "TertiaryDNS", "DNSPrefix"} {
			network.after = nativeValue(network.after, key, values[key])
		}
	}
	disc, err := prepareNative(discovery, nil)
	if err != nil {
		return err
	}
	if !disc.existed {
		return fmt.Errorf("native discovery configuration is missing")
	}
	disc.after = nativeValue(disc.after, "communication_vlan", vlan)
	flag, err := prepareNative(pending, nil)
	if err != nil {
		return err
	}
	flag.after, flag.mode = bootID, 0o600
	var written []nativeWrite
	for _, item := range []nativeWrite{network, disc, flag} {
		if item.existed && bytes.Equal(item.before, item.after) {
			continue
		}
		if err := atomicNative(item.path, item.after, item.mode); err != nil {
			var restoreErrors []string
			restoreItems := append(written, item) // the rename may have succeeded before a directory-sync error
			for i := len(restoreItems) - 1; i >= 0; i-- {
				previous := restoreItems[i]
				var restore error
				if previous.existed {
					restore = atomicNative(previous.path, previous.before, previous.mode)
				} else {
					restore = os.Remove(previous.path)
					if os.IsNotExist(restore) {
						restore = nil
					}
				}
				if restore != nil {
					restoreErrors = append(restoreErrors, restore.Error())
				}
			}
			if len(restoreErrors) > 0 {
				return fmt.Errorf("save failed: %w; rollback incomplete: %s", err, strings.Join(restoreErrors, "; "))
			}
			return fmt.Errorf("save failed; previous settings restored: %w", err)
		}
		written = append(written, item)
	}
	return nil
}
