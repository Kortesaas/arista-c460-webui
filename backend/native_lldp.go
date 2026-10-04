package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"
)

const lldpSettingsFile = "/opt/c460-webui/lldp-settings.json"

type LLDPTiming struct {
	Interval int `json:"interval"`
	Hold     int `json:"hold"`
}

func (s LLDPTiming) validate() error {
	if s.Interval < 5 || s.Interval > 3600 || s.Hold < 2 || s.Hold > 10 {
		return errors.New("LLDP interval must be 5–3600 seconds and hold multiplier 2–10")
	}
	return nil
}

type LLDPNeighbor struct {
	Interface       string   `json:"interface"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	ChassisID       string   `json:"chassisId"`
	PortID          string   `json:"portId"`
	PortDescription string   `json:"portDescription"`
	Addresses       []string `json:"addresses"`
	Age             string   `json:"age"`
	TTL             string   `json:"ttl"`
}
type LLDPState struct {
	LLDPTiming
	Saved         *LLDPTiming    `json:"saved"`
	Neighbors     []LLDPNeighbor `json:"neighbors"`
	NeighborError string         `json:"neighborError,omitempty"`
}

func parseLLDPTiming(text string) (LLDPTiming, error) {
	var tree map[string]any
	if err := json.Unmarshal([]byte(text), &tree); err != nil {
		return LLDPTiming{}, err
	}
	interval, _ := strconv.Atoi(str(dig(tree, "configuration", "config", "tx-delay")))
	hold, _ := strconv.Atoi(str(dig(tree, "configuration", "config", "tx-hold")))
	result := LLDPTiming{Interval: interval, Hold: hold}
	if interval <= 0 || hold <= 0 {
		return result, errors.New("LLDP daemon did not return timing settings")
	}
	return result, nil
}

type namedObject struct {
	name  string
	value map[string]any
}

func namedObjects(raw any) []namedObject {
	var result []namedObject
	switch value := raw.(type) {
	case map[string]any:
		for name, entry := range value {
			if object, ok := entry.(map[string]any); ok {
				result = append(result, namedObject{name, object})
			}
		}
	case []any:
		for _, entry := range value {
			result = append(result, namedObjects(entry)...)
		}
	}
	return result
}
func stringList(raw any) []string {
	result := []string{}
	switch v := raw.(type) {
	case string:
		if v != "" {
			result = append(result, v)
		}
	case []any:
		for _, entry := range v {
			if s, ok := entry.(string); ok && s != "" {
				result = append(result, s)
			}
		}
	}
	return result
}
func parseLLDPNeighbors(text string) ([]LLDPNeighbor, error) {
	var tree map[string]any
	if err := json.Unmarshal([]byte(text), &tree); err != nil {
		return nil, err
	}
	result := []LLDPNeighbor{}
	for _, iface := range namedObjects(dig(tree, "lldp", "interface")) {
		entry := iface.value
		chassis := namedObjects(entry["chassis"])
		if len(chassis) == 0 {
			continue
		}
		for _, c := range chassis {
			result = append(result, LLDPNeighbor{Interface: iface.name, Name: c.name, Description: str(c.value["descr"]), ChassisID: str(dig(c.value, "id", "value")), PortID: str(dig(entry, "port", "id", "value")), PortDescription: str(dig(entry, "port", "descr")), Addresses: stringList(c.value["mgmt-ip"]), Age: str(entry["age"]), TTL: str(dig(entry, "port", "ttl"))})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Interface != result[j].Interface {
			return result[i].Interface < result[j].Interface
		}
		return result[i].ChassisID < result[j].ChassisID
	})
	return result, nil
}

type lldpBackend struct {
	path string
	run  vendorRunner
}

func (b lldpBackend) read(ctx context.Context) (LLDPTiming, error) {
	out, err := b.run(ctx, "/sbin/lldpcli", "-f", "json", "show", "configuration")
	if err != nil {
		return LLDPTiming{}, err
	}
	return parseLLDPTiming(out)
}
func (b lldpBackend) apply(ctx context.Context, input LLDPTiming) error {
	if err := input.validate(); err != nil {
		return err
	}
	for _, v := range []struct {
		key   string
		value int
	}{{"tx-interval", input.Interval}, {"tx-hold", input.Hold}} {
		if _, err := b.run(ctx, "/sbin/lldpcli", "configure", "lldp", v.key, strconv.Itoa(v.value)); err != nil {
			return err
		}
	}
	observed, err := b.read(ctx)
	if err != nil {
		return err
	}
	if observed != input {
		return errors.New("LLDP daemon did not apply the requested timing")
	}
	return nil
}
func (b lldpBackend) save(ctx context.Context, input LLDPTiming) error {
	if err := input.validate(); err != nil {
		return err
	}
	before, err := b.read(ctx)
	if err != nil {
		return err
	}
	file, err := prepareNative(b.path, nil)
	if err != nil {
		return err
	}
	file.after, err = json.MarshalIndent(input, "", "  ")
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if e := b.apply(rollbackCtx, before); e != nil {
			return fmt.Errorf("%w; could not restore LLDP timing: %v", cause, e)
		}
		return fmt.Errorf("%w; previous LLDP timing restored", cause)
	}
	if before != input {
		if err = b.apply(ctx, input); err != nil {
			return rollback(err)
		}
	}
	if err = atomicNative(b.path, file.after, 0600); err != nil {
		var restore error
		if file.existed {
			restore = atomicNative(b.path, file.before, file.mode)
		} else {
			restore = os.Remove(b.path)
			if os.IsNotExist(restore) {
				restore = nil
			}
		}
		if restore != nil {
			return rollback(fmt.Errorf("save failed: %w; saved-file rollback failed: %v", err, restore))
		}
		return rollback(err)
	}
	return nil
}
func readSavedLLDP(path string) (*LLDPTiming, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var saved LLDPTiming
	if err = json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	if err = saved.validate(); err != nil {
		return nil, err
	}
	return &saved, nil
}
func (a *API) getLLDP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	b := lldpBackend{path: lldpSettingsFile, run: runVendorTool}
	timing, err := b.read(ctx)
	if err != nil {
		fail(w, 503, "LLDP service is not ready. Refresh and try again.")
		return
	}
	result := LLDPState{LLDPTiming: timing, Neighbors: []LLDPNeighbor{}}
	result.Saved, err = readSavedLLDP(b.path)
	if err != nil {
		result.NeighborError = "Saved LLDP settings could not be read"
	}
	out, err := b.run(ctx, "/sbin/lldpcli", "-f", "json", "show", "neighbors", "details")
	if err == nil {
		result.Neighbors, err = parseLLDPNeighbors(out)
	}
	if err != nil {
		result.NeighborError = "Could not read LLDP neighbors"
	}
	reply(w, 200, result)
}
func (a *API) updateLLDP(w http.ResponseWriter, r *http.Request) {
	var input LLDPTiming
	if !decode(w, r, &input) {
		return
	}
	if err := input.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	b := lldpBackend{path: lldpSettingsFile, run: runVendorTool}
	if err := b.save(ctx, input); err != nil {
		fail(w, 500, "Could not save LLDP timing: "+err.Error())
		return
	}
	reply(w, 200, map[string]bool{"ok": true})
}

// LLDP can restart independently of the UI. Reconcile only explicitly saved
// timing, leaving the vendor's power-negotiation and port settings untouched.
func (a *API) maintainLLDP(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastError := ""
	for {
		a.writeMu.Lock()
		saved, err := readSavedLLDP(lldpSettingsFile)
		if err == nil && saved != nil {
			checkCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			b := lldpBackend{path: lldpSettingsFile, run: runVendorTool}
			current, e := b.read(checkCtx)
			if e == nil && current != *saved {
				e = b.apply(checkCtx, *saved)
			}
			err = e
			cancel()
		}
		a.writeMu.Unlock()
		message := ""
		if err != nil {
			message = err.Error()
		}
		if message != "" && message != lastError {
			log.Printf("LLDP timing restore: %s", message)
		}
		lastError = message
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
