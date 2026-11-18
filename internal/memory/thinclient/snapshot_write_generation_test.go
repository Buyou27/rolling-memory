// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openIncrTestVault(t *testing.T, name string) (*Vault, *sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	v, err := NewVault(db, []byte("user-local-master-key-2026"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	return v, db, dbPath
}

// snapshotEquivalence asserts that the warm (generation-checked) snapshot of v
// converges to the same recallable unit set as a cold Vault rebuilt from the
// same SQLite file. Baseline+delta snapshots score fresh units against the small
// delta corpus by design, so hit energy may differ from a from-scratch rebuild;
// what must never diverge is which units are retrievable — cold-equivalent
// membership, never stale content.
func snapshotEquivalence(t *testing.T, v *Vault, dbPath string, queries []string) {
	t.Helper()
	coldDB, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatalf("open cold db: %v", err)
	}
	defer coldDB.Close()
	coldV, err := NewVault(coldDB, []byte("user-local-master-key-2026"))
	if err != nil {
		t.Fatalf("cold NewVault: %v", err)
	}

	warmSnap, err := v.loadCorpusSnapshot(context.Background())
	if err != nil {
		t.Fatalf("warm snapshot: %v", err)
	}
	coldSnap, err := coldV.buildCorpusSnapshot(context.Background())
	if err != nil {
		t.Fatalf("cold snapshot: %v", err)
	}
	if warmSnap == nil || coldSnap == nil {
		t.Fatalf("nil snapshot: warm=%v cold=%v", warmSnap == nil, coldSnap == nil)
	}
	if len(warmSnap.docs) != len(coldSnap.docs) {
		t.Fatalf("docs count diverged: warm=%d cold=%d", len(warmSnap.docs), len(coldSnap.docs))
	}
	for _, q := range queries {
		warmHits, wErr := v.SearchClientSDKDefault(q, 10)
		coldHits, cErr := coldV.SearchClientSDKDefault(q, 10)
		if (wErr == nil) != (cErr == nil) {
			t.Fatalf("query %q error diverged: warm=%v cold=%v", q, wErr, cErr)
		}
		warmSet := make(map[string]bool, len(warmHits))
		for _, h := range warmHits {
			warmSet[h.UnitID] = true
		}
		coldSet := make(map[string]bool, len(coldHits))
		for _, h := range coldHits {
			coldSet[h.UnitID] = true
		}
		for id := range coldSet {
			if !warmSet[id] {
				t.Fatalf("query %q: cold-hit unit %s missing from warm hits (warm=%v cold=%v)", q, id, warmHits, coldHits)
			}
		}
		for id := range warmSet {
			if !coldSet[id] {
				t.Fatalf("query %q: warm-hit unit %s missing from cold hits (warm=%v cold=%v)", q, id, warmHits, coldHits)
			}
		}
	}
}

// TestSnapshotWriteGenerationEquivalence covers the post-write snapshot
// lifecycle: a plaintext save bumps the write generation instead of discarding
// the snapshot, and the next load must re-verify fingerprints and converge to
// the exact state of a cold rebuild.
func TestSnapshotWriteGenerationEquivalence(t *testing.T) {
	v, _, dbPath := openIncrTestVault(t, "gen_equiv.db")
	docs := []struct {
		id, text string
		occ      int64
	}{
		{"d1", "Albert visited the Porto lighthouse and photographed the Douro river sunset.", 1700000100},
		{"d2", "The Dongguan train arrived at noon; dinner was braised goose with friends.", 1700000200},
		{"d3", "Cambridge saw its first snow of late 2022 on December 11th around 22:40.", 1700000300},
		{"d4", "NeurIPS poster session: recruiting QR codes lined the convention hallway.", 1700000400},
	}
	for _, d := range docs {
		sents := SplitSentences(d.text)
		if err := v.SaveLocalPlaintext(d.id, d.occ, d.text, sents, IngestResponse{AddressURI: "mem://" + d.id}); err != nil {
			t.Fatalf("save %s: %v", d.id, err)
		}
	}
	if _, err := v.loadCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}
	// Write after a published snapshot: generation gap must route the next load
	// through the fingerprint re-check and land on the cold-equivalent state.
	extra := "A milk cow cat appeared in Oxford near the covered market."
	if err := v.SaveLocalPlaintext("d5", 1700000500, extra, SplitSentences(extra), IngestResponse{AddressURI: "mem://d5"}); err != nil {
		t.Fatalf("save d5: %v", err)
	}
	snapshotEquivalence(t, v, dbPath, []string{
		"Porto lighthouse sunset",
		"first snow Cambridge 2022",
		"milk cow cat Oxford",
		"NeurIPS recruiting QR",
	})
}

// TestSnapshotWriteGenerationManyWrites exercises a write burst (debounced
// background refresh) followed by immediate foreground queries.
func TestSnapshotWriteGenerationManyWrites(t *testing.T) {
	v, _, dbPath := openIncrTestVault(t, "gen_burst.db")
	queries := []string{"storm shelter basement", "birthday cake lemon"}
	for i := 0; i < 24; i++ {
		text := fmt.Sprintf("Entry %02d: the storm shelter basement stocked lemon birthday cake and %d candles.", i, i)
		id := fmt.Sprintf("b%02d", i)
		if err := v.SaveLocalPlaintext(id, int64(1700001000+i*60), text, SplitSentences(text), IngestResponse{AddressURI: "mem://" + id}); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
		if i%7 == 3 {
			// Interleave queries mid-burst: every read must already see every
			// prior write (read-your-writes), matching a cold rebuild.
			snapshotEquivalence(t, v, dbPath, queries)
		}
	}
	snapshotEquivalence(t, v, dbPath, queries)
}

// TestSnapshotRevisionGateCrossConnectionWrite proves the O(1) revision gate is
// safe against writers the in-process generation counter cannot see: the
// triggers live in the database file, so a second connection's INSERT, UPDATE
// and DELETE each bump docs_rev, and the next load must observe the change and
// converge to the cold-rebuild state instead of serving the stale snapshot.
func TestSnapshotRevisionGateCrossConnectionWrite(t *testing.T) {
	v, _, dbPath := openIncrTestVault(t, "gen_xproc.db")
	base := "The expedition cached supplies near the glacier moraine camp."
	if err := v.SaveLocalPlaintext("x1", 1700002000, base, SplitSentences(base), IngestResponse{AddressURI: "mem://x1"}); err != nil {
		t.Fatalf("save x1: %v", err)
	}
	if _, err := v.loadCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}

	// Foreign writer: a separate connection the Vault knows nothing about.
	ext, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatalf("open ext: %v", err)
	}
	defer ext.Close()
	if _, err := ext.Exec(`INSERT INTO thin_vault_docs(doc_id, layer, occurred_at, inserted_at, raw_text, sentences_json) VALUES ('x2', 'episodic', 1700002100, 1700002100, 'A hidden aurora photograph from the northern ridge.', '[]')`); err != nil {
		t.Fatalf("ext insert: %v", err)
	}
	snapshotEquivalence(t, v, dbPath, []string{"aurora northern ridge", "glacier moraine camp"})

	if _, err := ext.Exec(`UPDATE thin_vault_docs SET raw_text = 'A hidden aurora photograph from the SOUTHERN ridge.' WHERE doc_id = 'x2'`); err != nil {
		t.Fatalf("ext update: %v", err)
	}
	snapshotEquivalence(t, v, dbPath, []string{"aurora southern ridge"})

	if _, err := ext.Exec(`DELETE FROM thin_vault_docs WHERE doc_id = 'x2'`); err != nil {
		t.Fatalf("ext delete: %v", err)
	}
	snapshotEquivalence(t, v, dbPath, []string{"aurora ridge", "glacier moraine camp"})
}
