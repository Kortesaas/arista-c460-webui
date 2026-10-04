package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// OpenConfig on this firmware can mirror eth0's link state and counters to
// eth1. The kernel reports each actual interface independently.
func supplementEthernet(interfaces []Interface, root string) {
	for i := range interfaces {
		iface := &interfaces[i]
		if iface.Name != "eth0" && iface.Name != "eth1" {
			continue
		}
		base := filepath.Join(root, iface.Name)
		number := func(path string) (float64, bool) {
			raw, err := os.ReadFile(filepath.Join(base, path))
			if err != nil {
				return 0, false
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
			return n, err == nil && n >= 0
		}
		if carrier, ok := number("carrier"); ok {
			iface.Up = carrier == 1
		}
		if speed, ok := number("speed"); ok && speed > 0 {
			if speed >= 1000 {
				iface.Speed = strconv.FormatFloat(speed/1000, 'f', -1, 64) + " Gbit/s"
			} else {
				iface.Speed = strconv.FormatFloat(speed, 'f', -1, 64) + " Mbit/s"
			}
		}
		if duplex, err := os.ReadFile(filepath.Join(base, "duplex")); err == nil {
			v := strings.TrimSpace(string(duplex))
			if v == "full" || v == "half" {
				iface.Duplex = v
			}
		}
		for path, target := range map[string]*float64{"rx_bytes": &iface.InOctets, "tx_bytes": &iface.OutOctets, "rx_errors": &iface.InErrors, "tx_errors": &iface.OutErrors, "rx_dropped": &iface.InDiscard, "tx_dropped": &iface.OutDiscard} {
			if value, ok := number("statistics/" + path); ok {
				*target = value
			}
		}
	}
}
