// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"fmt"
	"testing"
)

func phrasePairDocAt(i int) Doc {
	const a, b uint64 = 0x8101, 0x8102
	return Doc{
		UnitID:     fmt.Sprintf("pair-%05d", i),
		OccurredAt: 1700000000 + int64(i)*60,
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

func phrasePairDocs(n int) []Doc {
	docs := make([]Doc, n)
	for i := range docs {
		docs[i] = phrasePairDocAt(i)
	}
	return docs
}

// TestPhraseDeltaAppendMatchesFullRebuild pins the base+delta append path: the shared base
// plus the append delta must expose exactly the postings a full rebuild produces.
func TestPhraseDeltaAppendMatchesFullRebuild(t *testing.T) {
	const baseDocs = 64
	base := make([]Doc, baseDocs)
	for i := range base {
		base[i] = seg2aMeasureDoc(i)
	}
	extra := seg2aMeasureDoc(baseDocs)
	eng := NewEngine(BuildIndex(base))
	eng.ensureClusterState()
	eng.AppendDocs([]Doc{extra})
	st := eng.ensureClusterState()
	if len(st.phraseDelta) == 0 {
		t.Fatal("append did not take the base+delta path")
	}

	corpus := append(append([]Doc(nil), base...), extra)
	full := NewEngine(BuildIndex(corpus))
	fullState := full.ensureClusterState()

	pair := st.phraseDelta[0].pair
	if got, want := st.phrasePostingCount(pair), len(fullState.phraseBasePostings(pair)); got != want {
		t.Fatalf("pair (%d,%d) count=%d, full rebuild=%d", pair.a, pair.b, got, want)
	}
	var got []int32
	st.visitPhrasePostings(pair, func(u int32) bool {
		got = append(got, u)
		return true
	})
	want := fullState.phraseBasePostings(pair)
	if len(got) != len(want) {
		t.Fatalf("pair (%d,%d) visited=%d, full rebuild=%d", pair.a, pair.b, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pair (%d,%d) unit %d = %d, full rebuild %d", pair.a, pair.b, i, got[i], want[i])
		}
	}
}

// TestPhrasePostingVisitorZeroAlloc pins the G4 consumer constraint: walking a hub pair
// (df=N) must not materialize a merged slice, even when the append delta is present.
func TestPhrasePostingVisitorZeroAlloc(t *testing.T) {
	const docs = 512
	const hubA, hubB uint64 = 0x8101, 0x8102
	eng := NewEngine(BuildIndex(phrasePairDocs(docs)))
	eng.ensureClusterState()
	eng.AppendDocs([]Doc{phrasePairDocAt(docs)})
	st := eng.ensureClusterState()
	if len(st.phraseDelta) == 0 {
		t.Fatal("append did not take the base+delta path")
	}
	pair := rmDyad{a: hubA, b: hubB}
	if got := st.phrasePostingCount(pair); got != docs+1 {
		t.Fatalf("hub df=%d, want %d", got, docs+1)
	}
	visit := func() {
		st.visitPhrasePostings(pair, func(int32) bool { return true })
	}
	if allocs := testing.AllocsPerRun(200, visit); allocs != 0 {
		t.Fatalf("hub posting walk allocated %.1f objects per run, want 0", allocs)
	}
}
