package main

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

type Device struct {
	Hostname      string   `json:"hostname"`
	SiteName      string   `json:"siteName"`
	Model         string   `json:"model"`
	Firmware      string   `json:"firmware"`
	MAC           string   `json:"mac"`
	MgmtIP        string   `json:"mgmtIp"`
	MgmtPrefix    int      `json:"mgmtPrefix"`
	Gateway       string   `json:"gateway"`
	Country       string   `json:"country"`
	UptimeSeconds float64  `json:"uptimeSeconds"`
	Load          []string `json:"load"`
	MemTotal      uint64   `json:"memTotal"`
	MemAvailable  uint64   `json:"memAvailable"`
	StorageTotal  uint64   `json:"storageTotal"`
	StorageFree   uint64   `json:"storageFree"`
	TemperatureC  *float64 `json:"temperatureC"`
	SSHEnabled    bool     `json:"sshEnabled"`
	UIVersion     string   `json:"uiVersion"`
}

var bannerField = regexp.MustCompile(`(Model|Build|Version)\s*:\s*\[([^\]]*)\]`)

func readDevice(cfg *Config) Device {
	site, _ := cfg.Labels()
	d := Device{SiteName: site, Model: "C-460", UIVersion: version, Load: []string{}}

	// The vendor SSH banner carries model and build strings.
	if raw, err := os.ReadFile("/opt/banner"); err == nil {
		for _, m := range bannerField.FindAllStringSubmatch(string(raw), -1) {
			switch m[1] {
			case "Model":
				d.Model = m[2]
			case "Build":
				d.Firmware = m[2]
			case "Version":
				if d.Firmware == "" {
					d.Firmware = m[2]
				}
			}
		}
	}
	if raw, err := os.ReadFile("/sys/class/net/eth0/address"); err == nil {
		d.MAC = strings.ToUpper(strings.TrimSpace(string(raw)))
	}
	if raw, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(raw)); len(f) > 0 {
			d.UptimeSeconds, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(raw)); len(f) >= 3 {
			d.Load = f[:3]
		}
	}
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			kb, _ := strconv.ParseUint(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				d.MemTotal = kb * 1024
			case "MemAvailable:":
				d.MemAvailable = kb * 1024
			}
		}
	}
	var fs syscall.Statfs_t
	if syscall.Statfs("/overlay", &fs) == nil || syscall.Statfs("/", &fs) == nil {
		d.StorageTotal = fs.Blocks * uint64(fs.Bsize)
		d.StorageFree = fs.Bavail * uint64(fs.Bsize)
	}
	d.TemperatureC = maxTemperature()
	if raw, err := os.ReadFile("/tmp/ath_country_code"); err == nil {
		if _, v, ok := strings.Cut(strings.TrimSpace(string(raw)), "="); ok {
			d.Country = countryName(v)
		}
	}
	d.MgmtIP, d.MgmtPrefix = interfaceIPv4("br0")
	d.Gateway = defaultGateway()
	return d
}

func maxTemperature() *float64 {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	var best *float64
	for _, z := range zones {
		raw, err := os.ReadFile(z)
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil || v <= 0 {
			continue
		}
		c := v / 1000
		if best == nil || c > *best {
			best = &c
		}
	}
	return best
}

func interfaceIPv4(name string) (string, int) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", 0
	}
	addrs, _ := iface.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			ones, _ := n.Mask.Size()
			return n.IP.String(), ones
		}
	}
	return "", 0
}

func defaultGateway() string {
	raw, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) > 2 && f[1] == "00000000" {
			b, err := hex.DecodeString(f[2])
			if err != nil || len(b) != 4 {
				continue
			}
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, binary.LittleEndian.Uint32(b))
			return ip.String()
		}
	}
	return ""
}

// countryName maps the ISO 3166-1 numeric regulatory code used by the driver.
func countryName(code string) string {
	names := map[string]string{
		"276": "DE", "40": "AT", "756": "CH", "250": "FR", "380": "IT", "528": "NL", "56": "BE",
		"208": "DK", "752": "SE", "578": "NO", "246": "FI", "724": "ES", "620": "PT", "616": "PL",
		"203": "CZ", "826": "GB", "372": "IE", "840": "US", "124": "CA", "36": "AU", "554": "NZ", "392": "JP",
	}
	if n, ok := names[code]; ok {
		return n
	}
	return code
}
