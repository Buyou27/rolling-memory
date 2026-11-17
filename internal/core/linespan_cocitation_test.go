// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"testing"
)

// buildLineSpanDocs 构造同一物理文档（共享 DocHash=D）的 3 枚行坐标切片与 1 枚
// 异文档对照：s1(1..40) 与 s2(41..80) 行紧邻、s3(2001..2040) 远端、x 无 DocHash。
func buildLineSpanDocs() []Doc {
	mk := func(id string, doc uint64, ls, le int, term uint64, ts int64) Doc {
		return Doc{
			UnitID: id, OccurredAt: ts, DocHash: doc,
			LineStart: ls, LineEnd: le,
			Terms:  map[uint64]Term{term: {Hash: term}},
			Counts: map[uint64]int{term: 1},
		}
	}
	return []Doc{
		mk("s1", 0xD0C, 1, 40, 101, 1700000000),
		mk("s2", 0xD0C, 41, 80, 102, 1710000000),
		mk("s3", 0xD0C, 2001, 2040, 103, 1720000000),
		mk("x1", 0, 0, 0, 104, 1730000000),
	}
}

// TestLineSpanContinuityFloor 钉死同 DocHash 行坐标连续核的三个解析锚点：
// 紧邻切片（gap=0）地板达满额 1/e（与显式引证边同强度）、远端切片回落到
// 共享 DocHash 的碰撞惊奇度基础增益之上、无行坐标单元严格保持既有行为。
func TestLineSpanContinuityFloor(t *testing.T) {
	idx := BuildIndex(buildLineSpanDocs())
	eng := NewEngine(idx)
	st := eng.ensureClusterState()

	pos := map[string]int32{}
	for u := 0; u < idx.N; u++ {
		pos[idx.UnitIDs[u]] = int32(u)
	}

	// 基础增益：DocHash df=3/N=4。
	baseGain := -math.Log(3.0/4.0) / math.Log(4.0)
	base := invE * baseGain

	// 1) 紧邻切片 (1..40)|(41..80)：gap=0 ⟹ 插值到满额 g=1，地板=1/e。
	adj := st.computeDocSubjectCos(idx, pos["s1"], pos["s2"])
	if math.Abs(adj-invE) > 1e-12 {
		t.Fatalf("邻接切片地板应达满额 1/e: got=%.12f want=%.12f", adj, invE)
	}

	// 2) 远端切片 (41..80)|(2001..2040)：gap=1920 ≫ e·meanSpan(=40) ⟹ 指数项趋零，
	//    地板回落到基础增益附近（严格高于基础值、显著低于满额）。
	far := st.computeDocSubjectCos(idx, pos["s2"], pos["s3"])
	if far <= base-1e-12 || far >= adj/2 {
		t.Fatalf("远端切片地板失准: got=%.12f base=%.12f adj=%.12f", far, base, adj)
	}
	wantFar := invE * (baseGain + (1.0-baseGain)*math.Exp(-1920.0/(math.E*40.0)))
	if math.Abs(far-wantFar) > 1e-12 {
		t.Fatalf("远端插值解析值失准: got=%.12f want=%.12f", far, wantFar)
	}

	// 3) 无 DocHash 对照单元：双向零地板。
	if got := st.computeDocSubjectCos(idx, pos["s1"], pos["x1"]); got != 0 {
		t.Fatalf("异文档对不得触发行距核: got=%.12f", got)
	}
}

// TestLineSpanDegenerateNoCoordinates 零行坐标退化态：同 DocHash 但 LineStart=0 的
// 存量数据必须逐位保持原碰撞惊奇度地板（核完全不点火）。
func TestLineSpanDegenerateNoCoordinates(t *testing.T) {
	docs := []Doc{
		{UnitID: "a", OccurredAt: 1700000000, DocHash: 0xD0C, Terms: map[uint64]Term{1: {Hash: 1}}, Counts: map[uint64]int{1: 1}},
		{UnitID: "b", OccurredAt: 1710000000, DocHash: 0xD0C, Terms: map[uint64]Term{2: {Hash: 2}}, Counts: map[uint64]int{2: 1}},
		{UnitID: "c", OccurredAt: 1720000000, DocHash: 0xABC, Terms: map[uint64]Term{3: {Hash: 3}}, Counts: map[uint64]int{3: 1}},
	}
	idx := BuildIndex(docs)
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	var a, b int32 = -1, -1
	for u := 0; u < idx.N; u++ {
		switch idx.UnitIDs[u] {
		case "a":
			a = int32(u)
		case "b":
			b = int32(u)
		}
	}
	want := invE * (-math.Log(2.0/3.0) / math.Log(3.0))
	if got := st.computeDocSubjectCos(idx, a, b); math.Abs(got-want) > 1e-12 {
		t.Fatalf("零行坐标必须保持既有信封地板: got=%.12f want=%.12f", got, want)
	}
}

// TestCoCitationDeclarativeFloor 钉死共引桥：同一 noetic 结晶的 CitedUnitIDs 内
// 两枚证据单元获得与显式引证边同强度的满额地板；未同列单元不受影响。
func TestCoCitationDeclarativeFloor(t *testing.T) {
	docs := []Doc{
		{UnitID: "ev1", OccurredAt: 1700000000, Terms: map[uint64]Term{1: {Hash: 1}}, Counts: map[uint64]int{1: 1}},
		{UnitID: "ev2", OccurredAt: 1710000000, Terms: map[uint64]Term{2: {Hash: 2}}, Counts: map[uint64]int{2: 1}},
		{UnitID: "ev3", OccurredAt: 1720000000, Terms: map[uint64]Term{3: {Hash: 3}}, Counts: map[uint64]int{3: 1}},
		{UnitID: "noe", OccurredAt: 1730000000, Layer: "noetic", CitedIDs: []string{"ev1", "ev2"},
			Terms: map[uint64]Term{4: {Hash: 4}}, Counts: map[uint64]int{4: 1}},
	}
	idx := BuildIndex(docs)
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	pos := map[string]int32{}
	for u := 0; u < idx.N; u++ {
		pos[idx.UnitIDs[u]] = int32(u)
	}
	if !st.hasCoCitationEdge(pos["ev1"], pos["ev2"]) || !st.hasCoCitationEdge(pos["ev2"], pos["ev1"]) {
		t.Fatal("共引边必须对称物化")
	}
	if st.hasCoCitationEdge(pos["ev1"], pos["ev3"]) {
		t.Fatal("未同列单元不得获得共引边")
	}
	if got := st.computeDocSubjectCos(idx, pos["ev1"], pos["ev2"]); math.Abs(got-invE) > 1e-12 {
		t.Fatalf("共引对地板应为满额 1/e: got=%.12f", got)
	}
	if got := st.computeDocSubjectCos(idx, pos["ev1"], pos["ev3"]); got != 0 {
		t.Fatalf("非共引对地板应为零: got=%.12f", got)
	}
	// 共引伙伴必须进入 provenance 邻域遍历（时空簇扩展消费面）。
	seen := map[int32]bool{}
	st.forEachProvenancePeer(idx, pos["ev1"], func(p int32) { seen[p] = true })
	if !seen[pos["ev2"]] || !seen[pos["noe"]] {
		t.Fatalf("provenance 邻域缺共引/引证伙伴: %v", seen)
	}
}

// TestDeliveryChainAndFiberOrthogonalDecomposition verifies S7:
//  1. GroupHitsByClusterFractalWithQuery extracts the vertical ExplanationChain
//     from PeakUnitID conditioned on qTerms, prioritizing unknown explanatory
//     payload outside Q over candidates that merely repeat query terms;
//  2. Horizontal FiberUnitIDs and vertical ExplanationChain are strictly
//     orthogonal (zero duplicate unit IDs across Hits, ExplanationChain, and
//     FiberUnitIDs), while keeping Hits order and energy 0-ULP untouched.
func TestDeliveryChainAndFiberOrthogonalDecomposition(t *testing.T) {
	const ts int64 = 1700005000
	mk := func(id string, t int64, cited []string, termIDs ...uint64) Doc {
		terms := make(map[uint64]Term, len(termIDs))
		counts := make(map[uint64]int, len(termIDs))
		pos := make(map[uint64][]TermPosition, len(termIDs))
		for idx, h := range termIDs {
			terms[h] = Term{Hash: h, Group: h, Bits: 40, Flags: 1}
			counts[h] = 1
			pos[h] = []TermPosition{{Sentence: 0, Slot: idx + 1}}
		}
		return Doc{
			UnitID:     id,
			OccurredAt: t,
			CitedIDs:   cited,
			Terms:      terms,
			Counts:     counts,
			Positions:  pos,
		}
	}

	// Filler docs to give rare payload terms higher surprisal than common background.
	var docs []Doc
	for i := 0; i < 12; i++ {
		docs = append(docs, mk("bg-"+string(rune('a'+i)), ts-int64(86400*(i+2)), nil, uint64(9000+i)))
	}
	// Seed hit matching query anchors (101, 102).
	docs = append(docs, mk("u-seed", ts, nil, 101, 102))
	// Candidate that only adds term 103 which is already known in the query ground set Q.
	docs = append(docs, mk("u-q-echo", ts+1, nil, 101, 102, 103))
	// Candidate that explains the seed with unseen salient payload terms (201, 202, 203).
	docs = append(docs, mk("u-explainer", ts+2, nil, 101, 102, 201, 202, 203))
	// Second-hop law citing u-explainer with further unseen synthesis payload (301, 302, 303, 304).
	docs = append(docs, mk("u-law", ts+3600, []string{"u-explainer"}, 201, 202, 203, 301, 302, 303, 304))
	// Horizontal episode sibling (same episode, distinct aspect).
	docs = append(docs, mk("u-sibling", ts+3, nil, 401, 402))

	idx := BuildIndex(docs)
	eng := NewEngine(idx)

	// Query already contains 101, 102, 103: u-q-echo contributes zero unseen
	// explanatory payload beyond Q, whereas u-explainer -> u-law forms a 2-hop
	// monotonic explanation chain.
	qTerms := map[uint64]Term{
		101: {Hash: 101, Group: 101, Bits: 40, Flags: 1},
		102: {Hash: 102, Group: 102, Bits: 40, Flags: 1},
		103: {Hash: 103, Group: 103, Bits: 40, Flags: 1},
	}
	chainQ, ok := eng.TraceExplanationChainWithQuery("u-seed", true, 3, qTerms)
	if !ok || len(chainQ) < 2 {
		t.Fatalf("expected >=2 hops in query-conditioned explanation chain, got %+v (ok=%v)", chainQ, ok)
	}
	if chainQ[0].UnitID != "u-explainer" || chainQ[1].UnitID != "u-law" {
		t.Fatalf("expected query-conditioned chain [u-explainer -> u-law], got [%s -> %s]", chainQ[0].UnitID, chainQ[1].UnitID)
	}
	if chainQ[1].CumulativeTrans >= chainQ[0].CumulativeTrans {
		t.Fatalf("expected strict DPI attenuation along chain, got %v -> %v", chainQ[0].CumulativeTrans, chainQ[1].CumulativeTrans)
	}

	hits := []Hit{
		{UnitID: "u-seed", Energy: 25.5},
	}
	groups := eng.GroupHitsByClusterFractalWithQuery(hits, DefaultEpisodeFiberCap, qTerms)
	if len(groups) != 1 {
		t.Fatalf("expected 1 cluster group, got %d", len(groups))
	}
	g := groups[0]
	if len(g.ExplanationChain) < 2 || g.ExplanationChain[0].UnitID != "u-explainer" {
		t.Fatalf("expected ExplanationChain attached to group, got %+v", g.ExplanationChain)
	}
	chainSet := make(map[string]bool, len(g.ExplanationChain))
	for _, step := range g.ExplanationChain {
		chainSet[step.UnitID] = true
	}
	foundSibling := false
	for _, fid := range g.FiberUnitIDs {
		if fid == "u-seed" {
			t.Fatalf("delivered hit u-seed must not appear in FiberUnitIDs")
		}
		if chainSet[fid] {
			t.Fatalf("vertical chain unit %s must not duplicate in horizontal FiberUnitIDs (%+v)", fid, g.FiberUnitIDs)
		}
		if fid == "u-sibling" {
			foundSibling = true
		}
	}
	if !foundSibling {
		t.Fatalf("expected horizontal sibling u-sibling in FiberUnitIDs, got %+v", g.FiberUnitIDs)
	}
	if hits[0].UnitID != "u-seed" || hits[0].Energy != 25.5 {
		t.Fatalf("0-ULP violation on hits slice: %+v", hits)
	}
}
