// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"testing"
)

// occupancyTailLog returns log P(D ≤ d), where D is the number of distinct days
// occupied when n occurrences are placed independently and uniformly over m
// days. The law is exact: P(D = k) = C(m,k)·k!·S(n,k)/m^n with S the Stirling
// numbers of the second kind, evaluated in log space by a one-row DP.
func occupancyTailLog(d, n, m float64) float64 {
	dd, nn := int(d), int(n)
	if dd <= 0 {
		return math.Inf(-1)
	}
	if dd > nn {
		dd = nn
	}
	if dd > int(m) {
		dd = int(m)
	}
	neg := math.Inf(-1)
	prev := make([]float64, dd+1)
	for i := range prev {
		prev[i] = neg
	}
	prev[0] = 0
	for i := 1; i <= nn; i++ {
		cur := make([]float64, dd+1)
		for k := range cur {
			cur[k] = neg // S(i,0)=0 and S(i,k)=0 for k > i
		}
		for k := 1; k <= dd && k <= i; k++ {
			a := prev[k] + math.Log(float64(k))
			b := prev[k-1]
			switch {
			case a == neg:
				cur[k] = b
			case b == neg:
				cur[k] = a
			case a > b:
				cur[k] = a + math.Log1p(math.Exp(b-a))
			default:
				cur[k] = b + math.Log1p(math.Exp(a-b))
			}
		}
		prev = cur
	}
	acc := neg
	for k := 1; k <= dd; k++ {
		if prev[k] == neg {
			continue
		}
		// log(C(m,k)·k!) = Γ(m+1) − Γ(m−k+1); the k! cancels the Σ over onto maps.
		lchoose, _ := math.Lgamma(m + 1)
		lrest, _ := math.Lgamma(m - float64(k) + 1)
		lp := lchoose - lrest + prev[k] - n*math.Log(m)
		switch {
		case acc == neg:
			acc = lp
		case lp > acc:
			acc = lp + math.Log1p(math.Exp(acc-lp))
		default:
			acc = acc + math.Log1p(math.Exp(lp-acc))
		}
	}
	return acc
}

// occupancyTailUnionBound returns log of the rigorous union bound
// C(m,d)·(d/m)^n ≥ P(D ≤ d), valid for every (n, m, d).
func occupancyTailUnionBound(d, n, m float64) float64 {
	if d <= 0 {
		return math.Inf(-1)
	}
	lchoose, _ := math.Lgamma(m + 1)
	lk, _ := math.Lgamma(d + 1)
	lrest, _ := math.Lgamma(m - d + 1)
	return lchoose - lk - lrest + n*math.Log(d/m)
}

// expectedOccupiedDaysOf is the null mean M(1-(1-1/M)^n) recomputed here so the
// law under test does not depend on the production helper's implementation.
func expectedOccupiedDaysOf(n, m float64) float64 {
	if m <= 1 {
		return 1
	}
	return -m * math.Expm1(n*math.Log1p(-1/m))
}

// TestOccupancyNullTailMatchesBruteForce pins the exact law against exhaustive
// enumeration of every placement for small (n, m).
func TestOccupancyNullTailMatchesBruteForce(t *testing.T) {
	for m := 2; m <= 6; m++ {
		for n := 2; n <= 6; n++ {
			counts := make([]int, n+1) // counts[k] = placements occupying k days
			total := 0
			placement := make([]int, n)
			var walk func(int)
			walk = func(i int) {
				if i == n {
					seen := make(map[int]struct{}, n)
					for _, day := range placement {
						seen[day] = struct{}{}
					}
					counts[len(seen)]++
					total++
					return
				}
				for day := 0; day < m; day++ {
					placement[i] = day
					walk(i + 1)
				}
			}
			walk(0)
			for d := 1; d <= n; d++ {
				want := 0
				for k := 1; k <= d; k++ {
					want += counts[k]
				}
				got := math.Exp(occupancyTailLog(float64(d), float64(n), float64(m)))
				expect := float64(want) / float64(total)
				if math.Abs(got-expect) > 1e-12 {
					t.Fatalf("m=%d n=%d d=%d: tail=%v want %v", m, n, d, got, expect)
				}
			}
		}
	}
}

// TestOccupancyNullBoundaryNeverLooserThanOneDayOfChance is the derivation that
// backs the anchor shape gate: at the live operating point κ_occ ≥ 1-1/e, i.e.
// D ≤ ⌊E₀/e⌋... precisely D ≤ ⌊(1-1/e)·E₀⌋, the independent-placement null
// assigns that occupancy at most 1/m probability — one day of chance — for every
// n ≥ 2. The supremum is attained exactly at n = 2, where P(D=1) = 1/m.
//
// Equivalently: no term admitted by the shape gate can be a chance artifact of
// the same size as a single coincidental day collision, regardless of its
// frequency. This is what makes the empirical 1/e an operating point with a
// derived worst-case guarantee rather than a bare guess.
func TestOccupancyNullBoundaryNeverLooserThanOneDayOfChance(t *testing.T) {
	// n ≤ exactLimit is checked by the exact tail; larger n by the rigorous
	// union bound, which is far below 1/m there and keeps the test cheap.
	const exactLimit = 60
	daysGrid := []float64{3, 4, 5, 6, 8, 10, 15, 20, 30, 60, 120, 365, 730, 1249, 1460, 2920}
	for _, m := range daysGrid {
		worst := 0.0
		worstN := 0
		for n := 2; n <= 140; n++ {
			fn := float64(n)
			e0 := expectedOccupiedDaysOf(fn, m)
			dStar := math.Floor((1 - 1/math.E) * e0)
			var logTail float64
			if n <= exactLimit {
				logTail = occupancyTailLog(dStar, fn, m)
			} else {
				logTail = occupancyTailUnionBound(dStar, fn, m)
			}
			p := math.Exp(logTail)
			if p > worst {
				worst, worstN = p, n
			}
			limit := 1 / m * (1 + 1e-9) // log-space round-trip slack
			if p > limit {
				t.Fatalf("m=%v n=%d: P(D<=%v | null)=%v exceeds the 1/m=%v guarantee", m, n, dStar, p, 1/m)
			}
		}
		if worstN != 2 {
			t.Fatalf("m=%v: worst case should be n=2 (exact one-day coincidence), got n=%d p=%v", m, worstN, worst)
		}
		if math.Abs(worst-1/m) > 1e-9*1/m {
			t.Fatalf("m=%v: n=2 must attain exactly 1/m, got %v", m, worst)
		}
	}
}

// TestOccupancyNullTailDecaysWithFrequency documents the monotone decay that
// separates a significance test from a shape measure: at the same relative
// collapse the tail falls by orders of magnitude as n grows, so a fixed
// significance level cannot act like a fixed κ boundary.
func TestOccupancyNullTailDecaysWithFrequency(t *testing.T) {
	const m = 1249.0
	prev := math.Inf(1)
	var first, last float64
	// n ≥ 10 keeps the boundary floor negligible; below that the admitted set is
	// a coarse ladder of integer teeth (see the teeth test).
	for i, n := range []float64{10, 20, 50, 100, 200, 400} {
		e0 := expectedOccupiedDaysOf(n, m)
		dStar := math.Floor((1 - 1/math.E) * e0)
		p := math.Exp(occupancyTailLog(dStar, n, m))
		if p >= prev {
			t.Fatalf("tail must decay with n: n=%v p=%v prev=%v", n, p, prev)
		}
		if i == 0 {
			first = p
		}
		last = p
		prev = p
	}
	t.Logf("at the same relative collapse the null tail falls from %v (n=10) to %v (n=400): a fixed α is a different gate at each frequency", first, last)
}

// TestOccupancyBoundaryIsAStaircaseOfIntegerTeeth documents why the threshold's
// exact value is immaterial for rare terms: day counts are integers, so the
// admitted set is the ladder d ≤ ⌊(1-1/e)·E₀⌋ and the realized κ at the boundary
// is always ≥ 1/e, jumping between teeth as n changes.
func TestOccupancyBoundaryIsAStaircaseOfIntegerTeeth(t *testing.T) {
	const m = 1249.0
	prevTooth := math.Inf(1)
	for n := 2; n <= 8; n++ {
		e0 := expectedOccupiedDaysOf(float64(n), m)
		dStar := math.Floor((1 - 1/math.E) * e0)
		kappa := 1 - dStar/e0
		if kappa < 1/math.E-1e-15 {
			t.Fatalf("realized boundary must never fall below 1/e: n=%d kappa=%v", n, kappa)
		}
		if kappa > 0.75 {
			t.Fatalf("realized boundary drifted implausibly high: n=%d kappa=%v", n, kappa)
		}
		prevTooth = kappa
	}
	t.Logf("realized boundary κ for n=2..8 oscillates inside [1/e, 0.75] (last tooth %v): any threshold inside the gaps decides these terms identically", prevTooth)
}

// sampledOccupiedDaysExact returns E[D] when the n documents are a uniform
// sample without replacement from a corpus whose days hold counts[j] documents.
// Day j stays empty with probability C(N-c_j,n)/C(N,n), advanced by the exact
// one-step recursion q(k) = q(k-1)·(N-c-k+1)/(N-k+1).
func sampledOccupiedDaysExact(counts []int32, totalN, n float64) float64 {
	sum := 0.0
	for _, c := range counts {
		q := 1.0
		for k := 1.0; k <= n; k++ {
			q *= (totalN - float64(c) - k + 1) / (totalN - k + 1)
			if q < 0 {
				q = 0
				break
			}
		}
		sum += 1 - q
	}
	return sum
}

// TestOccupancySampledNullMatchesEnumeration pins the document-sampling null
// (sampling without replacement from the corpus's own day occupancy) against
// exhaustive enumeration of all n-subsets for a tiny corpus.
func TestOccupancySampledNullMatchesEnumeration(t *testing.T) {
	counts := []int32{1, 2, 3}
	totalN := 6.0
	dayOf := []int{0, 1, 1, 2, 2, 2}
	for n := 1; n <= 5; n++ {
		total, occ := 0, 0
		for mask := 0; mask < 1<<len(dayOf); mask++ {
			if bitsSet(mask) != n {
				continue
			}
			var seen [3]bool
			for i := range dayOf {
				if mask&(1<<i) != 0 {
					seen[dayOf[i]] = true
				}
			}
			k := 0
			for _, s := range seen {
				if s {
					k++
				}
			}
			total++
			occ += k
		}
		want := float64(occ) / float64(total)
		got := sampledOccupiedDaysExact(counts, totalN, float64(n))
		if math.Abs(got-want) > 1e-12 {
			t.Fatalf("n=%d: sampled E[D]=%v want %v", n, got, want)
		}
	}
}

func bitsSet(mask int) int {
	c := 0
	for ; mask != 0; mask &= mask - 1 {
		c++
	}
	return c
}

// TestOccupancyDocumentSamplingIsAFrequencyDependentThresholdShift states the
// relation between the two nulls: replacing the equal-share (uniform-day) null
// with the corpus's own document-sampling null multiplies the expected
// occupancy by r(n) = E_sampled/E_uniform ≤ 1, which maps the gate κ ≥ τ onto
// the uniform-day gate κ ≥ 1 - r(n)(1-τ). The replacement is therefore not a
// re-parameterization of one operating point but a frequency-dependent
// tightening: busy-day corpora (r < 1) demand more collapse from frequent terms
// and leave rare terms unchanged.
func TestOccupancyDocumentSamplingIsAFrequencyDependentThresholdShift(t *testing.T) {
	// Bursty synthetic corpus: 1,200 days, document mass 2× more concentrated
	// than uniform (one heavy day, a mid tier, and a long singleton tail).
	var counts []int32
	for i := 0; i < 1; i++ {
		counts = append(counts, 120)
	}
	for i := 0; i < 60; i++ {
		counts = append(counts, 6)
	}
	for i := 0; i < 900; i++ {
		counts = append(counts, 1)
	}
	totalN := 0.0
	for _, c := range counts {
		totalN += float64(c)
	}
	m := float64(len(counts))

	prev := 1.0
	for _, n := range []float64{2, 5, 10, 20, 50, 100, 200, 400} {
		eSampled := sampledOccupiedDaysExact(counts, totalN, n)
		eUniform := expectedOccupiedDaysOf(n, m)
		r := eSampled / eUniform
		if r > 1+1e-12 {
			t.Fatalf("document sampling cannot occupy more days than the equal-share null: n=%v r=%v", n, r)
		}
		if r < 0.5 {
			t.Fatalf("r(n) collapsed implausibly: n=%v r=%v", n, r)
		}
		prev = math.Min(prev, r)
		// Gate equivalence: κ_sampled ≥ τ ⟺ κ_uniform ≥ 1 - r(1-τ).
		for _, tau := range []float64{0.1353, 1 / math.E, 0.5} {
			for _, dObs := range []float64{1, 5, 17, 40} {
				if dObs > eSampled {
					continue
				}
				kSampled := math.Max(0, 1-dObs/eSampled)
				shifted := 1 - r*(1-tau)
				kUniform := 1 - dObs/eUniform
				if (kSampled >= tau) != (kUniform >= shifted-1e-12) {
					t.Fatalf("gate mismatch n=%v d=%v tau=%v: sampled %v uniform %v shifted %v",
						n, dObs, tau, kSampled, kUniform, shifted)
				}
			}
		}
	}
	t.Logf("bursty corpus N=%v M=%v peaked at r=%v: document sampling only ever tightens the gate (τ' = 1-r(1-τ) ≥ τ) and does so frequency-dependently", totalN, m, prev)
}
