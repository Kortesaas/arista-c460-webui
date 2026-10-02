package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementSaveValidationAndOrdering(t *testing.T) {
	valid := `{"mode":"static","ipv4":"10.20.30.41","netmask":"255.255.255.0","gateway":"10.20.30.1","dns":["10.20.30.1"],"dnsSearch":"","commVlan":"99"}`
	for _, tc := range []struct {
		name   string
		body   string
		ready  bool
		failAt int
		status int
		writes int
	}{
		{"not ready", valid, false, 0, http.StatusServiceUnavailable, 0},
		{"VLAN injection", strings.Replace(valid, `"99"`, `"99; reboot"`, 1), true, 0, http.StatusBadRequest, 0},
		{"gateway outside subnet", strings.Replace(valid, `"gateway":"10.20.30.1"`, `"gateway":"10.20.31.1"`, 1), true, 0, http.StatusBadRequest, 0},
		{"tagged management", valid, true, 0, http.StatusOK, 2},
		{"preserve VLAN", strings.Replace(valid, `,"commVlan":"99"`, "", 1), true, 0, http.StatusOK, 1},
		{"address save fails", valid, true, 1, http.StatusBadGateway, 1},
		{"VLAN save fails", valid, true, 2, http.StatusBadGateway, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commands []string
			a := &API{cli: &CLIInfo{}, command: func(_ context.Context, cmd string) (string, error) {
				commands = append(commands, cmd)
				if tc.failAt == len(commands) {
					return "", errors.New("firmware rejected command")
				}
				return "", nil
			}}
			if tc.ready {
				a.cli.management = Management{Mode: "static", CommVLAN: "untagged"}
			}
			w := httptest.NewRecorder()
			a.updateManagement(w, httptest.NewRequest(http.MethodPut, "/api/management", strings.NewReader(tc.body)))
			if w.Code != tc.status || len(commands) != tc.writes {
				t.Fatalf("status=%d commands=%v body=%s", w.Code, commands, w.Body.String())
			}
			if len(commands) == 2 && (!strings.HasPrefix(commands[0], "force vlan static id 99 ") || commands[1] != "force vlan communication id 99") {
				t.Fatalf("management VLAN selected before address configured: %v", commands)
			}
			if tc.name == "preserve VLAN" && !strings.HasPrefix(commands[0], "force vlan static id U ") {
				t.Fatalf("current native VLAN not preserved: %v", commands)
			}
			if tc.failAt == 2 && !strings.Contains(w.Body.String(), "Address settings were saved") {
				t.Fatalf("partial change not explained: %s", w.Body.String())
			}
		})
	}
}
