// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "math"

// DecayReferenceEnergy returns the zero-floor natural decay reference of one ranked
// hit list: the peak candidate's own direct query-collision energy when it is
// positive, otherwise its total energy.
//
// Hop1 and Hop2 are borrowed from the peak's same-cluster neighbors rather than
// collided with the query, so they describe ambient cluster density. Using them to
// raise the stopping threshold would charge an isolated-date record (which carries
// its own complementary evidence but has no dense same-day neighborhood to borrow
// from) against the peak's neighborhood mass. The direct term is the peak's own
// collision self-information, so it is the term that belongs on the reference side
// of the envelope.
func DecayReferenceEnergy(hits []Hit) float64 {
	if len(hits) == 0 {
		return 0
	}
	if direct := hits[0].Direct; direct > 0 && !math.IsNaN(direct) && !math.IsInf(direct, 0) {
		return direct
	}
	if energy := hits[0].Energy; energy > 0 && !math.IsNaN(energy) && !math.IsInf(energy, 0) {
		return energy
	}
	return 0
}

// NaturalDecayPrefixCount returns the length of the longest ordered prefix of the
// ranked hits that stays inside the zero-floor natural decay envelope
//
//	E(u_k | Q) >= e^-1 * DecayReferenceEnergy(hits)
//
// capped at maxItems. The result is zero when no candidate carries positive
// query-collision energy (nothing in the archive collided with the query, so the
// delivery window is empty) and maxItems when every candidate shares the same
// non-random collision plateau. Both cases keep the rule free of any minimum
// floor and free of any fixed item count.
func NaturalDecayPrefixCount(hits []Hit, maxItems int) int {
	if len(hits) == 0 || maxItems <= 0 {
		return 0
	}
	limit := len(hits)
	if limit > maxItems {
		limit = maxItems
	}
	ref := DecayReferenceEnergy(hits)
	if ref <= 0 {
		return 0
	}
	cutoff := ref / math.E
	count := 0
	for i := 0; i < limit; i++ {
		energy := hits[i].Energy
		if math.IsNaN(energy) || math.IsInf(energy, 0) || energy < cutoff {
			break
		}
		count++
	}
	return count
}

// NaturalDecayEnergiesPrefixCount is the energy-slice form of
// NaturalDecayPrefixCount for callers that hold only scores. The slice must be in
// ranked order; the reference is its first element.
func NaturalDecayEnergiesPrefixCount(energies []float64, maxItems int) int {
	if len(energies) == 0 || maxItems <= 0 {
		return 0
	}
	limit := len(energies)
	if limit > maxItems {
		limit = maxItems
	}
	ref := energies[0]
	if math.IsNaN(ref) || math.IsInf(ref, 0) || ref <= 0 {
		return 0
	}
	cutoff := ref / math.E
	count := 0
	for i := 0; i < limit; i++ {
		energy := energies[i]
		if math.IsNaN(energy) || math.IsInf(energy, 0) || energy < cutoff {
			break
		}
		count++
	}
	return count
}
