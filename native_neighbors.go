package main

import (
	"net"
	"os"
	"strconv"
	"strings"
)

// OpenConfig often reports addresses learned from DHCP only. A complete local
// ARP entry can also identify a station using a manually assigned IPv4 address.
// Avoid guessing if the same MAC has more than one address in the cache.
func parseARPAddresses(text string) map[string]string {
	addresses := map[string]string{}
	ambiguous := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 6 {
			continue
		}
		ip := net.ParseIP(fields[0])
		flags, err := strconv.ParseUint(fields[2], 0, 8)
		mac := strings.ToLower(fields[3])
		if err != nil || flags&2 == 0 || ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || !macPattern.MatchString(mac) || mac == "00:00:00:00:00:00" {
			continue
		}
		if old, ok := addresses[mac]; ok && old != ip.String() {
			ambiguous[mac] = true
		}
		addresses[mac] = ip.String()
	}
	for mac := range ambiguous {
		delete(addresses, mac)
	}
	return addresses
}
func supplementClientAddresses(clients []Client, arp map[string]string) {
	for i := range clients {
		if clients[i].IPv4 == "" {
			if ip := arp[strings.ToLower(clients[i].MAC)]; ip != "" {
				clients[i].IPv4 = ip
				clients[i].IPv4Source = "arp"
			}
		}
	}
}
func readARPAddresses() map[string]string {
	data, _ := os.ReadFile("/proc/net/arp")
	return parseARPAddresses(string(data))
}
