// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package text

import "testing"

// TestShannonPrimitivesContract pins the canonical Shannon-bit values of the
// single primitive home. Recall ranking and eval scoring delegate here, so these
// numbers are the repo-wide contract.
func TestShannonPrimitivesContract(t *testing.T) {
	if got := RuneShannonBits('a'); got != 2.0 {
		t.Fatalf("RuneShannonBits('a') = %v, want 2.0", got)
	}
	if got := RuneShannonBits('猫'); got != 10.0 {
		t.Fatalf("RuneShannonBits('猫') = %v, want 10.0 (Han)", got)
	}
	if got := StringShannonBits("猫"); got != 10.0 {
		t.Fatalf("StringShannonBits(猫) = %v, want 10.0", got)
	}
	// Non-carrier strings contribute zero bits (punctuation-only, no alnum).
	if got := StringShannonBits("----"); got != 0.0 {
		t.Fatalf("StringShannonBits(----) = %v, want 0.0", got)
	}
	// Stutter flood is damped below the atomic concept floor.
	if got := StringShannonBits("aaaaaaaaaa"); got >= AtomicConceptEntropyBits {
		t.Fatalf("StringShannonBits(aaaaaaaaaa) = %v, want < %v", got, AtomicConceptEntropyBits)
	}
	if got := StringShannonBits(""); got != 0.0 {
		t.Fatalf("StringShannonBits(empty) = %v, want 0.0", got)
	}
}

// TestScriptClassifiers locks the language-agnostic script classification used by
// ids/token gating.
func TestScriptClassifiers(t *testing.T) {
	if !IsPureCJK("食べ") || IsPureCJK("cat") {
		t.Fatal("IsPureCJK contract broken")
	}
	if !IsContinuousSyllabicOrIdeographic('한') || !IsContinuousSyllabicOrIdeographic('猫') {
		t.Fatal("IsContinuousSyllabicOrIdeographic must accept Hangul/Han")
	}
	if IsContinuousSyllabicOrIdeographic('a') {
		t.Fatal("latin letters are not continuous-script carriers")
	}
}
