// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"fmt"
	"math"
	"testing"
)

// TestTermEnergyCalendarCohesionIsRetrievalKappa pins the exported
// CalendarCohesion to the retrieval path's κ_occ. The two used to be separate
// implementations and silently diverged when F-43 removed the (n−1)/(n+2)
// shrink and the −1 shift from only one of them.
func TestTermEnergyCalendarCohesionIsRetrievalKappa(t *testing.T) {
	base := int64(1700000000)
	docs := make([]Doc, 0, 12)
	for i := 0; i < 12; i++ {
		d := epTestDoc(fmt.Sprintf("coh-%02d", i), base+int64(i/3)*86400)
		if i < 6 {
			d.Terms[999] = Term{Hash: 999, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: 999}
			d.Counts[999] = 1
			d.Positions[999] = []TermPosition{{Sentence: 0, Slot: 0}}
		}
		docs = append(docs, d)
	}
	eng := NewEngine(BuildIndex(docs))
	defer eng.Close()
	prof, ok := eng.TermEnergyVector(999)
	if !ok {
		t.Fatalf("词项 999 应有能量向量")
	}
	st := eng.ensureClusterState()
	if st == nil || st.totalDaysM != 4 {
		t.Fatalf("语料应有 4 个活跃日，实际 %v", st.totalDaysM)
	}
	want := calendarConcentration(2, 6, st.totalDaysM) // 6 次出现落在 2 天
	if math.Abs(prof.CalendarCohesion-want) > 1e-12 {
		t.Fatalf("CalendarCohesion 必须等于检索路径 κ_occ：got %v want %v", prof.CalendarCohesion, want)
	}
	if prof.DF != 6 {
		t.Fatalf("df 应为 6，实际 %d", prof.DF)
	}
}

// TestInferredOrdinalDayUsesExactCalendarConcentration pins the df=0 inferred
// ordinal-day dispatch to the same κ_occ estimator as the term path. The
// dispatch term is the day itself and every observed unit is on that day
// (dObs = 1); the retired (n−1)/(n+2) shrink would keep the term-path pins
// green while silently producing a different mass here.
func TestInferredOrdinalDayUsesExactCalendarConcentration(t *testing.T) {
	const targetDay = 20600
	const otherDay = 20590
	mk := func(id string, day int64) Doc {
		return Doc{
			UnitID:     id,
			OccurredAt: day*86400 + 3600,
			Terms:      map[uint64]Term{7: {Hash: 7, Group: 7, Bits: 40, Flags: 1, Role: RolePrimary}},
			Counts:     map[uint64]int{7: 9},
			Positions:  map[uint64][]TermPosition{7: {{Sentence: 0, Slot: 1}}},
		}
	}
	docs := []Doc{mk("u0", targetDay), mk("u1", targetDay), mk("u2", otherDay)}
	eng := NewEngine(BuildIndex(docs))
	defer eng.Close()
	const beacon uint64 = 0xbeac0
	const beaconBits uint16 = 60
	// 语料外哈希 ⇒ df=0，走推断序数日分支。
	qTerms := map[uint64]Term{
		beacon: {
			Hash:     beacon,
			Group:    beacon,
			Role:     RoleClosedBeacon,
			ExtFlags: ExtFlagSeenMorphOrClos,
			Bits:     beaconBits,
			Sent:     targetDay >> 16,
			Slot:     targetDay & 0xFFFF,
		},
	}
	_ = eng.SearchWithPositions(qTerms, nil, 10)
	st := eng.ensureClusterState()
	sc := eng.queryScratch
	if st == nil || sc == nil {
		t.Fatalf("cluster state unavailable: st=%v sc=%v", st, sc)
	}
	const dfDay = 2.0
	want := math.Log1p((float64(len(docs)) - dfDay + 0.5) / (dfDay + 0.5))
	want *= 1.0 + calendarConcentration(1, dfDay, st.totalDaysM)*transmissionPerHop
	if st.entryMeanBits > 0 {
		want *= bitsGainSqrt(float64(beaconBits) / st.entryMeanBits)
	}
	want *= 1.0 + eng.FlagsGain/eulerNumber

	found := 0
	for u := 0; u < 2; u++ {
		for _, m := range sc.candidates[u].matches {
			if m.group != beacon {
				continue
			}
			found++
			if math.Abs(m.mass-want) > 1e-12 {
				t.Fatalf("unit %d: inferred-day mass = %v, want %v (exact κ_occ with dObs=1)", u, m.mass, want)
			}
		}
	}
	if found != 2 {
		t.Fatalf("inferred-day matches on the target day = %d, want 2", found)
	}
}
