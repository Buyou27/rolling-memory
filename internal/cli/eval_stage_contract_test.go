// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvalStandaloneStagesFailClosed locks the CLI contract for the reserved
// pipeline stages: they used to silently run the complete evaluation (LLM
// billing, scope purge) even though their help promised a single stage; they
// must now refuse with a pointer to the full-pipeline entry.
func TestEvalStandaloneStagesFailClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
	for _, stage := range []string{"import", "ingest", "run", "grade"} {
		var stdout, stderr bytes.Buffer
		code := Main([]string{"eval", stage}, strings.NewReader(""), &stdout, &stderr)
		if code == 0 {
			t.Fatalf("eval %s exited 0:\n%s\n%s", stage, stdout.String(), stderr.String())
		}
		combined := stdout.String() + stderr.String()
		if !strings.Contains(combined, "not a standalone stage yet") {
			t.Fatalf("eval %s: refusal message missing:\n%s", stage, combined)
		}
	}

	// Positive control: the read-only ledger path still works and is not
	// captured by the stage refusal.
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"eval", "jobs"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("eval jobs exited %d:\n%s\n%s", code, stdout.String(), stderr.String())
	}
}
