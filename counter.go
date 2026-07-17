package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// maxJobNumber is the inclusive upper bound (0xFFFFFF). The counter wraps back
// to 0x000000 once it passes this value.
const maxJobNumber = 0xFFFFFF

// formatJobNumber renders a job number as 6 lowercase hexadecimal digits.
func formatJobNumber(n uint32) string {
	return fmt.Sprintf("%06x", n&maxJobNumber)
}

// parseJobNumber parses a user-supplied 6-digit hex job number.
func parseJobNumber(s string) (uint32, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 16, 32)
	if err != nil || v > maxJobNumber {
		return 0, fmt.Errorf("invalid job number: %q", s)
	}
	return uint32(v), nil
}

// nextJobNumber reads jobcounter, increments (wrapping at maxJobNumber) and
// persists the new value, returning the number to assign to the new job.
func (w *Workspace) nextJobNumber() (uint32, error) {
	cur := uint32(0)
	if data, err := os.ReadFile(w.CounterFile()); err == nil {
		if v, perr := strconv.ParseUint(strings.TrimSpace(string(data)), 16, 32); perr == nil {
			cur = uint32(v)
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}

	next := cur + 1
	if next > maxJobNumber {
		next = 0
	}
	if err := os.WriteFile(w.CounterFile(), []byte(formatJobNumber(next)), 0o644); err != nil {
		return 0, err
	}
	return next, nil
}
