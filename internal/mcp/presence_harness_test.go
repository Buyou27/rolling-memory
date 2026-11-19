// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// presenceEnvVars are every environment variable the identity registry reads.
var presenceEnvVars = []string{
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

func clearPresenceEnv(t *testing.T) {
	t.Helper()
	for _, name := range presenceEnvVars {
		t.Setenv(name, "")
	}
	// The test binary itself is a child of the developer's host. Ancestry is
	// correct in production but would make the "unknown" cases impossible.
	t.Setenv("ROLLING_MEMORY_DISABLE_PROCESS_ANCESTRY", "1")
	t.Setenv("ROLLING_MEMORY_WORKSPACE", "")
}

func writePresenceRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "my-repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	return repo
}

// TestResolveMCPPresenceClientUsesLiveIdentity pins the client-side authority:
// the sidecar's session marker and cwd produce the carrier, incarnation, and
// locus for the call.
func TestResolveMCPPresenceClientUsesLiveIdentity(t *testing.T) {
	clearPresenceEnv(t)
	t.Setenv("OPENCODE_SESSION_ID", "ses_1")
	t.Chdir(writePresenceRepo(t))

	coords := resolveMCPPresence(nil, map[string]any{}, true)
	if coords == nil {
		t.Fatal("expected presence coordinates")
	}
	if coords.Carrier != "harness:opencode" || coords.Incarnation != "coding/opencode/main" {
		t.Fatalf("carrier/incarnation = %q/%q", coords.Carrier, coords.Incarnation)
	}
	if coords.Locus != "my-repo" {
		t.Fatalf("locus = %q, want my-repo", coords.Locus)
	}
}

// TestResolveMCPPresenceNeverBorrowsSavedCarrier locks the historical bug: a
// stale presence file naming ZCode must never turn an unknown client into
// ZCode. The ledger is display state, not identity.
func TestResolveMCPPresenceNeverBorrowsSavedCarrier(t *testing.T) {
	clearPresenceEnv(t)
	t.Chdir(writePresenceRepo(t))
	stateDir := t.TempDir()
	saved := `{"carrier":"harness:zcode","locus":"TT","updated_at":1}`
	if err := os.WriteFile(filepath.Join(stateDir, "active_workspace.json"), []byte(saved), 0o600); err != nil {
		t.Fatalf("write saved presence: %v", err)
	}

	coords := resolveMCPPresence(nil, map[string]any{}, true)
	if coords == nil || coords.Carrier != "" || coords.Incarnation != "" {
		t.Fatalf("unknown client borrowed an identity: %+v", coords)
	}
	if coords.Locus != "my-repo" {
		t.Fatalf("locus = %q, want my-repo", coords.Locus)
	}
}

// TestResolveMCPPresenceDaemonIgnoresItsOwnEnvironment pins the shared-daemon
// boundary: the daemon's environment and cwd are not the caller's, so without
// forwarded coordinates it must report no identity.
func TestResolveMCPPresenceDaemonIgnoresItsOwnEnvironment(t *testing.T) {
	clearPresenceEnv(t)
	t.Setenv("CODEX_THREAD_ID", "t1")
	t.Chdir(writePresenceRepo(t))

	if coords := resolveMCPPresence(nil, map[string]any{}, false); coords != nil {
		t.Fatalf("daemon invented presence from its own env: %+v", coords)
	}
	coords := resolveMCPPresence(nil, map[string]any{
		"carrier": "harness:opencode",
		"locus":   "Rolling-Memory",
	}, false)
	if coords == nil || coords.Carrier != "harness:opencode" || coords.Locus != "Rolling-Memory" {
		t.Fatalf("explicit forwarded coordinates were not honored: %+v", coords)
	}
}

func TestResolveMCPPresenceExplicitCwdSetsLocusOnly(t *testing.T) {
	clearPresenceEnv(t)
	repo := writePresenceRepo(t)
	t.Chdir(t.TempDir())

	coords := resolveMCPPresence(nil, map[string]any{"cwd": repo}, true)
	if coords == nil || coords.Locus != "my-repo" {
		t.Fatalf("explicit cwd locus = %+v", coords)
	}
	if coords.Carrier != "" {
		t.Fatalf("explicit cwd must not invent a carrier: %+v", coords)
	}
}
