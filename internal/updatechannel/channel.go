// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package updatechannel defines the closed set of release channels used by the
// CLI, the web console, and the optional scheduled check.
package updatechannel

import (
	"fmt"
	"strings"
)

const (
	Stable = "stable"
	Beta   = "beta"
	Dev    = "dev"

	// Default is the channel used when no explicit preference is saved. The
	// project is still in its early stage, so new installs follow main until
	// the release cadence stabilizes; users can switch to stable/beta at any
	// time.
	Default = Dev
)

var valid = []string{Stable, Beta, Dev}

// Normalize returns the canonical channel name.
func Normalize(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return Default, nil
	}
	for _, candidate := range valid {
		if value == candidate {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("update channel must be one of stable, beta, dev; got %q", raw)
}
