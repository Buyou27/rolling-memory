// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"path/filepath"
	"testing"
)

func TestEngineNativeDeltaOverlayWithDiskClusterCSR(t *testing.T) {
	dir := t.TempDir()
	csrPath := filepath.Join(dir, "base.zcsr")

	baseDocs := []Doc{
		{
			UnitID: "u1", OccurredAt: 1700000100, Layer: "episodic",
			Terms:     map[uint64]Term{101: {Hash: 101, Bits: 40, Role: RolePrimary}, 102: {Hash: 102, Bits: 36, Role: RolePrimary}},
			Counts:    map[uint64]int{101: 1, 102: 1},
			Positions: map[uint64][]TermPosition{101: {{Sentence: 1, Slot: 1}}, 102: {{Sentence: 1, Slot: 2}}},
		},
		{
			UnitID: "u2", OccurredAt: 1700000200, Layer: "episodic",
			Terms:     map[uint64]Term{201: {Hash: 201, Bits: 42, Role: RolePrimary}},
			Counts:    map[uint64]int{201: 1},
			Positions: map[uint64][]TermPosition{201: {{Sentence: 1, Slot: 1}}},
		},
	}

	built := NewEngine(BuildIndex(baseDocs))
	built.MaterializeClusterState()
	if err := WriteDiskClusterCSR(csrPath, built); err != nil {
		t.Fatalf("WriteDiskClusterCSR: %v", err)
	}

	baseEng, err := OpenDiskClusterEngine(csrPath)
	if err != nil {
		t.Fatalf("OpenDiskClusterEngine: %v", err)
	}
	defer func() { _ = baseEng.Close() }()

	// 1. Overlay a newly inserted unit u3, an updated unit u1 (replacing term 101 with 301), and a deleted unit u2.
	deltaDocs := []Doc{
		{
			UnitID: "u1", OccurredAt: 1700000100, Layer: "episodic",
			Terms:     map[uint64]Term{301: {Hash: 301, Bits: 44, Role: RolePrimary}, 102: {Hash: 102, Bits: 36, Role: RolePrimary}},
			Counts:    map[uint64]int{301: 1, 102: 1},
			Positions: map[uint64][]TermPosition{301: {{Sentence: 1, Slot: 1}}, 102: {{Sentence: 1, Slot: 2}}},
		},
		{
			UnitID: "u3", OccurredAt: 1700000300, Layer: "episodic",
			Terms:     map[uint64]Term{401: {Hash: 401, Bits: 48, Role: RolePrimary}},
			Counts:    map[uint64]int{401: 1},
			Positions: map[uint64][]TermPosition{401: {{Sentence: 1, Slot: 1}}},
		},
	}
	overlay := baseEng.OverlayDelta(deltaDocs, []string{"u2"})
	if !overlay.HasDelta() || overlay.DeltaDocCount() != 2 {
		t.Fatalf("expected active delta overlay with 2 docs, got has=%v count=%d", overlay.HasDelta(), overlay.DeltaDocCount())
	}

	// Stale atom 101 from u1's old baseline version must be suppressed.
	hitsOld := overlay.SearchWithPositions(
		map[uint64]Term{101: {Hash: 101, Bits: 40, Role: RolePrimary}},
		map[uint64][]TermPosition{101: {{Sentence: 1, Slot: 1}}},
		10,
	)
	if len(hitsOld) != 0 {
		t.Fatalf("stale atom 101 from replaced u1 must be masked, got %v", hitsOld)
	}

	// Deleted unit u2 (atom 201) must be suppressed.
	hitsDeleted := overlay.SearchWithPositions(
		map[uint64]Term{201: {Hash: 201, Bits: 42, Role: RolePrimary}},
		map[uint64][]TermPosition{201: {{Sentence: 1, Slot: 1}}},
		10,
	)
	if len(hitsDeleted) != 0 {
		t.Fatalf("deleted unit u2 must be masked, got %v", hitsDeleted)
	}
	if overlay.ForEachUnitDocEntry("u2", nil) {
		t.Fatalf("ForEachUnitDocEntry must return false for masked deleted unit u2")
	}

	// Fresh atom 301 on updated u1 and 401 on newly inserted u3 must hit immediately.
	hitsNew := overlay.SearchWithPositions(
		map[uint64]Term{
			301: {Hash: 301, Bits: 44, Role: RolePrimary},
			401: {Hash: 401, Bits: 48, Role: RolePrimary},
		},
		map[uint64][]TermPosition{
			301: {{Sentence: 1, Slot: 1}},
			401: {{Sentence: 1, Slot: 2}},
		},
		10,
	)
	if len(hitsNew) != 2 {
		t.Fatalf("expected 2 hits (u1 and u3) from delta overlay, got %v", hitsNew)
	}

	// ForEachUnitDocEntry on u1 must observe the updated term 301, never stale 101.
	seenTerms := make(map[uint64]bool)
	if !overlay.ForEachUnitDocEntry("u1", func(ev DocEntryView) { seenTerms[ev.ID] = true }) {
		t.Fatalf("ForEachUnitDocEntry must succeed on updated u1")
	}
	if !seenTerms[301] || seenTerms[101] {
		t.Fatalf("ForEachUnitDocEntry on u1 returned unexpected terms: %v", seenTerms)
	}

	// ExportDocs on overlay must return the exact active set (updated u1 + new u3, without deleted u2).
	exported := overlay.ExportDocs()
	if len(exported) != 2 || exported[0].UnitID != "u1" || exported[1].UnitID != "u3" {
		t.Fatalf("unexpected ExportDocs from overlay: %+v", exported)
	}

	// Immutable baseline engine must remain untouched.
	if baseEng.HasDelta() {
		t.Fatalf("OverlayDelta must not mutate the underlying baseline engine")
	}
}
