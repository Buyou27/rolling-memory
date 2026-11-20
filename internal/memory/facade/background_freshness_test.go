// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// TestLocalRecallCarriesBackgroundFreshness verifies the delivery face: recall names each
// mount's last successful pass, and a failed pass is visible instead of making a stalled
// mount look fresh.
func TestLocalRecallCarriesBackgroundFreshness(t *testing.T) {
	memory := newVaultOnlyMemory(t)
	client, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mounted.md"), []byte("# 挂载记录\n固定目录的内容进入本地索引。\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddBackgroundDir(root); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.SyncBackgroundDirs(ctx); err != nil {
		t.Fatal(err)
	}

	result, err := memory.Recall(facade.RecallRequest{Cue: "挂载记录 本地索引", NoCrystallize: true})
	if err != nil {
		t.Fatal(err)
	}
	freshness, ok := result.Meta["background"].(map[string]any)
	if !ok {
		t.Fatalf("recall meta lacks background freshness: %#v", result.Meta)
	}
	mounts, ok := freshness["mounts"].([]map[string]any)
	if !ok || len(mounts) != 1 {
		t.Fatalf("background mounts = %#v", freshness["mounts"])
	}
	if got := mounts[0]["status"]; got != "ok" {
		t.Fatalf("mount status = %v, want ok", got)
	}
	if got := mounts[0]["tracked_files"]; got != 1 {
		t.Fatalf("tracked files = %v, want 1", got)
	}
	if _, ok := freshness["last_success_at"]; !ok {
		t.Fatalf("freshness lacks last_success_at: %#v", freshness)
	}

	// A pass that cannot stat the mount root fails before scanning; recall must say so and
	// keep the last successful timestamp.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SyncBackgroundDirs(ctx); err == nil {
		t.Fatal("sync over a removed mount root must fail")
	}
	result, err = memory.Recall(facade.RecallRequest{Cue: "挂载记录 本地索引", NoCrystallize: true})
	if err != nil {
		t.Fatal(err)
	}
	freshness, ok = result.Meta["background"].(map[string]any)
	if !ok {
		t.Fatalf("recall meta lacks background freshness after failure: %#v", result.Meta)
	}
	mounts, _ = freshness["mounts"].([]map[string]any)
	if len(mounts) != 1 {
		t.Fatalf("background mounts after failure = %#v", freshness["mounts"])
	}
	if got := mounts[0]["status"]; got != "error" {
		t.Fatalf("mount status = %v, want error", got)
	}
	if got, _ := mounts[0]["last_error"].(string); got == "" {
		t.Fatalf("mount error is empty: %#v", mounts[0])
	}
	if got := freshness["error_mounts"]; got != 1 {
		t.Fatalf("error mounts = %v, want 1", got)
	}
	if _, ok := mounts[0]["last_success_at"]; !ok {
		t.Fatalf("failed pass dropped the last success: %#v", mounts[0])
	}
}
