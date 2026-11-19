// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "testing"

// TestEndorsePairDispatchCoversAllIndices pins the adapter pool at the empty,
// single, two-unit and multi-worker boundaries: every event index except the
// unused zero slot is evaluated exactly once with its own worker-local scratch.
func TestEndorsePairDispatchCoversAllIndices(t *testing.T) {
	old := endorseWorkersOverride
	endorseWorkersOverride = 4
	defer func() {
		endorseWorkersOverride = old
		endorseUnitEvaluations.Store(0)
	}()
	for _, n := range []int{0, 1, 2, 17} {
		endorseUnitEvaluations.Store(0)
		out := dispatchEndorsePairs(n, func(i int, local *rmQueryScratch) float64 {
			if local == nil {
				t.Fatalf("nil worker scratch at n=%d index=%d", n, i)
			}
			return float64(i)
		})
		if len(out) != n {
			t.Fatalf("n=%d returned %d slots", n, len(out))
		}
		for i := 1; i < n; i++ {
			if out[i] != float64(i) {
				t.Fatalf("n=%d out[%d]=%v, want %d", n, i, out[i], i)
			}
		}
		want := n - 1
		if want < 0 {
			want = 0
		}
		if got := endorseUnitEvaluations.Load(); got != int64(want) {
			t.Fatalf("n=%d evaluated %d indices, want %d", n, got, want)
		}
	}
}
