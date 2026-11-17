// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "testing"

func TestStandaloneRelationOperatorsResetProfilePoolsAdversarial(t *testing.T) {
	docs := []Doc{
		{
			UnitID:     "u1",
			OccurredAt: 1000,
			Terms: map[uint64]Term{
				101: {Hash: 101, Group: 101, Bits: 60, Flags: 1},
				102: {Hash: 102, Group: 102, Bits: 70, Flags: 1},
			},
			Counts: map[uint64]int{101: 1, 102: 1},
			Positions: map[uint64][]TermPosition{
				101: {{Sentence: 0, Slot: 0}},
				102: {{Sentence: 0, Slot: 1}},
			},
		},
		{
			UnitID:     "u2",
			OccurredAt: 1010,
			Terms: map[uint64]Term{
				101: {Hash: 101, Group: 101, Bits: 60, Flags: 1},
				103: {Hash: 103, Group: 103, Bits: 80, Flags: 1},
				104: {Hash: 104, Group: 104, Bits: 85, Flags: 1},
			},
			Counts: map[uint64]int{101: 1, 103: 1, 104: 1},
			Positions: map[uint64][]TermPosition{
				101: {{Sentence: 0, Slot: 0}},
				103: {{Sentence: 0, Slot: 1}},
				104: {{Sentence: 0, Slot: 2}},
			},
		},
	}
	eng := NewEngine(BuildIndex(docs))

	if _, ok := eng.EvaluateUnitPairRelation("u1", "u2"); !ok {
		t.Fatalf("EvaluateUnitPairRelation failed")
	}
	firstPairItems := len(eng.queryScratch.profileItemPool)
	firstPairPos := len(eng.queryScratch.profilePosPool)
	if firstPairItems == 0 || firstPairPos == 0 {
		t.Fatalf("expected non-empty profile pools after first call, got items=%d pos=%d", firstPairItems, firstPairPos)
	}

	for i := 0; i < 50; i++ {
		if _, ok := eng.EvaluateUnitPairRelation("u1", "u2"); !ok {
			t.Fatalf("EvaluateUnitPairRelation iter %d failed", i)
		}
	}
	if got := len(eng.queryScratch.profileItemPool); got != firstPairItems {
		t.Fatalf("EvaluateUnitPairRelation leaked profileItemPool across standalone calls: got len=%d want %d", got, firstPairItems)
	}
	if got := len(eng.queryScratch.profilePosPool); got != firstPairPos {
		t.Fatalf("EvaluateUnitPairRelation leaked profilePosPool across standalone calls: got len=%d want %d", got, firstPairPos)
	}

	if _, ok := eng.InspectUnitRelations("u1", 10); !ok {
		t.Fatalf("InspectUnitRelations failed")
	}
	firstInspectItems := len(eng.queryScratch.profileItemPool)
	for i := 0; i < 50; i++ {
		if _, ok := eng.InspectUnitRelations("u1", 10); !ok {
			t.Fatalf("InspectUnitRelations iter %d failed", i)
		}
	}
	if got := len(eng.queryScratch.profileItemPool); got != firstInspectItems {
		t.Fatalf("InspectUnitRelations leaked profileItemPool across standalone calls: got len=%d want %d", got, firstInspectItems)
	}

	hits := []Hit{{UnitID: "u1", Energy: 10}, {UnitID: "u2", Energy: 8}}
	_ = eng.GroupHitsByClusterFractalWithQuery(hits, 16, nil)
	firstGroupItems := len(eng.queryScratch.profileItemPool)
	for i := 0; i < 50; i++ {
		_ = eng.GroupHitsByClusterFractalWithQuery(hits, 16, nil)
	}
	if got := len(eng.queryScratch.profileItemPool); got != firstGroupItems {
		t.Fatalf("GroupHitsByClusterFractalWithQuery leaked profileItemPool across standalone calls: got len=%d want %d", got, firstGroupItems)
	}

	_ = eng.ApplyUnitRelationMarginalFilter(hits, nil)
	firstFilterItems := len(eng.queryScratch.profileItemPool)
	for i := 0; i < 50; i++ {
		_ = eng.ApplyUnitRelationMarginalFilter(hits, nil)
	}
	if got := len(eng.queryScratch.profileItemPool); got != firstFilterItems {
		t.Fatalf("ApplyUnitRelationMarginalFilter leaked profileItemPool across standalone calls: got len=%d want %d", got, firstFilterItems)
	}
}
