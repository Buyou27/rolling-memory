// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"fmt"
	"math"
	"testing"
)

// coneTestDoc builds a unit carrying the given term hashes at the given clock.
func coneTestDoc(id string, ts int64, terms map[uint64]Term) Doc {
	counts := make(map[uint64]int, len(terms))
	positions := make(map[uint64][]TermPosition, len(terms))
	slot := 1
	for h := range terms {
		counts[h] = 1
		positions[h] = []TermPosition{{Sentence: 0, Slot: slot}}
		slot += 2
	}
	return Doc{UnitID: id, OccurredAt: ts, Terms: terms, Counts: counts, Positions: positions}
}

func coneTerm(h uint64, bits uint16, flags uint8) Term {
	return Term{Hash: h, Bits: bits, Flags: flags, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: h, Sent: 1, Slot: 1}
}

// TestAnchorConeClockVsDayGrid verifies that anchor-cone membership depends on
// clock distance to the nearest proper-anchor unit, never on UTC day-cell
// identity. The test sets the two mechanisms in direct opposition on a dense
// timeline (300s filler cadence keeps tau_macro at minutes, so the bucket
// expansion bridges neither witness):
//
//   - "overnight": 20 min across midnight from the anchor — clock window 1.0,
//     day grid 1/e (different UTC day);
//   - "same-day-far": 23h20m before the anchor on the same UTC day — clock
//     window 1/e (|dt| > T/2), day grid 1.0 (same cell).
//
// Equal lexical mass makes the gate the only separator, so the clock window
// must rank overnight strictly above same-day-far (the day grid ranks them in
// the opposite order), and the full ranked output must be invariant under
// global time translations (+2h flips the anchor's day cell, which inverts
// the day grid's verdict while the clock window cannot move).
func TestAnchorConeClockVsDayGrid(t *testing.T) {
	const (
		anchorH = uint64(0xA11C0)
		shellH  = uint64(0x5E11)
		noiseH  = uint64(0x0153)
	)
	midnight := int64(1700006400)     // exact UTC midnight
	properFlags := uint8(1<<0 | 1<<1) // rmBitUpper | rmBitCamel (proper-noun anchor mask)

	build := func(shift int64) []Hit {
		var docs []Doc
		// Dense background stream (300s cadence, 10h) pins tau_macro to the
		// minute scale so neither witness is reachable through the timed
		// bucket expansion; only the membership rule under test decides.
		for i := 0; i < 120; i++ {
			docs = append(docs, coneTestDoc(
				fmt.Sprintf("bg-%03d", i),
				midnight-12*3600+int64(i)*300+shift,
				map[uint64]Term{noiseH: coneTerm(noiseH, 50, 0)},
			))
		}
		docs = append(docs,
			// Proper anchor ten minutes before midnight.
			coneTestDoc("anchor", midnight-600+shift, map[uint64]Term{
				anchorH: coneTerm(anchorH, 220, properFlags),
			}),
			// Clock-near witness across the midnight cell edge.
			coneTestDoc("overnight", midnight+600+shift, map[uint64]Term{
				shellH: coneTerm(shellH, 60, 0),
			}),
			// Clock-far witness inside the anchor's UTC day cell.
			coneTestDoc("same-day-far", midnight-84000+shift, map[uint64]Term{
				shellH: coneTerm(shellH, 60, 0),
			}),
		)
		idx := BuildIndex(docs)
		q := map[uint64]Term{
			anchorH: coneTerm(anchorH, 220, properFlags),
			shellH:  coneTerm(shellH, 60, 0),
		}
		return NewEngine(idx).Search(q, 8)
	}

	energyOf := func(hits []Hit, id string) float64 {
		for _, h := range hits {
			if h.UnitID == id {
				return h.Energy
			}
		}
		t.Fatalf("unit %s missing from hits", id)
		return math.NaN()
	}

	ref := build(0)
	eOver, eFar := energyOf(ref, "overnight"), energyOf(ref, "same-day-far")
	// Clock semantics: the 20-minute witness strictly outranks the 23h20m
	// witness. The day grid outputs the inverted order here.
	if eOver <= eFar {
		t.Fatalf("clock-near witness not above clock-far same-day witness (day-grid inversion): overnight=%.12f same-day-far=%.12f", eOver, eFar)
	}
	// Strict time-shift invariance of the full ranked output.
	for _, shift := range []int64{2 * 3600, 7*3600 + 13*60, 86399} {
		got := build(shift)
		if len(got) != len(ref) {
			t.Fatalf("hit count changed under +%ds: %d vs %d", shift, len(got), len(ref))
		}
		for i := range ref {
			if got[i].UnitID != ref[i].UnitID || math.Abs(got[i].Energy-ref[i].Energy) > 1e-9 {
				t.Fatalf("ranking not shift-invariant under +%ds at %d: %s/%.12f vs %s/%.12f",
					shift, i, ref[i].UnitID, ref[i].Energy, got[i].UnitID, got[i].Energy)
			}
		}
	}
}
