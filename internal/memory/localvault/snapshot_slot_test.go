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

// Each build publishes an immutable slot and flips CURRENT; the previous slot
// stays as PREV and older generations are pruned.
func TestSnapshotSlotPublishAndRetention(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slot_publish.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("slot-publish-key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SaveLocalPlaintext("doc-a", 1700000000, "Alice met Bob in Paris.", nil, IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("first build: %v", err)
	}
	slot1, ok := readSnapshotPointer(snapshotCurrentPath(dbPath))
	if !ok {
		t.Fatal("CURRENT missing after first build")
	}
	for _, name := range []string{snapshotSlotCSR, snapshotSlotGen, snapshotSlotUnits, snapshotSlotDocs} {
		if _, err := os.Stat(filepath.Join(snapshotSlotDir(dbPath, slot1), name)); err != nil {
			t.Fatalf("slot %s missing %s: %v", slot1, name, err)
		}
	}
	if !PublishedSnapshotExists(dbPath) {
		t.Fatal("published snapshot not detected")
	}

	if err := v.SaveLocalPlaintext("doc-b", 1700000010, "Carol booked a hotel.", nil, IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("second build: %v", err)
	}
	slot2, _ := readSnapshotPointer(snapshotCurrentPath(dbPath))
	if slot2 == "" || slot2 == slot1 {
		t.Fatalf("second build did not publish a new slot: %q -> %q", slot1, slot2)
	}
	if prev, _ := readSnapshotPointer(snapshotPrevPath(dbPath)); prev != slot1 {
		t.Fatalf("PREV = %q, want %q", prev, slot1)
	}
	if _, err := os.Stat(snapshotSlotDir(dbPath, slot1)); err != nil {
		t.Fatalf("previous slot pruned too early: %v", err)
	}

	if err := v.SaveLocalPlaintext("doc-c", 1700000020, "Dave took the train.", nil, IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("third build: %v", err)
	}
	slot3, _ := readSnapshotPointer(snapshotCurrentPath(dbPath))
	if slot3 == "" || slot3 == slot2 {
		t.Fatalf("third build did not publish a new slot: %q -> %q", slot2, slot3)
	}
	if prev, _ := readSnapshotPointer(snapshotPrevPath(dbPath)); prev != slot2 {
		t.Fatalf("PREV after third build = %q, want %q", prev, slot2)
	}
	if _, err := os.Stat(snapshotSlotDir(dbPath, slot1)); !os.IsNotExist(err) {
		t.Fatalf("old slot %s was not pruned: %v", slot1, err)
	}

	coldDB, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer coldDB.Close()
	cold, err := NewVault(coldDB, []byte("slot-publish-key"))
	if err != nil {
		t.Fatal(err)
	}
	cold.SetBuildAllowed(false)
	snap, err := cold.buildCorpusSnapshot(context.Background())
	if err != nil || snap == nil || len(snap.docs) != 3 {
		t.Fatalf("cold snapshot: %v docs=%d", err, len(snap.docs))
	}
}
