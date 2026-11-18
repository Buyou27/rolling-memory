// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import "testing"

func TestExpandCausalDerivationGraphAndPauliExclusionAdversarial(t *testing.T) {
	// Graph:
	//   100 --[causes (llm-closed-set)]--> 200 --[derived_from (llm-closed-set)]<-- 300
	//   100 --[derived_from (sx-sxo-refinement)]--> 999 (noise: morphological refinement must be ignored)
	//   200 --[causes]--> 100 (adversarial cycle: must not loop infinitely)
	allEdges := []CausalDerivationEdge{
		{LHS: 100, RHS: 200, MetaKind: "causes", Evidence: "llm-closed-set"},
		{LHS: 300, RHS: 200, MetaKind: "derived_from", Evidence: "llm-closed-set"},
		{LHS: 100, RHS: 999, MetaKind: "derived_from", Evidence: "sx-sxo-refinement"},
		{LHS: 200, RHS: 100, MetaKind: "causes", Evidence: "llm-closed-set"},
	}
	exp, hops, roots, err := ExpandCausalDerivationGraph([]uint64{100}, 2, 16, 64, func(cur []uint64, _ int) ([]CausalDerivationEdge, error) {
		inCur := make(map[uint64]bool, len(cur))
		for _, id := range cur {
			inCur[id] = true
		}
		var out []CausalDerivationEdge
		for _, e := range allEdges {
			if inCur[e.LHS] || inCur[e.RHS] {
				out = append(out, e)
			}
		}
		return out, nil
	})
	if err != nil {
		t.Fatalf("ExpandCausalDerivationGraph error: %v", err)
	}
	if len(exp) != 2 || hops[200] != 1 || hops[300] != 2 || roots[200] != 100 || roots[300] != 100 {
		t.Fatalf("expected 2-hop causal/derivation expansion [200(hop=1), 300(hop=2)] rooted at 100, got exp=%v hops=%v roots=%v", exp, hops, roots)
	}
	if _, ok := hops[999]; ok {
		t.Fatalf("sx-sxo-refinement edge to 999 must be ignored in propositional causal chase")
	}

	// Pauli Exclusion inside C6 weak container:
	//   501 --[supersedes]--> 500 (500 must be excluded)
	//   100 --[conflicts_with]--> 600 (100 is in active primary set => 600 must be excluded)
	//   700 --[conflicts_with]--> 701 (both in primary set => neither auto-excluded)
	negEdges := []CausalDerivationEdge{
		{LHS: 501, RHS: 500, MetaKind: "supersedes", Evidence: "closes_prior"},
		{LHS: 100, RHS: 600, MetaKind: "conflicts_with", Evidence: "llm-closed-set"},
		{LHS: 700, RHS: 701, MetaKind: "conflicts_with", Evidence: "llm-closed-set"},
	}
	excl := ComputeStrongNegativeExcludedSet([]uint64{100, 700, 701}, negEdges)
	if !excl[500] || !excl[600] {
		t.Fatalf("expected 500 (superseded) and 600 (conflicting with primary 100) to be excluded, got %v", excl)
	}
	if excl[100] || excl[501] || excl[700] || excl[701] {
		t.Fatalf("unexpected exclusion of primary assertions: %v", excl)
	}
}
