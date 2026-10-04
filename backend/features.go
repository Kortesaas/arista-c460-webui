package main

// Advanced per-network and per-radio settings. Each one is an OpenConfig leaf
// that this firmware's converter translates, paired with the native key it
// produces in /opt/ap/ap.conf, so the UI can show what the firmware actually
// runs next to what was requested. A missing leaf means "firmware default".

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
)

type featureDef struct {
	Key       string   // API name
	Container []string // OpenConfig container below the SSID/radio entry
	Leaf      string
	Int       bool   // integer instead of on/off
	Min, Max  int    // integer range
	Native    string // ap.conf key for readback ("" = none)
}

var ssidFeatureDefs = []featureDef{
	{Key: "rrm", Container: []string{"config"}, Leaf: "dot11k", Native: "NEIGHBOR_11K_ENABLE"},
	{Key: "load", Container: []string{"config"}, Leaf: "qbss-load", Native: "BSS_LOAD_ENABLE"},
	{Key: "bssTransition", Container: []string{"dot11v", "config"}, Leaf: "dot11v-bsstransition", Native: "BSS_TRANSITION_11V_ENABLE"},
	{Key: "fastRoaming", Container: []string{"dot11r", "config"}, Leaf: "dot11r", Native: "FT_ENABLE"},
	{Key: "okc", Container: []string{"config"}, Leaf: "okc", Native: "AP_OKC_ENABLED"},
	{Key: "bandSteering", Container: []string{"band-steering", "config"}, Leaf: "band-steering", Native: "BS_BAND_STEERING_ENABLED"},
	{Key: "multicastFilter", Container: []string{"config"}, Leaf: "multicast-filter", Native: "BROADCAST_MULTICAST_OPT_ENABLED"},
	{Key: "broadcastFilter", Container: []string{"config"}, Leaf: "broadcast-filter", Native: "PROXYARP_ENABLE"},
	{Key: "advertiseName", Container: []string{"config"}, Leaf: "advertise-apname", Native: "ADVERTISE_APNAME"},
}

var radioFeatureDefs = []featureDef{
	{Key: "dlOfdma", Container: []string{"config"}, Leaf: "dl-ofdma-enabled", Native: "DL_OFDMA_ENABLED"},
	{Key: "ulOfdma", Container: []string{"config"}, Leaf: "ul-ofdma-enabled", Native: "UL_OFDMA_ENABLED"},
	{Key: "dlMuMimo", Container: []string{"config"}, Leaf: "dl-mu-mimo-enabled", Native: "MU_MIMO_ENABLED"},
	{Key: "ulMuMimo", Container: []string{"config"}, Leaf: "ul-mu-mimo-enabled", Native: "UL_MU_MIMO_ENABLED"},
	{Key: "bssColoring", Container: []string{"config"}, Leaf: "bss-coloring", Native: "BSS_COLOR_ENABLED"},
	{Key: "spatialReuse", Container: []string{"config"}, Leaf: "spatial-reuse", Native: "SR_ENABLED"},
	{Key: "dtpMin", Container: []string{"config"}, Leaf: "dtp-min", Int: true, Min: 1, Max: 30, Native: "TPC_MIN_TX_POWER"},
	{Key: "dtpMax", Container: []string{"config"}, Leaf: "dtp-max", Int: true, Min: 1, Max: 30, Native: "TPC_MAX_TX_POWER"},
}

// featureValues reads the configured values; absent leaves are null.
func featureValues(entry map[string]any, defs []featureDef) map[string]any {
	out := map[string]any{}
	for _, d := range defs {
		v := dig(entry, append(append([]string{}, d.Container...), d.Leaf)...)
		switch x := v.(type) {
		case bool:
			out[d.Key] = x
		case float64:
			out[d.Key] = int(x)
		case string:
			if n, err := strconv.Atoi(x); err == nil && d.Int {
				out[d.Key] = n
			} else {
				out[d.Key] = nil
			}
		default:
			out[d.Key] = nil
		}
	}
	return out
}

// ------------------------------------------------------- native readback

var nativeSectionStart = regexp.MustCompile(`^\[ (VAP|RADIO|RADIO_v2)_START(?:=\d+)? \]$`)
var nativeSectionEnd = regexp.MustCompile(`^\[ (VAP|RADIO|RADIO_v2)_END(?:=\d+)? \]$`)

// nativeSections splits ap.conf into VAP and radio sections as key/value maps.
func nativeSections(path string) (vaps, radios []map[string]string, err error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		return nil, nil, err
	}
	var cur map[string]string
	var kind string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if m := nativeSectionStart.FindStringSubmatch(line); m != nil {
			cur, kind = map[string]string{}, m[1]
			continue
		}
		if nativeSectionEnd.MatchString(line) {
			if cur != nil {
				if kind == "VAP" {
					vaps = append(vaps, cur)
				} else if len(cur) > 1 {
					radios = append(radios, cur)
				}
			}
			cur = nil
			continue
		}
		if cur == nil {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			cur[k] = strings.Trim(v, `'"`)
		}
	}
	return vaps, radios, nil
}

func nativeFor(defs []featureDef, section map[string]string) map[string]any {
	out := map[string]any{}
	for _, d := range defs {
		if d.Native == "" || section == nil {
			continue
		}
		v, ok := section[d.Native]
		if !ok {
			continue
		}
		if d.Int {
			if n, err := strconv.Atoi(v); err == nil {
				out[d.Key] = n
			}
			continue
		}
		out[d.Key] = v != "0" && v != "" && v != "false"
	}
	return out
}

func (a *API) nativeVAP(ssid string) map[string]string {
	vaps, _, err := nativeSections(a.apConfPath())
	if err != nil {
		return nil
	}
	for _, v := range vaps {
		if v["AP_SSID"] == ssid {
			return v
		}
	}
	return nil
}

func (a *API) nativeRadio(band string) map[string]string {
	_, radios, err := nativeSections(a.apConfPath())
	if err != nil {
		return nil
	}
	for _, r := range radios {
		if strings.TrimSuffix(r["WIRELESS_BAND"], "G") == band {
			return r
		}
	}
	return nil
}

func (a *API) apConfPath() string {
	if a.overrides != nil && a.overrides.apConf != "" {
		return a.overrides.apConf
	}
	return nativeAPConf
}

// --------------------------------------------------------------- writes

type featureChange struct {
	body    map[string]any // nested containers with the new leaves
	deletes [][]string     // container path + leaf, relative to the entry
	changed bool
}

// planFeatures validates the request and builds the merge update.
func planFeatures(defs []featureDef, entry map[string]any, input map[string]json.RawMessage) (featureChange, error) {
	known := map[string]featureDef{}
	for _, d := range defs {
		known[d.Key] = d
	}
	for k := range input {
		if _, ok := known[k]; !ok {
			return featureChange{}, fmt.Errorf("unknown setting %q", k)
		}
	}
	fc := featureChange{body: map[string]any{}}
	for _, d := range defs {
		raw, present := input[d.Key]
		if !present {
			continue
		}
		path := append(append([]string{}, d.Container...), d.Leaf)
		if string(raw) == "null" {
			if dig(entry, path...) != nil {
				fc.deletes = append(fc.deletes, path)
				fc.changed = true
			}
			continue
		}
		var value any
		if d.Int {
			var n int
			if err := json.Unmarshal(raw, &n); err != nil || n < d.Min || n > d.Max {
				return featureChange{}, fmt.Errorf("%s must be %d–%d or null", d.Key, d.Min, d.Max)
			}
			value = n
		} else {
			var b bool
			if err := json.Unmarshal(raw, &b); err != nil {
				return featureChange{}, fmt.Errorf("%s must be true, false or null", d.Key)
			}
			value = b
		}
		node := fc.body
		for _, c := range d.Container {
			next, _ := node[c].(map[string]any)
			if next == nil {
				next = map[string]any{}
				node[c] = next
			}
			node = next
		}
		node[d.Leaf] = value
		fc.changed = true
	}
	return fc, nil
}

// decodeFeatureInput reads and validates a request before anything is locked.
// mobilityDomain is a stable id per network name (FNV-1a). The firmware
// writes it to hostapd as decimal digits where hostapd expects four hex
// digits, so it stays within 1000–9999 like the firmware's own default.
func mobilityDomain(ssid string) int {
	h := uint32(2166136261)
	for i := 0; i < len(ssid); i++ {
		h ^= uint32(ssid[i])
		h *= 16777619
	}
	return 1000 + int(h%9000)
}

func decodeFeatureInput(w http.ResponseWriter, r *http.Request, defs []featureDef) (map[string]json.RawMessage, bool) {
	var input map[string]json.RawMessage
	if !decode(w, r, &input) {
		return nil, false
	}
	if _, err := planFeatures(defs, map[string]any{}, input); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return input, true
}

func (a *API) updateSSIDFeatures(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeFeatureInput(w, r, ssidFeatureDefs)
	if !ok {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	name := r.PathValue("name")
	entry, ok := a.poller.SSIDEntry(name)
	if !ok {
		fail(w, 404, "Network not found")
		return
	}
	fc, err := planFeatures(ssidFeatureDefs, entry, input)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !fc.changed {
		reply(w, 200, map[string]any{"ok": true, "changed": false})
		return
	}
	// 802.11r only roams between APs in the same mobility domain. Derive it
	// from the network name, so every AP running this UI uses the same one.
	if on, _ := dig(fc.body, "dot11r", "config", "dot11r").(bool); on {
		fc.body["dot11r"].(map[string]any)["config"].(map[string]any)["dot11r-domainid"] = mobilityDomain(name)
	}
	// The config container is always sent whole, so its other leaves stay.
	cfg, _ := entry["config"].(map[string]any)
	merged := clone(cfg)
	if part, ok := fc.body["config"].(map[string]any); ok {
		for k, v := range part {
			merged[k] = v
		}
	}
	fc.body["config"] = merged
	fc.body["name"] = name
	var paths []*gpb.Path
	for _, p := range fc.deletes {
		elems := []*gpb.PathElem{elem("ssids"), elem("ssid", "name", name)}
		for _, e := range p {
			elems = append(elems, elem(e))
		}
		paths = append(paths, a.gnmi.apPath(elems...))
	}
	a.apply(w, r, "update advanced settings for "+name, map[string]any{"ssids": map[string]any{"ssid": []any{fc.body}}}, paths)
}

func (a *API) radioFeatures(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		fail(w, 400, "invalid radio id")
		return
	}
	cfg, freq, ok := a.poller.RadioConfig(id)
	if !ok {
		fail(w, 404, "radio not found")
		return
	}
	entry := map[string]any{"config": cfg}
	reply(w, 200, map[string]any{"settings": featureValues(entry, radioFeatureDefs), "native": nativeFor(radioFeatureDefs, a.nativeRadio(band(freq)))})
}

func (a *API) updateRadioFeatures(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		fail(w, 400, "invalid radio id")
		return
	}
	input, ok := decodeFeatureInput(w, r, radioFeatureDefs)
	if !ok {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	cfg, freq, ok := a.poller.RadioConfig(id)
	if !ok {
		fail(w, 404, "radio not found")
		return
	}
	entry := map[string]any{"config": cfg}
	fc, err := planFeatures(radioFeatureDefs, entry, input)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	merged := clone(cfg)
	if part, ok := fc.body["config"].(map[string]any); ok {
		for k, v := range part {
			merged[k] = v
		}
	}
	for _, p := range fc.deletes {
		delete(merged, p[len(p)-1])
	}
	lo, hasLo := num(merged["dtp-min"])
	hi, hasHi := num(merged["dtp-max"])
	if hasLo && hasHi && lo > hi {
		fail(w, 400, "the minimum automatic power must not exceed the maximum")
		return
	}
	if !fc.changed {
		reply(w, 200, map[string]any{"ok": true, "changed": false})
		return
	}
	merged["id"] = id
	merged["operating-frequency"] = freq
	var paths []*gpb.Path
	for _, p := range fc.deletes {
		paths = append(paths, a.gnmi.apPath(elem("radios"), elem("radio", "id", strconv.Itoa(id), "operating-frequency", freq), elem("config"), elem(p[len(p)-1])))
	}
	body := map[string]any{"radios": map[string]any{"radio": []any{map[string]any{"id": id, "operating-frequency": freq, "config": merged}}}}
	a.apply(w, r, fmt.Sprintf("update advanced settings for radio %d", id), body, paths)
}

// setFeatures keeps only the settings that differ from the firmware default.
func setFeatures(values map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range values {
		if v != nil {
			out[k] = v
		}
	}
	return out
}

// mergeFeatures writes saved feature values (from a backup) into an
// OpenConfig list entry, validating them like an interactive change.
func mergeFeatures(defs []featureDef, entry map[string]any, values map[string]any, ssid string) error {
	if len(values) == 0 {
		return nil
	}
	input := map[string]json.RawMessage{}
	for k, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		input[k] = raw
	}
	fc, err := planFeatures(defs, map[string]any{}, input)
	if err != nil {
		return err
	}
	if ssid != "" {
		if on, _ := dig(fc.body, "dot11r", "config", "dot11r").(bool); on {
			fc.body["dot11r"].(map[string]any)["config"].(map[string]any)["dot11r-domainid"] = mobilityDomain(ssid)
		}
	}
	for container, part := range fc.body {
		target, _ := entry[container].(map[string]any)
		if target == nil {
			entry[container] = part
			continue
		}
		mergeInto(target, part.(map[string]any))
	}
	return nil
}

func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if d, ok := dst[k].(map[string]any); ok {
				mergeInto(d, sub)
				continue
			}
		}
		dst[k] = v
	}
}
