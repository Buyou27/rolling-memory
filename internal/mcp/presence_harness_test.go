// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/text"
)

// presenceEnvVars are every environment variable text.DetectHarnessFromEnv reads.
var presenceEnvVars = []string{
	"ROLLING_MEMORY_HARNESS",
	"DSH_SESSION_ID", "DSH_SHELL",
	"ZCODE_SESSION_ID", "ZCODE_WORKSPACE", "TERM_PROGRAM",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDECODE",
	"CURSOR_TRACE_ID", "WINDSURF_SESSION",
	"CODEX_THREAD_ID", "CODEX_HOME",
}

func clearPresenceEnv(t *testing.T) {
	t.Helper()
	for _, name := range presenceEnvVars {
		t.Setenv(name, "")
	}
}

// TestResolvedHarnessHintPrecedence pins the daemon-side identity order: the
// live process environment outranks the ZCode in-flight tool directory, which
// outranks the shared saved-carrier file.
func TestResolvedHarnessHintPrecedence(t *testing.T) {
	clearPresenceEnv(t)
	saved := text.PresenceCoordinates{Carrier: "harness:zcode", Locus: "Rolling-Memory"}

	if got := resolvedHarnessHint(saved, true); got != "zcode" {
		t.Fatalf("in-flight zcode hint = %q, want zcode", got)
	}
	if got := resolvedHarnessHint(saved, false); got != "zcode" {
		t.Fatalf("saved zcode hint = %q, want zcode", got)
	}
	if got := resolvedHarnessHint(text.PresenceCoordinates{Carrier: "harness:cli"}, false); got != "" {
		t.Fatalf("generic cli carrier must not be inherited, got %q", got)
	}

	t.Setenv("DSH_SESSION_ID", "s1")
	if got := resolvedHarnessHint(saved, true); got != "dsh" {
		t.Fatalf("live env must outrank in-flight dir and saved carrier, got %q", got)
	}
	t.Setenv("DSH_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "t1")
	if got := resolvedHarnessHint(saved, false); got != "codex" {
		t.Fatalf("live codex env must outrank a saved zcode carrier, got %q", got)
	}
}

// TestApplyLiveHarnessIdentity pins the L2 fallback fix: a daemon running
// inside Codex must not keep the saved ZCode carrier, while a matching host
// keeps the saved incarnation (it may name a subagent role).
func TestApplyLiveHarnessIdentity(t *testing.T) {
	clearPresenceEnv(t)
	saved := text.PresenceCoordinates{
		Carrier:     "harness:zcode",
		Locus:       "Rolling-Memory",
		Incarnation: "coding/zcode/explore",
	}
	if got := applyLiveHarnessIdentity(saved); got.Carrier != "harness:zcode" || got.Incarnation != "coding/zcode/explore" {
		t.Fatalf("no fingerprint must keep saved identity, got %+v", got)
	}

	t.Setenv("CODEX_THREAD_ID", "t1")
	got := applyLiveHarnessIdentity(saved)
	if got.Carrier != "harness:codex" || got.Incarnation != "coding/codex/main" {
		t.Fatalf("mismatched carrier must be restamped, got %+v", got)
	}
	if got.Locus != "Rolling-Memory" {
		t.Fatalf("locus must survive the restamp, got %q", got.Locus)
	}

	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("ZCODE_SESSION_ID", "z1")
	if got := applyLiveHarnessIdentity(saved); got.Incarnation != "coding/zcode/explore" {
		t.Fatalf("matching host must keep the saved incarnation, got %+v", got)
	}
}

// TestResolveMCPPresenceLiveEnvOutranksSavedCarrier drives the whole presence
// resolution through the L2 saved-state fallback: a daemon running inside
// Codex while the last hook writer was ZCode must stamp harness:codex, and no
// live fingerprint must keep the saved carrier intact.
func TestResolveMCPPresenceLiveEnvOutranksSavedCarrier(t *testing.T) {
	clearPresenceEnv(t)
	t.Setenv("ROLLING_MEMORY_WORKSPACE", "")
	t.Setenv("HOME", t.TempDir()) // keep the ZCode in-flight probe away from real state
	t.Chdir(t.TempDir())          // outside any repository: exercise the L2 fallback

	memory := newMemory(t)
	stateDir := filepath.Dir(memory.Config.Log)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	saved := `{"world":"actual","carrier":"harness:zcode","locus":"Rolling-Memory",` +
		`"incarnation":"coding/zcode/main","updated_at":1}`
	if err := os.WriteFile(filepath.Join(stateDir, "active_workspace.json"), []byte(saved), 0o600); err != nil {
		t.Fatalf("write saved presence: %v", err)
	}

	coords := resolveMCPPresence(memory, map[string]any{}, true)
	if coords == nil || coords.Carrier != "harness:zcode" {
		t.Fatalf("no fingerprint must keep the saved carrier, got %+v", coords)
	}

	t.Setenv("CODEX_THREAD_ID", "t1")
	coords = resolveMCPPresence(memory, map[string]any{}, true)
	if coords == nil || coords.Carrier != "harness:codex" || coords.Incarnation != "coding/codex/main" {
		t.Fatalf("live codex env must restamp the saved zcode carrier, got %+v", coords)
	}
	if coords.Locus != "Rolling-Memory" {
		t.Fatalf("locus must survive the restamp, got %q", coords.Locus)
	}
}

// TestResolveMCPPresencePersistsLiveCoordinates pins the write-side migration:
// a live resolution (the daemon's own repository cwd) refreshes the shared
// active_workspace.json, while the L2 saved-state fallback never rewrites the
// document — echoing file-derived coordinates would fake activity recency.
func TestResolveMCPPresencePersistsLiveCoordinates(t *testing.T) {
	clearPresenceEnv(t)
	t.Setenv("ROLLING_MEMORY_WORKSPACE", "")
	t.Setenv("HOME", t.TempDir()) // keep the ZCode in-flight probe away from real state
	memory := newMemory(t)
	stateDir := filepath.Dir(memory.Config.Log)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	repo := filepath.Join(t.TempDir(), "my-repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	t.Chdir(repo)
	if coords := resolveMCPPresence(memory, map[string]any{}, true); coords == nil || coords.Locus != "my-repo" {
		t.Fatalf("live cwd must resolve the repository locus, got %+v", coords)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "active_workspace.json"))
	if err != nil {
		t.Fatalf("live resolution must persist presence: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode persisted presence: %v", err)
	}
	if doc["locus"] != "my-repo" || doc["updated_at"] == nil {
		t.Fatalf("persisted document mismatch: %v", doc)
	}

	// Outside any repository the L2 fallback reads the saved document and must
	// leave its timestamp untouched.
	t.Chdir(t.TempDir())
	saved := `{"world":"actual","carrier":"harness:zcode","locus":"my-repo","updated_at":1}`
	if err := os.WriteFile(filepath.Join(stateDir, "active_workspace.json"), []byte(saved), 0o600); err != nil {
		t.Fatalf("seed saved presence: %v", err)
	}
	if coords := resolveMCPPresence(memory, map[string]any{}, true); coords == nil || coords.Locus != "my-repo" {
		t.Fatalf("fallback must read the saved locus, got %+v", coords)
	}
	raw, err = os.ReadFile(filepath.Join(stateDir, "active_workspace.json"))
	if err != nil {
		t.Fatalf("re-read presence: %v", err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode fallback presence: %v", err)
	}
	if at, _ := doc["updated_at"].(float64); int64(at) != 1 {
		t.Fatalf("saved-state fallback must not rewrite updated_at, got %v", doc["updated_at"])
	}
}
