// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade

import (
	"math"
	"testing"
)

func TestPeakDecayReferenceUsesPeakDirectEnergy(t *testing.T) {
	candidates := []deliveredCandidate{
		{score: 2.0, direct: 1.0},
		{score: 0.5, direct: 0.5},
	}
	peakScore, ref := peakDecayReference(candidates)
	if peakScore != 2.0 || ref != 1.0 {
		t.Fatalf("peak %v reference %v, want peak 2 and direct reference 1", peakScore, ref)
	}
	if candidates[1].score < ref/math.E {
		t.Fatalf("a candidate above the direct envelope %v must stay inside the window", ref/math.E)
	}
}

func TestPeakDecayReferenceFallsBackToTotalEnergy(t *testing.T) {
	peakScore, ref := peakDecayReference([]deliveredCandidate{{score: 3.0}})
	if peakScore != 3.0 || ref != 3.0 {
		t.Fatalf("peak %v reference %v, want total energy 3 when direct evidence is absent", peakScore, ref)
	}
	if peakScore, ref := peakDecayReference(nil); peakScore != 0 || ref != 0 {
		t.Fatalf("empty candidate list: peak %v reference %v, want zeros", peakScore, ref)
	}
}
