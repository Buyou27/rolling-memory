// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"path/filepath"
	"testing"
)

// Close() 必须在 munmap 之前把仍指向映射区的词表视图（TermKeys / rawTermDF/Mass/Bits/Flags）
// 克隆到 Go 堆，并停用持有 mmap 倒排池的 csrPostingUnits 闭包；否则卸载映射后任何
// TermDFOf / ForEachPostingUnit 读取都会触碰已解除的地址空间（SIGSEGV）。
func TestEngineCloseMmapViewCloneAdversarial(t *testing.T) {
	docs := []Doc{
		{
			UnitID:     "u1",
			OccurredAt: 100,
			Terms:      map[uint64]Term{11: {Hash: 11, Group: 11, Bits: 40, Flags: 1}},
			Counts:     map[uint64]int{11: 2},
			Positions:  map[uint64][]TermPosition{11: {{Sentence: 0, Slot: 1}}},
		},
		{
			UnitID:     "u2",
			OccurredAt: 200,
			Terms: map[uint64]Term{
				11: {Hash: 11, Group: 11, Bits: 40, Flags: 1},
				22: {Hash: 22, Group: 22, Bits: 60, Flags: 1},
			},
			Counts:    map[uint64]int{11: 1, 22: 1},
			Positions: map[uint64][]TermPosition{22: {{Sentence: 0, Slot: 2}}},
		},
	}
	engMem := NewEngine(BuildIndex(docs))
	_ = engMem.ensureClusterState()
	path := filepath.Join(t.TempDir(), "close.zcsr")
	if err := WriteDiskClusterCSR(path, engMem); err != nil {
		t.Fatalf("WriteDiskClusterCSR: %v", err)
	}
	disk, err := OpenDiskClusterEngine(path)
	if err != nil {
		t.Fatalf("OpenDiskClusterEngine: %v", err)
	}
	idx := disk.Index
	wantDF := idx.TermDFOf(11)
	wantBits := idx.TermBitsOf(11)
	wantKeys := len(idx.TermKeys)
	unitsBefore := 0
	idx.ForEachPostingUnit(11, func(int32) { unitsBefore++ })
	if wantDF <= 0 || wantBits == 0 || wantKeys == 0 || unitsBefore == 0 {
		t.Fatalf("disk index pre-Close incomplete: df=%d bits=%d keys=%d units=%d", wantDF, wantBits, wantKeys, unitsBefore)
	}
	if err := disk.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Post-Close reads must stay in the Go heap: no access to unmapped memory.
	if got := idx.TermDFOf(11); got != wantDF {
		t.Fatalf("post-Close TermDFOf(11)=%d want %d", got, wantDF)
	}
	if got := idx.TermBitsOf(11); got != wantBits {
		t.Fatalf("post-Close TermBitsOf(11)=%d want %d", got, wantBits)
	}
	if got := len(idx.TermKeys); got != wantKeys {
		t.Fatalf("post-Close TermKeys len=%d want %d", got, wantKeys)
	}
	unitsAfter := 0
	idx.ForEachPostingUnit(11, func(int32) { unitsAfter++ })
	if unitsAfter != 0 {
		t.Fatalf("post-Close posting iteration must degrade to empty on the dead engine, got %d", unitsAfter)
	}
	if len(idx.UnitIDs) == 0 || idx.UnitIDs[0] == "" {
		t.Fatalf("post-Close UnitIDs must remain readable: %+v", idx.UnitIDs)
	}
}
