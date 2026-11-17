// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import "testing"

// B09 红/绿判据 ①：一句内多个结构化日期信标（cal: 主信标与裸 ISO）必须各自占独立物理
// 词槽；历史实现对「越过词表的额外主词」只开一个共享端槽，多个日期载体共槽坍缩
// （旧闸实测：六枚载体全部塌到同一槽；修复后各得独立递增槽）。
func TestMultiDateBeaconsGetDistinctSlotsAdversarial(t *testing.T) {
	ext := NewTermExtractor(DemoSecret)
	// 斜杠日期不由 SplitWords 切成 ISO 字面词 ⇒ 句尾结构化日期追加为本词表外主词，
	// 正是「多日期共槽」路径的真实触发几何。
	terms, _, posMap := ext.ExtractTermsWithPositions("from 2023/05/01 to 2023/06/02 they travelled.")
	var slots []int
	for _, key := range []string{"cal:2023-05-01", "2023-05-01", "cal:2023-06-02", "2023-06-02"} {
		h := ext.Encoder().KeyedHash64(key)
		if _, ok := terms[h]; !ok {
			t.Fatalf("missing date carrier term %q", key)
		}
		pos := posMap[h]
		if len(pos) == 0 {
			t.Fatalf("missing positions for date carrier %q", key)
		}
		slots = append(slots, pos[0].Slot)
	}
	seen := map[int]bool{}
	for _, s := range slots {
		if seen[s] {
			t.Fatalf("date carriers collapsed onto one slot: %v", slots)
		}
		seen[s] = true
	}
}

// B09 红/绿判据 ②：客户端 SDK 形态（无谐波外挂 ⇒ 自适应分子项生效）下，宽词拆出的额外
// 主词不得使后续正文词被锁死在同一 docSlot；每个后续正文词仍须保有各自的位置。
func TestAdaptivePiecesDoNotCollapseFollowingWordsAdversarial(t *testing.T) {
	ext := &TermExtractor{enc: NewBlindEncoder(DemoSecret)} // expander=nil ⇒ 自适应项路径
	terms, _, posMap := ext.ExtractTermsWithPositions("antidisestablishmentarianism foo bar")
	hFoo := ext.Encoder().KeyedHash64("foo")
	hBar := ext.Encoder().KeyedHash64("bar")
	if _, ok := terms[hFoo]; !ok {
		t.Fatal("foo missing")
	}
	if _, ok := terms[hBar]; !ok {
		t.Fatal("bar missing")
	}
	fooPos, barPos := posMap[hFoo], posMap[hBar]
	if len(fooPos) == 0 || len(barPos) == 0 {
		t.Fatalf("missing positions: foo=%v bar=%v", fooPos, barPos)
	}
	if fooPos[0].Slot == barPos[0].Slot {
		t.Fatalf("following words collapsed onto one slot: foo=%d bar=%d", fooPos[0].Slot, barPos[0].Slot)
	}
	if barPos[0].Slot <= fooPos[0].Slot {
		t.Fatalf("word order not preserved: foo=%d bar=%d", fooPos[0].Slot, barPos[0].Slot)
	}
}
