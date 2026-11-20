// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// A vault whose units all carry the same occurred_at second is paired inside one
// spacetime bucket; doctor must surface that exposure instead of leaving it to be
// discovered as a snapshot that grows quadratically.
func TestDoctorFlagsCollapsedEventTime(t *testing.T) {
	memory := newVaultOnlyMemoryWithScope(t, "mem:collapsed-time")
	// pairs per unit is (N-1)/2 inside one bucket, so 220 units clear the 100x line.
	for i := 0; i < 220; i++ {
		content := fmt.Sprintf("Unit %d records the Glasgow hotel cost in GBP.", i)
		if _, err := memory.Remember(facade.RememberRequest{
			Content:    &content,
			Session:    "s1",
			OccurredAt: 1700000000,
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	report, err := memory.Doctor()
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	checks, ok := report["checks"].([]any)
	if !ok {
		t.Fatalf("doctor checks = %T, want []any", report["checks"])
	}
	found := false
	for _, entry := range checks {
		check, ok := entry.(map[string]any)
		if !ok || check["name"] != "event_time" {
			continue
		}
		found = true
		if check["ok"] != false {
			t.Fatalf("event_time check = %v, want a failed check", check["ok"])
		}
	}
	if !found {
		t.Fatal("doctor reported no event_time check")
	}

	warnings, _ := report["warnings"].([]any)
	flagged := false
	for _, warning := range warnings {
		if strings.Contains(fmt.Sprint(warning), "occurred_at") {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("doctor warnings = %v, want one naming occurred_at", warnings)
	}
}

// A vault with spread event times must pass the same check.
func TestDoctorAcceptsSpreadEventTime(t *testing.T) {
	memory := newVaultOnlyMemoryWithScope(t, "mem:spread-time")
	for i := 0; i < 40; i++ {
		content := fmt.Sprintf("Unit %d records the Glasgow hotel cost in GBP.", i)
		if _, err := memory.Remember(facade.RememberRequest{
			Content:    &content,
			Session:    "s1",
			OccurredAt: 1700000000 + int64(i)*3600,
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	report, err := memory.Doctor()
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	checks, _ := report["checks"].([]any)
	for _, entry := range checks {
		check, ok := entry.(map[string]any)
		if !ok || check["name"] != "event_time" {
			continue
		}
		if check["ok"] != true {
			t.Fatalf("event_time check = %v, want a passing check", check["ok"])
		}
		return
	}
	t.Fatal("doctor reported no event_time check")
}
