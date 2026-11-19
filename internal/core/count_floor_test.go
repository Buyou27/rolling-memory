// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "testing"

// TestCountDomainFloorsPreserveLegacyE2Cutoffs pins the dimensional rewrite of
// the two document/unit-count guards: the legacy form compared a count against
// the dimensionless e², which is dimensionally inhomogeneous; because both
// quantities are integers the comparison is exactly equivalent to a cutoff at 8.
// The test enumerates the equivalence and pins the boundary so a future edit
// cannot silently shift it.
func TestCountDomainFloorsPreserveLegacyE2Cutoffs(t *testing.T) {
	const legacyCutoff = eulerNumber * eulerNumber
	first := -1
	for n := 0; n <= 4096; n++ {
		legacy := float64(n) > legacyCutoff
		named := n >= 8
		if legacy != named {
			t.Fatalf("计数域改写必须与旧 e² 判据逐点等价：n=%d legacy=%v named=%v", n, legacy, named)
		}
		if legacy && first < 0 {
			first = n
		}
	}
	if first != 8 {
		t.Fatalf("旧判据的首个满足值应为 8，实际 %d", first)
	}
	if !(float64(7) <= legacyCutoff) {
		t.Fatalf("7 必须落在旧判据之下（e²=%.17g）", legacyCutoff)
	}
}
