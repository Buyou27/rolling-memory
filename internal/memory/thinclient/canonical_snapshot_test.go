// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The canonical snapshot builds from Store.LoadIndex, persists the .zcsr
// baseline, and serves later writes through the bounded delta overlay.
func TestCanonicalSnapshotUsesStoreIndexAndDelta(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "canonical_snapshot.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("canonical-snapshot-key"))
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{
		"Alice met Bob in Paris on 2026-01-02.",
		"Carol booked a hotel near the harbour for the weekend.",
		"Dave paid fifty pounds for the boat tour ticket.",
	} {
		if err := v.SaveLocalPlaintext("doc-"+string(rune('a'+i)), 1700000000+int64(i), text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := v.buildCorpusSnapshot(context.Background())
	if err != nil || snap == nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if !strings.HasPrefix(snap.freshnessSignature, "v5|") {
		t.Fatalf("snapshot must carry the canonical v5 signature, got %q", snap.freshnessSignature)
	}
	eng := snap.ensureSnapshotCoreEngine(v)
	if eng == nil || eng.Index == nil || eng.Index.N != 3 {
		t.Fatalf("canonical engine must index three units, got %+v", eng)
	}
	if _, err := os.Stat(dbPath + ".zcsr"); err != nil {
		t.Fatalf(".zcsr baseline must be persisted: %v", err)
	}
	if _, err := os.Stat(dbPath + ".zcsr.units"); err != nil {
		t.Fatalf("unit revision sidecar must be persisted: %v", err)
	}

	if err := v.SaveLocalPlaintext("doc-d", 1700000010, "Erin took the train to Berlin in March.", nil, IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	next, err := v.buildCorpusSnapshot(context.Background())
	if err != nil || next == nil {
		t.Fatalf("build snapshot after write: %v", err)
	}
	nextEng := next.ensureSnapshotCoreEngine(v)
	if nextEng == nil {
		t.Fatal("delta engine must be available")
	}
	if !nextEng.HasDelta() || nextEng.DeltaDocCount() != 1 {
		t.Fatalf("expected a one-unit delta overlay, hasDelta=%v count=%d", nextEng.HasDelta(), nextEng.DeltaDocCount())
	}
}
