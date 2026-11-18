// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package eval

import (
	"os"
	"strings"

	core "github.com/rocareer/rolling-memory/internal/core"
)

// coverageFloorEnabled gates the salient-coverage floor arm. The default (off)
// keeps every recorded reading byte-identical: the delivery window then follows
// the pure natural decay envelope. ROLLING_MEMORY_COVERAGE_FLOOR=1 couples the
// envelope with the kernel coverage floor, so the window never closes while a
// salient query group that some in-pool candidate covers has not been delivered.
func coverageFloorEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ROLLING_MEMORY_COVERAGE_FLOOR"))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// salientFirstCoverRanks returns, per salient query group, the 1-based rank of
// the first candidate in retIDs[:maxItems] that formally covers it. Salience
// follows the endogenous second-order scale the selection pipeline itself uses:
// a query group is salient when its corpus term mass reaches the query's own
// energy scale mu(Q) = sum(m^2)/sum(m) over the query's groups. Groups covered
// nowhere inside the inspected pool contribute no rank — the candidate scope
// cannot certify whole-archive absence, and padding to the cap would only add
// evidence-free items.
//
// Read-only by construction: it walks the immutable index through the engine's
// public per-unit entry view and never touches ranking or energies.
func (e *LocalEngine) salientFirstCoverRanks(question string, retIDs []string, maxItems int) []int {
	if e == nil || len(retIDs) == 0 || maxItems <= 0 {
		return nil
	}
	eng, az, err := e.ensureCoreRMEngine()
	if err != nil || eng == nil || az == nil || eng.Index == nil {
		return nil
	}
	limit := len(retIDs)
	if limit > maxItems {
		limit = maxItems
	}
	// The query extraction and the per-unit entry walks read engine state that
	// recall also touches, so they share the same query mutex as recall itself.
	e.coreQueryMu.Lock()
	defer e.coreQueryMu.Unlock()
	qTerms, _, _ := az.ExtractQueryWithPositions(eng.Index, question)
	if len(qTerms) == 0 {
		return nil
	}
	groupMass := make(map[uint64]float64, len(qTerms))
	for _, atom := range qTerms {
		g := atom.Group
		if g == 0 {
			g = atom.Hash
		}
		m, ok := eng.Index.TermMass(g)
		if !ok || m <= 0 {
			continue
		}
		if m > groupMass[g] {
			groupMass[g] = m
		}
	}
	var tot, sq float64
	for _, m := range groupMass {
		tot += m
		sq += m * m
	}
	if tot <= 0 {
		return nil
	}
	muQ := sq / tot
	salient := make(map[uint64]bool, len(groupMass))
	for g, m := range groupMass {
		if m >= muQ {
			salient[g] = true
		}
	}
	if len(salient) == 0 {
		return nil
	}
	covered := make(map[uint64]bool, len(salient))
	ranks := make([]int, 0, len(salient))
	for rank := 0; rank < limit; rank++ {
		unitID := strings.TrimSpace(retIDs[rank])
		if unitID == "" {
			continue
		}
		eng.ForEachUnitDocEntry(unitID, func(v core.DocEntryView) {
			g := v.Group
			if g == 0 {
				g = v.ID
			}
			if salient[g] && !covered[g] {
				covered[g] = true
				ranks = append(ranks, rank+1)
			}
		})
		if len(covered) == len(salient) {
			break
		}
	}
	if len(ranks) == 0 {
		return nil
	}
	return ranks
}
