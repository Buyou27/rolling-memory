// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math/rand"
	"sort"
	"testing"
)

type tieKeySort []uint64

func (s tieKeySort) Len() int           { return len(s) }
func (s tieKeySort) Less(i, j int) bool { return s[i] < s[j] }
func (s tieKeySort) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// TestSortSortSliceTiePermutationParity measures whether sort.Sort and
// sort.Slice produce identical permutations for the same comparator when keys
// tie. If they can differ, every tie-bearing sort swap must keep sort.Slice.
func TestSortSortSliceTiePermutationParity(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	diffs := 0
	for trial := 0; trial < 500; trial++ {
		n := 2 + rng.Intn(300)
		a := make([]uint64, n)
		for i := range a {
			a[i] = uint64(rng.Intn(4)) // heavy ties
		}
		b := append([]uint64(nil), a...)
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
		sort.Sort(tieKeySort(b))
		for i := range a {
			if a[i] != b[i] {
				diffs++
				break
			}
		}
	}
	t.Logf("tie-heavy trials=500 permutation diffs=%d", diffs)
}
