package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func cpuFixture(counters string, cores int) string {
	raw := "cpu " + counters + "\n"
	for i := 0; i < cores; i++ {
		raw += fmt.Sprintf("cpu%d 0 0 0 0 0 0 0 0\n", i)
	}
	return raw
}

func TestCPUUsageBetweenSamples(t *testing.T) {
	var sampler cpuSampler
	if usage, cores := sampler.update(cpuFixture("100 0 0 300 100 0 0 0 900 0", 4), nil); usage != nil || cores != 4 {
		t.Fatalf("first sample must establish a baseline: %v, %d", usage, cores)
	}
	// 100 execution ticks out of 500 across all four cores. I/O wait is
	// inactive, and the changing guest counter must not be counted twice.
	usage, cores := sampler.update(cpuFixture("200 0 0 600 200 0 0 0 1900 0", 4), nil)
	if usage == nil || *usage != 20 || cores != 4 {
		t.Fatalf("want 20%% across four cores, got %v, %d", usage, cores)
	}
	usage, _ = sampler.update(cpuFixture("200 0 0 1000 200 0 0 0 1900 0", 4), nil)
	if usage == nil || *usage != 0 {
		t.Fatalf("fully idle interval must read zero, got %v", usage)
	}
	usage, _ = sampler.update(cpuFixture("600 0 0 1000 200 0 0 0 1900 0", 4), nil)
	if usage == nil || *usage != 100 {
		t.Fatalf("fully busy interval must read 100, got %v", usage)
	}
}

func TestCPUSamplerUnavailableAndReset(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		err  error
	}{
		{"read failure", "", errors.New("unavailable")},
		{"malformed", cpuFixture("invalid 0 0 0", 4), nil},
		{"missing counters", "intr 100\n", nil},
		{"counter reset", cpuFixture("1 0 0 1", 4), nil},
		{"core count changed", cpuFixture("200 0 0 200", 2), nil},
		{"no elapsed ticks", cpuFixture("100 0 0 100", 4), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sampler cpuSampler
			sampler.update(cpuFixture("100 0 0 100", 4), nil)
			if usage, _ := sampler.update(tc.raw, tc.err); usage != nil {
				t.Fatalf("must not fabricate a reading: %v", *usage)
			}
			if tc.err != nil || tc.name == "malformed" || tc.name == "missing counters" {
				if usage, _ := sampler.update(cpuFixture("200 0 0 200", 4), nil); usage != nil {
					t.Fatal("recovery must establish a fresh baseline")
				}
			}
		})
	}
}

func TestParseCPUStatRejectsIncompleteData(t *testing.T) {
	for _, raw := range []string{"", "cpu 1 2 3\ncpu0 0\n", "cpu 1 2 3 4\n", cpuFixture("1 2 3 4", 1) + "cpu 1 2 3 4\n", strings.Replace(cpuFixture("1 2 3 4", 1), "cpu 1", "cpu -1", 1)} {
		if _, err := parseCPUStat(raw); err == nil {
			t.Errorf("accepted invalid CPU counters: %q", raw)
		}
	}
}
