// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"path/filepath"
	"testing"
)

func TestOpenCodeMCPInstallCheckAndUninstall(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	configHome := filepath.Join(home, "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	if _, err := writeText(filepath.Join(configHome, "opencode", "opencode.json"), `{
  "$schema": "https://opencode.ai/config.json",
  "model": "provider/model",
  "mcp": {
    "servers": {
      "keep": {"type": "local", "command": ["other"]}
    }
  }
}`); err != nil {
		t.Fatal(err)
	}

	openCode := harnessIndex(BuildExtendedHarnesses(home))["opencode"]
	options := DefaultInstallOptions(home, project)
	options.Name = "rolling-memory"
	options.SkillName = options.Name
	options.Binary = "/bin/sh"
	options.DB = filepath.Join(home, "memory.db")
	options.Scope = "user-test"
	options.MCPOnly = true
	if jsonEntryIsManaged(map[string]any{
		"command": []any{options.Binary, "mcp"},
	}, options) {
		t.Fatal("OpenCode command array without local transport type must not be treated as managed")
	}

	installed := Install([]Harness{openCode}, options)
	if got := firstAction(t, installed, "opencode", "mcp").Status; got != StatusInstalled {
		t.Fatalf("install status = %q, report = %#v", got, installed.Actions)
	}
	entry := objectAt(t, readJSONObject(t, openCode.MCPPath), "mcp", "servers", options.Name)
	if entry["type"] != "local" {
		t.Fatalf("transport = %#v", entry["type"])
	}
	command, ok := entry["command"].([]any)
	if !ok || len(command) != 2 || command[0] != options.Binary || command[1] != "mcp" {
		t.Fatalf("command = %#v", entry["command"])
	}
	env := objectAt(t, entry, "environment")
	if env["ROLLING_MEMORY_DB"] != options.DB || env["ROLLING_MEMORY_SCOPE"] != options.Scope {
		t.Fatalf("environment = %#v", env)
	}
	if objectAt(t, readJSONObject(t, openCode.MCPPath), "mcp", "servers")["keep"] == nil {
		t.Fatal("unrelated MCP entry was not preserved")
	}
	data := readJSONObject(t, openCode.MCPPath)
	if data["model"] != "provider/model" || data["$schema"] != "https://opencode.ai/config.json" {
		t.Fatalf("unrelated config was not preserved: %#v", data)
	}

	checked := Check([]Harness{openCode}, options, false)
	if got := firstAction(t, checked, "opencode", "mcp").Status; got != StatusOK {
		t.Fatalf("check status = %q, report = %#v", got, checked.Actions)
	}

	removed := Uninstall([]Harness{openCode}, options)
	if got := firstAction(t, removed, "opencode", "mcp").Status; got != StatusRemoved {
		t.Fatalf("uninstall status = %q, report = %#v", got, removed.Actions)
	}
	servers := objectAt(t, readJSONObject(t, openCode.MCPPath), "mcp", "servers")
	if servers[options.Name] != nil || servers["keep"] == nil {
		t.Fatalf("servers after uninstall = %#v", servers)
	}
}

func TestOpenCodeProjectMCPInstall(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	openCode := harnessIndex(BuildExtendedHarnesses(home))["opencode"]
	options := DefaultInstallOptions(home, project)
	options.Name = "rolling-memory"
	options.SkillName = options.Name
	options.Binary = "/bin/sh"
	options.Project = true
	options.MCPOnly = true

	report := Install([]Harness{openCode}, options)
	if got := firstAction(t, report, "opencode", "mcp").Status; got != StatusInstalled {
		t.Fatalf("project install status = %q, report = %#v", got, report.Actions)
	}
	want := filepath.Join(project, ".opencode", "opencode.json")
	if got := firstAction(t, report, "opencode", "mcp").Path; got != want {
		t.Fatalf("project config path = %q, want %q", got, want)
	}
	if err := validateMCPRegistration(openCode, report.Options, want); err != nil {
		t.Fatalf("project registration invalid: %v", err)
	}
}
