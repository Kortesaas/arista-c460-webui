package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type BridgeStatus struct {
	Name      string   `json:"name"`
	VLAN      *int     `json:"vlan"`
	VLANMode  string   `json:"vlanMode"`
	Up        bool     `json:"up"`
	Addresses []string `json:"addresses"`
	Members   []string `json:"members"`
	Networks  []string `json:"networks"`
}
type NetworkRoute struct {
	Destination string `json:"dst"`
	Gateway     string `json:"gateway"`
	Interface   string `json:"dev"`
}
type NetworkNeighbor struct {
	Address   string   `json:"dst"`
	Interface string   `json:"dev"`
	MAC       string   `json:"lladdr"`
	State     []string `json:"state"`
	Gateway   bool     `json:"gateway"`
}
type NetworkSnapshot struct {
	Bridges   []BridgeStatus    `json:"bridges"`
	Routes    []NetworkRoute    `json:"routes"`
	Neighbors []NetworkNeighbor `json:"neighbors"`
	Warnings  []string          `json:"warnings"`
	SampledAt time.Time         `json:"sampledAt"`
}

func parseVLANs(raw string) map[string]int {
	result := map[string]int{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 3 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(fields[1]))
		if err == nil && id >= 1 && id <= 4094 {
			result[strings.TrimSpace(fields[0])] = id
		}
	}
	return result
}
func bridgeVLAN(members []string, vlans map[string]int) (*int, string) {
	seen := map[int]bool{}
	untagged := false
	for _, member := range members {
		if id, ok := vlans[member]; ok {
			seen[id] = true
		} else {
			untagged = true
		}
	}
	if len(members) == 0 {
		return nil, "unknown"
	}
	if len(seen) == 0 {
		return nil, "native"
	}
	if len(seen) == 1 && !untagged {
		for id := range seen {
			return &id, "tagged"
		}
	}
	return nil, "mixed"
}
func collectBridges(root string, vlans map[string]int, state APState, addresses map[string][]string) []BridgeStatus {
	bridges := []BridgeStatus{}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		base := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(base, "bridge")); err != nil {
			continue
		}
		members := []string{}
		raw, _ := os.ReadDir(filepath.Join(base, "brif"))
		for _, m := range raw {
			members = append(members, m.Name())
		}
		sort.Strings(members)
		flags, _ := os.ReadFile(filepath.Join(base, "flags"))
		bits, _ := strconv.ParseUint(strings.TrimSpace(string(flags)), 0, 32)
		vlan, mode := bridgeVLAN(members, vlans)
		b := BridgeStatus{Name: entry.Name(), VLAN: vlan, VLANMode: mode, Up: bits&1 != 0, Addresses: []string{}, Members: members, Networks: []string{}}
		b.Addresses = append(b.Addresses, addresses[b.Name]...)
		for _, ssid := range state.SSIDs {
			for _, m := range members {
				bare := strings.SplitN(m, ".", 2)[0]
				mac, _ := os.ReadFile(filepath.Join(root, bare, "address"))
				if strings.TrimSpace(string(mac)) == "" {
					continue
				}
				found := false
				for _, vap := range ssid.BSSIDs {
					if strings.EqualFold(strings.TrimSpace(string(mac)), vap.BSSID) {
						found = true
						break
					}
				}
				if found {
					b.Networks = append(b.Networks, ssid.Name)
					break
				}
			}
		}
		sort.Strings(b.Networks)
		bridges = append(bridges, b)
	}
	return bridges
}
func readNetwork(ctx context.Context, state APState) NetworkSnapshot {
	result := NetworkSnapshot{Routes: []NetworkRoute{}, Neighbors: []NetworkNeighbor{}, Warnings: []string{}, SampledAt: time.Now()}
	vlans, vlanErr := os.ReadFile("/proc/net/vlan/config")
	addresses := map[string][]string{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		list, _ := iface.Addrs()
		for _, addr := range list {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && !ip.IsLinkLocalUnicast() && !ip.IsLoopback() {
				addresses[iface.Name] = append(addresses[iface.Name], addr.String())
			}
		}
	}
	result.Bridges = collectBridges("/sys/class/net", parseVLANs(string(vlans)), state, addresses)
	if vlanErr != nil {
		result.Warnings = append(result.Warnings, "VLAN membership could not be read")
		for i := range result.Bridges {
			result.Bridges[i].VLAN = nil
			result.Bridges[i].VLANMode = "unknown"
		}
	}
	for _, family := range []string{"-4", "-6"} {
		out, err := runVendorTool(ctx, "ip", "-j", family, "route", "show")
		var routes []NetworkRoute
		if err == nil {
			err = json.Unmarshal([]byte(out), &routes)
		}
		if err != nil {
			result.Warnings = append(result.Warnings, "IPv"+strings.TrimPrefix(family, "-")+" routes could not be read")
		} else {
			for _, route := range routes {
				// Each radio/VLAN has a redundant link-local route. Keep the
				// route view focused on destinations used for management.
				ip, _, parseErr := net.ParseCIDR(route.Destination)
				if parseErr == nil && ip.IsLinkLocalUnicast() {
					continue
				}
				result.Routes = append(result.Routes, route)
			}
		}
	}
	out, err := runVendorTool(ctx, "ip", "-j", "neigh", "show")
	if err == nil {
		err = json.Unmarshal([]byte(out), &result.Neighbors)
	}
	if err != nil {
		result.Neighbors = []NetworkNeighbor{}
		result.Warnings = append(result.Warnings, "Network neighbours could not be read")
	}
	for i := range result.Neighbors {
		for _, route := range result.Routes {
			if route.Destination == "default" && route.Gateway == result.Neighbors[i].Address && route.Interface == result.Neighbors[i].Interface {
				result.Neighbors[i].Gateway = true
			}
		}
	}
	sort.Slice(result.Neighbors, func(i, j int) bool { return result.Neighbors[i].Address < result.Neighbors[j].Address })
	return result
}
func (a *API) networkStatus(w http.ResponseWriter, r *http.Request) {
	a.networkMu.Lock()
	defer a.networkMu.Unlock()
	if a.networkCache == nil || time.Since(a.networkCache.SampledAt) > 5*time.Second {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		value := readNetwork(ctx, a.poller.Snapshot())
		a.networkCache = &value
	}
	reply(w, 200, a.networkCache)
}
