// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotDiskGuard(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "disk_guard.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("disk-guard-key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SaveLocalPlaintext("doc-a", 1700000000, "Alice met Bob in Paris.", nil, IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("baseline build: %v", err)
	}
	original := snapshotFreeDiskBytes
	defer func() { snapshotFreeDiskBytes = original }()
	snapshotFreeDiskBytes = func(string) (int64, error) { return 1, nil }
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err == nil {
		t.Fatal("low disk space must block the build")
	}
	snapshotFreeDiskBytes = func(string) (int64, error) { return 10 << 30, nil }
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("plenty of disk must allow the build: %v", err)
	}
}

// A destructive bulk purge invalidates the published generation; the reader
// must serve warming until a new slot at the live purge epoch is published.
func TestPurgeEpochBlocksStaleSnapshot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "purge_epoch.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("purge-epoch-key"))
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{
		"Alice met Bob in Paris.",
		"Carol booked a hotel near the harbour.",
	} {
		if err := v.SaveLocalPlaintext("doc-"+string(rune('a'+i)), 1700000000+int64(i), text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("baseline build: %v", err)
	}
	reader, err := NewVault(db, []byte("purge-epoch-key"))
	if err != nil {
		t.Fatal(err)
	}
	reader.SetBuildAllowed(false)
	if snap, err := reader.buildCorpusSnapshot(context.Background()); err != nil || snap == nil || len(snap.docs) != 2 {
		t.Fatalf("baseline reader snapshot: %v docs=%d", err, len(snap.docs))
	}
	if _, err := db.Exec(`DELETE FROM rm_docs WHERE unit_id = 'doc-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM rm_terms WHERE unit_id = 'doc-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM rm_unit_meta WHERE unit_id = 'doc-a'`); err != nil {
		t.Fatal(err)
	}
	if err := v.BumpPurgeEpoch(context.Background()); err != nil {
		t.Fatalf("bump purge epoch: %v", err)
	}
	if snap, err := reader.buildCorpusSnapshot(context.Background()); err != nil || snap != nil {
		t.Fatalf("stale generation must serve warming, snap=%+v err=%v", snap, err)
	}
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("post-purge build: %v", err)
	}
	fresh, err := NewVault(db, []byte("purge-epoch-key"))
	if err != nil {
		t.Fatal(err)
	}
	fresh.SetBuildAllowed(false)
	snap, err := fresh.buildCorpusSnapshot(context.Background())
	if err != nil || snap == nil {
		t.Fatalf("post-purge snapshot: %v", err)
	}
	if len(snap.docs) != 1 || snap.purgeEpoch != 1 {
		t.Fatalf("post-purge docs=%d epoch=%d, want 1/1", len(snap.docs), snap.purgeEpoch)
	}
	if _, exists := snap.docIdxByID["doc-a"]; exists {
		t.Fatal("purged unit still present after the new generation")
	}
}

// A corrupt CURRENT pointer must fall back to the legacy single-slot files.
func TestSnapshotCorruptPointerFallsBackToLegacy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pointer_fallback.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("pointer-fallback-key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SaveLocalPlaintext("doc-a", 1700000000, "Alice met Bob in Paris.", nil, IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	if err := v.WarmupCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("legacy warmup: %v", err)
	}
	if _, err := os.Stat(dbPath + ".zcsr"); err != nil {
		t.Fatalf("legacy cluster missing: %v", err)
	}
	if err := os.MkdirAll(snapshotSlotsDir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshotCurrentPath(dbPath), []byte("v1 missing-slot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !PublishedSnapshotExists(dbPath) {
		t.Fatal("corrupt CURRENT must fall back to the legacy cluster")
	}
	csr, _, _, _, slot, ok := resolvePublishedSnapshotPaths(dbPath)
	if !ok || slot != "" || csr != dbPath+".zcsr" {
		t.Fatalf("resolved %q slot=%q ok=%v, want legacy", csr, slot, ok)
	}
	if err := os.WriteFile(snapshotCurrentPath(dbPath), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !PublishedSnapshotExists(dbPath) {
		t.Fatal("malformed CURRENT must fall back to the legacy cluster")
	}
}
