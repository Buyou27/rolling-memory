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
