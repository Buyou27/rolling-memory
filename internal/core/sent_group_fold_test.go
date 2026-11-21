// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"math/rand"
	"testing"
)

// TestFoldSentGroupsMatchesFullSort pins the counting-scatter fold to the
// comparison-sort fold: identical sequence, identical folded weights.
func TestFoldSentGroupsMatchesFullSort(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var sc rmQueryScratch
	for trial := 0; trial < 3000; trial++ {
		n := rng.Intn(400)
		sentSpan := 1 + rng.Intn(40)
		if trial%17 == 0 {
			sentSpan = 5000 // exercises the sparse fallback
		}
		src := make([]rmStructSentGroupW, n)
		for i := range src {
			src[i] = rmStructSentGroupW{
				sent:   rng.Intn(sentSpan),
				group:  uint64(rng.Intn(9)),
				weight: float64(1 + rng.Intn(100000)),
			}
		}
		want := sortAndFoldSentGroups(append([]rmStructSentGroupW(nil), src...))
		got := sc.foldSentGroups(append([]rmStructSentGroupW(nil), src...))
		if len(got) != len(want) {
			t.Fatalf("trial=%d len=%d want=%d", trial, len(got), len(want))
		}
		for i := range want {
			if got[i].sent != want[i].sent || got[i].group != want[i].group ||
				math.Float64bits(got[i].weight) != math.Float64bits(want[i].weight) {
				t.Fatalf("trial=%d item=%d got=(%d,%d,%v) want=(%d,%d,%v)",
					trial, i, got[i].sent, got[i].group, got[i].weight,
					want[i].sent, want[i].group, want[i].weight)
			}
		}
	}
}

func TestFoldSentGroupsDegenerate(t *testing.T) {
	var sc rmQueryScratch
	if got := sc.foldSentGroups(nil); len(got) != 0 {
		t.Fatalf("nil fold=%d", len(got))
	}
	one := []rmStructSentGroupW{{sent: 3, group: 9, weight: 2}}
	if got := sc.foldSentGroups(one); len(got) != 1 || got[0] != one[0] {
		t.Fatalf("single fold=%v", got)
	}
	same := []rmStructSentGroupW{
		{sent: 1, group: 2, weight: 5},
		{sent: 1, group: 2, weight: 9},
		{sent: 1, group: 2, weight: 7},
	}
	got := sc.foldSentGroups(same)
	if len(got) != 1 || got[0].sent != 1 || got[0].group != 2 || got[0].weight != 9 {
		t.Fatalf("same-group fold=%v", got)
	}
}
