// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package recall

import (
	"strings"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/memory/delivery"
)

func TestShannonProductionSecurityAndRobustnessProbes(t *testing.T) {
	// Probe P1: Space/hyphen-separated periodic flood ("ha ha ha ha ha", "a-a-a-a a-a-a-a") must collapse < 8.0b
	p1a := EffectiveShannonBits("a-a-a-a a-a-a-a")
	p1b := EffectiveShannonBits("ha ha ha ha ha ha")
	if p1a >= delivery.AtomicConceptEntropyBits || p1b >= delivery.AtomicConceptEntropyBits {
		t.Fatalf("[Probe P1] space/hyphen-separated flood leaked: p1a=%.2fb, p1b=%.2fb (want < 8.0b)", p1a, p1b)
	}

	// Probe P2: Tail-appended punctuated/tandem stutter after 64-char prefix ("normal..." + "a.a.a.a"*100 or "abab"*50)
	prefix64 := "TheQuickBrownFoxJumpsOverTheLazyDogAndRunsIntoTheForestToday1234"
	stutterTail := strings.Repeat("a.", 100)
	tandemTail := strings.Repeat("abab", 32)
	bPrefix := EffectiveShannonBits(prefix64)
	bWithStutter := EffectiveShannonBits(prefix64 + " " + stutterTail)
	bWithTandem := EffectiveShannonBits(prefix64 + " " + tandemTail)
	if bWithStutter-bPrefix > 5.0 {
		t.Fatalf("[Probe P2a] tail punctuated stutter 'a.'*100 after 64-char prefix inflated bits by +%.2fb (want <= 5.0b)", bWithStutter-bPrefix)
	}
	if bWithTandem-bPrefix > 5.0 {
		t.Fatalf("[Probe P2b] tail tandem flood 'abab'*32 after 64-char prefix inflated bits by +%.2fb (want <= 5.0b)", bWithTandem-bPrefix)
	}

	// Probe P3: FastLongestVerbatimSubspan O(N log N) vs O(N^3) CPU DoS defense on 1500-rune mismatched anchor (< 20ms)
	turnText := strings.Repeat("abcdefghij", 200)
	badAnchor := strings.Repeat("klmnopqrst", 150) // 1500 runes, 0 overlap with turnText
	t0 := time.Now()
	gotMismatch := FastLongestVerbatimSubspan(turnText, badAnchor)
	elapsed := time.Since(t0)
	if gotMismatch != "" || elapsed > 25*time.Millisecond {
		t.Fatalf("[Probe P3] FastLongestVerbatimSubspan DoS guard failed: got=%q, elapsed=%v (want < 25ms)", gotMismatch, elapsed)
	}
	gotMatch := FastLongestVerbatimSubspan("Noise prefix: Caroline moved to Sweden in 2023. Noise suffix.", "Yesterday Caroline moved to Sweden in 2023!")
	if gotMatch != "Caroline moved to Sweden in 2023" {
		t.Fatalf("[Probe P3] FastLongestVerbatimSubspan exact match failed: got %q", gotMatch)
	}
}
