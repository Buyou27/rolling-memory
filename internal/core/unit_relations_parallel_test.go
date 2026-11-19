// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"fmt"
	"testing"
)

// TestQueryHasSpecificTerm pins the applicability predicate of the intra-event
// endorsement pass: a query made only of terms at or above the N/e³ anchor cut
// has no specific term to anchor an event cluster, while any term below it does.
func TestQueryHasSpecificTerm(t *testing.T) {
	const (
		commonH = uint64(0xC011)
		rareH   = uint64(0xA11CE)
	)
	docs := make([]Doc, 0, 40)
	for i := 0; i < 40; i++ {
		docs = append(docs, coneTestDoc(
			fmt.Sprintf("common-%02d", i),
			int64(i*60),
			map[uint64]Term{commonH: coneTerm(commonH, 60, 0)},
		))
	}
	docs = append(docs, coneTestDoc("rare", 6000, map[uint64]Term{rareH: coneTerm(rareH, 220, 0)}))
	idx := BuildIndex(docs)
	if idx.N < 41 {
		t.Fatalf("corpus N=%d, want >= 41", idx.N)
	}
	high := idx.HighDFThreshold()
	if high <= 0 {
		t.Fatalf("high-DF threshold=%d, want > 0", high)
	}
	if queryHasSpecificTerm(nil, nil, high) || queryHasSpecificTerm(idx, nil, high) {
		t.Fatal("empty query reported a specific term")
	}
	commonOnly := map[uint64]Term{commonH: coneTerm(commonH, 60, 0)}
	if queryHasSpecificTerm(idx, commonOnly, high) {
		t.Fatalf("common-only query reported a specific term (df=%d highDF=%d)", idx.TermDF(commonH), high)
	}
	withRare := map[uint64]Term{
		commonH: coneTerm(commonH, 60, 0),
		rareH:   coneTerm(rareH, 220, 0),
	}
	if !queryHasSpecificTerm(idx, withRare, high) {
		t.Fatalf("rare term not recognized as specific (df=%d highDF=%d)", idx.TermDF(rareH), high)
	}
}

// TestEvalPairLocalMatchesSerial pins the worker-local pair evaluation used by
// the parallel endorsement pass to the serial evaluator: the shared-cache
// bypass must not change any returned metric.
func TestEvalPairLocalMatchesSerial(t *testing.T) {
	const (
		sharedH = uint64(0x5A5A)
		leftH   = uint64(0x1E71)
		rightH  = uint64(0x1E72)
	)
	docs := []Doc{
		coneTestDoc("left", 1000, map[uint64]Term{
			sharedH: coneTerm(sharedH, 120, 0),
			leftH:   coneTerm(leftH, 80, 0),
		}),
		coneTestDoc("right", 1060, map[uint64]Term{
			sharedH: coneTerm(sharedH, 120, 0),
			rightH:  coneTerm(rightH, 90, 0),
		}),
		coneTestDoc("third", 2000, map[uint64]Term{
			leftH:  coneTerm(leftH, 80, 0),
			rightH: coneTerm(rightH, 90, 0),
		}),
	}
	idx := BuildIndex(docs)
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	eng.ensureQueryScratch(idx.N, st.clusterN).resetProfilePools()
	high := idx.HighDFThreshold()
	profLeft := eng.buildUnitGroupProfile(st, 0, high)
	profRight := eng.buildUnitGroupProfile(st, 1, high)
	if len(profLeft.items) == 0 || len(profRight.items) == 0 {
		t.Fatalf("empty profiles: left=%d right=%d", len(profLeft.items), len(profRight.items))
	}
	qSet := map[uint64]struct{}{sharedH: {}}
	serial := eng.evalPairFromProfilesQ(st, 0, profLeft, 1, profRight, qSet)
	local := eng.evalPairFromProfilesQLocal(st, 0, profLeft, 1, profRight, qSet, &rmQueryScratch{})
	if serial != local {
		t.Fatalf("local pair evaluation differs from serial:\nserial=%+v\nlocal =%+v", serial, local)
	}
}
