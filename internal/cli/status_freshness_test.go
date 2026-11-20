// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"strings"
	"testing"
)

// TestBackgroundFreshnessLine verifies the status line reports counts and ages, including
// the failure case, without inventing a verdict.
func TestBackgroundFreshnessLine(t *testing.T) {
	healthy := map[string]any{
		"mounts":      []map[string]any{{"root": "/work", "status": "ok"}},
		"age_seconds": int64(125),
	}
	if got := backgroundFreshnessLine(healthy); got != "1 mounted · last sync 2m ago" {
		t.Fatalf("healthy line = %q", got)
	}

	failing := map[string]any{
		"mounts":       []map[string]any{{"root": "/work", "status": "error"}},
		"age_seconds":  int64(7200),
		"error_mounts": 1,
	}
	got := backgroundFreshnessLine(failing)
	if !strings.Contains(got, "1 mounted") || !strings.Contains(got, "2h ago") || !strings.Contains(got, "1 failing") {
		t.Fatalf("failing line = %q", got)
	}

	never := map[string]any{
		"mounts":              []map[string]any{{"root": "/new", "status": "never_synced"}},
		"never_synced_mounts": 1,
	}
	if got := backgroundFreshnessLine(never); !strings.Contains(got, "never synced") {
		t.Fatalf("never-synced line = %q", got)
	}
}

func TestFormatFreshnessAgeUnits(t *testing.T) {
	cases := map[int64]string{
		5:         "5s ago",
		125:       "2m ago",
		7200:      "2h ago",
		3 * 86400: "3d ago",
	}
	for seconds, want := range cases {
		if got := formatFreshnessAge(seconds); got != want {
			t.Fatalf("formatFreshnessAge(%d) = %q, want %q", seconds, got, want)
		}
	}
}
