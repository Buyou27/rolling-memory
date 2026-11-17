// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package text

import "testing"

// harnessEnvVars are every environment variable DetectHarnessFromEnv reads.
var harnessEnvVars = []string{
	"ROLLING_MEMORY_HARNESS",
	"DSH_SESSION_ID", "DSH_SHELL",
	"ZCODE_SESSION_ID", "ZCODE_WORKSPACE", "TERM_PROGRAM",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDECODE",
	"CURSOR_TRACE_ID", "WINDSURF_SESSION",
	"CODEX_THREAD_ID", "CODEX_HOME",
}

func clearHarnessEnv(t *testing.T) {
	t.Helper()
	for _, name := range harnessEnvVars {
		t.Setenv(name, "")
	}
}

func TestDetectHarnessFromEnvFingerprints(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"override", map[string]string{"ROLLING_MEMORY_HARNESS": "custom"}, "custom"},
		{"dsh session", map[string]string{"DSH_SESSION_ID": "s1"}, "dsh"},
		{"dsh shell", map[string]string{"DSH_SHELL": "/bin/zsh"}, "dsh"},
		{"zcode session", map[string]string{"ZCODE_SESSION_ID": "z1"}, "zcode"},
		{"zcode term", map[string]string{"TERM_PROGRAM": "ZCode"}, "zcode"},
		{"claude env", map[string]string{"CLAUDECODE": "1"}, "claude-code"},
		{"claude entrypoint", map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"}, "claude-code"},
		{"cursor", map[string]string{"CURSOR_TRACE_ID": "c1"}, "cursor"},
		{"windsurf", map[string]string{"WINDSURF_SESSION": "w1"}, "windsurf"},
		{"codex thread", map[string]string{"CODEX_THREAD_ID": "t1"}, "codex"},
		{"codex home", map[string]string{"CODEX_HOME": "/tmp/codex"}, "codex"},
		{"none", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearHarnessEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := DetectHarnessFromEnv(); got != tc.want {
				t.Fatalf("DetectHarnessFromEnv = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDetectHarnessFromEnvOrdering pins the marker-strength order: session
// markers stamped by the hosting process outrank plain exports a nested
// session inherits (a DSH session started inside a ZCode terminal), and every
// host-specific marker outranks a plain CODEX_HOME shell export.
func TestDetectHarnessFromEnvOrdering(t *testing.T) {
	clearHarnessEnv(t)
	t.Setenv("DSH_SESSION_ID", "s1")
	t.Setenv("ZCODE_SESSION_ID", "z1")
	t.Setenv("TERM_PROGRAM", "zcode")
	if got := DetectHarnessFromEnv(); got != "dsh" {
		t.Fatalf("nested DSH inside ZCode = %q, want dsh", got)
	}

	clearHarnessEnv(t)
	t.Setenv("CODEX_HOME", "/tmp/codex")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	if got := DetectHarnessFromEnv(); got != "claude-code" {
		t.Fatalf("claude with a CODEX_HOME export = %q, want claude-code", got)
	}
}

// TestDetectHarnessPresenceUsesEnvFingerprints locks the carrier a presence
// stamp receives when the caller carries no explicit harness name.
func TestDetectHarnessPresenceUsesEnvFingerprints(t *testing.T) {
	clearHarnessEnv(t)
	t.Setenv("CODEX_THREAD_ID", "t1")
	coords := DetectHarnessPresence("", "", "")
	if coords.Carrier != "harness:codex" {
		t.Fatalf("carrier = %q, want harness:codex", coords.Carrier)
	}
	clearHarnessEnv(t)
	coords = DetectHarnessPresence("", "", "")
	if coords.Carrier != "harness:cli" {
		t.Fatalf("unknown host carrier = %q, want harness:cli", coords.Carrier)
	}
}
