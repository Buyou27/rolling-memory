// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestStatusCarriesBackgroundFreshness verifies the operator surface: status names each
// mount's last successful pass, so a stalled mount is visible without opening the console.
func TestStatusCarriesBackgroundFreshness(t *testing.T) {
	memory := newVaultOnlyMemory(t)
	client, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mounted.md"), []byte("# 挂载\n状态页应能看到挂载的新鲜度。\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddBackgroundDir(root); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SyncBackgroundDirs(context.Background()); err != nil {
		t.Fatal(err)
	}

	status, err := memory.Status(memory.Config.Scope)
	if err != nil {
		t.Fatal(err)
	}
	freshness, ok := status["background"].(map[string]any)
	if !ok {
		t.Fatalf("status lacks background freshness: %#v", status)
	}
	mounts, ok := freshness["mounts"].([]map[string]any)
	if !ok || len(mounts) != 1 {
		t.Fatalf("background mounts = %#v", freshness["mounts"])
	}
	if got := mounts[0]["status"]; got != "ok" {
		t.Fatalf("mount status = %v, want ok", got)
	}
	if _, ok := freshness["last_success_at"]; !ok {
		t.Fatalf("freshness lacks last_success_at: %#v", freshness)
	}
}
