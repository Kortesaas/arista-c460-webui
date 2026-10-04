package main

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func radioWidthFixture() *API {
	return &API{poller: &Poller{
		raw: map[string]any{"radios": map[string]any{"radio": []any{map[string]any{
			"id": float64(2), "operating-frequency": "openconfig-wifi-types:FREQ_6GHZ",
			"config": map[string]any{"channel-width": 160, "channel": 5, "enabled": true, "transmit-power": 23, "scanning": true},
		}}}},
		state: APState{Radios: []Radio{{ID: 2, Band: "6", Width: 160, AllowedChannels: []int{5}}}},
	}}
}

func TestRadioWidthSchemaBoundary(t *testing.T) {
	a := radioWidthFixture()
	req := radioRequest{Enabled: true, Channel: 5, Width: 320, Power: 23}
	// An invalid request must be stopped before calling the AP, including
	// requests from a browser that still has the old options cached.
	raw, _ := json.Marshal(req)
	r := httptest.NewRequest("PUT", "/api/radios/2", strings.NewReader(string(raw)))
	r.SetPathValue("id", "2")
	w := httptest.NewRecorder()
	a.updateRadio(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "use 160 MHz") {
		t.Fatal(w.Code, w.Body.String())
	}
	id := 2
	if plan, err := a.planBatch([]batchChange{{Kind: "radio", ID: &id, Radio: &req}}); err == nil || plan != nil || !strings.Contains(err.Error(), "use 160 MHz") {
		t.Fatal("staged 320 MHz update not rejected", plan, err)
	}
	before, _, _ := a.poller.RadioConfig(2)
	for _, width := range []int{20, 40, 80, 160} {
		req.Width = width
		entry, err := a.radioEntry(2, req)
		cfg, _ := entry["config"].(map[string]any)
		if err != nil || cfg["channel-width"] != width || cfg["scanning"] != true {
			t.Fatal("valid width or unrelated configuration lost", width, err)
		}
	}
	after, _, _ := a.poller.RadioConfig(2)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("validation mutated the AP snapshot")
	}
}
