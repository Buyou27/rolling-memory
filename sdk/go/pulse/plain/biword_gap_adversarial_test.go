// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import "testing"

func countBiwordRole(atoms []BlindTokenTerm) int {
	n := 0
	for _, a := range atoms {
		if a.Role == RoleBiwordHomomorph {
			n++
		}
	}
	return n
}

// B08 回归：闭集二元同态投影（RoleBiwordHomomorph）只允许跨越「经扫描证实为纯空白」的
// 间隔注入；当位置不可校验（重叠词/前部同形子串推进使 Index 落空，或当前词本身未定位）
// 时必须 fail-closed，禁止隔着未审察区段误注入。
func TestBiwordHomomorphFailsClosedOnUnverifiableGapAdversarial(t *testing.T) {
	az := NewTermExtractor(DemoSecret)
	enc := az.enc

	// 触发对：new + zealand ⇒ "newzealand"（@geo:cc:nz 词键），A 段 stemAlnum≥3。
	// 对照 1（可校验的纯空白间隔）：必须注入。
	gapOK := enc.packSentenceWords("new zealand zz", []string{"new", "zealand", "zz"})
	if got := countBiwordRole(gapOK); got != 1 {
		t.Fatalf("pure-space gap must still inject the biword (control needs teeth), got %d", got)
	}
	// 对照 2（可校验的标点间隔）：不得注入。
	punctOK := enc.packSentenceWords("new, zealand zz", []string{"new", "zealand", "zz"})
	if got := countBiwordRole(punctOK); got != 0 {
		t.Fatalf("punctuation gap must not inject the biword, got %d", got)
	}
	// 反例（不可校验：后段仅出现在扫描位之前，Index 落空）：fail-closed，不得注入。
	unverifiable := enc.packSentenceWords("zealand new, zz", []string{"new", "zealand"})
	if got := countBiwordRole(unverifiable); got != 0 {
		t.Fatalf("unverifiable gap must not inject the biword, got %d", got)
	}
}
