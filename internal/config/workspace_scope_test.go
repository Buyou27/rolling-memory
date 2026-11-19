// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package config

import "testing"

// TestWorkspaceScopeNameDerivation pins the per-workspace scope derivation:
// the ROLLING_MEMORY_LOCUS override wins, otherwise the cwd basename; unusable
// names fall back to "" so callers keep their existing scope.
func TestWorkspaceScopeNameDerivation(t *testing.T) {
	cases := []struct {
		override, wd, want string
	}{
		{"Rolling-Memory", "/srv/workspaces/other-workspace", "Rolling-Memory"},
		{"", "/srv/workspaces/Rolling-Memory", "Rolling-Memory"},
		{"", "/srv/workspaces/Rolling-Memory/", "Rolling-Memory"},
		{"", "/", ""},
		{"", "", ""},
		{"", ".", ""},
		{"a:b", "/tmp/x", "a-b"},
		{"bad name", "/tmp/x", ""},
	}
	for _, tc := range cases {
		if got := WorkspaceScopeName(tc.override, tc.wd); got != tc.want {
			t.Fatalf("WorkspaceScopeName(%q, %q) = %q, want %q", tc.override, tc.wd, got, tc.want)
		}
	}
}

// TestLoadScopeFromWorkspaceSwitch pins [cloud.sync] scope_from_workspace:
// default scopes derive from the workspace, explicit scopes win unchanged.
