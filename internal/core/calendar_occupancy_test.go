// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"testing"
)

func TestExpectedOccupiedDaysIsExactMultinomialExpectation(t *testing.T) {
	if got := expectedOccupiedDays(3, 3); math.Abs(got-19.0/9.0) > 1e-12 {
		t.Fatalf("E(3,3) = %v, want 19/9 = 3·(1-(2/3)^3)", got)
	}
	if got := expectedOccupiedDays(1, 365); math.Abs(got-1) > 1e-12 {
		t.Fatalf("one placement occupies one day, got %v", got)
	}
	if got := expectedOccupiedDays(7, 1); got != 1 {
		t.Fatalf("a single active day must read as occupied, got %v", got)
	}
	for _, n := range []float64{2, 5, 40} {
		for _, m := range []float64{2, 10, 1000} {
			got := expectedOccupiedDays(n, m)
			if got < 1-1e-12 || got > math.Min(n, m)+1e-9 {
				t.Fatalf("E(%v,%v) = %v outside [1, min(n,m)]", n, m, got)
			}
		}
	}
	if got := expectedOccupiedDays(10, 1e9); math.Abs(got-10) > 1e-6 {
		t.Fatalf("large-M limit E(10,1e9) = %v, want 10", got)
	}
}

func TestCalendarConcentrationBounds(t *testing.T) {
	const n, m = 10.0, 1000.0
	e := expectedOccupiedDays(n, m)
	if got := calendarConcentration(e, n, m); got != 0 {
		t.Fatalf("observed occupancy at the null expectation: concentration %v, want 0", got)
	}
	if got := calendarConcentration(n+5, n, m); got != 0 {
		t.Fatalf("occupancy above the null expectation: concentration %v, want 0", got)
	}
	single := calendarConcentration(1, n, m)
	if math.Abs(single-(1-1/e)) > 1e-12 || single < 0 || single >= 1 {
		t.Fatalf("single-day cluster: concentration %v, want 1 - 1/E = %v", single, 1-1/e)
	}
	if !(calendarConcentration(1, 40, m) > single) {
		t.Fatalf("a single-day cluster must concentrate more as its occurrence count grows")
	}
}
