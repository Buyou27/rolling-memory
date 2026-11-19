// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"unicode"
	"unicode/utf8"
)

// SharesEdgeIdeograph reports whether two tokens containing ideographs overlap by sharing
// the last rune of one with the first rune of the other (sliding 2-gram overlap of a single 3-rune word).
func SharesEdgeIdeograph(a, b string) bool {
	if StringShannonBits(a) < AtomicConceptEntropyBits || StringShannonBits(b) < AtomicConceptEntropyBits {
		return false
	}
	firstA, wFirstA := utf8.DecodeRuneInString(a)
	lastA, _ := utf8.DecodeLastRuneInString(a)
	firstB, wFirstB := utf8.DecodeRuneInString(b)
	lastB, _ := utf8.DecodeLastRuneInString(b)
	if wFirstA <= 0 || wFirstA >= len(a) || wFirstB <= 0 || wFirstB >= len(b) {
		return false
	}
	hasIdeo := false
	for _, r := range a {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			hasIdeo = true
			break
		}
	}
	if !hasIdeo {
		return false
	}
	return lastA == firstB || lastB == firstA
}
