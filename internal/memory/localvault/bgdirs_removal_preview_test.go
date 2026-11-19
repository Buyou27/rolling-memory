// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPreviewBackgroundDirRemovalReportsRebindsAndPurgesWithoutWrites pins the contract of
// PreviewBackgroundDirRemoval: the preview resolves the same plan a removal would execute —
// files still covered by another mount are re-binds, uncovered files are purges with their
// chunks and citing Noetic records counted — and it writes nothing at all.
func TestPreviewBackgroundDirRemovalReportsRebindsAndPurgesWithoutWrites(t *testing.T) {
	root := newBackgroundTestDir(t)
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub mount: %v", err)
	}
	pathA := filepath.Join(root, "a.md")
	pathB := filepath.Join(sub, "b.md")
	if err := os.WriteFile(pathA, []byte("parent-owned document is purged when its only mount leaves"), 0o600); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("child-owned document survives through the remaining parent mount"), 0o600); err != nil {
		t.Fatalf("write b.md: %v", err)
	}

	client, db := newBackgroundTestClient(t, &backgroundTransportStub{})
	ctx := context.Background()
	if _, err := client.AddBackgroundDir(root); err != nil {
		t.Fatalf("AddBackgroundDir(root): %v", err)
	}
	if _, err := client.AddBackgroundDir(sub); err != nil {
		t.Fatalf("AddBackgroundDir(sub): %v", err)
	}
	if _, err := client.SyncBackgroundDirs(ctx); err != nil {
		t.Fatalf("SyncBackgroundDirs: %v", err)
	}
	unitsA := loadBackgroundUnitIDs(t, db, pathA)
	unitsB := loadBackgroundUnitIDs(t, db, pathB)
	if len(unitsA) == 0 || len(unitsB) == 0 {
		t.Fatalf("expected tracked units for both files, got a=%v b=%v", unitsA, unitsB)
	}
	citedJSON, err := json.Marshal(unitsA)
	if err != nil {
		t.Fatalf("encode cited units: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO rm_noetic(
			noetic_id, occurred_at, status, query_text, answer_text, cited_unit_ids_json, cited_sentences_json
		) VALUES ('NOETIC-CITED', 1, 'active', 'q', 'a', ?, '[]')`, string(citedJSON)); err != nil {
		t.Fatalf("insert cited Noetic: %v", err)
	}

	preview, err := client.PreviewBackgroundDirRemoval(root)
	if err != nil {
		t.Fatalf("PreviewBackgroundDirRemoval(root): %v", err)
	}
	if preview.RootPath != root {
		t.Fatalf("preview root = %q, want %q", preview.RootPath, root)
	}
	if preview.RemainingMounts != 1 || len(preview.RemainingRoots) != 1 || preview.RemainingRoots[0] != sub {
		t.Fatalf("preview remaining mounts = %d %v, want the sub mount %q", preview.RemainingMounts, preview.RemainingRoots, sub)
	}
	// b.md belongs to the deeper sub mount, so removing the parent has nothing to re-bind:
	// the parent-owned file leaves with its only covering mount.
	if preview.ReboundFiles != 0 || len(preview.ReboundSample) != 0 {
		t.Fatalf("preview rebinds = %d %v, want none (b.md is owned by the sub mount)", preview.ReboundFiles, preview.ReboundSample)
	}
	if preview.PurgedFiles != 1 || preview.PurgedChunks != len(unitsA) {
		t.Fatalf("preview purges = %d files / %d chunks, want 1 file / %d chunks", preview.PurgedFiles, preview.PurgedChunks, len(unitsA))
	}
	if len(preview.PurgedSample) != 1 || preview.PurgedSample[0] != pathA {
		t.Fatalf("preview purge sample = %v, want %q", preview.PurgedSample, pathA)
	}
	if preview.StaledNoetic != 1 {
		t.Fatalf("preview staled Noetic = %d, want 1", preview.StaledNoetic)
	}

	// A preview must leave every plane untouched: both mounts, all file/chunk rows and the
	// cited Noetic status stay exactly as they were.
	mounts, err := client.Vault.ListBackgroundDirs()
	if err != nil || len(mounts) != 2 {
		t.Fatalf("preview changed mounts: mounts=%+v err=%v", mounts, err)
	}
	if got := countBackgroundRows(t, db, `SELECT COUNT(*) FROM rm_bg_files WHERE abs_path = ?`, pathA); got != 1 {
		t.Fatalf("preview removed file metadata for %s: %d", pathA, got)
	}
	if got := countBackgroundRows(t, db, `SELECT COUNT(*) FROM rm_bg_chunks WHERE abs_path = ?`, pathA); got == 0 {
		t.Fatalf("preview removed chunks for %s", pathA)
	}
	if got := loadNoeticStatus(t, db, "NOETIC-CITED"); got != "active" {
		t.Fatalf("preview staled the cited Noetic early: status=%q", got)
	}

	// Removing the child mount instead: its only file is covered by the parent, so the
	// preview reports a pure re-bind with zero purges and zero citation impact.
	childPreview, err := client.PreviewBackgroundDirRemoval(sub)
	if err != nil {
		t.Fatalf("PreviewBackgroundDirRemoval(sub): %v", err)
	}
	if childPreview.ReboundFiles != 1 || childPreview.PurgedFiles != 0 || childPreview.PurgedChunks != 0 || childPreview.StaledNoetic != 0 {
		t.Fatalf("child preview = %+v, want one rebind and zero purges", childPreview)
	}
	if len(childPreview.ReboundSample) != 1 || childPreview.ReboundSample[0] != pathB {
		t.Fatalf("child preview rebind sample = %v, want %q", childPreview.ReboundSample, pathB)
	}

	// The executed removal must land exactly what the parent preview projected.
	report, err := client.RemoveBackgroundDirReport(ctx, root)
	if err != nil {
		t.Fatalf("RemoveBackgroundDirReport(root): %v", err)
	}
	if report.ReboundFiles != preview.ReboundFiles || len(report.RemovedUnitIDs) != preview.PurgedChunks {
		t.Fatalf("removal (rebinds=%d purged=%d) diverged from preview (rebinds=%d purged=%d)",
			report.ReboundFiles, len(report.RemovedUnitIDs), preview.ReboundFiles, preview.PurgedChunks)
	}
	if got := loadNoeticStatus(t, db, "NOETIC-CITED"); got != "stale" {
		t.Fatalf("cited Noetic status after real removal = %q, want stale", got)
	}
}

// TestPreviewIsAdvisoryAndExecutionReplansAgainstCurrentMounts pins the TOCTOU contract:
// a preview is a projection at request time, and the executed removal always re-resolves
// the removal plan against the mount set it actually sees. A mount added after the
// preview therefore turns a projected purge into a re-bind instead of destroying units.
func TestPreviewIsAdvisoryAndExecutionReplansAgainstCurrentMounts(t *testing.T) {
	root := newBackgroundTestDir(t)
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub mount: %v", err)
	}
	path := filepath.Join(sub, "doc.md")
	if err := os.WriteFile(path, []byte("covered later by the parent mount added after preview"), 0o600); err != nil {
		t.Fatalf("write doc.md: %v", err)
	}
	client, db := newBackgroundTestClient(t, &backgroundTransportStub{})
	ctx := context.Background()
	if _, err := client.AddBackgroundDir(sub); err != nil {
		t.Fatalf("AddBackgroundDir(sub): %v", err)
	}
	if _, err := client.SyncBackgroundDirs(ctx); err != nil {
		t.Fatalf("SyncBackgroundDirs: %v", err)
	}

	preview, err := client.PreviewBackgroundDirRemoval(sub)
	if err != nil {
		t.Fatalf("PreviewBackgroundDirRemoval(sub): %v", err)
	}
	if preview.PurgedFiles != 1 || preview.PurgedChunks == 0 || preview.ReboundFiles != 0 {
		t.Fatalf("preview before the parent mount = %+v, want one projected purge", preview)
	}

	// The mount set changes after the preview: the parent now covers the child's file.
	if _, err := client.AddBackgroundDir(root); err != nil {
		t.Fatalf("AddBackgroundDir(root): %v", err)
	}
	if _, err := client.SyncBackgroundDirs(ctx); err != nil {
		t.Fatalf("sync after parent mount: %v", err)
	}

	report, err := client.RemoveBackgroundDirReport(ctx, sub)
	if err != nil {
		t.Fatalf("RemoveBackgroundDirReport(sub): %v", err)
	}
	if report.ReboundFiles != 1 || len(report.RemovedUnitIDs) != 0 {
		t.Fatalf("execution did not re-plan against the current mounts: %+v", report)
	}
	if got := countBackgroundRows(t, db, `SELECT COUNT(*) FROM rm_bg_files WHERE abs_path = ?`, path); got != 1 {
		t.Fatalf("re-planned removal purged a covered file: %d", got)
	}
}

// TestPreviewForNeverMountedPathReportsZeros：对从未挂载的路径预演必须是全零而非报错，
// 且不产生任何写入——防误敲路径时把别的挂载当目标。
func TestPreviewForNeverMountedPathReportsZeros(t *testing.T) {
	client, db := newBackgroundTestClient(t, &backgroundTransportStub{})
	ctx := context.Background()
	root := newBackgroundTestDir(t)
	if err := os.WriteFile(filepath.Join(root, "kept.md"), []byte("still mounted and untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddBackgroundDir(root); err != nil {
		t.Fatalf("AddBackgroundDir: %v", err)
	}
	if _, err := client.SyncBackgroundDirs(ctx); err != nil {
		t.Fatalf("SyncBackgroundDirs: %v", err)
	}

	preview, err := client.PreviewBackgroundDirRemoval(filepath.Join(t.TempDir(), "never-mounted"))
	if err != nil {
		t.Fatalf("preview of an unmounted path errored: %v", err)
	}
	if preview.PurgedFiles != 0 || preview.ReboundFiles != 0 || preview.PurgedChunks != 0 || preview.StaledNoetic != 0 {
		t.Fatalf("unmounted-path preview reported work: %+v", preview)
	}
	if mounts, err := client.Vault.ListBackgroundDirs(); err != nil || len(mounts) != 1 {
		t.Fatalf("preview changed mounts: mounts=%+v err=%v", mounts, err)
	}
	if got := countBackgroundRows(t, db, `SELECT COUNT(*) FROM rm_bg_files`); got != 1 {
		t.Fatalf("preview changed tracked files: %d", got)
	}
}
