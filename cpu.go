package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

type cpuSnapshot struct {
	ticks [8]uint64
	cores int
}

// Guest time is already included in user/nice; only the first eight counters
// contribute to the total. I/O wait is not CPU execution time.
func parseCPUStat(raw string) (cpuSnapshot, error) {
	var sample cpuSnapshot
	found := false
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "cpu" {
			if found || len(fields) < 5 {
				return sample, fmt.Errorf("invalid aggregate CPU counters")
			}
			found = true
			for i := 0; i < len(sample.ticks) && i+1 < len(fields); i++ {
				value, err := strconv.ParseUint(fields[i+1], 10, 64)
				if err != nil {
					return sample, err
				}
				sample.ticks[i] = value
			}
		} else if strings.HasPrefix(fields[0], "cpu") {
			if n, err := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu")); err == nil && n >= 0 {
				sample.cores++
			}
		}
	}
	if !found || sample.cores == 0 {
		return sample, fmt.Errorf("missing CPU counters or cores")
	}
	return sample, nil
}

type cpuSampler struct {
	mu       sync.Mutex
	previous *cpuSnapshot
}

func (s *cpuSampler) Sample() (*float64, int) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return s.update("", err)
	}
	return s.update(string(raw), nil)
}

// The first valid sample establishes a baseline instead of displaying usage
// averaged since boot. Later samples cover the actual interval between polls.
func (s *cpuSampler) update(raw string, readErr error) (*float64, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := parseCPUStat(raw)
	if readErr != nil || err != nil {
		s.previous = nil
		return nil, 0
	}
	previous := s.previous
	s.previous = &current
	if previous == nil || previous.cores != current.cores {
		return nil, current.cores
	}
	var total, inactive uint64
	for i, value := range current.ticks {
		if value < previous.ticks[i] {
			return nil, current.cores
		}
		delta := value - previous.ticks[i]
		total += delta
		if i == 3 || i == 4 {
			inactive += delta
		}
	}
	if total == 0 {
		return nil, current.cores
	}
	usage := 100 * float64(total-inactive) / float64(total)
	return &usage, current.cores
}
