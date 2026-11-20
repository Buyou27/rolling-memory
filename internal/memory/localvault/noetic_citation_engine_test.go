// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rocareer/rolling-memory/internal/core"
)

// The external co-citation view is derived once per noetic fingerprint: adding
// a recollection rebuilds it, while activating a handle changes no citation set
// and must reuse the cached engine.
func TestCoreEngineWithNoeticCitationsDerivesAndCaches(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "noetic-edges.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := NewVault(db, []byte("noetic-edges-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	if err := vault.SaveLocalNoetic(LocalNoeticRecord{
		NoeticID:     "nt_edges",
		OccurredAt:   100,
		QueryText:    "citation cue",
		AnswerText:   "ans",
		CitedUnitIDs: []string{"u1", "u2"},
	}); err != nil {
		t.Fatalf("SaveLocalNoetic: %v", err)
	}
	if _, err := vault.loadActiveNoeticRecords(); err != nil {
		t.Fatalf("loadActiveNoeticRecords: %v", err)
	}
	base := core.NewEngine(core.BuildIndex([]core.Doc{{UnitID: "u1"}, {UnitID: "u2"}, {UnitID: "u3"}}))
	snap := &vaultCorpusSnapshot{coreEngine: base}
	derived := snap.coreEngineWithNoeticCitations(vault, base)
	if derived == nil || derived == base {
		t.Fatalf("expected a derived engine, got %p (base %p)", derived, base)
	}
	if again := snap.coreEngineWithNoeticCitations(vault, base); again != derived {
		t.Fatalf("derived engine was rebuilt for the same noetic fingerprint")
	}
	if _, err := vault.ActivateNoetics([]string{"nt_edges"}, 200); err != nil {
		t.Fatalf("ActivateNoetics: %v", err)
	}
	if still := snap.coreEngineWithNoeticCitations(vault, base); still != derived {
		t.Fatalf("activation rebuilt the citation view; cited sets did not change")
	}
	// Re-crystallizing the same evidence refreshes the trace but not its declared
	// pairs, so the derived CSR is reused.
	if err := vault.SaveLocalNoetic(LocalNoeticRecord{
		NoeticID:     "nt_edges",
		OccurredAt:   300,
		QueryText:    "citation cue rephrased",
		AnswerText:   "ans",
		CitedUnitIDs: []string{"u2", "u1"},
	}); err != nil {
		t.Fatalf("SaveLocalNoetic re-crystallize: %v", err)
	}
	if _, err := vault.loadActiveNoeticRecords(); err != nil {
		t.Fatalf("reload noetics: %v", err)
	}
	if still := snap.coreEngineWithNoeticCitations(vault, base); still != derived {
		t.Fatalf("re-crystallizing the same evidence rebuilt the citation view")
	}
}

// BenchmarkWithExternalCoCitations measures the cost of folding many declared
// citation records into a derived engine (CSR rebuild only, index shared).
func BenchmarkWithExternalCoCitations(b *testing.B) {
	docs := make([]core.Doc, 0, 100)
	for i := 0; i < 100; i++ {
		docs = append(docs, core.Doc{UnitID: "u" + strconv.Itoa(i)})
	}
	base := core.NewEngine(core.BuildIndex(docs))
	sets := make([][]string, 0, 2000)
	for i := 0; i < 2000; i++ {
		set := make([]string, 0, 6)
		for j := 0; j < 6; j++ {
			set = append(set, "u"+strconv.Itoa((i*7+j*13)%100))
		}
		sets = append(sets, set)
	}
	_ = base.WithExternalCoCitations(sets) // warm the base state outside the timer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if derived := base.WithExternalCoCitations(sets); derived == nil {
			b.Fatal("nil derived engine")
		}
	}
}
