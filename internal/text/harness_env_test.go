// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package text

import "testing"

// harnessEnvVars are every environment variable the identity registry reads.
// AI_AGENT is included because the test process itself may be a child of a
// host (the developer's terminal), and an uncleared marker would win every
// case.
var harnessEnvVars = []string{
	"ROLLING_MEMORY_HARNESS", "AI_AGENT",
	"DSH_SESSION_ID", "DSH_SHELL",
	"ZCODE_SESSION_ID", "ZCODE_WORKSPACE", "ZCODE_PROJECT_DIR", "TERM_PROGRAM",
	"CLAUDE_SESSION_ID", "CLAUDE_CODE_ENTRYPOINT", "CLAUDECODE", "CLAUDE_PROJECT_DIR",
	"OPENCODE_SESSION_ID", "OPENCODE", "OPENCODE_CLIENT", "OPENCODE_TERMINAL",
	"CURSOR_TRACE_ID", "WINDSURF_SESSION",
	"CODEX_THREAD_ID", "CODEX_HOME",
	"CODEBUDDY_SESSION_ID", "CODEBUDDY_HOME",
	"WORKBUDDY_SESSION_ID", "WORKBUDDY_HOME",
	"DOUBAO_SESSION_ID", "DOUBAO_HOME",
	"CLINE_SESSION_ID", "CLINE_HOME",
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
		{"claude env", map[string]string{"CLAUDECODE": "1"}, "claude-code"},
		{"claude entrypoint", map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"}, "claude-code"},
		{"opencode session", map[string]string{"OPENCODE_SESSION_ID": "o1"}, "opencode"},
		{"opencode agent", map[string]string{"AI_AGENT": "opencode"}, "opencode"},
		{"cursor", map[string]string{"CURSOR_TRACE_ID": "c1"}, "cursor"},
		{"windsurf", map[string]string{"WINDSURF_SESSION": "w1"}, "windsurf"},
		{"codex thread", map[string]string{"CODEX_THREAD_ID": "t1"}, "codex"},
		// Plain shell exports describe a workspace, not an identity. They
		// are inherited by every descendant and must not name the host.
		{"zcode project export", map[string]string{"ZCODE_PROJECT_DIR": "/tmp/demo"}, ""},
		{"zcode terminal", map[string]string{"TERM_PROGRAM": "ZCode"}, ""},
		{"claude project export", map[string]string{"CLAUDE_PROJECT_DIR": "/tmp/demo"}, ""},
		{"codex home export", map[string]string{"CODEX_HOME": "/tmp/codex"}, ""},
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
// markers stamped by the hosting process outrank weak shell exports, and a
// nested DSH session outranks the ZCode marker it inherited from its
// launching terminal.
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
	if coords.Carrier != "" {
		t.Fatalf("unknown host carrier = %q, want empty (no borrowed identity)", coords.Carrier)
	}
	if coords.Incarnation != "" {
		t.Fatalf("unknown host incarnation = %q, want empty", coords.Incarnation)
	}
}

func TestDetectHarnessPresenceRecognizesOpenCode(t *testing.T) {
	clearHarnessEnv(t)
	t.Setenv("OPENCODE_SESSION_ID", "ses_1")
	coords := DetectHarnessPresence("", "", "")
	if coords.Carrier != "harness:opencode" {
		t.Fatalf("carrier = %q, want harness:opencode", coords.Carrier)
	}
	if coords.Incarnation != "coding/opencode/main" {
		t.Fatalf("incarnation = %q, want coding/opencode/main", coords.Incarnation)
	}
}
