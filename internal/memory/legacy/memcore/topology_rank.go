// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import (
	"strings"
	"sync/atomic"
)

// LegacyUndirectedTopologyMode is a test-only A/B toggle (default false).
// When true, closure traversal reverts to undirected 2-hop BFS and disables topology bridge / band gating.
var LegacyUndirectedTopologyMode atomic.Bool

// HierarchyMetaEdge represents a structural meta-graph edge between two uint64 node IDs in memory_meta.
// For Kind == "subtype_of", LHS is the specific hyponym (child phrase) and RHS is the general hypernym (parent head concept).
type HierarchyMetaEdge struct {
	LHS      uint64 `gorm:"column:lhs"`
	RHS      uint64 `gorm:"column:rhs"`
	Kind     string `gorm:"column:meta_kind"`
	Evidence string `gorm:"column:evidence"`
}

// TopologyRowView is a host-agnostic view of a scored candidate assertion for graph-topology ranking and deduplication.
type TopologyRowView struct {
	NodeID          uint64
	SubjectID       uint64
	RelID           uint64
	ObjectID        uint64
	ObjectLiteral   string
	SourceTraceHash string
	Status          string
	Score           float64
	LexScore        float64
	NonSubj         bool
	OrigNonSubj     bool
	DirectXO        bool
}

// TopologyBoost holds the additive score delta and signal promotion computed from graph topology.
type TopologyBoost struct {
	Delta       float64
	PromoteNS   bool
	PromoteOrig bool
}

// CausalDerivationEdge represents a directed lower-half cognitive algebra meta-edge
// (causes, derived_from, supersedes, conflicts_with) between assertion/entity nodes.
// Universality: 100% language-agnostic pure graph operator (Axiom 22).
type CausalDerivationEdge struct {
	LHS      uint64 `gorm:"column:lhs"`
	RHS      uint64 `gorm:"column:rhs"`
	MetaKind string `gorm:"column:meta_kind"`
	Evidence string `gorm:"column:evidence"`
}

// ExpandCausalDerivationGraph performs bounded bidirectional graph traversal along
// lower-half propositional edges (`causes` and `derived_from`, up to `depth` hops)
// starting from `seedAssertions`.
//   - Forward/backward along `causes` (cause <-> effect) and `derived_from` (conclusion <-> premise);
//   - Filters out morphological `sx-sxo-refinement` edges so only genuine propositional links traverse;
//   - Returns newly discovered target node IDs (excluding initial seeds), their hop distance (1..depth),
//     and the originating root seed ID `rootSeedOf[target]` for score propagation and C6 wormhole coupling.
func ExpandCausalDerivationGraph(
	seedAssertions []uint64,
	depth, fanout, maxCap int,
	fetch func(cur []uint64, limit int) ([]CausalDerivationEdge, error),
) (expanded []uint64, hopByNode map[uint64]int, rootSeedOf map[uint64]uint64, err error) {
	hopByNode = make(map[uint64]int)
	rootSeedOf = make(map[uint64]uint64)
	if len(seedAssertions) == 0 || depth <= 0 || fetch == nil {
		return nil, hopByNode, rootSeedOf, nil
	}
	if depth > 3 {
		depth = 3
	}
	if fanout <= 0 || fanout > 64 {
		fanout = 24
	}
	if maxCap <= 0 || maxCap > 256 {
		maxCap = 64
	}
	seen := make(map[uint64]bool, len(seedAssertions)*2)
	frontier := make([]uint64, 0, len(seedAssertions))
	for _, id := range seedAssertions {
		if id > 0 && !seen[id] {
			seen[id] = true
			rootSeedOf[id] = id
			frontier = append(frontier, id)
		}
	}
	for hop := 1; hop <= depth && len(frontier) > 0 && len(expanded) < maxCap; hop++ {
		edges, ferr := fetch(frontier, len(frontier)*fanout)
		if ferr != nil {
			return expanded, hopByNode, rootSeedOf, ferr
		}
		if len(edges) == 0 {
			break
		}
		nextFrontier := make([]uint64, 0, len(edges))
		perParent := make(map[uint64]int, len(frontier))
		for _, e := range edges {
			if e.LHS == 0 || e.RHS == 0 || e.LHS == e.RHS {
				continue
			}
			if e.Evidence == "sx-sxo-refinement" || strings.HasPrefix(e.Evidence, "bitfield-") {
				continue
			}
			if e.MetaKind != "causes" && e.MetaKind != "derived_from" {
				continue
			}
			// Bidirectional chase: if LHS is in current frontier, reach RHS; if RHS is in frontier, reach LHS.
			for _, pair := range [][2]uint64{{e.LHS, e.RHS}, {e.RHS, e.LHS}} {
				src, dst := pair[0], pair[1]
				root, okSrc := rootSeedOf[src]
				if !okSrc || seen[dst] || perParent[src] >= fanout {
					continue
				}
				seen[dst] = true
				perParent[src]++
				hopByNode[dst] = hop
				rootSeedOf[dst] = root
				expanded = append(expanded, dst)
				nextFrontier = append(nextFrontier, dst)
				if len(expanded) >= maxCap {
					break
				}
			}
			if len(expanded) >= maxCap {
				break
			}
		}
		frontier = nextFrontier
	}
	return expanded, hopByNode, rootSeedOf, nil
}

// ComputeStrongNegativeExcludedSet computes the Pauli exclusion set of assertion node IDs
// that are superseded or contradicted by the active primary assertions `activePrimaryIDs`.
//   - For `supersedes` (LHS=new supersedes RHS=old): RHS is always excluded whenever an active supersedes edge targets it;
//   - For `conflicts_with` (symmetric exclusion): if one endpoint is in `activePrimaryIDs` and the other is not, the non-primary endpoint is excluded from weak C6 companion slots.
func ComputeStrongNegativeExcludedSet(activePrimaryIDs []uint64, edges []CausalDerivationEdge) map[uint64]bool {
	excluded := make(map[uint64]bool)
	if len(edges) == 0 {
		return excluded
	}
	prim := make(map[uint64]bool, len(activePrimaryIDs))
	for _, id := range activePrimaryIDs {
		if id > 0 {
			prim[id] = true
		}
	}
	for _, e := range edges {
		if e.LHS == 0 || e.RHS == 0 || e.LHS == e.RHS {
			continue
		}
		switch e.MetaKind {
		case "supersedes":
			// LHS (new) supersedes RHS (old): old assertion must never resurrect via weak C6 container.
			excluded[e.RHS] = true
		case "conflicts_with":
			if prim[e.LHS] && !prim[e.RHS] {
				excluded[e.RHS] = true
			} else if prim[e.RHS] && !prim[e.LHS] {
				excluded[e.LHS] = true
			}
		}
	}
	return excluded
}
