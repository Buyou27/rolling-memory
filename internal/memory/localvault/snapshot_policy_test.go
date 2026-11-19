// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A read-only recall surface must never materialise the full cluster. It serves
// a warming snapshot until the background builder publishes one.
func TestSnapshotOnlyVaultDoesNotBuildOnRead(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshot_only.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("snapshot-only-key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SaveLocalPlaintext("doc-1", 1700000000, "Alice met Bob in Paris on 2026-01-02.", nil, IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	v.SetBuildAllowed(false)
	if snap, err := v.buildCorpusSnapshot(context.Background()); err != nil || snap != nil {
		t.Fatalf("snapshot-only read must stay warming, snap=%+v err=%v", snap, err)
	}
	if _, err := os.Stat(dbPath + ".zcsr"); !os.IsNotExist(err) {
		t.Fatalf("snapshot-only read created a cluster: %v", err)
	}
	if err := v.WarmupCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("snapshot-only warmup: %v", err)
	}
	if _, err := os.Stat(dbPath + ".zcsr"); !os.IsNotExist(err) {
		t.Fatalf("snapshot-only warmup created a cluster: %v", err)
	}

	v.SetBuildAllowed(true)
	if err := v.WarmupCorpusSnapshot(context.Background()); err != nil {
		t.Fatalf("build-allowed warmup: %v", err)
	}
	if _, err := os.Stat(dbPath + ".zcsr"); err != nil {
		t.Fatalf("build-allowed warmup must publish the cluster: %v", err)
	}
}

// The O(1) corpus counters must stay in lockstep with the canonical tables.
func TestCanonicalCountsTrackTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "canonical_counts.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("canonical-counts-key"))
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
	assertCorpusCounts := func() {
		t.Helper()
		var docsCnt, termsCnt, liveDocs, liveTerms int64
		if err := db.QueryRow(`SELECT docs_cnt, terms_cnt FROM rm_revision WHERE id = 1`).Scan(&docsCnt, &termsCnt); err != nil {
			t.Fatalf("rm_revision counts: %v", err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&liveDocs); err != nil {
			t.Fatalf("rm_docs count: %v", err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM rm_terms`).Scan(&liveTerms); err != nil {
			t.Fatalf("rm_terms count: %v", err)
		}
		if docsCnt != liveDocs || termsCnt != liveTerms {
			t.Fatalf("counters drifted: docs %d/%d terms %d/%d", docsCnt, liveDocs, termsCnt, liveTerms)
		}
	}
	assertCorpusCounts()
	if _, err := db.Exec(`DELETE FROM rm_docs WHERE unit_id = 'doc-a'`); err != nil {
		t.Fatalf("delete doc: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM rm_terms WHERE unit_id = 'doc-a'`); err != nil {
		t.Fatalf("delete terms: %v", err)
	}
	assertCorpusCounts()
}

func TestAcquireBuildLockIsSingleFlightAndStaleSafe(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "build_lock.sqlite")
	release, acquired, err := AcquireBuildLock(dbPath, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("first acquire: acquired=%v err=%v", acquired, err)
	}
	if _, second, err := AcquireBuildLock(dbPath, time.Minute); err != nil || second {
		t.Fatalf("second acquire must fail: acquired=%v err=%v", second, err)
	}
	release()
	release, acquired, err = AcquireBuildLock(dbPath, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquire after release: acquired=%v err=%v", acquired, err)
	}
	release()

	lockPath := dbPath + ".build.lock"
	if err := os.WriteFile(lockPath, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	release, acquired, err = AcquireBuildLock(dbPath, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("stale lock must be reclaimed: acquired=%v err=%v", acquired, err)
	}
	release()
}
