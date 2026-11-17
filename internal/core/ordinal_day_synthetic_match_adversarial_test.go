// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "testing"

// B07 回归：df=0 的推断序数日信标（Sent/Slot 高低 16 位承载 dStar）把目标日单元
// 以「仅质量」合成匹配注资；该匹配必须不带 entry（-1 哨兵）与 positions，严禁把
// 单元 entries[0] 的 tf、伴生 flags 与句槽位置张冠李戴给日期信标；异日单元不得注资。
func TestInferredOrdinalDaySyntheticMatchCarriesNoFabricatedEntryAdversarial(t *testing.T) {
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
	e := NewEngine(BuildIndex(docs))
	const beacon uint64 = 0xbeac0 // 语料外哈希 ⇒ df=0 走推断序数日分支
	qTerms := map[uint64]Term{
		beacon: {
			Hash:     beacon,
			Group:    beacon,
			Role:     RoleClosedBeacon,
			ExtFlags: ExtFlagSeenMorphOrClos,
			Bits:     60,
			Sent:     targetDay >> 16,
			Slot:     targetDay & 0xFFFF,
		},
	}
	_ = e.SearchWithPositions(qTerms, nil, 10)
	sc := e.queryScratch
	if sc == nil || len(sc.candidates) != len(docs) {
		t.Fatalf("scratch candidates unavailable: %+v", sc)
	}
	hasBeaconMatch := func(u int) bool {
		for _, m := range sc.candidates[u].matches {
			if m.group == beacon {
				return true
			}
		}
		return false
	}
	for u := range docs {
		for _, m := range sc.candidates[u].matches {
			if m.group != beacon {
				continue
			}
			if m.entry != -1 {
				t.Fatalf("unit %d: synthetic day match fabricated entry=%d", u, m.entry)
			}
			if len(m.positions) != 0 {
				t.Fatalf("unit %d: synthetic day match fabricated positions=%v", u, m.positions)
			}
			if m.mass <= 0 {
				t.Fatalf("unit %d: synthetic day match lost its mass credit", u)
			}
		}
	}
	if !hasBeaconMatch(0) || !hasBeaconMatch(1) || hasBeaconMatch(2) {
		t.Fatalf("day dispatch coverage wrong: u0=%v u1=%v u2=%v",
			hasBeaconMatch(0), hasBeaconMatch(1), hasBeaconMatch(2))
	}
}
