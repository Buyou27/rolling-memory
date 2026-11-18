// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package epistemic

import "testing"

func contains(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// 证据覆盖的查询词进入锚点，未被证据覆盖的词进入缺失集，缺失质量为正。
func TestDeriveVoidSplitsAnchorAndMissing(t *testing.T) {
	v := DeriveVoid("alice bought a red bicycle", []string{"alice bought a bicycle"})
	if len(v.AnchorKeys) == 0 {
		t.Fatalf("expected anchored keys, got %+v", v)
	}
	if !contains(v.MissingKeys, "red") {
		t.Fatalf("expected red in missing keys, got %+v", v.MissingKeys)
	}
	if contains(v.AnchorKeys, "red") {
		t.Fatalf("red must not be anchored, got %+v", v.AnchorKeys)
	}
	if v.MissingMassNats <= 0 || v.CuriosityNats <= 0 || v.Signature == 0 {
		t.Fatalf("expected positive missing mass/curiosity and non-zero signature, got %+v", v)
	}
	if v.CuriosityNats < v.MissingMassNats {
		t.Fatalf("curiosity must not undercut missing mass: %+v", v)
	}
}

// 证据完全覆盖查询时没有空洞，好奇心为零。
func TestDeriveVoidFullyCovered(t *testing.T) {
	v := DeriveVoid("alice bought a bicycle", []string{"alice bought a bicycle"})
	if len(v.MissingKeys) != 0 {
		t.Fatalf("expected no missing keys, got %+v", v.MissingKeys)
	}
	if v.CuriosityNats != 0 || v.MissingMassNats != 0 {
		t.Fatalf("fully covered query must have zero curiosity, got %+v", v)
	}
	if v.Signature == 0 {
		t.Fatalf("anchored-only void still carries a signature, got %+v", v)
	}
}

// 单文本批次没有证据，全部查询词都处于缺失态，且不应 panic 或产生 NaN。
func TestDeriveVoidNoEvidence(t *testing.T) {
	v := DeriveVoid("alice bought a red bicycle", nil)
	if len(v.AnchorKeys) != 0 || len(v.MissingKeys) == 0 {
		t.Fatalf("single-text batch must be all missing, got %+v", v)
	}
	if v.CuriosityNats <= 0 {
		t.Fatalf("missing-only void must carry curiosity, got %+v", v)
	}
}

// 同输入必须逐位稳定（签名、质量、集合顺序）。
func TestDeriveVoidDeterministic(t *testing.T) {
	q := "爱丽丝买了一辆红色的自行车"
	e := []string{"爱丽丝买了一辆自行车", "她昨天骑了这辆自行车"}
	a, b := DeriveVoid(q, e), DeriveVoid(q, e)
	if a.Signature != b.Signature || a.CuriosityNats != b.CuriosityNats ||
		len(a.AnchorKeys) != len(b.AnchorKeys) || len(a.MissingKeys) != len(b.MissingKeys) {
		t.Fatalf("derivation must be deterministic:\n a=%+v\n b=%+v", a, b)
	}
	if a.Signature == 0 {
		t.Fatalf("CJK query must derive a signature, got %+v", a)
	}
}

// 新证据补齐缺失词后，同一词从缺失集移入锚点集。
func TestDeriveVoidEvidenceClosesGap(t *testing.T) {
	open := DeriveVoid("alice bought a red bicycle", []string{"alice bought a bicycle"})
	closed := DeriveVoid("alice bought a red bicycle", []string{"alice bought a bicycle", "the red bicycle is hers"})
	if !contains(open.MissingKeys, "red") {
		t.Fatalf("precondition: red must be missing, got %+v", open)
	}
	if contains(closed.MissingKeys, "red") || !contains(closed.AnchorKeys, "red") {
		t.Fatalf("added evidence must anchor red, got %+v", closed)
	}
	if open.Signature == closed.Signature {
		t.Fatalf("void signature must change when the gap closes")
	}
}

// 拼图闭合要求同时命中锚点与缺失词；只命中一侧不算闭合。
func TestSnapVoidRequiresBothSides(t *testing.T) {
	v := DeriveVoid("alice bought a red bicycle", []string{"alice bought a bicycle"})
	if !SnapVoid(v, "the red bicycle belongs to alice") {
		t.Fatalf("unit with anchor+missing must snap, void=%+v", v)
	}
	if SnapVoid(v, "alice") {
		t.Fatalf("anchor-only unit must not snap")
	}
	if SnapVoid(v, "red") {
		t.Fatalf("missing-only unit must not snap")
	}
	if SnapVoid(Void{}, "anything") {
		t.Fatalf("empty void must not snap")
	}
}

// 边界：空查询、空证据、纯标点输入均返回零值，不产生 NaN。
func TestDeriveVoidDegenerateInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    string
		e    []string
	}{
		{"empty", "", nil},
		{"blank", "   ", []string{" ", "\t"}},
		{"punctuation", "!!!", []string{"???"}},
	} {
		v := DeriveVoid(tc.q, tc.e)
		if v.Signature != 0 || v.CuriosityNats != 0 || len(v.AnchorKeys) != 0 || len(v.MissingKeys) != 0 {
			t.Fatalf("%s: expected zero void, got %+v", tc.name, v)
		}
	}
}
