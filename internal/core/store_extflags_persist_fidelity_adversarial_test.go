// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"path/filepath"
	"testing"
)

// B05 回归：SQLite 往返必须恢复完整 16-bit ExtFlags（persist 列位 16-31 承载），
// LoadIndex 与 BuildIndex 的语料摘要逐位一致；低 3 位掩码值保持不入 UnitExtFlags
// （与 BuildIndex 的高位过滤同构）；旧行缺位自然读作零（向后兼容）。
func TestStoreExtFlagsHighBitsPersistRoundtripAdversarial(t *testing.T) {
	const hi uint16 = 1 << 9
	docs := []Doc{{
		UnitID:     "u1",
		OccurredAt: 1700000000,
		Terms: map[uint64]Term{
			11: {Hash: 11, Group: 11, Bits: 48, Flags: 1, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary | hi, Sent: 1, Slot: 1},
			22: {Hash: 22, Group: 22, Bits: 32, Flags: 1, Role: RoleMorphStem, ExtFlags: ExtFlagSeenSubword, Sent: 1, Slot: 2},
		},
		Counts:    map[uint64]int{11: 1, 22: 1},
		Positions: map[uint64][]TermPosition{11: {{Sentence: 0, Slot: 1}}, 22: {{Sentence: 0, Slot: 2}}},
	}}
	mem := BuildIndex(docs)
	st, err := OpenStore(filepath.Join(t.TempDir(), "extflags-fidelity.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Ingest(docs); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if wantDigest, gotDigest := ComputeCorpusDigest(mem), ComputeCorpusDigest(got); wantDigest != gotDigest {
		t.Fatalf("corpus digest divergence after store roundtrip: BuildIndex=%016x LoadIndex=%016x", wantDigest, gotDigest)
	}
	if len(got.UnitExtFlags) != 1 || got.UnitExtFlags[0][11] != ExtFlagSeenPrimary|hi {
		t.Fatalf("UnitExtFlags[0][11]=%#x want %#x", got.UnitExtFlags[0][11], ExtFlagSeenPrimary|hi)
	}
	if _, exists := got.UnitExtFlags[0][22]; exists {
		t.Fatalf("low-3-only ExtFlags must stay out of UnitExtFlags")
	}
}
