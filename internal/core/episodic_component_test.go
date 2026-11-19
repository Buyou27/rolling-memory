// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"fmt"
	"math"
	"testing"
)

// epTestDoc builds a minimal single-term unit at the given timestamp so the
// episodic partition is exercised through the real BuildIndex -> ensureClusterState
// chain rather than a parallel reimplementation.
func epTestDoc(id string, ts int64) Doc {
	h := uint64(len(id))*1e9 + uint64(ts%1e6) + 7
	return Doc{
		UnitID:     id,
		OccurredAt: ts,
		Terms: map[uint64]Term{
			h: {Hash: h, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: h, Sent: 1, Slot: 1},
		},
		Counts:    map[uint64]int{h: 1},
		Positions: map[uint64][]TermPosition{h: {{Sentence: 0, Slot: 1}}},
	}
}

func episodePartition(t *testing.T, times map[string]int64) map[string]int32 {
	t.Helper()
	docs := make([]Doc, 0, len(times))
	for id, ts := range times {
		docs = append(docs, epTestDoc(id, ts))
	}
	idx := BuildIndex(docs)
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	if st == nil {
		t.Fatal("ensureClusterState returned nil")
	}
	out := make(map[string]int32, len(times))
	for id := range times {
		out[id] = st.episodeOf[idx.UnitOf[id]]
	}
	return out
}

// samePartition reports whether two labelings induce the same set partition
// (component ids may be renumbered; only co-membership must agree).
func samePartition(a, b map[string]int32) bool {
	for x := range a {
		for y := range a {
			if (a[x] == a[y]) != (b[x] == b[y]) {
				return false
			}
		}
	}
	return true
}

// TestEpisodicComponentTimeShiftInvariance verifies the structural-channel
// phase-freedom theorem: the episodic gap-split partition is strictly invariant
// under any global time translation, whereas a fixed calendar-day grid is not
// (the test corpus straddles a UTC midnight). A burst separated by seconds
// across 23:59 -> 00:01 stays in one component.
func TestEpisodicComponentTimeShiftInvariance(t *testing.T) {
	midnight := int64(1700006400) // divisible by 86400 (exact UTC midnight)
	base := map[string]int64{
		// Cross-midnight burst: 23:59:50 / 00:00:10 / 00:01:30.
		"burst-a": midnight - 10,
		"burst-b": midnight + 10,
		"burst-c": midnight + 90,
		// Same-calendar-day but far simultaneous session ~9h later.
		"far-session-a": midnight + 9*3600,
		"far-session-b": midnight + 9*3600 + 25,
		// Distant decoy days away.
		"decoy": midnight + 86400*12,
	}
	ref := episodePartition(t, base)
	if ref["burst-a"] != ref["burst-b"] || ref["burst-b"] != ref["burst-c"] {
		t.Fatalf("cross-midnight burst split across components: %v", ref)
	}
	if ref["burst-a"] == ref["decoy"] {
		t.Fatalf("distant decoy merged into burst component: %v", ref)
	}
	dayChanged := false
	for _, shift := range []int64{2 * 3600, 7*3600 + 13*60, 86399} {
		shifted := make(map[string]int64, len(base))
		for id, ts := range base {
			shifted[id] = ts + shift
		}
		got := episodePartition(t, shifted)
		if !samePartition(ref, got) {
			t.Fatalf("episodic partition not shift-invariant under +%ds: ref=%v got=%v", shift, ref, got)
		}
		// Prove the test has teeth: the day grid must disagree somewhere.
		for id, ts := range base {
			if ts/86400 != shifted[id]/86400 && base["burst-a"]/86400 == base["burst-b"]/86400 {
				_ = id
				dayChanged = true
			}
		}
		if shift%86400 != 0 {
			for x := range base {
				for y := range base {
					if (base[x]/86400 == base[y]/86400) != (shifted[x]/86400 == shifted[y]/86400) {
						dayChanged = true
					}
				}
			}
		}
	}
	if !dayChanged {
		t.Fatal("probe corpus never crossed a day boundary under shift; test lost its teeth")
	}
}

// TestEpisodicComponentDegenerateStates covers the five-question degenerate
// edges: single unit, two buckets, all-identical timestamps, uniform flow
// (sigma=0), and untimed units.
func TestEpisodicComponentDegenerateStates(t *testing.T) {
	// Single unit: exactly one component.
	if p := episodePartition(t, map[string]int64{"only": 1700000000}); p["only"] != 0 {
		t.Fatalf("single unit component = %d, want 0", p["only"])
	}
	// Two buckets with gap g: tau = g under either estimator, strict > keeps
	// them merged (fail-open towards continuity).
	p2 := episodePartition(t, map[string]int64{"a": 1700000000, "b": 1700000000 + 7200})
	if p2["a"] != p2["b"] {
		t.Fatalf("two-bucket corpus split: %v", p2)
	}
	// All identical timestamps: one bucket, one component.
	p3 := episodePartition(t, map[string]int64{"x": 1700000000, "y": 1700000000, "z": 1700000000})
	if p3["x"] != p3["y"] || p3["y"] != p3["z"] {
		t.Fatalf("same-second corpus split: %v", p3)
	}
	// Uniform flow (sigma=0): tau equals the common gap, strict > never
	// splits, the whole stream is one component and the kernel alone decays.
	uniform := make(map[string]int64, 8)
	for i := int64(0); i < 8; i++ {
		uniform[string(rune('a'+i))] = 1700000000 + i*3600
	}
	pu := episodePartition(t, uniform)
	for id := range uniform {
		if pu[id] != pu["a"] {
			t.Fatalf("uniform flow split: %v", pu)
		}
	}
	// Untimed unit: sentinel -1, excluded from every component.
	docs := []Doc{epTestDoc("timed", 1700000000), epTestDoc("untimed", 0)}
	idx := BuildIndex(docs)
	st := NewEngine(idx).ensureClusterState()
	if st.episodeOf[idx.UnitOf["untimed"]] != -1 {
		t.Fatalf("untimed unit got component %d, want -1", st.episodeOf[idx.UnitOf["untimed"]])
	}
	if st.episodeOf[idx.UnitOf["timed"]] != 0 {
		t.Fatalf("timed unit got component %d, want 0", st.episodeOf[idx.UnitOf["timed"]])
	}
}

// TestEpisodicComponentBurstSplitScale verifies at a larger bursty scale that
// gap-splitting lands on session granularity: dense bursts stay whole and the
// component count stays bounded by the burst count, linear in buckets.
func TestEpisodicComponentBurstSplitScale(t *testing.T) {
	times := make(map[string]int64, 400)
	base := int64(1700000000)
	for b := 0; b < 40; b++ {
		start := base + int64(b)*6*3600 // bursts 6h apart
		for i := 0; i < 10; i++ {
			times[string(rune('A'+b%26))+string(rune('a'+i))+string(rune('0'+b/26))] = start + int64(i)*20
		}
	}
	docs := make([]Doc, 0, len(times))
	for id, ts := range times {
		docs = append(docs, epTestDoc(id, ts))
	}
	idx := BuildIndex(docs)
	st := NewEngine(idx).ensureClusterState()
	if st.episodeCount < 2 || st.episodeCount > 40 {
		t.Fatalf("episodeCount = %d, want within (1, 40] for 40 bursts", st.episodeCount)
	}
	// Every intra-burst pair must share a component (gap 20s << tau).
	for b := 0; b < 40; b++ {
		first := string(rune('A'+b%26)) + "a" + string(rune('0'+b/26))
		last := string(rune('A'+b%26)) + "j" + string(rune('0'+b/26))
		if st.episodeOf[idx.UnitOf[first]] != st.episodeOf[idx.UnitOf[last]] {
			t.Fatalf("burst %d split across components", b)
		}
	}
	if math.IsNaN(float64(st.episodeCount)) {
		t.Fatal("unreachable")
	}
}

// TestEpisodicWarmDomainCrossMidnightCosine verifies that the subject-cos warm
// domain (which pairs get precomputed and served from cache) is keyed by
// episodic components instead of calendar-day buckets. The value semantics are
// unchanged — pairs outside the warm domain fall through to direct computation
// — so the assertions verify (a) cross-midnight bursts are co-episode and
// cache-served, (b) the precompute and read gates are exact duals (zero silent
// in-gate misses), and (c) untimed pairs stay outside the warm domain and take
// the direct-path value (declarative floors included).
func TestEpisodicWarmDomainCrossMidnightCosine(t *testing.T) {
	midnight := int64(1700006400)
	sharedA, sharedB := uint64(0xA1), uint64(0xB2)
	mk := func(id string, ts int64) Doc {
		return Doc{
			UnitID:     id,
			OccurredAt: ts,
			Terms: map[uint64]Term{
				sharedA: {Hash: sharedA, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: sharedA, Sent: 1, Slot: 1},
				sharedB: {Hash: sharedB, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: sharedB, Sent: 1, Slot: 2},
			},
			Counts:    map[uint64]int{sharedA: 1, sharedB: 1},
			Positions: map[uint64][]TermPosition{sharedA: {{Sentence: 0, Slot: 1}}, sharedB: {{Sentence: 0, Slot: 2}}},
		}
	}
	docs := []Doc{
		mk("pre-midnight", midnight-10),
		mk("post-midnight", midnight+10),
		mk("decoy-far", midnight+86400*30),
	}
	// Spacer noise keeps the pair outside the direct-neighbor band (span=2)
	// so the warm domain is exercised through the co-episode branch alone.
	for i := 0; i < 6; i++ {
		docs = append(docs, epTestDoc("spacer-"+string(rune('a'+i)), midnight+int64(i-3)))
	}
	docs = append(docs, epTestDoc("untimed-x", 0), epTestDoc("untimed-y", 0))
	idx := BuildIndex(docs)
	st := NewEngine(idx).ensureClusterState()
	a, b := idx.UnitOf["pre-midnight"], idx.UnitOf["post-midnight"]
	if st.episodeOf[a] != st.episodeOf[b] {
		t.Fatalf("cross-midnight burst not co-episode: %d vs %d", st.episodeOf[a], st.episodeOf[b])
	}
	if got, want := st.docSubjectCos(idx, nil, a, b), st.computeDocSubjectCos(idx, a, b); want <= 0 || got != want {
		t.Fatalf("cross-midnight co-episode cosine = %v, want direct %v > 0", got, want)
	}
	// Cache/read parity: every timed pair admitted by the warm domain must
	// return exactly the directly computed cosine, whether already cached or
	// calculated on demand. Untimed sentinel pairs (-1) take the direct pathway.
	for u := 0; u < idx.N; u++ {
		for v := u + 1; v < idx.N; v++ {
			x, y := int32(u), int32(v)
			if (st.episodeOf[x] >= 0 && st.episodeOf[x] == st.episodeOf[y]) || st.isDirectTimeNeighbor(x, y) {
				if got, want := st.docSubjectCos(idx, nil, x, y), st.computeDocSubjectCos(idx, x, y); got != want {
					t.Fatalf("warm-domain pair (%d,%d) cache=%v direct=%v", u, v, got, want)
				}
			}
		}
	}
	ux, uy := idx.UnitOf["untimed-x"], idx.UnitOf["untimed-y"]
	if st.episodeOf[ux] != -1 || st.episodeOf[uy] != -1 {
		t.Fatalf("untimed units got components %d/%d, want -1", st.episodeOf[ux], st.episodeOf[uy])
	}
	// Untimed pairs are not part of the precompute population, so the read gate
	// must fall through to direct computation instead of serving a cached zero:
	// the warm domain chooses which pairs are cached, never the value.
	if got, want := st.docSubjectCos(idx, nil, ux, uy), st.computeDocSubjectCos(idx, ux, uy); got != want {
		t.Fatalf("untimed pair cache=%v direct=%v (cache must not change the value)", got, want)
	}
	// A declared citation edge is timing-independent: an untimed cited pair
	// keeps the full 1/e declarative floor via the direct pathway. Distinct
	// content terms keep the content cosine at zero so the floor itself shows.
	mkCite := func(id string, h uint64) Doc {
		return Doc{
			UnitID:     id,
			OccurredAt: 0,
			Terms:      map[uint64]Term{h: {Hash: h, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: h, Sent: 1, Slot: 1}},
			Counts:     map[uint64]int{h: 1},
			Positions:  map[uint64][]TermPosition{h: {{Sentence: 0, Slot: 1}}},
		}
	}
	citeDocs := []Doc{
		mkCite("cite-a", 0xC1AE01),
		mkCite("cite-b", 0xC1AE02),
	}
	citeDocs[0].CitedIDs = []string{"cite-b"}
	citeIdx := BuildIndex(citeDocs)
	citeSt := NewEngine(citeIdx).ensureClusterState()
	ca, cb := citeIdx.UnitOf["cite-a"], citeIdx.UnitOf["cite-b"]
	if got, want := citeSt.docSubjectCos(citeIdx, nil, ca, cb), 1/eulerNumber; math.Abs(got-want) > 1e-12 {
		t.Fatalf("untimed cited pair cosine = %v, want citation floor %v", got, want)
	}
}

// TestEpisodeRostersBatchMatchesPerUnitScan pins the batch roster snapshot to
// the single-unit view: for every unit the component id and the member list
// must match EpisodeRosterOf bit-for-bit, including ascending order, the
// untimed sentinel and out-of-range units.
func TestEpisodeRostersBatchMatchesPerUnitScan(t *testing.T) {
	const base = int64(1700000000)
	var docs []Doc
	// Four bursts of six units, 6h apart, with intra-burst spacing well below
	// tau_episode so each burst stays one component.
	for b := 0; b < 4; b++ {
		start := base + int64(b)*6*3600
		for i := 0; i < 6; i++ {
			docs = append(docs, epTestDoc(fmt.Sprintf("burst-%d-%d", b, i), start+int64(i)*20))
		}
	}
	docs = append(docs, epTestDoc("untimed-a", 0), epTestDoc("untimed-b", 0))
	idx := BuildIndex(docs)
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	rosters := eng.EpisodeRosters()
	if len(rosters) != st.episodeCount {
		t.Fatalf("EpisodeRosters len = %d, want episodeCount %d", len(rosters), st.episodeCount)
	}
	covered := 0
	for ep, roster := range rosters {
		if len(roster) == 0 {
			t.Fatalf("episode %d has no members", ep)
		}
		for i, u := range roster {
			if i > 0 && roster[i-1] >= u {
				t.Fatalf("episode %d roster not strictly ascending: %v", ep, roster)
			}
			if got := st.episodeOf[u]; got != int32(ep) {
				t.Fatalf("episode %d contains unit %d labeled %d", ep, u, got)
			}
		}
		covered += len(roster)
	}
	if covered != 24 {
		t.Fatalf("rosters cover %d units, want 24 timed units", covered)
	}
	for u := 0; u < idx.N; u++ {
		want := eng.EpisodeRosterOf(int32(u))
		ep := eng.EpisodeIDOf(int32(u))
		if ep < 0 {
			if want != nil {
				t.Fatalf("unit %d untimed but roster = %v", u, want)
			}
			continue
		}
		got := rosters[ep]
		if len(got) != len(want) {
			t.Fatalf("unit %d roster len = %d, want %d", u, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("unit %d roster[%d] = %d, want %d", u, i, got[i], want[i])
			}
		}
	}
	if got := eng.EpisodeIDOf(-1); got != -1 {
		t.Fatalf("EpisodeIDOf(-1) = %d, want -1", got)
	}
	if got := eng.EpisodeIDOf(int32(idx.N)); got != -1 {
		t.Fatalf("EpisodeIDOf(N) = %d, want -1", got)
	}
	if got := eng.EpisodeRosterOf(int32(idx.N)); got != nil {
		t.Fatalf("EpisodeRosterOf(N) = %v, want nil", got)
	}
}

// TestEpisodeRostersDriveFractalFiberAnnex checks the delivery annex still uses
// the episode roster: every fiber comes from the delivered group's component,
// no unit is duplicated across fibers/chain/hits, and the annex is drawn from
// the same roster the batch snapshot exposes.
func TestEpisodeRostersDriveFractalFiberAnnex(t *testing.T) {
	const base = int64(1700000000)
	var docs []Doc
	for i := 0; i < 5; i++ {
		docs = append(docs, epTestDoc(fmt.Sprintf("fiber-%d", i), base+int64(i)*20))
	}
	docs = append(docs, epTestDoc("decoy", base+86400*30))
	idx := BuildIndex(docs)
	eng := NewEngine(idx)
	first := idx.UnitOf["fiber-0"]
	ep := eng.EpisodeIDOf(first)
	if ep < 0 {
		t.Fatal("expected a timed episode for fiber-0")
	}
	roster := eng.EpisodeRosters()[ep]
	inRoster := make(map[string]bool, len(roster))
	for _, u := range roster {
		inRoster[idx.UnitIDs[u]] = true
	}
	hits := []Hit{{UnitID: "fiber-0", Energy: 34.5}}
	groups := eng.GroupHitsByClusterFractalWithQuery(hits, DefaultEpisodeFiberCap, nil)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	seen := map[string]bool{"fiber-0": true}
	for _, step := range groups[0].ExplanationChain {
		if seen[step.UnitID] {
			t.Fatalf("chain step %s duplicated", step.UnitID)
		}
		seen[step.UnitID] = true
	}
	for _, fid := range groups[0].FiberUnitIDs {
		if seen[fid] {
			t.Fatalf("fiber %s duplicated across chain/hits", fid)
		}
		if !inRoster[fid] {
			t.Fatalf("fiber %s is not a member of episode %d (%v)", fid, ep, roster)
		}
		seen[fid] = true
	}
	for _, u := range roster {
		if !seen[idx.UnitIDs[u]] {
			t.Fatalf("episode member %s missing from fibers/chain/hits", idx.UnitIDs[u])
		}
	}
	if hits[0].UnitID != "fiber-0" || hits[0].Energy != 34.5 {
		t.Fatalf("0-ULP violation on hits slice: %+v", hits)
	}
}

// BenchmarkEpisodeRosterBatch contrasts the per-unit roster scan with the
// single-pass batch snapshot used by graph/delivery callers.
func BenchmarkEpisodeRosterBatch(b *testing.B) {
	const units = 2048
	docs := make([]Doc, units)
	for i := range docs {
		docs[i] = epTestDoc(fmt.Sprintf("bench-%04d", i), 1700000000+int64(i/32)*6*3600+int64(i%32)*20)
	}
	eng := NewEngine(BuildIndex(docs))
	eng.ensureClusterState()
	b.Run("per-unit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for u := 0; u < units; u++ {
				_ = eng.EpisodeRosterOf(int32(u))
			}
		}
	})
	b.Run("batch", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = eng.EpisodeRosters()
		}
	})
}
