// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"strings"
	"testing"
)

// TestMaintenanceSpaceArgsMatchCLIContract 锁定控制台 → `maintain` 子命令的参数契约：
// 每个动作、预演/执行、保留份数与缓存开关都必须逐位生成 CLI 认识的参数。
func TestMaintenanceSpaceArgsMatchCLIContract(t *testing.T) {
	const db = "/tmp/vault.db"
	cases := []struct {
		name   string
		action string
		apply  bool
		keep   int
		caches bool
		want   string
	}{
		{"status", "status", false, 0, false, "maintain status --db /tmp/vault.db --json"},
		{"clean-preview", "clean", false, 0, false, "maintain clean --db /tmp/vault.db --json"},
		{"clean-preview-keep-caches", "clean", false, 3, true, "maintain clean --db /tmp/vault.db --json --keep 3 --caches"},
		{"clean-apply", "clean", true, 2, false, "maintain clean --db /tmp/vault.db --json --keep 2 --apply --yes"},
		{"compact-preview", "compact", false, 0, false, "maintain compact --db /tmp/vault.db --json"},
		{"compact-apply", "compact", true, 0, false, "maintain compact --db /tmp/vault.db --json --apply --yes"},
	}
	for _, tc := range cases {
		got := strings.Join(maintenanceSpaceArgs(tc.action, tc.apply, tc.keep, tc.caches, db), " ")
		if got != tc.want {
			t.Fatalf("%s: args = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeMaintenanceSpaceAction(t *testing.T) {
	if action, err := normalizeMaintenanceSpaceAction(""); err != nil || action != "status" {
		t.Fatalf("empty action must default to status, got %q err=%v", action, err)
	}
	if action, err := normalizeMaintenanceSpaceAction(" COMPACT "); err != nil || action != "compact" {
		t.Fatalf("action must normalize case/space, got %q err=%v", action, err)
	}
	if _, err := normalizeMaintenanceSpaceAction("vacuum-all"); err == nil {
		t.Fatalf("unknown action must be rejected")
	}
}

func TestClampMaintenanceSpaceKeep(t *testing.T) {
	if got := clampMaintenanceSpaceKeep(-5); got != 0 {
		t.Fatalf("negative keep must clamp to 0 (engine default), got %d", got)
	}
	if got := clampMaintenanceSpaceKeep(3); got != 3 {
		t.Fatalf("in-range keep must pass through, got %d", got)
	}
	if got := clampMaintenanceSpaceKeep(maintenanceSpaceKeepCeiling + 99); got != maintenanceSpaceKeepCeiling {
		t.Fatalf("keep must clamp to the ceiling, got %d", got)
	}
}
