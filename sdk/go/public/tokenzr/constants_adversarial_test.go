// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package tokenzr

import (
	"math"
	"strings"
	"testing"
)

// TestNamedConstantsAreBitIdenticalToMathE is the regression pair for naming the
// per-hop transmission in the sealed prior anchors: the named constants must keep
// the exact double values, otherwise the frozen anchor readings drift.
func TestNamedConstantsAreBitIdenticalToMathE(t *testing.T) {
	if math.Float64bits(eulerNumber) != math.Float64bits(math.E) {
		t.Fatalf("eulerNumber must be bit-identical to math.E")
	}
	if math.Float64bits(transmissionPerHop) != math.Float64bits(1.0/math.E) {
		t.Fatalf("transmissionPerHop must be bit-identical to 1/math.E")
	}
	if math.Abs(transmissionPerHop*eulerNumber-1) > 1e-15 {
		t.Fatalf("per-hop transmission and e must be reciprocal, got %v", transmissionPerHop*eulerNumber)
	}
}

// TestInitialPriorAnchorDegenerateInputsAdversarial pins the placeholder anchors on
// hostile inputs: the composition must stay finite and non-negative, and the empty
// word must stay exactly zero.
func TestInitialPriorAnchorDegenerateInputsAdversarial(t *testing.T) {
	if got := AtomInitialEnergyBits(""); got != 0 {
		t.Fatalf("empty word must compose to exactly zero, got %v", got)
	}
	for _, w := range []string{" ", "\t", "a", "Porto", "2023-12-19", "CamelCaseIdentifier", strings.Repeat("x", 512)} {
		got := AtomInitialEnergyBits(w)
		if math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("anchor energy for %q must be finite, got %v", w, got)
		}
		if got < 0 {
			t.Fatalf("anchor energy for %q must be non-negative, got %v", w, got)
		}
	}
	// The composition is linear in the carrier term: increasing the carrier cannot
	// lower the energy, and the transmission split stays inside [0,1].
	if transmissionPerHop <= 0 || transmissionPerHop >= 1 {
		t.Fatalf("transmission split must lie in (0,1), got %v", transmissionPerHop)
	}
}
