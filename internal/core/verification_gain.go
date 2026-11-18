// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "math"

// VerificationGain returns the continuous logarithmic reinforcement multiplier for a
// record whose later hits re-consolidated it `reuseCount` times: exactly 1.0 when the
// record has never been re-verified, otherwise 1 + ln(1 + reuseCount).
//
// Each re-verification adds evidence on the same slowly-growing log scale as the other
// continuous energy gains, so the multiplier is strictly monotone in reuseCount and
// bounded in growth (ln) without any fixed ceiling: a hard cap would flatten ordering
// between heavily re-verified records onto an arbitrary plateau.
func VerificationGain(reuseCount int) float64 {
	if reuseCount <= 0 {
		return 1.0
	}
	return 1.0 + math.Log1p(float64(reuseCount))
}
