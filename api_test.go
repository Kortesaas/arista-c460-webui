package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementSaveValidation(t *testing.T) {
	valid := `{"mode":"static","ipv4":"10.20.30.41","netmask":"255.255.255.0","gateway":"10.20.30.1","dns":["10.20.30.1"],"dnsSearch":"","commVlan":"99"}`
	for _, tc := range []struct {
		name, body     string
		ready, fail    bool
		status, writes int
		vlan           string
	}{
		{"not ready", valid, false, false, http.StatusServiceUnavailable, 0, ""},
		{"VLAN injection", strings.Replace(valid, `"99"`, `"99; reboot"`, 1), true, false, http.StatusBadRequest, 0, ""},
		{"gateway outside subnet", strings.Replace(valid, `"gateway":"10.20.30.1"`, `"gateway":"10.20.31.1"`, 1), true, false, http.StatusBadRequest, 0, ""},
		{"tagged management", valid, true, false, http.StatusOK, 1, "99"},
		{"preserve VLAN", strings.Replace(valid, `,"commVlan":"99"`, "", 1), true, false, http.StatusOK, 1, "untagged"},
		{"identical settings", strings.Replace(strings.Replace(valid, `,"commVlan":"99"`, "", 1), "10.20.30.41", "10.20.30.40", 1), true, false, http.StatusOK, 0, ""},
		{"save fails", valid, true, true, http.StatusInternalServerError, 1, "99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			a := &API{cli: &CLIInfo{}, stage: func(req ManagementRequest, vlan string) error {
				writes++
				if vlan != tc.vlan || req.IPv4 != "10.20.30.41" {
					t.Fatalf("wrong staging input: %+v vlan=%s", req, vlan)
				}
				if tc.fail {
					return errors.New("write failed")
				}
				return nil
			}}
			if tc.ready {
				a.cli.management = Management{Mode: "static", CommVLAN: "untagged", IPv4: "10.20.30.40", Netmask: "255.255.255.0", Gateway: "10.20.30.1", DNS: []string{"10.20.30.1"}}
			}
			w := httptest.NewRecorder()
			a.updateManagement(w, httptest.NewRequest(http.MethodPut, "/api/management", strings.NewReader(tc.body)))
			if w.Code != tc.status || writes != tc.writes {
				t.Fatalf("status=%d writes=%d body=%s", w.Code, writes, w.Body.String())
			}
			if tc.name == "identical settings" && !strings.Contains(w.Body.String(), `"changed":false`) {
				t.Fatalf("no-op not reported: %s", w.Body.String())
			}
		})
	}
}
