// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// 宽 (句, 槽) 坐标超出 16 位打包范围时必须经精确坐标分支判邻近，
// 远端句对不得因 16 位钳位而获得同句共现增益。
func TestWidePositionNoFalseAdjacencyAdversarial(t *testing.T) {
	build := func(wide bool) *Engine {
		positions := map[uint64][]TermPosition{
			1: {{Sentence: 0, Slot: 1}},
			2: {{Sentence: 0, Slot: 2}},
		}
		if wide {
			positions = map[uint64][]TermPosition{
				1: {{Sentence: 70000, Slot: 1}},
				2: {{Sentence: 80000, Slot: 2}},
			}
		}
		doc := Doc{
			UnitID:     "a",
			OccurredAt: 100,
			Terms: map[uint64]Term{
				1: {Hash: 1, Group: 1, Bits: 10, Flags: 1},
				2: {Hash: 2, Group: 2, Bits: 12, Flags: 1},
			},
			Counts:    map[uint64]int{1: 1, 2: 1},
			Positions: positions,
		}
		return NewEngine(BuildIndex([]Doc{doc}))
	}
	qTerms := map[uint64]Term{1: {Hash: 1}, 2: {Hash: 2}}
	qPos := map[uint64][]TermPosition{
		1: {{Sentence: 0, Slot: 1}},
		2: {{Sentence: 0, Slot: 2}},
	}
	wideHits := build(true).SearchWithPositions(qTerms, qPos, 10)
	adjHits := build(false).SearchWithPositions(qTerms, qPos, 10)
	if len(wideHits) == 0 || len(adjHits) == 0 {
		t.Fatalf("expected hits from both engines: wide=%v adj=%v", wideHits, adjHits)
	}
	if wideHits[0].Energy >= adjHits[0].Energy {
		t.Fatalf("wide-separated pair scored >= same-sentence adjacent pair: wide=%.6f adj=%.6f (false adjacency not eliminated)",
			wideHits[0].Energy, adjHits[0].Energy)
	}
}

// 段头计数在长度算术之前必须做可代表上界校验：+2^62 级超界计数曾以整数溢出绕过
// 布局比对并触发 unsafe.Slice 越界 panic，读取端必须在校验和自洽前提下直接拒绝。
func TestDiskParseRejectsOverflowingSectionCountsAdversarial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rm_overflow_guard.zcsr")
	eng := NewEngine(BuildIndex([]Doc{{
		UnitID:     "a",
		OccurredAt: 1,
		Terms:      map[uint64]Term{1: {Hash: 1}},
		Counts:     map[uint64]int{1: 1},
	}}))
	if err := WriteDiskClusterCSR(path, eng); err != nil {
		t.Fatalf("WriteDiskClusterCSR: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	table := crc32.MakeTable(crc32.Castagnoli)
	for _, tc := range []struct {
		name string
		off  int
	}{
		{"docCount", 8},
		{"entryCount", 24},
		{"posCount", 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mut := append([]byte(nil), raw...)
			orig := binary.LittleEndian.Uint64(mut[tc.off : tc.off+8])
			binary.LittleEndian.PutUint64(mut[tc.off:tc.off+8], orig+(uint64(1)<<62))
			binary.LittleEndian.PutUint32(mut[zcsrHeaderCRCOffset:zcsrHeaderCRCOffset+4], crc32.Checksum(mut[0:zcsrHeaderCRCOffset], table))
			if err := os.WriteFile(path, mut, 0o600); err != nil {
				t.Fatalf("write mutated segment: %v", err)
			}
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("open panicked instead of rejecting: %v", r)
				}
			}()
			if _, err := OpenDiskClusterEngine(path); err == nil {
				t.Fatalf("expected overflowing %s to be rejected", tc.name)
			}
		})
	}
}
