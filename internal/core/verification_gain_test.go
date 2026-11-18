// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"testing"
)

func TestVerificationGain(t *testing.T) {
	cases := []struct {
		reuse int
		want  float64
	}{
		{-3, 1.0},
		{0, 1.0},
		{1, 1.0 + math.Log1p(1)},
		{2, 1.0 + math.Log1p(2)},
		{14, 1.0 + math.Log1p(14)},
		{15, 1.0 + math.Log1p(15)},
		{1000, 1.0 + math.Log1p(1000)},
	}
	for _, c := range cases {
		if got := VerificationGain(c.reuse); math.Abs(got-c.want) > 1e-12 {
			t.Fatalf("VerificationGain(%d) = %v, want %v", c.reuse, got, c.want)
		}
	}
	// 严格单调：复核次数越多，增益越大（无人工平台期）。
	prev := 0.0
	for n := 0; n <= 4096; n++ {
		g := VerificationGain(n)
		if g < prev {
			t.Fatalf("VerificationGain not monotone at reuse=%d: %v < %v", n, g, prev)
		}
		prev = g
	}
}
