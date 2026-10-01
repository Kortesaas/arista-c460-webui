package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TrustCheck mirrors the firmware's boot-time checks (/etc/rc.d/fs_init with
// /opt/sensor/scripts/pre_overlay_check.sh and post_overlay_check.sh; see also
// deploy/overlay-check.sh). If any of them would fail, the next boot deletes
// the whole writable layer and switches firmware partitions, so a reboot from
// the UI is refused.
type TrustCheck struct {
	OK       bool     `json:"ok"`
	Problems []string `json:"problems"`
}

const overlayUpper = "/overlay/upper"

var protectedDirs = []string{"/usr", "/bin", "/etc", "/opt/lib", "/opt/keys", "/opt/dhclient", "/opt/udhcpc", "/opt/scripts", "/opt/sensor/scripts", "/opt/init.d", "/lib", "/sbin", "/lib64"}
var exemptPrefixes = []string{"/etc/resolv.conf", "/etc/profile", "/sbin/csr8x11-a12-bt4.2-patch.psr", "/usr/local/etc/", "/lib/firmware/"}
var pcrFiles = []string{"/opt/passwd", "/opt/group", "/opt/shells", "/opt/sensor/sensor-shell", "/opt/sensor/sensor.md5"}

func runTrustCheck(ctx context.Context) TrustCheck {
	var problems []string
	for _, dir := range protectedDirs {
		root := overlayUpper + dir
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return nil
			}
			rel := strings.TrimPrefix(path, overlayUpper)
			for _, ex := range exemptPrefixes {
				if strings.HasPrefix(rel, ex) {
					return nil
				}
			}
			problems = append(problems, "file in protected directory: "+rel)
			return nil
		})
	}
	for _, f := range pcrFiles {
		upper, err := os.ReadFile(overlayUpper + f)
		if err != nil {
			continue // not modified in the overlay
		}
		rom, err := os.ReadFile("/rom" + f)
		if err != nil || !bytes.Equal(upper, rom) {
			problems = append(problems, "measured file changed: "+f)
		}
	}
	md5ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(md5ctx, "md5sum", "-c", "/opt/sensor/sensor.md5")
	cmd.Dir = "/"
	cmd.Env = cleanEnv()
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasSuffix(line, ": OK") {
			problems = append(problems, "vendor file check: "+line)
		}
	}
	if len(problems) > 20 {
		problems = append(problems[:20], "…and more")
	}
	return TrustCheck{OK: len(problems) == 0, Problems: problems}
}
