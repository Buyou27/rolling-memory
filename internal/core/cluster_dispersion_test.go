// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"testing"
)

func TestFamilyDispersionIsEntropyShareOfDocumentCount(t *testing.T) {
	if got := familyDispersion(2, math.Ln2); math.Abs(got-1) > 1e-12 {
		t.Fatalf("two documents naming two distinct partners: dispersion %v, want 1", got)
	}
	if got := familyDispersion(30, 0); got != 0 {
		t.Fatalf("single partner carrying every co-occurrence: dispersion %v, want 0", got)
	}
	if got := familyDispersion(1, 0); got != 0 {
		t.Fatalf("single document: dispersion %v, want 0", got)
	}
	if got := familyDispersion(4, 10*math.Log(4)); got != 1 {
		t.Fatalf("partners outnumbering documents must saturate at 1, got %v", got)
	}
}

func TestFamilyDispersionBoundedAcrossCountVectors(t *testing.T) {
	cases := [][]int32{
		{1},
		{1, 1},
		{5, 5},
		{30, 30},
		{1, 1, 1, 1, 1, 1, 1, 1},
		{100, 1, 1},
		{7, 3, 2, 1},
	}
	for _, counts := range cases {
		keys := make([]uint64, len(counts))
		for i := range keys {
			keys[i] = uint64(i + 1)
		}
		_, entropy := familyEntropy(keys, counts)
		for _, df := range []int{1, 2, len(counts) + 1, 1000} {
			if got := familyDispersion(df, entropy); got < 0 || got > 1 {
				t.Fatalf("counts %v df %d: dispersion %v outside [0, 1]", counts, df, got)
			}
		}
	}
}

func TestFamilyEffectiveWeightKeepsIndependentPairsAsDistinctEvents(t *testing.T) {
	const n = 1000
	total, entropy := familyEntropy([]uint64{1, 2}, []int32{1, 1})
	got := familyEffectiveWeight(2, 0, 2, total, entropy, n)
	want := math.Log1p((n + 1) / 3.0)
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("df=2 in two clusters with distinct partners: weight %v, want %v (effective df 2)", got, want)
	}
}

func TestFamilyEffectiveWeightRanksRepeatedPartnersAsBursty(t *testing.T) {
	const n = 1000
	burst := familyEffectiveWeight(30, 30, 30, 60, math.Ln2, n)
	dispersed := familyEffectiveWeight(30, 30, 30, 60, math.Log(60), n)
	if !(burst > dispersed) {
		t.Fatalf("two partners repeated across 30 documents (weight %v) must outweigh 60 distinct partners (weight %v)", burst, dispersed)
	}
}
