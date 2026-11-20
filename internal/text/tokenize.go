// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package text

import (
	"unicode"
	"unicode/utf8"

	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

// Token classification invariants:
//  1. Zero static stopword lists: classification never drops tokens via hardcoded stopword or closed-class tables;
//  2. Semantic neutrality: relation and coreference semantics belong to the language model; these operators provide structural and statistical support only;
//  3. Continuous Shannon + BPE gain: StringShannonBits and TokenInformationMass serve solely as continuous ranking and budget weights, never as boolean cutoff gates.

// IsContinuousSyllabicOrIdeographic reports whether r belongs to a script without word spaces.
func IsContinuousSyllabicOrIdeographic(r rune) bool {
	return r == 'ー' ||
		unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Thai, r) ||
		unicode.Is(unicode.Lao, r) ||
		unicode.Is(unicode.Khmer, r) ||
		unicode.Is(unicode.Myanmar, r) ||
		unicode.Is(unicode.Tibetan, r)
}

// IsPureCJK reports whether s consists solely of Han/Hiragana/Katakana characters.
func IsPureCJK(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != 'ー' && !unicode.Is(unicode.Han, r) && !unicode.Is(unicode.Hiragana, r) && !unicode.Is(unicode.Katakana, r) {
			return false
		}
	}
	return true
}

// TokenInformationMass computes the composite Shannon + BPE prior + morphological + proper-noun information gain of a token.
// Note: Used strictly for gain ranking and budget prioritization ("只负责增益，不负责一刀切生死过滤").
func TokenInformationMass(tk string) float64 {
	mass := 0.5*StringShannonBits(tk) + 0.5*tokenzr.EffectiveLexicalBits(tk)
	if tokenzr.IsBPEProperNounPrior(tk) {
		mass += 2.5
	}
	return mass
}

// IsHighGainToken reports whether tk belongs to the Primary Gain Tier for ordering priority
// (never used to delete tokens; low-gain tokens remain in the Secondary Residual Tier).
func IsHighGainToken(tk string) bool {
	if tk == "" {
		return false
	}
	if r0, w0 := utf8.DecodeRuneInString(tk); w0 > 0 {
		if unicode.IsDigit(r0) {
			return true
		}
		if (r0 == '#' || unicode.Is(unicode.Sc, r0)) && len(tk) > w0 {
			if r1, _ := utf8.DecodeRuneInString(tk[w0:]); unicode.IsDigit(r1) {
				return true
			}
		}
	}
	if IsPureCJK(tk) {
		if StringShannonBits(tk) < 2.0*AtomicConceptEntropyBits {
			return tokenzr.TokenPriorBits(tk) >= 11.20
		}
		return tokenzr.TokenPriorBits(tk) >= 12.25
	}
	// Colloquial expressive elongation ("sooooo", "ummmmm"), syllabic laughter ("haha", "hahaha"),
	// and auxiliary negation truncation stems ("didn", "wasn") are evaluated on their canonical form
	// and never hijack the Primary Gain Tier.
	if collapsed := tokenzr.CollapseMarkovElongatedWord(tk); collapsed != "" && collapsed != tk {
		if StringShannonBits(collapsed) > 0.50*AtomicConceptEntropyBits || tokenzr.BPEInflectionValence(collapsed) >= 1 {
			return IsHighGainToken(collapsed)
		}
		return false
	}
	if tokenzr.IsBPEContractedAuxStem(tk) || tokenzr.IsBPEUninflectedTandemReduplication(tk) {
		return false
	}
	if tokenzr.IsBPEProperNounPrior(tk) {
		return true
	}
	b := StringShannonBits(tk)
	pr := tokenzr.TokenPriorBits(tk)
	val := tokenzr.BPEInflectionValence(tk)
	if b == 4.0 {
		return val >= 2 && pr >= 9.60
	}
	if b <= 6.0 && pr < 10.30 {
		return false
	}
	if (val >= 2 && pr >= 9.75) || (val >= 1 && pr >= 10.30) {
		return true
	}
	return pr >= 11.20 || (pr >= 11.00 && TokenInformationMass(tk) >= 10.30)
}
