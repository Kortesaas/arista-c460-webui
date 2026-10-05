//go:build !linux

package main

import "os/exec"

func protectCaptureProcess(cmd *exec.Cmd) {}
