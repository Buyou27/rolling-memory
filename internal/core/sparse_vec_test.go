// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
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
