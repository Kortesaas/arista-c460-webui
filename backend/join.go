package main

// Join codes: the standard "WIFI:" string that phones read from a QR code.
// It contains the password, so only administrators get it and every request
// is written to the change log.

import (
	"net/http"
	"strings"
)

func wifiEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, `:`, `\:`, `"`, `\"`).Replace(s)
}

// joinPayload builds the QR content. WPA3 and mixed networks use T:WPA,
// which phones treat as "WPA family" and connect with the best mode offered.
func joinPayload(ssid, opmode, password string, hidden bool) string {
	var b strings.Builder
	b.WriteString("WIFI:")
	if needsPassword(opmode) {
		b.WriteString("T:WPA;S:" + wifiEscape(ssid) + ";P:" + wifiEscape(password) + ";")
	} else {
		b.WriteString("T:nopass;S:" + wifiEscape(ssid) + ";")
	}
	if hidden {
		b.WriteString("H:true;")
	}
	b.WriteString(";")
	return b.String()
}

func (a *API) joinCode(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cfg, ok := a.poller.SSIDConfig(name)
	if !ok {
		fail(w, http.StatusNotFound, "network not found")
		return
	}
	opmode := str(cfg["opmode"])
	if opmode == "WPA3_SAE" && a.overrides.IsMixed(name) {
		opmode = opModeMixed
	}
	password := str(firstOf(cfg["wpa3-psk"], cfg["wpa2-psk"]))
	hidden, _ := cfg["hidden"].(bool)
	reply(w, http.StatusOK, map[string]any{
		"ssid":     name,
		"opmode":   opmode,
		"password": password,
		"hidden":   hidden,
		"payload":  joinPayload(name, opmode, password, hidden),
	})
}
