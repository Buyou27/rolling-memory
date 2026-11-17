// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import (
	"math"
	"strings"
	"time"

	"github.com/rocareer/rolling-memory/sdk/go/public/closedset"
)

// OrdinalSpanAnnotation is the delivery-layer fact statement produced by the
// corpus-endogenous ordinal day resolver. It is delivery metadata only and is
// never consumed as a primary day-gating beacon: day-level signal is
// common-mode across the intra-day population, whereas as a delivery annotation
// it provides an explicit boundary hint for the downstream reader.
type OrdinalSpanAnnotation struct {
	Polarity    string `json:"polarity"`      // "begin" | "end"
	ResolvedDay string `json:"resolved_day"`  // ISO day the span boundary lands on
	SingleRun   bool   `json:"single_run"`    // emission gate: exactly one dense run
	RunSpanDays int    `json:"run_span_days"` // inclusive day span of the resolved run
	AnchorDF    int    `json:"anchor_df"`     // document frequency of the proper anchor
}

// ResolveOrdinalSpanAnnotation detects an ordinal-polarity frame in the query
// text and, when the corpus time geometry is unambiguous, resolves the span
// boundary day as a delivery annotation:
//
//  1. Polarity: the query carries surfaces from exactly one polarity class of
//     the Part III table (closedset.LookupOrdinalPolarity; both or neither ->
//     no frame, nil).
//  2. Anchor: the proper-cased primary query term with df in [2, N/e]
//     (dedup floor = minimum span support; N/e ≈ 36.8% corpus extension
//     bound) holding maximal group mass.
//  3. Day histogram: the anchor's postings per reference-clock UTC day; days
//     below maxUnits/e^2 (≈ 13.5% of the peak day count) are sparse-mention
//     noise; survivors merge into runs tolerating one missing day.
//  4. Emission gate: unambiguous dominant run; polarity picks the boundary
//     (begin -> first day, end -> last day).
func (a *TermExtractor) ResolveOrdinalSpanAnnotation(idx QueryIndexView, queryText string) *OrdinalSpanAnnotation {
	if a == nil || a.enc == nil || queryIndexAbsent(idx) || idx.CorpusSize() == 0 || queryText == "" {
		return nil
	}
	var hasBegin, hasEnd bool
	for _, sent := range splitSentences(CanonicalizeApostrophe(queryText)) {
		for _, w := range SplitWords(sent) {
			lw := strings.ToLower(strings.Trim(w, ",.;:!?()[]{}\"'"))
			switch closedset.LookupOrdinalPolarity(lw) {
			case -1:
				hasBegin = true
			case 1:
				hasEnd = true
			}
		}
	}
	if hasBegin == hasEnd {
		return nil
	}
	pole := "begin"
	if hasEnd {
		pole = "end"
	}

	qTerms, _, _ := a.ExtractQueryWithPositions(idx, queryText)
	nOverE := float64(idx.CorpusSize()) / math.E
	var anchorH uint64
	var anchorMass float64
	for h, qa := range qTerms {
		if qa.Role != RolePrimary || qa.Flags&FlagHasUpper == 0 {
			continue
		}
		df := idx.TermDF(h)
		if df < 2 || float64(df) > nOverE {
			continue
		}
		g := qa.Group
		if g == 0 {
			g = h
		}
		if m, ok := idx.TermMassOf(g); ok && m > anchorMass {
			anchorMass = m
			anchorH = h
		}
	}
	if anchorH == 0 {
		return nil
	}

	const noDay = int64(-1) << 62
	dayUnits := make(map[int64]int, 32)
	maxUnits := 0
	idx.ForEachPostingUnit(anchorH, func(unit int32) {
		d := noDay
		if occurredAt, narratedAt, ingestedAt, ok := idx.UnitTime(unit); ok {
			ts := occurredAt
			if ts <= 0 {
				ts = narratedAt
			}
			if ts <= 0 {
				ts = ingestedAt
			}
			if ts > 0 {
				d = ts / 86400
			}
		}
		if d == noDay {
			return
		}
		dayUnits[d]++
		if dayUnits[d] > maxUnits {
			maxUnits = dayUnits[d]
		}
	})
	if len(dayUnits) == 0 {
		return nil
	}
	runLo, runHi, dStar, ok := resolveOrdinalSpanBoundary(dayUnits, maxUnits, pole == "end")
	if !ok {
		return nil
	}
	return &OrdinalSpanAnnotation{
		Polarity:    pole,
		ResolvedDay: time.Unix(dStar*86400, 0).UTC().Format("2006-01-02"),
		SingleRun:   true,
		RunSpanDays: int(runHi-runLo) + 1,
		AnchorDF:    idx.TermDF(anchorH),
	}
}

// resolveOrdinalSpanBoundary resolves an unambiguous span boundary day from an
// anchor's per-day posting histogram using a two-stage relative density filter:
//  1. Broad support pass (floor = maxUnits / e^2 ≈ 13.5% of peak day, tolerating a 1-day gap):
//     if all surviving days form a single continuous run, its boundary is returned
//     directly (preserving low-count arrival/departure boundary days of single trips).
//  2. Core dominance fallback (floor = maxUnits / e ≈ 36.8% of peak day, strictly contiguous
//     days): when pre-event notifications split the broad histogram into
//     multiple runs, contiguous high-density runs are formed; if a unique core run
//     exists or the largest core run dominates the runner-up by at least e
//     (count(R_1) >= e * count(R_2), rejecting multi-visit ties), its boundary day
//     is returned.
func resolveOrdinalSpanBoundary(dayUnits map[int64]int, maxUnits int, isEnd bool) (runLo, runHi, dStar int64, ok bool) {
	if len(dayUnits) == 0 || maxUnits <= 0 {
		return 0, 0, 0, false
	}
	floor2 := float64(maxUnits) / (math.E * math.E)
	days := make([]int64, 0, len(dayUnits))
	for d, n := range dayUnits {
		if float64(n) >= floor2 {
			days = append(days, d)
		}
	}
	if len(days) == 0 {
		return 0, 0, 0, false
	}
	for i := 1; i < len(days); i++ {
		for j := i; j > 0 && days[j] < days[j-1]; j-- {
			days[j], days[j-1] = days[j-1], days[j]
		}
	}
	runLo, runHi = days[0], days[0]
	singleRun := true
	for _, d := range days[1:] {
		if d <= runHi+2 {
			runHi = d
			continue
		}
		singleRun = false
		break
	}
	if !singleRun {
		floor1 := float64(maxUnits) / math.E
		coreDays := days[:0]
		for _, d := range days {
			if float64(dayUnits[d]) >= floor1 {
				coreDays = append(coreDays, d)
			}
		}
		if len(coreDays) == 0 {
			return 0, 0, 0, false
		}
		type dayRun struct {
			lo, hi int64
			cnt    int
		}
		var coreRuns []dayRun
		cur := dayRun{lo: coreDays[0], hi: coreDays[0], cnt: dayUnits[coreDays[0]]}
		for _, d := range coreDays[1:] {
			if d <= cur.hi+1 {
				cur.hi = d
				cur.cnt += dayUnits[d]
			} else {
				coreRuns = append(coreRuns, cur)
				cur = dayRun{lo: d, hi: d, cnt: dayUnits[d]}
			}
		}
		coreRuns = append(coreRuns, cur)
		bestIdx := 0
		secondCnt := 0
		for i := 1; i < len(coreRuns); i++ {
			if coreRuns[i].cnt > coreRuns[bestIdx].cnt {
				secondCnt = coreRuns[bestIdx].cnt
				bestIdx = i
			} else if coreRuns[i].cnt > secondCnt {
				secondCnt = coreRuns[i].cnt
			}
		}
		if len(coreRuns) > 1 && float64(coreRuns[bestIdx].cnt) < math.E*float64(secondCnt) {
			return 0, 0, 0, false
		}
		runLo, runHi = coreRuns[bestIdx].lo, coreRuns[bestIdx].hi
	}
	dStar = runLo
	if isEnd {
		dStar = runHi
	}
	return runLo, runHi, dStar, true
}
