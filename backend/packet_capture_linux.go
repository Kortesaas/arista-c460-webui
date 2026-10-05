package main

import (
	"os/exec"
	"syscall"
)

// Stop tcpdump if the manager crashes or is forcibly killed. vendorShell execs
// the tool in the same process, so the death signal follows it onto tcpdump.
func protectCaptureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
