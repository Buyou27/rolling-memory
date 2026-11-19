// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "math"

// calendarConcentration is the calendar-day concentration of a term with n
// occurrences observed on dObs distinct days out of m active days, 1 - D_obs/E_0,
// where E_0 is the exact expected number of occupied days under n independent
// uniform placements. Observed occupancy at or above the null expectation earns
// zero, and a single-day cluster approaches 1 as n grows.
func calendarConcentration(dObs, n, m float64) float64 {
	return math.Max(0, 1-dObs/expectedOccupiedDays(n, m))
}

// expectedOccupiedDays returns the exact expected number of occupied days
// M(1 - (1 - 1/M)^n), evaluated as -M·expm1(n·log1p(-1/M)) to keep precision when
// n is small relative to M. A single active day is always occupied.
func expectedOccupiedDays(n, m float64) float64 {
	if m <= 1 {
		return 1
	}
	return -m * math.Expm1(n*math.Log1p(-1/m))
}
