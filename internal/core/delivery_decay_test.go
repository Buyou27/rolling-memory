// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"testing"
)

func TestDecayReferenceEnergyPrefersDirectCollision(t *testing.T) {
	// The peak borrows cluster corroboration from its same-day neighbors. The
	// reference must stay on its own direct collision energy so an isolated-date
	// record is not charged for a dense neighborhood it does not share.
	hits := []Hit{
		{UnitID: "peak", Energy: 407.32, Direct: 249.50, Hop1: 44.00, Hop2: 113.82},
		{UnitID: "isolated", Energy: 97.64, Direct: 96.74, Hop1: 0, Hop2: 0.90},
	}
	if got, want := DecayReferenceEnergy(hits), 249.50; got != want {
		t.Fatalf("DecayReferenceEnergy = %v, want %v", got, want)
	}
	if got, want := NaturalDecayPrefixCount(hits, 64), 2; got != want {
		t.Fatalf("NaturalDecayPrefixCount = %d, want %d (the isolated record clears the envelope)", got, want)
	}
}

func TestNaturalDecayPrefixCountCases(t *testing.T) {
	cases := []struct {
		name     string
		hits     []Hit
		maxItems int
		want     int
	}{
		{
			name:     "empty window on zero collision",
			hits:     []Hit{{UnitID: "a"}, {UnitID: "b"}},
			maxItems: 64,
			want:     0,
		},
		{
			name:     "no hits",
			hits:     nil,
			maxItems: 64,
			want:     0,
		},
		{
			name:     "zero cap",
			hits:     []Hit{{UnitID: "a", Energy: 10, Direct: 10}},
			maxItems: 0,
			want:     0,
		},
		{
			name:     "single sharp peak contracts",
			hits:     []Hit{{UnitID: "a", Energy: 24.0, Direct: 24.0}, {UnitID: "b", Energy: 6.2, Direct: 6.2}, {UnitID: "c", Energy: 5.1, Direct: 5.1}},
			maxItems: 64,
			want:     1,
		},
		{
			name: "flat plateau expands to the cap",
			hits: []Hit{
				{UnitID: "a", Energy: 12.0, Direct: 12.0},
				{UnitID: "b", Energy: 12.0, Direct: 12.0},
				{UnitID: "c", Energy: 12.0, Direct: 12.0},
			},
			maxItems: 2,
			want:     2,
		},
		{
			name: "cap bounds a long plateau",
			hits: []Hit{
				{UnitID: "a", Energy: 18.0, Direct: 18.0},
				{UnitID: "b", Energy: 17.6, Direct: 17.6},
				{UnitID: "c", Energy: 17.2, Direct: 17.2},
				{UnitID: "d", Energy: 4.2, Direct: 4.2},
			},
			maxItems: 3,
			want:     3,
		},
		{
			name: "prefix stops at the first failure",
			hits: []Hit{
				{UnitID: "a", Energy: 30.0, Direct: 30.0},
				{UnitID: "b", Energy: 20.0, Direct: 20.0},
				{UnitID: "c", Energy: 2.0, Direct: 2.0},
				{UnitID: "d", Energy: 25.0, Direct: 25.0},
			},
			maxItems: 64,
			want:     2,
		},
		{
			name:     "nan energies never extend the window",
			hits:     []Hit{{UnitID: "a", Energy: 10, Direct: 10}, {UnitID: "b", Energy: math.NaN()}},
			maxItems: 64,
			want:     1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NaturalDecayPrefixCount(tc.hits, tc.maxItems); got != tc.want {
				t.Fatalf("NaturalDecayPrefixCount = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNaturalDecayEnergiesPrefixCountScaleInvariance(t *testing.T) {
	base := []float64{24.0, 6.2, 5.1, 4.0}
	for _, scale := range []float64{0.25, 1.0, 3.5} {
		scaled := make([]float64, len(base))
		for i, v := range base {
			scaled[i] = scale * v
		}
		if got, want := NaturalDecayEnergiesPrefixCount(scaled, 64), 1; got != want {
			t.Fatalf("scale %v: NaturalDecayEnergiesPrefixCount = %d, want %d", scale, got, want)
		}
	}
	if got := NaturalDecayEnergiesPrefixCount([]float64{0, 0}, 64); got != 0 {
		t.Fatalf("zero energies must yield an empty window, got %d", got)
	}
}

func TestCoverageFloorPrefixCount(t *testing.T) {
	cases := []struct {
		name     string
		ranks    []int
		maxItems int
		want     int
	}{
		{name: "empty ranks", ranks: nil, maxItems: 64, want: 0},
		{name: "zero cap", ranks: []int{4}, maxItems: 0, want: 0},
		{name: "ignores nonpositive", ranks: []int{0, -3}, maxItems: 64, want: 0},
		{name: "ignores beyond cap", ranks: []int{70}, maxItems: 64, want: 0},
		{name: "takes the largest first-cover", ranks: []int{1, 2, 4}, maxItems: 64, want: 4},
		{name: "clamped to cap", ranks: []int{3, 64}, maxItems: 64, want: 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CoverageFloorPrefixCount(tc.ranks, tc.maxItems); got != tc.want {
				t.Fatalf("CoverageFloorPrefixCount = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNaturalDecayCoveragePrefixCountExtendsOnlyAndKeepsSilence(t *testing.T) {
	// Zero collision keeps the empty window even if a caller passes ranks:
	// a formal cover cannot exist without a positive collision.
	silent := []Hit{{UnitID: "a"}, {UnitID: "b"}}
	if got := NaturalDecayCoveragePrefixCount(silent, []int{3}, 64); got != 0 {
		t.Fatalf("zero collision must keep silence, got %d", got)
	}

	// The partial-coverage case: the decay envelope closes at 2, but a salient
	// group is first covered by the rank-4 candidate (the only one carrying the
	// missing concept), so the coupled window extends to 4.
	hits := []Hit{
		{UnitID: "peak", Energy: 398.5, Direct: 380.1},
		{UnitID: "r2", Energy: 276.8, Direct: 276.4},
		{UnitID: "r3", Energy: 120.0, Direct: 119.3},
		{UnitID: "complement", Energy: 118.1, Direct: 117.0},
		{UnitID: "r5", Energy: 40.0, Direct: 39.0},
	}
	decay := NaturalDecayPrefixCount(hits, 64)
	if decay != 2 {
		t.Fatalf("decay window = %d, want 2", decay)
	}
	if got, want := NaturalDecayCoveragePrefixCount(hits, []int{1, 2, 4}, 64), 4; got != want {
		t.Fatalf("coupled window = %d, want %d (extends to the first complement cover)", got, want)
	}
	// The floor never shrinks a window that already covers everything: when the
	// largest first-cover rank sits inside the decay window, the decay count wins.
	if got, want := NaturalDecayCoveragePrefixCount(hits, []int{1, 2}, 64), 2; got != want {
		t.Fatalf("coupled window = %d, want %d (floor inside the decay window)", got, want)
	}
	// Ranks beyond the item cap cannot stretch the window past the cap.
	if got, want := NaturalDecayCoveragePrefixCount(hits, []int{1, 2, 90}, 64), 2; got != want {
		t.Fatalf("coupled window = %d, want %d (out-of-cap rank ignored)", got, want)
	}
}
