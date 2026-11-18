// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func TestSparseVectorFourTierDynamicEnergyAndAdversarialProofs(t *testing.T) {
	const (
		hAlice    uint64 = 0x1001
		hAliceSfx uint64 = 0x1002 // companion in Alice's group
		hKyoto    uint64 = 0x2001
		hCeramics uint64 = 0x3001
		hKiln     uint64 = 0x4001
		hGlaze    uint64 = 0x5001
		hCommon   uint64 = 0x9001
	)

	mkDoc := func(id string, ts int64, terms map[uint64]Term, counts map[uint64]int, pos map[uint64][]TermPosition) Doc {
		return Doc{
			UnitID:     id,
			OccurredAt: ts,
			Terms:      terms,
			Counts:     counts,
			Positions:  pos,
		}
	}

	baseTS := int64(1712000000)
	docs := []Doc{
		// Episode 0: u-1 and u-2 are complementary records in the same event; u-clone is an exact echo of u-1.
		mkDoc("u-1", baseTS, map[uint64]Term{
			hAlice:    {Bits: 50, Flags: 1 << 0, Group: hAlice, Role: RolePrimary},
			hAliceSfx: {Bits: 40, Flags: 1 << 4, Group: hAlice, Role: RoleMorphStem},
			hKyoto:    {Bits: 50, Flags: 1 << 0, Group: hKyoto, Role: RolePrimary},
			hCeramics: {Bits: 60, Flags: 0, Group: hCeramics, Role: RolePrimary},
		}, map[uint64]int{
			hAlice: 2, hAliceSfx: 2, hKyoto: 1, hCeramics: 1,
		}, map[uint64][]TermPosition{
			hAlice:    {{Sentence: 0, Slot: 1}, {Sentence: 0, Slot: 6}},
			hAliceSfx: {{Sentence: 0, Slot: 1}, {Sentence: 0, Slot: 6}},
			hKyoto:    {{Sentence: 0, Slot: 2}},
			hCeramics: {{Sentence: 0, Slot: 3}},
		}),
		mkDoc("u-clone", baseTS+10, map[uint64]Term{
			hAlice:    {Bits: 50, Flags: 1 << 0, Group: hAlice, Role: RolePrimary},
			hAliceSfx: {Bits: 40, Flags: 1 << 4, Group: hAlice, Role: RoleMorphStem},
			hKyoto:    {Bits: 50, Flags: 1 << 0, Group: hKyoto, Role: RolePrimary},
			hCeramics: {Bits: 60, Flags: 0, Group: hCeramics, Role: RolePrimary},
		}, map[uint64]int{
			hAlice: 2, hAliceSfx: 2, hKyoto: 1, hCeramics: 1,
		}, map[uint64][]TermPosition{
			hAlice:    {{Sentence: 0, Slot: 1}, {Sentence: 0, Slot: 6}},
			hAliceSfx: {{Sentence: 0, Slot: 1}, {Sentence: 0, Slot: 6}},
			hKyoto:    {{Sentence: 0, Slot: 2}},
			hCeramics: {{Sentence: 0, Slot: 3}},
		}),
		mkDoc("u-2", baseTS+60, map[uint64]Term{
			hAlice: {Bits: 50, Flags: 1 << 0, Group: hAlice, Role: RolePrimary},
			hKyoto: {Bits: 50, Flags: 1 << 0, Group: hKyoto, Role: RolePrimary},
			hKiln:  {Bits: 45, Flags: 1 << 2, Group: hKiln, Role: RolePrimary},
			hGlaze: {Bits: 55, Flags: 0, Group: hGlaze, Role: RolePrimary},
		}, map[uint64]int{
			hAlice: 1, hKyoto: 1, hKiln: 1, hGlaze: 1,
		}, map[uint64][]TermPosition{
			hAlice: {{Sentence: 0, Slot: 1}},
			hKyoto: {{Sentence: 0, Slot: 4}}, // wider slot distance d=3
			hKiln:  {{Sentence: 0, Slot: 5}},
			hGlaze: {{Sentence: 0, Slot: 6}},
		}),
		// Distant episode records so df varies across terms.
		mkDoc("u-3", baseTS+86400*10, map[uint64]Term{
			hAlice:  {Bits: 50, Flags: 1 << 0, Group: hAlice, Role: RolePrimary},
			hCommon: {Bits: 30, Flags: 0, Group: hCommon, Role: RolePrimary},
		}, map[uint64]int{hAlice: 1, hCommon: 1}, map[uint64][]TermPosition{
			hAlice:  {{Sentence: 0, Slot: 1}},
			hCommon: {{Sentence: 0, Slot: 2}},
		}),
		mkDoc("u-4", baseTS+86400*10+10, map[uint64]Term{
			hKyoto:  {Bits: 50, Flags: 1 << 0, Group: hKyoto, Role: RolePrimary},
			hCommon: {Bits: 30, Flags: 0, Group: hCommon, Role: RolePrimary},
		}, map[uint64]int{hKyoto: 1, hCommon: 1}, map[uint64][]TermPosition{
			hKyoto:  {{Sentence: 0, Slot: 1}},
			hCommon: {{Sentence: 0, Slot: 2}},
		}),
	}

	idx := BuildIndex(docs)
	eng := NewEngine(idx)

	// 1. Tier-1 (Term): verify self-energy, partner sparse vector, and unit sparse vector.
	termProf, ok := eng.TermEnergyVector(hAlice)
	if !ok {
		t.Fatal("expected TermEnergyVector(hAlice) to succeed")
	}
	if termProf.DF != 4 || termProf.SelfInfoNats <= 0 || termProf.EffectiveWeightNats <= 0 {
		t.Fatalf("unexpected term profile scalars: %+v", termProf)
	}
	if _, hasKyotoPartner := termProf.PartnerVector.Lookup(hKyoto); !hasKyotoPartner {
		t.Fatalf("expected hAlice PartnerVector to include co-occurring hKyoto: %+v", termProf.PartnerVector.Entries)
	}
	if termProf.UnitVector.Len() != 4 {
		t.Fatalf("expected hAlice UnitVector nnz=4, got %d", termProf.UnitVector.Len())
	}
	u1Idx, _ := idx.LookupUnit("u-1")
	u2Idx, _ := idx.LookupUnit("u-2")
	// u-1 has tf=2 for hAlice while u-2 has tf=1 -> u-1's saturated unit coordinate weight must exceed u-2's.
	u1Entry, okU1 := termProf.UnitVector.Lookup(uint64(u1Idx))
	u2Entry, okU2 := termProf.UnitVector.Lookup(uint64(u2Idx))
	if !okU1 || !okU2 || u1Entry.WeightNats <= u2Entry.WeightNats {
		t.Fatalf("expected tf=2 unit coordinate (%v) > tf=1 coordinate (%v)", u1Entry.WeightNats, u2Entry.WeightNats)
	}

	// 2. Tier-2 (Phrase): verify canonical symmetry, super-additive binding, and slot-distance kernel attenuation.
	phrAB, okAB := eng.PhraseEnergyVector(hAlice, hKyoto)
	phrBA, okBA := eng.PhraseEnergyVector(hKyoto, hAlice)
	if !okAB || !okBA {
		t.Fatal("expected PhraseEnergyVector(hAlice, hKyoto) to succeed in both orders")
	}
	if phrAB.BindingEnergyNats != phrBA.BindingEnergyNats || phrAB.JointSurprisalNats != phrBA.JointSurprisalNats {
		t.Fatalf("phrase vector must be symmetric under argument swap: AB=%+v BA=%+v", phrAB, phrBA)
	}
	if phrAB.BindingEnergyNats <= 0 || phrAB.SuperAdditiveRatio <= 0 || phrAB.SuperAdditiveRatio >= 1 {
		t.Fatalf("invalid phrase binding metrics: %+v", phrAB)
	}
	// In u-1, hAlice and hKyoto are at adjacent slots (d=1, kern=1);
	// in u-2, they are at slots 1 and 4 (d=3, kern=exp(-2/e) < 1).
	pU1, okPU1 := phrAB.UnitVector.Lookup(uint64(u1Idx))
	pU2, okPU2 := phrAB.UnitVector.Lookup(uint64(u2Idx))
	if !okPU1 || !okPU2 || pU1.WeightNats <= pU2.WeightNats {
		t.Fatalf("expected adjacent phrase occurrence in u-1 (%v) > distant occurrence in u-2 (%v)", pU1.WeightNats, pU2.WeightNats)
	}

	// 3. Tier-3 (Unit): verify intra-group idempotent max folding (hAlice + hAliceSfx -> 1 group entry) and Rényi scale.
	uProf1, ok := eng.UnitEnergyVector("u-1")
	if !ok {
		t.Fatal("expected UnitEnergyVector(u-1) to succeed")
	}
	if uProf1.GroupVector.Len() != 3 {
		t.Fatalf("expected u-1 GroupVector to fold {hAlice, hAliceSfx} into 1 group (total 3 groups), got %d: %+v",
			uProf1.GroupVector.Len(), uProf1.GroupVector.Entries)
	}
	wantMu := (uProf1.GroupVector.NormL2 * uProf1.GroupVector.NormL2) / uProf1.GroupVector.TotalMassNats
	if math.Abs(uProf1.GroupVector.RenyiMuNats-wantMu) > 1e-12 {
		t.Fatalf("RenyiMuNats mismatch: got %v want %v", uProf1.GroupVector.RenyiMuNats, wantMu)
	}
	if uProf1.PhraseVector.IsEmpty() {
		t.Fatal("expected u-1 PhraseVector to be non-empty")
	}

	// 4. Tier-4 (Cluster) + Adversarial Proof:
	//    (a) Idempotent clone invariance: {u-1, u-clone} must have the EXACT same EnvelopeVector.TotalMassNats as {u-1}!
	clSingle, ok := eng.ClusterEnergyVector([]string{"u-1"})
	if !ok {
		t.Fatal("expected singleton ClusterEnergyVector to succeed")
	}
	clClone, ok := eng.ClusterEnergyVector([]string{"u-1", "u-clone"})
	if !ok {
		t.Fatal("expected clone pair ClusterEnergyVector to succeed")
	}
	if math.Abs(clClone.EnvelopeVector.TotalMassNats-clSingle.EnvelopeVector.TotalMassNats) > 1e-12 {
		t.Fatalf("idempotence violation (a ∨ a != a): single=%v clonePair=%v",
			clSingle.EnvelopeVector.TotalMassNats, clClone.EnvelopeVector.TotalMassNats)
	}
	if math.Abs(clClone.EchoRedundancyRatio-0.5) > 1e-12 {
		t.Fatalf("expected 2-clone cluster EchoRedundancyRatio=0.5, got %v", clClone.EchoRedundancyRatio)
	}

	//    (b) Complementary union: {u-1, u-2} expands support to 5 groups with strictly larger envelope energy.
	clComp, ok := eng.ClusterEnergyVector([]string{"u-1", "u-2"})
	if !ok {
		t.Fatal("expected complementary ClusterEnergyVector to succeed")
	}
	if clComp.EnvelopeVector.Len() != 5 || clComp.EnvelopeVector.TotalMassNats <= clSingle.EnvelopeVector.TotalMassNats {
		t.Fatalf("expected complementary cluster to expand support to 5 and increase envelope mass: %+v", clComp)
	}
	if clComp.IntersectionVector.Len() != 2 {
		t.Fatalf("expected {u-1, u-2} intersection support to be {hAlice, hKyoto} (len 2), got %d", clComp.IntersectionVector.Len())
	}

	//    (c) EntityCluster telescoping contraction: {hAlice, hKyoto} has minDF=4, clusterDF=3 -> contraction ln(5/4) > 0.
	entCl, ok := eng.EntityClusterEnergyVector([]uint64{hAlice, hKyoto})
	if !ok {
		t.Fatal("expected EntityClusterEnergyVector({hAlice, hKyoto}) to succeed")
	}
	wantShrink := math.Log(5.0 / 4.0)
	if entCl.ClusterDF != 3 || math.Abs(entCl.ContractionNats-wantShrink) > 1e-12 {
		t.Fatalf("entity cluster contraction mismatch: df=%d got=%v want=%v", entCl.ClusterDF, entCl.ContractionNats, wantShrink)
	}

	//    (d) EpisodeEnergyVector for episode 0 ({u-1, u-clone, u-2}).
	epCl, ok := eng.EpisodeEnergyVector(0)
	if !ok || len(epCl.MemberUnits) != 3 {
		t.Fatalf("expected EpisodeEnergyVector(0) with 3 units, got ok=%v members=%v", ok, epCl.MemberUnits)
	}
	if math.Abs(epCl.EnvelopeVector.TotalMassNats-clComp.EnvelopeVector.TotalMassNats) > 1e-12 {
		t.Fatalf("episode envelope must absorb u-clone idempotently: ep=%v comp=%v",
			epCl.EnvelopeVector.TotalMassNats, clComp.EnvelopeVector.TotalMassNats)
	}

	// 5. Pairwise Binomial Decomposition & Conditioned Scalar Readout:
	uProf2, _ := eng.UnitEnergyVector("u-2")
	decomp := DecomposeSparsePair(uProf1.GroupVector, uProf2.GroupVector)
	rel, ok := eng.EvaluateUnitPairRelation("u-1", "u-2")
	if !ok {
		t.Fatal("EvaluateUnitPairRelation failed")
	}
	if math.Abs(decomp.SharedMinNats-rel.SharedNats) > 1e-12 ||
		math.Abs(decomp.SelfExcessNats-rel.SelfExcessNats) > 1e-12 ||
		math.Abs(decomp.PeerExcessNats-rel.PeerExcessNats) > 1e-12 ||
		math.Abs(decomp.SelfCoverage-rel.SelfCoverage) > 1e-12 ||
		math.Abs(decomp.PeerCoverage-rel.PeerCoverage) > 1e-12 {
		t.Fatalf("DecomposeSparsePair must match EvaluateUnitPairRelation bit-identically: decomp=%+v rel=%+v", decomp, rel)
	}
	// Binomial completeness identity: (C_u + R_u)(C_v + R_v) == 1.
	cU, cV := decomp.SelfCoverage, decomp.PeerCoverage
	rU, rV := 1.0-cU, 1.0-cV
	binomSum := cU*cV + cU*rV + rU*cV + rU*rV
	if math.Abs(binomSum-1.0) > 1e-12 {
		t.Fatalf("binomial identity violated: got %v", binomSum)
	}

	// Conditioned readout survival monotonicity:
	qGroups := map[uint64]float64{hAlice: 2.0, hKyoto: 2.0}
	rFresh := EvaluateConditionedReadout(uProf2.GroupVector, qGroups, 4.0, 2.0, 1.0, nil)
	rCov := EvaluateConditionedReadout(uProf2.GroupVector, qGroups, 4.0, 2.0, 1.0, map[uint64]float64{hAlice: 1.0, hKyoto: 1.0, hKiln: 1.0, hGlaze: 1.0})
	if rFresh.QueryCoverage != 1.0 || rCov.SurvivalAddressingNats >= rFresh.SurvivalAddressingNats {
		t.Fatalf("expected survival decay under non-zero coverage: fresh=%+v cov=%+v", rFresh, rCov)
	}
}

// TestSparsePairDecompositionSingleSourceEquivalence 钉死「profile 适配器」与
// 「SparseVector 适配器」共用同一套二项式分解公式：两条路径的全部结算量必须逐位相等
// （余弦除外——profile 不携带 L2 范数，按约定恒为 0）。
func TestSparsePairDecompositionSingleSourceEquivalence(t *testing.T) {
	rng := rand.New(rand.NewSource(20261118))
	buildProf := func(n int) unitGroupProfile {
		items := make([]unitGroupItem, 0, n)
		var tot, sq float64
		var group uint64
		for i := 0; i < n; i++ {
			group += uint64(rng.Intn(3) + 1) // 严格升序坐标
			w := rng.Float64() * 10
			if i%4 == 0 {
				w = 0.5 + float64(rng.Intn(3)) // 制造并列与低权重边界
			}
			items = append(items, unitGroupItem{group: group, weight: w, isCore: rng.Intn(2) == 0})
			tot += w
			sq += w * w
		}
		var mu float64
		if tot > 0 {
			mu = sq / tot
		}
		return unitGroupProfile{items: items, totalMass: tot, muMass: mu}
	}
	check := func(name string, pu, pv unitGroupProfile) {
		ix := intersectGroupProfiles(pu, pv, false)
		d := DecomposeSparsePair(pu.toSparseVector(), pv.toSparseVector())
		if ix.SharedGroups != d.SharedGroups ||
			ix.SharedMinNats != d.SharedMinNats ||
			ix.SelfExcessNats != d.SelfExcessNats ||
			ix.PeerExcessNats != d.PeerExcessNats ||
			ix.SelfSalientExcessNats != d.SelfSalientExcessNats ||
			ix.PeerSalientExcessNats != d.PeerSalientExcessNats ||
			ix.SelfCoverage != d.SelfCoverage ||
			ix.PeerCoverage != d.PeerCoverage ||
			ix.HarmonicCoverage != d.HarmonicCoverage {
			t.Fatalf("%s: adapter mismatch\n profile=%+v\n vector=%+v", name, ix.VectorPairDecomposition, d)
		}
		if ix.Cosine != 0 {
			t.Fatalf("%s: profile adapter must not report cosine, got %v", name, ix.Cosine)
		}
		if ix.SelfCoverage+(1-ix.SelfCoverage) != 1.0 || ix.PeerCoverage+(1-ix.PeerCoverage) != 1.0 {
			t.Fatalf("%s: binomial completeness violated", name)
		}
	}
	check("both-empty", unitGroupProfile{}, unitGroupProfile{})
	check("one-side-empty", unitGroupProfile{}, buildProf(6))
	check("single-shared",
		unitGroupProfile{items: []unitGroupItem{{group: 7, weight: 3, isCore: true}}, totalMass: 3, muMass: 3},
		unitGroupProfile{items: []unitGroupItem{{group: 7, weight: 3, isCore: true}}, totalMass: 3, muMass: 3})
	disjoint := buildProf(6)
	for i := range disjoint.items {
		disjoint.items[i].group += 1000
	}
	check("disjoint", buildProf(6), disjoint)
	same := buildProf(8)
	check("self-identical", same, same)
	for i := 0; i < 60; i++ {
		check(fmt.Sprintf("random-%d", i), buildProf(rng.Intn(20)), buildProf(rng.Intn(20)))
	}
}

// TestIntersectGroupProfilesZeroAlloc 钉死筛选热路径零分配：无掩码路径必须 0 allocs/op，
// 掩码路径只允许两份掩码切片。
func TestIntersectGroupProfilesZeroAlloc(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	mk := func(n int) unitGroupProfile {
		items := make([]unitGroupItem, n)
		var tot, sq float64
		for i := range items {
			w := 1 + rng.Float64()*5
			items[i] = unitGroupItem{group: uint64(i + 1), weight: w, isCore: i%2 == 0}
			tot += w
			sq += w * w
		}
		return unitGroupProfile{items: items, totalMass: tot, muMass: sq / tot}
	}
	pu, pv := mk(24), mk(24)
	if got := testing.AllocsPerRun(200, func() { _ = intersectGroupProfiles(pu, pv, false) }); got != 0 {
		t.Fatalf("no-mask decomposition must be zero-alloc, got %v allocs/op", got)
	}
	vu, vv := pu.toSparseVector(), pv.toSparseVector()
	if got := testing.AllocsPerRun(200, func() { _ = DecomposeSparsePair(vu, vv) }); got != 0 {
		t.Fatalf("vector decomposition must be zero-alloc, got %v allocs/op", got)
	}
}

// mkDyadCorpusFixture 构造小规模带随机 PhraseDyad 结构的语料：每个单元随机抽
// 2–6 个词项并生成同句邻近共现，用于搭档扫描等价与红队证伪。
func mkDyadCorpusFixture(rng *rand.Rand, nDocs int) []Doc {
	vocab := make([]uint64, 64)
	for i := range vocab {
		// 高 12 位打满不同前缀桶，覆盖跨桶二分路径。
		vocab[i] = (uint64(i) + 1) << 50
	}
	docs := make([]Doc, nDocs)
	for d := 0; d < nDocs; d++ {
		nTerms := 2 + rng.Intn(5)
		terms := map[uint64]Term{}
		counts := map[uint64]int{}
		pos := map[uint64][]TermPosition{}
		for s := 0; s < nTerms; s++ {
			h := vocab[rng.Intn(len(vocab))]
			terms[h] = Term{Bits: uint16(30 + rng.Intn(40)), Flags: 1 << 0, Group: h, Role: RolePrimary}
			counts[h] = 1 + rng.Intn(3)
			for c := 0; c < counts[h]; c++ {
				pos[h] = append(pos[h], TermPosition{Sentence: 0, Slot: s*4 + c})
			}
		}
		docs[d] = Doc{
			UnitID:     fmt.Sprintf("u-%03d", d),
			OccurredAt: int64(1712000000 + d*3600),
			Terms:      terms,
			Counts:     counts,
			Positions:  pos,
		}
	}
	return docs
}

// slowFullScanPartnerEntries 是早期实现的对拍参照：逐键全扫 phraseKeys，命中任一侧
// 即结算，与历史 TermEnergyVector 全扫语义逐位一致。
func slowFullScanPartnerEntries(idx *Index, st *rmClusterState, termID uint64, effW float64, highDFThresh int) []SparseEntry {
	var entries []SparseEntry
	for i, pk := range st.phraseKeys {
		var partner uint64
		if pk.a == termID {
			partner = pk.b
		} else if pk.b == termID {
			partner = pk.a
		} else {
			continue
		}
		postings := st.phraseUnits[st.phraseRowPtr[i]:st.phraseRowPtr[i+1]]
		if len(postings) == 0 {
			continue
		}
		wb, okB := st.effWeightOf(partner)
		if !okB || wb <= 0 {
			wb, _ = idx.TermMass(partner)
		}
		if wb <= 0 {
			continue
		}
		joint := math.Log1p((float64(idx.N) - float64(len(postings)) + 0.5) / (float64(len(postings)) + 0.5))
		bindMass := math.Min(effW, wb) * joint / (1.0 + joint)
		pDF := idx.TermDF(partner)
		entries = append(entries, SparseEntry{
			ID:           partner,
			WeightNats:   bindMass,
			SelfInfoNats: joint,
			TF:           int32(len(postings)),
			DF:           pDF,
			IsCore:       pDF <= highDFThresh,
		})
	}
	return entries
}

// TestPartnerScanDualIndexEquivalence 对拍「旧全扫 vs 新双臂二分」：所有词项 ×
// 多随机语料，PartnerVector 必须逐位全等（红队反例：跨前缀桶的 b 侧搭档、零搭档
// 词、全库唯一词、高重复词族、以及只有 b 侧入键的词项）。
func TestPartnerScanDualIndexEquivalence(t *testing.T) {
	for seed := int64(1); seed <= 5; seed++ {
		rng := rand.New(rand.NewSource(seed))
		docs := mkDyadCorpusFixture(rng, 12)
		idx := BuildIndex(docs)
		eng := NewEngine(idx)
		st := eng.ensureClusterState()
		highDF := idx.HighDFThreshold()
		for h := range idx.DF {
			effW, ok := st.effWeightOf(h)
			if !ok || effW <= 0 {
				effW, _ = idx.TermMass(h)
			}
			fast := st.partnerEntriesInto(idx, h, effW, highDF)
			slow := slowFullScanPartnerEntries(idx, st, h, effW, highDF)
			fastVec, slowVec := BuildSparseVector(fast), BuildSparseVector(slow)
			if fastVec.Len() != slowVec.Len() {
				t.Fatalf("seed %d term %x: nnz fast=%d slow=%d", seed, h, fastVec.Len(), slowVec.Len())
			}
			for i := range fastVec.Entries {
				fe, se := fastVec.Entries[i], slowVec.Entries[i]
				if fe.ID != se.ID || fe.WeightNats != se.WeightNats || fe.TF != se.TF || fe.DF != se.DF || fe.IsCore != se.IsCore {
					t.Fatalf("seed %d term %x: entry %d mismatch fast=%+v slow=%+v", seed, h, i, fe, se)
				}
			}
			// 前缀表零值态（b 侧退化全扫）也必须等价。
			var saved [4097]uint32
			saved = st.partnerPrefixPtr
			st.partnerIdx = nil
			st.partnerPrefixPtr = [4097]uint32{}
			fallback := st.partnerEntriesInto(idx, h, effW, highDF)
			fbVec := BuildSparseVector(fallback)
			if fbVec.Len() != slowVec.Len() {
				t.Fatalf("seed %d term %x: fallback nnz=%d slow=%d", seed, h, fbVec.Len(), slowVec.Len())
			}
			for i := range fbVec.Entries {
				if fbVec.Entries[i] != slowVec.Entries[i] {
					t.Fatalf("seed %d term %x: fallback entry %d mismatch", seed, h, i)
				}
			}
			st.partnerIdx, st.partnerPrefixPtr = nil, saved
			st.partnerIdx = nil
		}
	}
}

// TestPartnerScanAdversarialEdges 钉死红队反例：① 只在 b 侧入键的词项（主序桶
// 必 miss）；② 零搭档词项（两臂皆空）；③ 全库只出现一次的孤本词（df=1，搭档
// 存在但 partner 侧权重回退链路）；④ 键哈希前缀桶越界端点（>>52 == 0 与 4095）。
func TestPartnerScanAdversarialEdges(t *testing.T) {
	mkDoc := func(id string, terms map[uint64]Term, counts map[uint64]int, pos map[uint64][]TermPosition) Doc {
		return Doc{UnitID: id, OccurredAt: 1712000000, Terms: terms, Counts: counts, Positions: pos}
	}
	// 键规范序 a<=b ⇒ 搭档 p>termID 走 a 侧、p<termID 走 b 侧。hHighB 只有以
	// b 侧身份入键的搭档（hTiny < hHighB ⇒ 键 (hTiny, hHighB)）；hAlone 零
	// 搭档；hEdge1/hEdge4095 覆盖 >>52 前缀桶两端（0 撞 termID==0 哨兵故从 1 起）。
	hTiny, hHighB, hAlone := uint64(1), uint64(0xd4000000000000), uint64(0x30000000000000)
	hEdge1, hEdge4095 := uint64(1)<<52, uint64(4095)<<52
	docs := []Doc{
		mkDoc("e-1", map[uint64]Term{
			hTiny:  {Bits: 40, Group: hTiny, Role: RolePrimary},
			hHighB: {Bits: 40, Group: hHighB, Role: RolePrimary},
			hAlone: {Bits: 40, Group: hAlone, Role: RolePrimary},
		}, map[uint64]int{hTiny: 1, hHighB: 1, hAlone: 1}, map[uint64][]TermPosition{
			hTiny:  {{Sentence: 0, Slot: 1}},
			hHighB: {{Sentence: 0, Slot: 2}},
			hAlone: {{Sentence: 0, Slot: 8}},
		}),
		mkDoc("e-2", map[uint64]Term{
			hEdge1:    {Bits: 40, Group: hEdge1, Role: RolePrimary},
			hEdge4095: {Bits: 40, Group: hEdge4095, Role: RolePrimary},
		}, map[uint64]int{hEdge1: 1, hEdge4095: 1}, map[uint64][]TermPosition{
			hEdge1:    {{Sentence: 0, Slot: 1}},
			hEdge4095: {{Sentence: 0, Slot: 2}},
		}),
	}
	idx := BuildIndex(docs)
	eng := NewEngine(idx)
	st := eng.ensureClusterState()

	// hHighB 的唯一搭档 hTiny < hHighB ⇒ 键 (hTiny, hHighB)，hHighB 只在 b 侧。
	highProf, ok := eng.TermEnergyVector(hHighB)
	if !ok {
		t.Fatal("hHighB profile expected")
	}
	if _, has := highProf.PartnerVector.Lookup(hTiny); !has {
		t.Fatalf("hHighB must find b-side partner hTiny, got %+v", highProf.PartnerVector.Entries)
	}
	// hAlone 无任何搭档。
	aloneProf, _ := eng.TermEnergyVector(hAlone)
	if aloneProf.PartnerVector.Len() != 0 {
		t.Fatalf("hAlone must have zero partners, got %+v", aloneProf.PartnerVector.Entries)
	}
	// 桶端点 >>52 = 1 与 4095 互为搭档，双方都能从任一侧找到对方。
	e1Prof, ok1 := eng.TermEnergyVector(hEdge1)
	e4095Prof, ok4095 := eng.TermEnergyVector(hEdge4095)
	if !ok1 || !ok4095 {
		t.Fatal("edge-bucket profiles expected")
	}
	if _, has := e1Prof.PartnerVector.Lookup(hEdge4095); !has {
		t.Fatalf("bucket-1 term must find bucket-4095 partner: %+v", e1Prof.PartnerVector.Entries)
	}
	if _, has := e4095Prof.PartnerVector.Lookup(hEdge1); !has {
		t.Fatalf("bucket-4095 term must find bucket-1 partner: %+v", e4095Prof.PartnerVector.Entries)
	}
	// b 侧退化全扫（partnerIdx 空）在边界桶上同样等价。
	for _, h := range []uint64{hTiny, hHighB, hEdge1, hEdge4095} {
		effW, _ := st.effWeightOf(h)
		fast := BuildSparseVector(st.partnerEntriesInto(idx, h, effW, idx.HighDFThreshold()))
		slow := BuildSparseVector(slowFullScanPartnerEntries(idx, st, h, effW, idx.HighDFThreshold()))
		if fast.Len() != slow.Len() || (fast.Len() > 0 && fast.Entries[0] != slow.Entries[0]) {
			t.Fatalf("edge term %x: fast=%+v slow=%+v", h, fast.Entries, slow.Entries)
		}
	}
}
