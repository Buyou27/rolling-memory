// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import "testing"

func TestRecallLatencyStatsRobustSummary(t *testing.T) {
	cases := []struct {
		name     string
		samples  []int
		wantLast int
		wantAvg  int
		wantP50  int
		wantMax  int
	}{
		{name: "empty", samples: nil, wantLast: 0, wantAvg: 0, wantP50: 0, wantMax: 0},
		{name: "single", samples: []int{372}, wantLast: 372, wantAvg: 372, wantP50: 372, wantMax: 372},
		{name: "odd", samples: []int{900, 200, 500}, wantLast: 500, wantAvg: 533, wantP50: 500, wantMax: 900},
		{name: "even", samples: []int{200, 400, 600, 800}, wantLast: 800, wantAvg: 500, wantP50: 500, wantMax: 800},
		{name: "spike", samples: []int{204, 321, 372, 2291}, wantLast: 2291, wantAvg: 797, wantP50: 346, wantMax: 2291},
	}
	for _, tc := range cases {
		last, avg, p50, max := recallLatencyStats(tc.samples)
		if last != tc.wantLast || avg != tc.wantAvg || p50 != tc.wantP50 || max != tc.wantMax {
			t.Fatalf("%s: got last=%d avg=%d p50=%d max=%d, want last=%d avg=%d p50=%d max=%d",
				tc.name, last, avg, p50, max, tc.wantLast, tc.wantAvg, tc.wantP50, tc.wantMax)
		}
	}
}

func TestRecallLatencyTrendRelativeToAverage(t *testing.T) {
	if got := recallLatencyTrend(0, 0); got != 0 {
		t.Fatalf("no samples must yield zero trend, got %v", got)
	}
	if got := recallLatencyTrend(346, 797); got != -56.5 {
		t.Fatalf("median below average must trend negative, got %v", got)
	}
	if got := recallLatencyTrend(1000, 500); got != 100 {
		t.Fatalf("median above average must trend positive, got %v", got)
	}
}
