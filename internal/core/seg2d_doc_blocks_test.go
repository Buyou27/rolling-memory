// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"testing"
)

// docEntryLogicalEqual compares every rmDocEntry field except posOff: the block
// rows carry block-local offsets, so posOff is compared through positionsOf.
func docEntryLogicalEqual(a, b rmDocEntry) bool {
	return a.id == b.id && a.group == b.group && a.posLen == b.posLen &&
		a.tf == b.tf && a.bits == b.bits && a.flags == b.flags && a.roleExt == b.roleExt
}

func assertUint32SliceEqual(t *testing.T, label string, got, want []uint32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s len %d != %d", label, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] %d != %d", label, i, got[i], want[i])
		}
	}
}

// TestSeg2DBlockDocEntryParity is stage 2d-1b-a/2d-1c-a: the doc-entry blocks
// must reproduce every unit's row and positions value for value, the
// reassembled position pool must equal the state pool, and the query-time
// DocMass recompute must match Index.DocMass bit for bit at any block width.
func TestSeg2DBlockDocEntryParity(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpus(seg2dBlockUnits))
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	if st == nil {
		t.Fatal("cluster state is nil")
	}
	db := buildDocBlocksFromState(st, idx, seg2dBlockMaxUnits(idx.N, 8))
	if db == nil {
		t.Fatal("doc blocks are nil")
	}
	if len(db.bounds) != len(db.blocks)+1 || db.bounds[0] != 0 || int(db.bounds[len(db.bounds)-1]) != idx.N {
		t.Fatalf("bounds %v for N=%d", db.bounds, idx.N)
	}
	for i := 1; i < len(db.bounds); i++ {
		if db.bounds[i] <= db.bounds[i-1] {
			t.Fatalf("bounds not strictly increasing at %d: %v", i, db.bounds)
		}
	}
	entries := 0
	for u := int32(0); u < int32(idx.N); u++ {
		want := st.docEntriesOf(u)
		got, ok := db.docEntriesOf(u)
		if !ok {
			t.Fatalf("unit %d missing from the block table", u)
		}
		if len(got) != len(want) {
			t.Fatalf("unit %d entries %d != %d", u, len(got), len(want))
		}
		for i := range want {
			if !docEntryLogicalEqual(got[i], want[i]) {
				t.Fatalf("unit %d entry %d %+v != %+v", u, i, got[i], want[i])
			}
			gotPos, ok := db.positionsOf(u, got[i])
			if !ok {
				t.Fatalf("unit %d entry %d positions missing", u, i)
			}
			assertUint32SliceEqual(t, "positions", gotPos, st.entryPositionsOf(want[i]))
		}
		entries += len(got)
		mass, ok := db.docMassOf(idx, u)
		if !ok {
			t.Fatalf("unit %d docMass missing", u)
		}
		if math.Float64bits(mass) != math.Float64bits(idx.DocMass[u]) {
			t.Fatalf("unit %d docMass bits %016x != %016x", u, math.Float64bits(mass), math.Float64bits(idx.DocMass[u]))
		}
	}
	assertUint32SliceEqual(t, "assembled posPool", db.assemblePosPool(), st.posPool)
	if wide := db.assembleWidePos(); len(wide) != len(st.widePos) {
		t.Fatalf("assembled widePos entries %d != %d", len(wide), len(st.widePos))
	}
	t.Logf("doc-entry parity: units=%d entries=%d blocks=%d positions=%d", idx.N, entries, len(db.blocks), len(st.posPool))
}

// TestSeg2DBlockDocEntrySingleUnitBlocks pins the degenerate block width: one
// unit per block must stay exact (no empty blocks, no false rows).
func TestSeg2DBlockDocEntrySingleUnitBlocks(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpus(37))
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	if st == nil {
		t.Fatal("cluster state is nil")
	}
	db := buildDocBlocksFromState(st, idx, 1)
	if db == nil {
		t.Fatal("doc blocks are nil")
	}
	if len(db.blocks) != idx.N {
		t.Fatalf("blocks=%d want N=%d", len(db.blocks), idx.N)
	}
	for u := int32(0); u < int32(idx.N); u++ {
		want := st.docEntriesOf(u)
		got, ok := db.docEntriesOf(u)
		if !ok || len(got) != len(want) {
			t.Fatalf("unit %d entries ok=%v len=%d want %d", u, ok, len(got), len(want))
		}
		for i := range want {
			if !docEntryLogicalEqual(got[i], want[i]) {
				t.Fatalf("unit %d entry %d mismatch", u, i)
			}
			gotPos, ok := db.positionsOf(u, got[i])
			if !ok {
				t.Fatalf("unit %d entry %d positions missing", u, i)
			}
			assertUint32SliceEqual(t, "positions", gotPos, st.entryPositionsOf(want[i]))
		}
		if _, ok := db.docMassOf(idx, u); !ok {
			t.Fatalf("unit %d docMass missing", u)
		}
	}
	assertUint32SliceEqual(t, "assembled posPool", db.assemblePosPool(), st.posPool)
}

// TestSeg2DBlockDocEntryWidePositions pins the wide-coordinate rebasing on a
// hand-built state, since the synthetic corpus stays inside packed coordinates.
func TestSeg2DBlockDocEntryWidePositions(t *testing.T) {
	st := &rmClusterState{
		docRowPtr: []uint32{0, 2},
		docEntriesFlat: []rmDocEntry{
			{id: 0x11, posOff: 0, posLen: 2},
			{id: 0x22, posOff: 2, posLen: 1},
		},
		posPool: []uint32{1<<16 | 3, 2<<16 | 4, 5<<16 | 6},
		widePos: map[uint32]TermPosition{1: {Sentence: 70000, Slot: 3}},
	}
	idx := &Index{N: 1}
	db := buildDocBlocksFromState(st, idx, 1)
	if db == nil {
		t.Fatal("doc blocks are nil")
	}
	entries, ok := db.docEntriesOf(0)
	if !ok || len(entries) != 2 {
		t.Fatalf("entries ok=%v len=%d want 2", ok, len(entries))
	}
	if entries[0].posOff != 0 || entries[1].posOff != 2 {
		t.Fatalf("block-local offsets %d/%d want 0/2", entries[0].posOff, entries[1].posOff)
	}
	pos, ok := db.positionsOf(0, entries[0])
	if !ok {
		t.Fatal("positions missing")
	}
	assertUint32SliceEqual(t, "positions", pos, []uint32{1<<16 | 3, 2<<16 | 4})
	if sent, ok := db.entrySentExactAt(0, entries[0], 1); !ok || sent != 70000 {
		t.Fatalf("wide sentence=%d ok=%v want 70000", sent, ok)
	}
	if sent, ok := db.entrySentExactAt(0, entries[1], 0); !ok || sent != 5 {
		t.Fatalf("packed sentence=%d ok=%v want 5", sent, ok)
	}
	assertUint32SliceEqual(t, "assembled posPool", db.assemblePosPool(), st.posPool)
	wide := db.assembleWidePos()
	if got, ok := wide[1]; !ok || got != (TermPosition{Sentence: 70000, Slot: 3}) {
		t.Fatalf("assembled widePos[1]=%+v ok=%v", got, ok)
	}
}

// TestSeg2DBlockDocEntryWidePositionsAcrossBlocks covers the global-coordinate
// reassembly boundary: each block starts its local position offsets from zero,
// so matching local widePos keys in two blocks must map to different global keys.
func TestSeg2DBlockDocEntryWidePositionsAcrossBlocks(t *testing.T) {
	st := &rmClusterState{
		docRowPtr: []uint32{0, 1, 2},
		docEntriesFlat: []rmDocEntry{
			{id: 0x11, posOff: 0, posLen: 2},
			{id: 0x22, posOff: 2, posLen: 2},
		},
		posPool: []uint32{1<<16 | 3, 2<<16 | 4, 5<<16 | 6, 6<<16 | 7},
		widePos: map[uint32]TermPosition{
			1: {Sentence: 70000, Slot: 3},
			3: {Sentence: 70001, Slot: 7},
		},
	}
	idx := &Index{N: 2}
	db := buildDocBlocksFromState(st, idx, 1)
	if db == nil || len(db.blocks) != 2 {
		t.Fatalf("doc blocks=%d, want 2", len(db.blocks))
	}
	for u, wantSentence := range map[int32]int{0: 70000, 1: 70001} {
		entries, ok := db.docEntriesOf(u)
		if !ok || len(entries) != 1 {
			t.Fatalf("unit %d entries ok=%v len=%d want 1", u, ok, len(entries))
		}
		if sent, ok := db.entrySentExactAt(u, entries[0], 1); !ok || sent != wantSentence {
			t.Fatalf("unit %d wide sentence=%d ok=%v want %d", u, sent, ok, wantSentence)
		}
	}
	if _, ok := db.blocks[0].widePos[1]; !ok {
		t.Fatal("first block lacks its local wide offset")
	}
	if _, ok := db.blocks[1].widePos[1]; !ok {
		t.Fatal("second block lacks its local wide offset")
	}
	assertUint32SliceEqual(t, "assembled posPool", db.assemblePosPool(), st.posPool)
	wide := db.assembleWidePos()
	if len(wide) != len(st.widePos) {
		t.Fatalf("assembled widePos entries %d != %d", len(wide), len(st.widePos))
	}
	for offset, want := range st.widePos {
		if got, ok := wide[offset]; !ok || got != want {
			t.Fatalf("assembled widePos[%d]=%+v ok=%v want %+v", offset, got, ok, want)
		}
	}
}

// seg2dBlockCorpusWithPositions gives every doc explicit (sentence, slot)
// positions so the block position-pool rebasing is exercised at scale.
func seg2dBlockCorpusWithPositions(n int) []Doc {
	docs := seg2dBlockCorpus(n)
	for i := range docs {
		positions := make(map[uint64][]TermPosition, len(docs[i].Terms))
		count := 0
		for h := range docs[i].Terms {
			sentence := (i + int(h&0xFFFF)) % 9
			positions[h] = []TermPosition{
				{Sentence: sentence, Slot: 0},
				{Sentence: (sentence + 2) % 9, Slot: 1 + count%3},
			}
			count++
		}
		docs[i].Positions = positions
	}
	return docs
}

// TestSeg2DBlockDocEntryPositionedParity repeats the row parity with real
// positions at three block widths, so the per-block position pool and the
// reassembled global pool are both pinned.
func TestSeg2DBlockDocEntryPositionedParity(t *testing.T) {
	corpus := seg2dBlockCorpusWithPositions(96)
	idx := BuildIndex(corpus)
	st := NewEngine(idx).ensureClusterState()
	if st == nil {
		t.Fatal("cluster state is nil")
	}
	if len(st.posPool) == 0 {
		t.Fatal("fixture produced no positions")
	}
	for _, blocks := range []int{1, 3, 8} {
		db := buildDocBlocksFromState(st, idx, seg2dBlockMaxUnits(idx.N, blocks))
		if db == nil {
			t.Fatalf("blocks=%d: nil", blocks)
		}
		for u := int32(0); u < int32(idx.N); u++ {
			want := st.docEntriesOf(u)
			got, ok := db.docEntriesOf(u)
			if !ok || len(got) != len(want) {
				t.Fatalf("blocks=%d unit %d entries ok=%v len=%d want %d", blocks, u, ok, len(got), len(want))
			}
			for i := range want {
				if !docEntryLogicalEqual(got[i], want[i]) {
					t.Fatalf("blocks=%d unit %d entry %d mismatch", blocks, u, i)
				}
				gotPos, ok := db.positionsOf(u, got[i])
				if !ok {
					t.Fatalf("blocks=%d unit %d entry %d positions missing", blocks, u, i)
				}
				assertUint32SliceEqual(t, "positions", gotPos, st.entryPositionsOf(want[i]))
			}
			mass, ok := db.docMassOf(idx, u)
			if !ok || math.Float64bits(mass) != math.Float64bits(idx.DocMass[u]) {
				t.Fatalf("blocks=%d unit %d docMass mismatch", blocks, u)
			}
		}
		assertUint32SliceEqual(t, "assembled posPool", db.assemblePosPool(), st.posPool)
	}
	t.Logf("positioned parity: units=%d positions=%d", idx.N, len(st.posPool))
}

// TestSeg2DBlockDocEntryDegenerate covers the empty and single-unit states.
func TestSeg2DBlockDocEntryDegenerate(t *testing.T) {
	if buildDocBlocksFromState(nil, nil, 1) != nil {
		t.Fatal("nil inputs must return nil")
	}
	idx := BuildIndex(nil)
	if idx == nil {
		t.Fatal("BuildIndex(nil) returned nil")
	}
	st := NewEngine(idx).ensureClusterState()
	if st != nil {
		if db := buildDocBlocksFromState(st, idx, 4); db != nil {
			t.Fatalf("empty corpus blocks=%d", len(db.blocks))
		}
	}
}
