// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "testing"

// 16 位打包把句 70000 与句 80000 钳位到同一 0xFFFF 打包句号。宽语料的关系项局部
// 解释功若仍读打包槽距，异句远端词对会伪装成同句紧邻并兑付满额 exp(0) 核——
// 关系项槽距与焦点锚快照必须走精确坐标分支（F-39 残余收口）。
func TestWideRelationLocalExplainWorkNoAliasedAdjacencyAdversarial(t *testing.T) {
	build := func(anchorSentence, localSentence int) *Engine {
		u := Doc{
			UnitID:     "u",
			OccurredAt: 100,
			Terms: map[uint64]Term{
				1: {Hash: 1, Group: 1, Bits: 10, Flags: 1},
				2: {Hash: 2, Group: 2, Bits: 12, Flags: 1},
			},
			Counts: map[uint64]int{1: 1, 2: 1},
			Positions: map[uint64][]TermPosition{
				1: {{Sentence: anchorSentence, Slot: 5}},
				2: {{Sentence: localSentence, Slot: 6}},
			},
		}
		v := Doc{
			UnitID:     "v",
			OccurredAt: 200,
			Terms:      map[uint64]Term{1: {Hash: 1, Group: 1, Bits: 10, Flags: 1}},
			Counts:     map[uint64]int{1: 1},
			Positions:  map[uint64][]TermPosition{1: {{Sentence: 0, Slot: 1}}},
		}
		return NewEngine(BuildIndex([]Doc{u, v}))
	}

	wideEng := build(70000, 80000) // 两个宽句同时钳到 0xFFFF：打包假同句
	narrowEng := build(0, 0)       // 真同句紧邻（阴性对照：通道必须有牙）

	widePeer, ok := wideEng.EvaluateUnitPairRelation("u", "v")
	if !ok {
		t.Fatalf("wide relation lookup failed")
	}
	narrowPeer, ok := narrowEng.EvaluateUnitPairRelation("u", "v")
	if !ok {
		t.Fatalf("narrow relation lookup failed")
	}
	if narrowPeer.SelfExplainWorkNats <= 0 {
		t.Fatalf("narrow same-sentence twin must carry positive local explain work (test lacks teeth): %.9f", narrowPeer.SelfExplainWorkNats)
	}
	if widePeer.SelfExplainWorkNats != 0 {
		t.Fatalf("aliased packed adjacency leaked into wide local explain work: wide=%.9f narrow=%.9f", widePeer.SelfExplainWorkNats, narrowPeer.SelfExplainWorkNats)
	}
}

// 释放态（UnitPositions 已回收）的残余邻近回退必须以精确 (句, 槽) 判距：
// 打包钳位假同句不得让异句词对获得同句紧邻和声质量（释放不变性：回收映射前后读数必须一致）。
func TestWideReleasedProximityFallbackExactnessAdversarial(t *testing.T) {
	build := func(firstSentence, secondSentence int) *Engine {
		d1 := Doc{
			UnitID:     "d1",
			OccurredAt: 100,
			Terms: map[uint64]Term{
				1: {Hash: 1, Group: 1, Bits: 10, Flags: 1},
				2: {Hash: 2, Group: 2, Bits: 12, Flags: 1},
			},
			Counts: map[uint64]int{1: 1, 2: 1},
			Positions: map[uint64][]TermPosition{
				1: {{Sentence: firstSentence, Slot: 5}},
				2: {{Sentence: secondSentence, Slot: 6}},
			},
		}
		d2 := Doc{
			UnitID:     "d2",
			OccurredAt: 200,
			Terms:      map[uint64]Term{3: {Hash: 3, Group: 3, Bits: 10, Flags: 1}},
			Counts:     map[uint64]int{3: 1},
			Positions:  map[uint64][]TermPosition{3: {{Sentence: 0, Slot: 1}}},
		}
		return NewEngine(BuildIndex([]Doc{d1, d2}))
	}
	qTerms := map[uint64]Term{1: {Hash: 1}, 2: {Hash: 2}, 3: {Hash: 3}}
	qPos := map[uint64][]TermPosition{
		1: {{Sentence: 0, Slot: 1}},
		2: {{Sentence: 0, Slot: 2}},
		3: {{Sentence: 0, Slot: 3}},
	}
	masses := map[uint64]float64{1: 1, 2: 1}

	wideEng := build(70000, 80000)                    // 两个宽句同时钳到 0xFFFF：打包假同句
	_ = wideEng.SearchWithPositions(qTerms, qPos, 10) // 物化 clusterState 并绑定 CSR 导出
	wideUnit, ok := wideEng.Index.LookupUnit("d1")
	if !ok {
		t.Fatalf("wide unit lookup failed")
	}
	wideEng.Index.ReleaseTransientUnitMaps()
	if inc := wideEng.proximityIncrement(wideUnit, masses); inc != 0 {
		t.Fatalf("aliased cross-sentence pair earned released-state proximity energy: %.9f", inc)
	}

	narrowEng := build(0, 0)
	_ = narrowEng.SearchWithPositions(qTerms, qPos, 10)
	narrowUnit, ok := narrowEng.Index.LookupUnit("d1")
	if !ok {
		t.Fatalf("narrow unit lookup failed")
	}
	narrowEng.Index.ReleaseTransientUnitMaps()
	if inc := narrowEng.proximityIncrement(narrowUnit, masses); inc <= 0 {
		t.Fatalf("narrow same-sentence pair must earn positive released-state proximity (test lacks teeth): %.9f", inc)
	}
}
