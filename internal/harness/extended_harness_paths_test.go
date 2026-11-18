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
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	index := harnessIndex(BuildExtendedHarnesses(home))

	opencode := index["opencode"]
	if got, want := opencode.MCPPath, filepath.Join(home, "xdg", "opencode", "opencode.json"); got != want {
		t.Fatalf("OpenCode mcp path = %q, want %q", got, want)
	}
	if got, want := opencode.SkillUser, filepath.Join(home, "xdg", "opencode", "skills"); got == nil || *got != want {
		t.Fatalf("OpenCode skill root = %v, want %q", opencode.SkillUser, want)
	}
	if got, want := opencode.MCPProjectPath, filepath.Join(".opencode", "opencode.json"); got == nil || *got != want {
		t.Fatalf("OpenCode project config = %v, want %q", opencode.MCPProjectPath, want)
	}
	if opencode.MCPKind != "json" || opencode.HooksKind != "" {
		t.Fatalf("OpenCode adapter = %#v", opencode)
	}
	if err := os.MkdirAll(filepath.Join(home, "xdg", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "xdg", "opencode", "opencode.jsonc"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := harnessIndex(BuildExtendedHarnesses(home))["opencode"].MCPPath, filepath.Join(home, "xdg", "opencode", "opencode.jsonc"); got != want {
		t.Fatalf("OpenCode existing config path = %q, want %q", got, want)
	}

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
