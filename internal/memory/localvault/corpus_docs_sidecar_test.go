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

// A cold start must read the published docs sidecar instead of scanning the
// canonical tables; the change log covers everything after the build.
func TestCorpusDocsSidecarPowersColdStart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "docs_sidecar.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVault(db, []byte("docs-sidecar-key"))
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{
		"Alice met Bob in Paris on 2026-01-02.",
		"Carol booked a hotel near the harbour.",
		"Dave paid fifty pounds for the boat tour.",
	} {
		if err := v.SaveLocalPlaintext("doc-"+string(rune('a'+i)), 1700000000+int64(i), text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.RebuildCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	signature := v.canonicalSignature()
	if signature == "" {
		t.Fatal("empty canonical signature")
	}
	csrPath, _, _, docsPath, slot, ok := resolvePublishedSnapshotPaths(dbPath)
	if !ok || slot == "" {
		t.Fatal("rebuild did not publish a generation slot")
	}
	for _, path := range []string{csrPath, docsPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("published file missing: %s: %v", path, err)
		}
	}
	if !corpusDocsSidecarMatches(dbPath, signature) {
		t.Fatal("sidecar does not match the published signature")
	}
	if corpusDocsSidecarMatches(dbPath, "v5|wrong") {
		t.Fatal("sidecar matched a wrong signature")
	}
	if err := v.SaveLocalPlaintext("doc-d", 1700000010, "Erin took the train to Berlin in March.", nil, IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	// Fresh process: no in-memory snapshot, no cached docs. The published
	// sidecar is older than the live state; the change log must supply doc-d.
	coldDB, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer coldDB.Close()
	cold, err := NewVault(coldDB, []byte("docs-sidecar-key"))
	if err != nil {
		t.Fatal(err)
	}
	cold.SetBuildAllowed(false)
	snap, err := cold.buildCorpusSnapshot(context.Background())
	if err != nil || snap == nil {
		t.Fatalf("cold snapshot: %v", err)
	}
	if len(snap.docs) != 4 {
		t.Fatalf("cold docs = %d, want 4", len(snap.docs))
	}
	if snap.changeSeq == 0 {
		t.Fatal("cold snapshot lost the change-log watermark")
	}
	for _, id := range []string{"doc-a", "doc-b", "doc-c", "doc-d"} {
		if _, ok := snap.docIdxByID[id]; !ok {
			t.Fatalf("cold docs missing %s", id)
		}
	}
}
