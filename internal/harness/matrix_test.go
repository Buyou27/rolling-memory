// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"testing"
)

func TestHarnessRegistryCoversElevenHosts(t *testing.T) {
	fx := newFixture(t)
	registry := append(fx.registry(), BuildExtendedHarnesses(fx.home)...)
	want := []string{
		"zcode", "claude-code", "codex", "dsh", "codebuddy",
		"opencode", "cursor", "windsurf", "cline", "workbuddy", "doubao",
	}
	if len(registry) != len(want) {
		t.Fatalf("registry has %d harnesses, want %d", len(registry), len(want))
	}
	index := harnessIndex(registry)
	for _, id := range want {
		if _, ok := index[id]; !ok {
			t.Fatalf("registry is missing %s", id)
		}
	}
}

func TestInstallMatrixCoversAllRegisteredHarnesses(t *testing.T) {
	fx := newFixture(t)
	registry := append(fx.registry(), BuildExtendedHarnesses(fx.home)...)
	options := fx.options()
	report := Install(registry, options)
	assertNoFailures(t, report)
	for _, harness := range registry {
		if harness.MCPKind == "none" {
			continue
		}
		found := false
		for _, action := range report.Actions {
			if action.Harness == harness.ID && action.Component == "mcp" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s has no MCP action: %#v", harness.ID, report.ToDict())
		}
	}
	check := Check(registry, options, false)
	assertNoFailures(t, check)
}
