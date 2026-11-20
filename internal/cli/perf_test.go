// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"math"
	"reflect"
	"testing"
)

func TestParsePerfScalesOrdersAndDeduplicates(t *testing.T) {
	got, err := parsePerfScales(" 1000, 100 ,300,100, ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := []int{100, 300, 1000}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sizes = %v, want %v", got, want)
	}
	for _, raw := range []string{"", "100", "100,100", "0,100", "100,abc", "100,-5"} {
		if _, err := parsePerfScales(raw); err == nil {
			t.Fatalf("parsePerfScales(%q) accepted an invalid list", raw)
		}
	}
}

func TestPerfPercentileUsesNearestRank(t *testing.T) {
	sorted := []float64{1, 2, 3, 4}
	cases := []struct {
		fraction float64
		want     float64
	}{
		{fraction: 0, want: 1},
		{fraction: 0.5, want: 2},
		{fraction: 0.95, want: 4},
		{fraction: 1, want: 4},
	}
	for _, tc := range cases {
		if got := perfPercentile(sorted, tc.fraction); got != tc.want {
			t.Fatalf("p%v = %v, want %v", tc.fraction, got, tc.want)
		}
	}
	if got := perfPercentile(nil, 0.5); got != 0 {
		t.Fatalf("empty percentile = %v, want 0", got)
	}
}

func TestLogLogSlopeReadsTheExponent(t *testing.T) {
	sizes := []float64{10, 100, 1000}
	cases := []struct {
		name string
		ys   []float64
		want float64
	}{
		{name: "linear", ys: []float64{10, 100, 1000}, want: 1},
		{name: "flat", ys: []float64{5, 5, 5}, want: 0},
		{name: "quadratic", ys: []float64{100, 10000, 1000000}, want: 2},
	}
	for _, tc := range cases {
		if got := logLogSlope(sizes, tc.ys); math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("%s slope = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := logLogSlope([]float64{10}, []float64{1}); got != 0 {
		t.Fatalf("single sample slope = %v, want 0", got)
	}
}
