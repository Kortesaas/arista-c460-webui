package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// The SoC exposes the two Ethernet MACs as ".../3a514000.dp1" and
// ".../3a510000.dp2"; dpN is the physical socket labelled ETH N (LAN N in the
// vendor CLI). The logical names do not follow the sockets: the firmware makes
// eth0 whichever port currently carries the uplink.
var dpSuffix = regexp.MustCompile(`\.dp(\d+)$`)

func annotatePorts(ifaces []Interface, sysClassNet string) {
	for i := range ifaces {
		if link, err := os.Readlink(filepath.Join(sysClassNet, ifaces[i].Name, "device")); err == nil {
			if m := dpSuffix.FindStringSubmatch(link); m != nil {
				ifaces[i].Port, _ = strconv.Atoi(m[1])
			}
		}
		if ifaces[i].Name == "eth0" {
			ifaces[i].Role = "uplink"
		} else {
			ifaces[i].Role = "backup"
		}
	}
	sort.SliceStable(ifaces, func(a, b int) bool {
		pa, pb := ifaces[a].Port, ifaces[b].Port
		if pa == 0 || pb == 0 {
			return pa != 0
		}
		return pa < pb
	})
}
