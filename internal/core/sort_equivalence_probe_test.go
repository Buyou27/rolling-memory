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

// TestFeatureMatchMassGroupSortParity pins the canonical match-order swap: the
// typed helper must produce the exact permutation the former sort.Slice
// comparator produced. Groups are unique per candidate (upsertMatch merges by
// group) and masses are finite positive products, so the (mass, group) key is a
// strict total order and the permutation is uniquely determined; this test
// exercises that shape, including heavy mass ties, across the insertion-sort
// and pdqsort size branches.
func TestFeatureMatchMassGroupSortParity(t *testing.T) {
	oldLess := func(ms []rmFeatureMatch, i, j int) bool {
		if ms[i].mass != ms[j].mass {
			return ms[i].mass < ms[j].mass
		}
		return ms[i].group < ms[j].group
	}
	rng := rand.New(rand.NewSource(7))
	massChoices := []float64{0, 0.5, 1, 1, 2, 3.25, 7, 7, 11}
	for trial := 0; trial < 800; trial++ {
		n := 2 + rng.Intn(300)
		groups := rng.Perm(n)
		base := make([]rmFeatureMatch, n)
		for i := range base {
			base[i] = rmFeatureMatch{
				group: uint64(groups[i])*3 + 1,
				mass:  massChoices[rng.Intn(len(massChoices))],
				entry: int32(groups[i]),
			}
		}
		oldOrder := append([]rmFeatureMatch(nil), base...)
		newOrder := append([]rmFeatureMatch(nil), base...)
		sort.Slice(oldOrder, func(i, j int) bool { return oldLess(oldOrder, i, j) })
		sortFeatureMatchesByMassGroup(newOrder)
		for i := range oldOrder {
			if oldOrder[i].group != newOrder[i].group || oldOrder[i].mass != newOrder[i].mass || oldOrder[i].entry != newOrder[i].entry {
				t.Fatalf("trial %d n=%d: permutation diverged at %d: old{%d,%v,%d} new{%d,%v,%d}",
					trial, n, i,
					oldOrder[i].group, oldOrder[i].mass, oldOrder[i].entry,
					newOrder[i].group, newOrder[i].mass, newOrder[i].entry)
			}
		}
		for i := 1; i < len(newOrder); i++ {
			if newOrder[i].mass < newOrder[i-1].mass ||
				(newOrder[i].mass == newOrder[i-1].mass && newOrder[i].group < newOrder[i-1].group) {
				t.Fatalf("trial %d: order not ascending at %d", trial, i)
			}
		}
	}
}
