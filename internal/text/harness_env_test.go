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
