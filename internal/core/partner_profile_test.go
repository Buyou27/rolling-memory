// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "testing"

// TestTermEnergyProfileWithoutDualOrder covers both partner arms after the dual-order
// index was removed from the ingest/cold-start path: the canonical left term uses the
// binary-search arm and the right term uses the on-demand linear scan.
func TestTermEnergyProfileWithoutDualOrder(t *testing.T) {
	const a, b uint64 = 0x7101, 0x7102
	mk := func(id string, ts int64) Doc {
		return Doc{
			UnitID:     id,
			OccurredAt: ts,
			Terms: map[uint64]Term{
				a: {Bits: 40, Group: a, Role: RolePrimary, Sent: 0, Slot: 1},
				b: {Bits: 40, Group: b, Role: RolePrimary, Sent: 0, Slot: 2},
			},
			Counts: map[uint64]int{a: 1, b: 1},
			Positions: map[uint64][]TermPosition{
				a: {{Sentence: 0, Slot: 1}},
				b: {{Sentence: 0, Slot: 2}},
			},
		}
	}
	eng := NewEngine(BuildIndex([]Doc{mk("pair-1", 1700000000), mk("pair-2", 1700003600)}))

	left, ok := eng.TermEnergyVector(a)
	if !ok {
		t.Fatal("TermEnergyVector(a) failed")
	}
	if _, found := left.PartnerVector.Lookup(b); !found {
		t.Fatalf("a-side partner arm missing %d: %+v", b, left.PartnerVector.Entries)
	}

	right, ok := eng.TermEnergyVector(b)
	if !ok {
		t.Fatal("TermEnergyVector(b) failed")
	}
	if _, found := right.PartnerVector.Lookup(a); !found {
		t.Fatalf("b-side partner arm missing %d: %+v", a, right.PartnerVector.Entries)
	}
}
