package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnnotatePorts(t *testing.T) {
	root := t.TempDir()
	for name, dev := range map[string]string{"eth0": "../../../3a510000.dp2", "eth1": "../../../3a514000.dp1"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dev, filepath.Join(root, name, "device")); err != nil {
			t.Fatal(err)
		}
	}
	ifaces := []Interface{{Name: "eth0"}, {Name: "eth1"}}
	annotatePorts(ifaces, root)
	if ifaces[0].Name != "eth1" || ifaces[0].Port != 1 || ifaces[0].Role != "backup" {
		t.Fatalf("first: %+v", ifaces[0])
	}
	if ifaces[1].Name != "eth0" || ifaces[1].Port != 2 || ifaces[1].Role != "uplink" {
		t.Fatalf("second: %+v", ifaces[1])
	}
}
