// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"time"
)

// HitClusterGroup is the read-only delivery view of one cognitive cluster:
// hits sharing a SpacetimeCluster-adjacent origin are grouped for downstream
// LLM consumption without touching hit order, energy, or membership.
type HitClusterGroup struct {
	GroupID         int      `json:"group_id"`
	Day             string   `json:"day,omitempty"`
	UnitIDs         []string `json:"unit_ids"`
	PeakUnitID      string   `json:"peak_unit_id"`
	JointEnergyNats float64  `json:"joint_energy_nats"`
	// FiberUnitIDs is the fractal fiber annex (horizontal antichain): the
	// remaining roster of the group's endogenous episodic component that did NOT
	// make the delivered hit window or the directed explanation chain, in roster
	// order (≈ chronological), capped per group.
	// It is delivery metadata only — fibers never occupy hit seats, never carry
	// energy, and never affect ranking. Empty when the group's episode is fully
	// covered by the hits themselves (or the group has no episodic component).
	FiberUnitIDs []string `json:"fiber_unit_ids,omitempty"`
	// ExplanationChain is the directed explanation chain annex (vertical poset
	// chain, Slice S7): starting from the group's PeakUnitID, it traces the
	// monotonic upward ExplainsMe chain u_0 ◁ u_1 ◁ ... ◁ u_L under the four
	// parameter-free conservation laws (anti-echo, directional dominance,
	// prefix-cumulative salient novelty, and DPI attenuation). Like FiberUnitIDs,
	// it is read-only delivery metadata (0-ULP on Hits).
	ExplanationChain []ExplanationChainStep `json:"explanation_chain,omitempty"`
}

// GroupHitsByCluster reorganizes an already-ranked hit list into cognitive
// cluster groups (the view is derived strictly from the hits slice plus
// immutable index metadata, so the hits themselves stay bit-for-bit untouched).
//
// Grouping key, in priority order:
//  1. the unit's endogenous episodic component (gap-split partition):
//     time-shift invariant, so a midnight-spanning event stays one group
//     instead of inheriting the UTC calendar-day phase cut;
//  2. units without any clock fall back to their SpacetimeCluster bucket id;
//  3. units unknown to the index form singleton groups (fail-open: a delivery
//     view must never drop a hit).
//
// Day is a display-only calendar annotation (UTC day of the group's
// best-ranked timed member); it never participates in the grouping key.
// Groups are ordered by their best (lowest-index) hit, so the first group
// always contains the top-1 hit; UnitIDs inside a group preserve hit order.
// JointEnergyNats sums member energies as a display aid only (no dedup claim,
// no ranking consequence).
func (e *Engine) GroupHitsByCluster(hits []Hit) []HitClusterGroup {
	if e == nil || e.Index == nil || len(hits) == 0 {
		return nil
	}
	idx := e.Index
	st := e.ensureClusterState()

	type groupKey struct {
		ep     int32
		bucket int32
	}
	keyOf := func(unitID string) (groupKey, int64) {
		u, ok := idx.LookupUnit(unitID)
		if !ok {
			return groupKey{ep: -1, bucket: -1}, 0
		}
		var ts int64
		if int(u) < len(st.refTimes) {
			ts = st.refTimes[u]
		}
		if int(u) < len(st.episodeOf) {
			if ep := st.episodeOf[u]; ep >= 0 {
				return groupKey{ep: ep, bucket: -1}, ts
			}
		}
		if int(u) < len(st.clusters) {
			return groupKey{ep: -1, bucket: st.clusters[u]}, ts
		}
		return groupKey{ep: -1, bucket: -1}, ts
	}

	order := make([]groupKey, 0, 8)
	members := make(map[groupKey][]int, 8)
	dayTS := make(map[groupKey]int64, 8)
	hasCitations := st != nil && len(st.citePeers) > 0
	var hitUnits []int32
	if hasCitations {
		hitUnits = make([]int32, len(hits))
		for i, h := range hits {
			if u, ok := idx.LookupUnit(h.UnitID); ok {
				hitUnits[i] = u
			} else {
				hitUnits[i] = -1
			}
		}
	}
	for i, h := range hits {
		k, ts := keyOf(h.UnitID)
		if k.ep == -1 && k.bucket == -1 {
			// Unknown unit: singleton group keyed by its hit position.
			k = groupKey{ep: -1, bucket: int32(-2 - i)}
		}
		if _, seen := members[k]; !seen {
			if hasCitations && hitUnits[i] >= 0 {
				u := hitUnits[i]
				merged := false
				for _, ek := range order {
					for _, prevIdx := range members[ek] {
						if prevU := hitUnits[prevIdx]; prevU >= 0 && (st.hasCitationEdge(u, prevU) || st.hasCoCitationEdge(u, prevU)) {
							k = ek
							merged = true
							break
						}
					}
					if merged {
						break
					}
				}
			}
			if _, seenAfter := members[k]; !seenAfter {
				order = append(order, k)
			}
		}
		members[k] = append(members[k], i)
		if ts > 0 && dayTS[k] == 0 {
			dayTS[k] = ts
		}
	}

	out := make([]HitClusterGroup, 0, len(order))
	for gi, k := range order {
		idxs := members[k]
		g := HitClusterGroup{
			GroupID: gi + 1,
			UnitIDs: make([]string, 0, len(idxs)),
		}
		if ts := dayTS[k]; ts > 0 {
			g.Day = time.Unix(ts, 0).UTC().Format("2006-01-02")
		}
		peak := math.Inf(-1)
		for _, hi := range idxs {
			h := hits[hi]
			g.UnitIDs = append(g.UnitIDs, h.UnitID)
			g.JointEnergyNats += h.Energy
			if h.Energy > peak {
				peak = h.Energy
				g.PeakUnitID = h.UnitID
			}
		}
		out = append(out, g)
	}
	return out
}

// DefaultEpisodeFiberCap bounds the per-group fiber annex so dense episodes do
// not flood downstream readers while preserving same-episode context coverage.
const DefaultEpisodeFiberCap = 64

// GroupHitsByClusterFractal extends GroupHitsByCluster with the fractal fiber
// annex: each delivered group whose members carry an endogenous episodic
// component is annotated with the rest of that component's roster — the
// same-episode units that did not make the delivered hit window.
//
// Scale separation: the hits slice is the macro level (global energy
// competition, already truncated to the caller's delivery budget — "top-k cuts,
// never pads"), and the fiber annex is the micro level (intra-episode unfolding
// at zero seat cost). Fibers ride in roster order (unit order ≈ chronological),
// never carry energy, never occupy hit seats, and never touch ranking;
// perGroupCap <= 0 means uncapped.
//
// The base group fields are bit-for-bit identical to GroupHitsByCluster(hits),
// so hit coverage, intra-group hit order, and top-1 leadership are inherited
// by construction.
func (e *Engine) GroupHitsByClusterFractal(hits []Hit, perGroupCap int) []HitClusterGroup {
	return e.GroupHitsByClusterFractalWithQuery(hits, perGroupCap, nil)
}

// GroupHitsByClusterFractalWithQuery extends GroupHitsByClusterFractal with
// query-conditioned vertical explanation chain extraction: for each delivered
// cognitive cluster group, it traces the monotonic upward explanation chain
// from PeakUnitID (conditioned on qTerms when non-empty) and orthogonalizes
// the horizontal FiberUnitIDs antichain against the extracted chain steps so
// no unit is duplicated across Chain(L) and Antichain(m).
func (e *Engine) GroupHitsByClusterFractalWithQuery(hits []Hit, perGroupCap int, qTerms map[uint64]Term) []HitClusterGroup {
	groups := e.GroupHitsByCluster(hits)
	if len(groups) == 0 || e == nil || e.Index == nil {
		return groups
	}
	idx := e.Index
	st := e.ensureClusterState()
	delivered := make(map[string]bool, len(hits))
	for _, h := range hits {
		delivered[h.UnitID] = true
	}
	for gi := range groups {
		seenInChain := make(map[string]bool, 4)
		if peakID := groups[gi].PeakUnitID; peakID != "" {
			if rawChain, ok := e.TraceExplanationChainWithQuery(peakID, true, 0, qTerms); ok && len(rawChain) > 0 {
				groups[gi].ExplanationChain = rawChain
				for _, step := range rawChain {
					seenInChain[step.UnitID] = true
				}
			}
		}

		// Any member resolves the episode (group keys are episode-unique);
		// fallback-bucket and unknown-unit groups yield no episode roster, but
		// may still carry declarative citation/co-citation chain peers.
		var roster []int32
		for _, uid := range groups[gi].UnitIDs {
			if u, ok := idx.LookupUnit(uid); ok {
				if roster = e.EpisodeRosterOf(u); roster != nil {
					break
				}
			}
		}
		var chainPeers []int32
		if st != nil {
			for _, uid := range groups[gi].UnitIDs {
				u, ok := idx.LookupUnit(uid)
				if !ok {
					continue
				}
				chainPeers = append(chainPeers, st.citationPeersOf(u)...)
				chainPeers = append(chainPeers, st.coCitationPeersOf(u)...)
			}
		}
		if len(roster) == 0 && len(chainPeers) == 0 {
			continue
		}
		seenFiber := make(map[int32]bool, len(roster)+len(chainPeers))
		fibers := make([]string, 0, len(roster)+len(chainPeers))
		appendPeer := func(peer int32) bool {
			if peer < 0 || int(peer) >= len(idx.UnitIDs) || seenFiber[peer] {
				return true
			}
			seenFiber[peer] = true
			id := idx.UnitIDs[peer]
			if delivered[id] || seenInChain[id] {
				return true
			}
			if perGroupCap > 0 && len(fibers) >= perGroupCap {
				return false
			}
			fibers = append(fibers, string([]byte(id)))
			return true
		}
		for _, peer := range roster {
			if !appendPeer(peer) {
				break
			}
		}
		if perGroupCap <= 0 || len(fibers) < perGroupCap {
			for _, peer := range chainPeers {
				if !appendPeer(peer) {
					break
				}
			}
		}
		if len(fibers) > 0 {
			groups[gi].FiberUnitIDs = fibers
		}
	}
	return groups
}

// EpisodeRosterOf returns the full unit roster of the endogenous episodic
// component containing unit u (read-only delivery helper; empty when u carries
// no component). The roster is the component's complete population, not the
// query's hit window, so callers can expand a delivered group to its whole
// episode without touching ranking.
func (e *Engine) EpisodeRosterOf(u int32) []int32 {
	if e == nil || e.Index == nil {
		return nil
	}
	st := e.ensureClusterState()
	if int(u) >= len(st.episodeOf) {
		return nil
	}
	ep := st.episodeOf[u]
	if ep < 0 {
		return nil
	}
	roster := make([]int32, 0, 8)
	for v := range st.episodeOf {
		if st.episodeOf[v] == ep {
			roster = append(roster, int32(v))
		}
	}
	return roster
}
