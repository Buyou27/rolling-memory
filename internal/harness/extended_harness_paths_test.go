// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExtendedHarnessCurrentPaths pins the current host locations: Windsurf
// (Devin Desktop) writes under ~/.config/devin with the legacy
// ~/.codeium/windsurf config kept as a carrier, and Cline's settings moved to
// ~/.cline/data/settings plus per-file project rules.
func TestExtendedHarnessCurrentPaths(t *testing.T) {
	home := t.TempDir()
	index := harnessIndex(BuildExtendedHarnesses(home))

	windsurf := index["windsurf"]
	if got, want := windsurf.MCPPath, filepath.Join(home, ".config", "devin", "mcp_config.json"); got != want {
		t.Fatalf("windsurf mcp path = %q, want %q", got, want)
	}
	if got, want := windsurf.MCPLegacyPath, filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"); got != want {
		t.Fatalf("windsurf legacy path = %q, want %q", got, want)
	}
	if windsurf.SkillUser == nil || *windsurf.SkillUser != filepath.Join(home, ".config", "devin", "skills") {
		t.Fatalf("windsurf skill root = %v", windsurf.SkillUser)
	}
	if got, want := windsurf.Detect, filepath.Join(home, ".codeium", "windsurf"); got != want {
		t.Fatalf("windsurf detect without a devin dir = %q, want %q", got, want)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "devin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := harnessIndex(BuildExtendedHarnesses(home))["windsurf"].Detect, filepath.Join(home, ".config", "devin"); got != want {
		t.Fatalf("windsurf detect with a devin dir = %q, want %q", got, want)
	}

	cline := index["cline"]
	if got, want := cline.MCPPath, filepath.Join(home, ".cline", "data", "settings", "cline_mcp_settings.json"); got != want {
		t.Fatalf("cline mcp path = %q, want %q", got, want)
	}
	if got, want := cline.MCPLegacyPath, filepath.Join(home, ".cline", "mcp_settings.json"); got != want {
		t.Fatalf("cline legacy path = %q, want %q", got, want)
	}
	if cline.InstructionsProject != ".clinerules/rolling-memory.md" {
		t.Fatalf("cline project rules = %q", cline.InstructionsProject)
	}
}
