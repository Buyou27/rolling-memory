// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/core"
)

// TestL3ActivationChannelsAblation measures the three delivery channels on the
// same query and corpus: the base kernel, the structural co-citation view, and
// the scalar noetic merge. It is an observability test, not a score gate; run it
// with -v and compare the ranks of the cited evidence.
func TestL3ActivationChannelsAblation(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "l3-ablation.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := NewVault(db, []byte("l3-ablation-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	docs := []struct{ id, text string }{
		{"u1", "alpha beta gamma unrelated witness"},
		{"u2", "alpha beta delta cited first"},
		{"u3", "alpha beta epsilon cited second"},
	}
	for _, doc := range docs {
		if err := vault.SaveLocalPlaintext(doc.id, 100, doc.text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatalf("SaveLocalPlaintext %s: %v", doc.id, err)
		}
	}
	if err := vault.SaveLocalNoetic(LocalNoeticRecord{
		NoeticID:     "nt_ablation",
		OccurredAt:   200,
		QueryText:    "alpha beta",
		AnswerText:   "ans",
		CitedUnitIDs: []string{"u2", "u3"},
	}); err != nil {
		t.Fatalf("SaveLocalNoetic: %v", err)
	}
	snap, err := vault.loadCorpusSnapshot(context.Background())
	if err != nil || snap == nil || snap.coreEngine == nil {
		t.Fatalf("loadCorpusSnapshot: snap=%v err=%v", snap, err)
	}
	baseHits := searchCoreEngine(snap.coreEngine, vault, "alpha beta", nil, 10)
	structuralHits := snap.searchSnapshotCoreEngines(vault, "alpha beta", nil, 10)
	mergedHits, err := vault.SearchClientSDKDefault("alpha beta", 10)
	if err != nil {
		t.Fatalf("SearchClientSDKDefault: %v", err)
	}
	rankCore := func(hits []core.Hit, id string) int {
		for i := range hits {
			if hits[i].UnitID == id {
				return i + 1
			}
		}
		return 0
	}
	rankLocal := func(hits []QueryHit, id string) int {
		for i := range hits {
			if hits[i].UnitID == id {
				return i + 1
			}
		}
		return 0
	}
	t.Logf("base      u1=%d u2=%d u3=%d (n=%d)", rankCore(baseHits, "u1"), rankCore(baseHits, "u2"), rankCore(baseHits, "u3"), len(baseHits))
	t.Logf("struct    u1=%d u2=%d u3=%d (n=%d)", rankCore(structuralHits, "u1"), rankCore(structuralHits, "u2"), rankCore(structuralHits, "u3"), len(structuralHits))
	t.Logf("merged    u1=%d u2=%d u3=%d (n=%d)", rankLocal(mergedHits, "u1"), rankLocal(mergedHits, "u2"), rankLocal(mergedHits, "u3"), len(mergedHits))
	if cov := vault.NoeticEvidenceCoverage([]string{"u2", "u3"}, "alpha beta"); cov < 0.99 {
		t.Fatalf("cited set coverage = %.3f, want the full cue", cov)
	}
	if len(baseHits) == 0 || len(structuralHits) == 0 || len(mergedHits) == 0 {
		t.Fatal("one of the ablation arms returned no hits")
	}
}

// The second scenario splits the citation evidence across two documents that
// each cover only one query term; it checks whether co-citation lifts the pair
// or whether the scalar merge remains the only delivery lift.
func TestL3ActivationChannelsAblationComplementary(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "l3-ablation-complementary.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := NewVault(db, []byte("l3-ablation-complementary-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	docs := []struct{ id, text string }{
		{"d1", "alpha beta distractor one"},
		{"d2", "alpha beta distractor two"},
		{"d3", "alpha gamma cited one"},
		{"d4", "beta delta cited two"},
	}
	for _, doc := range docs {
		if err := vault.SaveLocalPlaintext(doc.id, 100, doc.text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatalf("SaveLocalPlaintext %s: %v", doc.id, err)
		}
	}
	if err := vault.SaveLocalNoetic(LocalNoeticRecord{
		NoeticID:     "nt_ablation_complementary",
		OccurredAt:   200,
		QueryText:    "alpha beta",
		AnswerText:   "ans",
		CitedUnitIDs: []string{"d3", "d4"},
	}); err != nil {
		t.Fatalf("SaveLocalNoetic: %v", err)
	}
	snap, err := vault.loadCorpusSnapshot(context.Background())
	if err != nil || snap == nil || snap.coreEngine == nil {
		t.Fatalf("loadCorpusSnapshot: snap=%v err=%v", snap, err)
	}
	baseHits := searchCoreEngine(snap.coreEngine, vault, "alpha beta", nil, 10)
	structuralHits := snap.searchSnapshotCoreEngines(vault, "alpha beta", nil, 10)
	mergedHits, err := vault.SearchClientSDKDefault("alpha beta", 10)
	if err != nil {
		t.Fatalf("SearchClientSDKDefault: %v", err)
	}
	rankCore := func(hits []core.Hit, id string) int {
		for i := range hits {
			if hits[i].UnitID == id {
				return i + 1
			}
		}
		return 0
	}
	rankLocal := func(hits []QueryHit, id string) int {
		for i := range hits {
			if hits[i].UnitID == id {
				return i + 1
			}
		}
		return 0
	}
	t.Logf("base      d1=%d d2=%d d3=%d d4=%d (n=%d)", rankCore(baseHits, "d1"), rankCore(baseHits, "d2"), rankCore(baseHits, "d3"), rankCore(baseHits, "d4"), len(baseHits))
	t.Logf("struct    d1=%d d2=%d d3=%d d4=%d (n=%d)", rankCore(structuralHits, "d1"), rankCore(structuralHits, "d2"), rankCore(structuralHits, "d3"), rankCore(structuralHits, "d4"), len(structuralHits))
	t.Logf("merged    d1=%d d2=%d d3=%d d4=%d (n=%d)", rankLocal(mergedHits, "d1"), rankLocal(mergedHits, "d2"), rankLocal(mergedHits, "d3"), rankLocal(mergedHits, "d4"), len(mergedHits))
	if cov := vault.NoeticEvidenceCoverage([]string{"d3", "d4"}, "alpha beta"); cov < 0.99 {
		t.Fatalf("complementary cited set coverage = %.3f, want the full cue", cov)
	}
	if baseRank, mergedRank := rankCore(baseHits, "d3"), rankLocal(mergedHits, "d3"); mergedRank == 0 || (baseRank > 0 && mergedRank >= baseRank) {
		t.Fatalf("coverage floor did not lift complementary evidence: d3 base=%d merged=%d", baseRank, mergedRank)
	}
	if len(baseHits) == 0 || len(structuralHits) == 0 || len(mergedHits) == 0 {
		t.Fatal("one of the ablation arms returned no hits")
	}
}
