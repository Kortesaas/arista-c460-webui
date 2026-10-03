package main

// SSID schedules: a network broadcasts only during its time windows. The
// scheduler acts on transitions, so switching a network on or off by hand
// lasts until the next scheduled change.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the AP has no zoneinfo database
)

type ScheduleWindow struct {
	Days  []int  `json:"days"`  // 0 = Sunday … 6 = Saturday
	Start string `json:"start"` // "HH:MM"
	End   string `json:"end"`   // "HH:MM"; at or before Start means it ends the next day
}

type SSIDSchedule struct {
	Enabled bool             `json:"enabled"`
	Windows []ScheduleWindow `json:"windows"`
}

type ScheduleStatus struct {
	SSIDSchedule
	Active  bool       `json:"active"`            // the network should be on now
	Next    *time.Time `json:"next,omitempty"`    // next scheduled change
	Waiting string     `json:"waiting,omitempty"` // why the schedule is not acting
}

var clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

func clockMinutes(s string) (int, bool) {
	m := clockPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	return int(m[1][0]-'0')*600 + int(m[1][1]-'0')*60 + int(m[2][0]-'0')*10 + int(m[2][1]-'0'), true
}

func (s SSIDSchedule) validate() error {
	if len(s.Windows) > 14 {
		return errors.New("at most 14 time windows")
	}
	if s.Enabled && len(s.Windows) == 0 {
		return errors.New("add at least one time window")
	}
	for i, w := range s.Windows {
		_, ok1 := clockMinutes(w.Start)
		_, ok2 := clockMinutes(w.End)
		if !ok1 || !ok2 {
			return fmt.Errorf("window %d: times must be HH:MM", i+1)
		}
		if len(w.Days) == 0 || len(w.Days) > 7 {
			return fmt.Errorf("window %d: select at least one day", i+1)
		}
		for _, d := range w.Days {
			if d < 0 || d > 6 {
				return fmt.Errorf("window %d: invalid day", i+1)
			}
		}
	}
	return nil
}

// activeAt reports whether t (in the schedule's zone) falls into a window.
func (s SSIDSchedule) activeAt(t time.Time) bool {
	minute := t.Hour()*60 + t.Minute()
	today := int(t.Weekday())
	yesterday := (today + 6) % 7
	for _, w := range s.Windows {
		start, _ := clockMinutes(w.Start)
		end, _ := clockMinutes(w.End)
		if end > start {
			if slices.Contains(w.Days, today) && minute >= start && minute < end {
				return true
			}
			continue
		}
		// Runs past midnight: the evening part today, the morning part from yesterday.
		if slices.Contains(w.Days, today) && minute >= start {
			return true
		}
		if slices.Contains(w.Days, yesterday) && minute < end {
			return true
		}
	}
	return false
}

// nextChange finds the next minute at which activeAt flips, within 8 days.
func (s SSIDSchedule) nextChange(now time.Time) *time.Time {
	t := now.Truncate(time.Minute)
	state := s.activeAt(t)
	for i := 0; i < 8*24*60; i++ {
		t = t.Add(time.Minute)
		if s.activeAt(t) != state {
			return &t
		}
	}
	return nil
}

// ------------------------------------------------------------- config

func (c *Config) Zone() *time.Location {
	c.mu.RLock()
	name := c.TimeZone
	c.mu.RUnlock()
	if name == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

func (c *Config) SetTimeZone(name string) error {
	if name != "" {
		if _, err := time.LoadLocation(name); err != nil || strings.HasPrefix(name, "/") {
			return fmt.Errorf("unknown time zone %q", name)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.TimeZone
	c.TimeZone = name
	if err := c.saveLocked(); err != nil {
		c.TimeZone = previous
		return err
	}
	return nil
}

func (c *Config) ScheduleFor(ssid string) (SSIDSchedule, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.Schedules[ssid]
	return s, ok
}

func (c *Config) AllSchedules() map[string]SSIDSchedule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]SSIDSchedule, len(c.Schedules))
	for k, v := range c.Schedules {
		out[k] = v
	}
	return out
}

// SetSchedule stores (or with newName "" removes) the schedule of oldName.
func (c *Config) SetSchedule(oldName, newName string, s *SSIDSchedule) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.Schedules
	next := make(map[string]SSIDSchedule, len(previous)+1)
	for k, v := range previous {
		next[k] = v
	}
	if s == nil {
		if v, ok := next[oldName]; ok {
			s = &v
		}
	}
	delete(next, oldName)
	if newName != "" && s != nil && (s.Enabled || len(s.Windows) > 0) {
		next[newName] = *s
	}
	c.Schedules = next
	if err := c.saveLocked(); err != nil {
		c.Schedules = previous
		return err
	}
	return nil
}

// ---------------------------------------------------------- scheduler

type Scheduler struct {
	mu     sync.Mutex
	last   map[string]bool // desired state at the previous evaluation
	status map[string]ScheduleStatus
}

func (a *API) renameSchedule(oldName, newName string) {
	if _, ok := a.cfg.ScheduleFor(oldName); !ok {
		return
	}
	if err := a.cfg.SetSchedule(oldName, newName, nil); err != nil {
		log.Printf("schedule: %v", err)
	}
	a.scheduler.mu.Lock()
	delete(a.scheduler.last, oldName)
	a.scheduler.mu.Unlock()
}

// ScheduleStatuses returns the latest evaluation for the UI.
func (a *API) scheduleStatuses() map[string]ScheduleStatus {
	a.scheduler.mu.Lock()
	defer a.scheduler.mu.Unlock()
	out := make(map[string]ScheduleStatus, len(a.scheduler.status))
	for k, v := range a.scheduler.status {
		out[k] = v
	}
	return out
}

func (a *API) evaluateSchedules(ctx context.Context) {
	schedules := a.cfg.AllSchedules()
	zone := a.cfg.Zone()
	now := time.Now().In(zone)
	_, hw, _ := a.cli.Snapshot()
	waiting := ""
	if hw.NTPSynced == nil || !*hw.NTPSynced || now.Year() < 2024 {
		waiting = "Waiting for the clock to synchronise (check the time servers)"
	}
	st := a.poller.Snapshot()
	enabled := map[string]bool{}
	for _, s := range st.SSIDs {
		enabled[s.Name] = s.Enabled
	}
	status := map[string]ScheduleStatus{}
	type flip struct {
		name string
		on   bool
	}
	var flips []flip
	a.scheduler.mu.Lock()
	if a.scheduler.last == nil {
		a.scheduler.last = map[string]bool{}
	}
	for name, sched := range schedules {
		s := ScheduleStatus{SSIDSchedule: sched, Waiting: waiting}
		if sched.Enabled {
			s.Active = sched.activeAt(now)
			s.Next = sched.nextChange(now)
			current, exists := enabled[name]
			previous, seen := a.scheduler.last[name]
			if waiting == "" && exists && st.Error == "" {
				// Act on transitions (and once after start or a schedule change).
				if (!seen || previous != s.Active) && current != s.Active {
					flips = append(flips, flip{name, s.Active})
				}
				a.scheduler.last[name] = s.Active
			}
		} else {
			delete(a.scheduler.last, name)
		}
		status[name] = s
	}
	a.scheduler.status = status
	a.scheduler.mu.Unlock()

	for _, f := range flips {
		a.setSSIDEnabled(ctx, f.name, f.on)
	}
}

func (a *API) setSSIDEnabled(ctx context.Context, name string, on bool) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	cfg, ok := a.poller.SSIDConfig(name)
	action := map[bool]string{true: "Schedule turned on network ", false: "Schedule turned off network "}[on] + quoted(name)
	entry := ChangeEntry{Time: time.Now(), User: "schedule", Address: "", Action: action, OK: true}
	if !ok {
		return
	}
	cfg["enabled"] = on
	if err := a.gnmi.SetAP(ctx, ssidBody(cfg), nil); err != nil {
		entry.OK, entry.Error = false, err.Error()
		log.Printf("schedule: %s: %v", name, err)
	} else {
		log.Printf("%s", action)
		a.poller.Refresh()
	}
	a.changes.Record(entry)
}

func (a *API) runScheduler(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		a.evaluateSchedules(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *API) updateSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var s SSIDSchedule
	if !decode(w, r, &s) {
		return
	}
	if err := s.validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := a.poller.SSIDConfig(name); !ok {
		fail(w, http.StatusNotFound, "network not found")
		return
	}
	if err := a.cfg.SetSchedule(name, name, &s); err != nil {
		fail(w, http.StatusInternalServerError, "Could not save the schedule")
		return
	}
	a.scheduler.mu.Lock()
	delete(a.scheduler.last, name) // apply the new schedule right away
	a.scheduler.mu.Unlock()
	a.evaluateSchedules(r.Context())
	reply(w, http.StatusOK, map[string]any{"ok": true, "schedule": a.scheduleStatuses()[name]})
}

func (a *API) updateTimeZone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TimeZone string `json:"timeZone"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := a.cfg.SetTimeZone(strings.TrimSpace(body.TimeZone)); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	a.evaluateSchedules(r.Context())
	reply(w, http.StatusOK, map[string]any{"ok": true, "timeZone": a.cfg.Zone().String()})
}
