// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package epistemic provides the shared, corpus-statistics-only derivation of
// Noetic epistemic voids: which query terms are anchored by cited evidence and
// which remain missing. It is a pure function over text and the canonical
// tokenzr tokenizer; it holds no word lists, no static priors, and performs no
// semantic judgement.
//
// Term mass is estimated from the local evidence batch itself: for N texts and
// a term appearing in df of them, m(t) = ln((N+1)/df) Nats — the null
// probability that a query term occupies df of the N+1 text slots. Missing mass
// is the summed mass of query terms with no evidence occurrence; curiosity adds
// the binary closure uncertainty H(p) of the missing fraction, so both terms
// stay in Nats and are zero when the void is empty.
package epistemic

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

// Void is the derived epistemic gap of one query against its cited evidence.
type Void struct {
	AnchorKeys      []string `json:"anchor_keys"`
	MissingKeys     []string `json:"missing_keys"`
	Signature       uint64   `json:"signature"`
	AnchorMassNats  float64  `json:"anchor_mass_nats"`
	MissingMassNats float64  `json:"missing_mass_nats"`
	CuriosityNats   float64  `json:"curiosity_nats"`
}

// DeriveVoid derives the void of query against the cited evidence texts.
// Empty queries and empty evidence yield a zero Void with no signature.
func DeriveVoid(query string, evidence []string) Void {
	texts := make([]string, 0, len(evidence)+1)
	if strings.TrimSpace(query) != "" {
		texts = append(texts, query)
	}
	for _, e := range evidence {
		if strings.TrimSpace(e) != "" {
			texts = append(texts, e)
		}
	}
	if len(texts) == 0 {
		return Void{}
	}

	sets := make([]map[string]struct{}, len(texts))
	df := make(map[string]int, 16)
	for i, text := range texts {
		sets[i] = tokenSet(text)
		for key := range sets[i] {
			df[key]++
		}
	}
	if len(sets[0]) == 0 {
		return Void{}
	}

	n := float64(len(texts))
	var anchor, missing []string
	var anchorMass, missingMass float64
	for key := range sets[0] {
		mass := math.Log((n + 1.0) / float64(df[key]))
		if df[key] >= 2 {
			anchor = append(anchor, key)
			anchorMass += mass
		} else {
			missing = append(missing, key)
			missingMass += mass
		}
	}
	sort.Strings(anchor)
	sort.Strings(missing)

	total := anchorMass + missingMass
	curiosity := 0.0
	if total > 0 {
		ratio := missingMass / total
		if ratio > 0 && ratio < 1 {
			curiosity = -ratio*math.Log(ratio) - (1-ratio)*math.Log(1-ratio)
		}
		curiosity += missingMass
	}
	return Void{
		AnchorKeys:      anchor,
		MissingKeys:     missing,
		Signature:       signature(anchor, missing),
		AnchorMassNats:  anchorMass,
		MissingMassNats: missingMass,
		CuriosityNats:   curiosity,
	}
}

// SnapVoid reports whether unitText fills a void: it must contain at least one
// anchor key and at least one missing key. The check is a pure set intersection;
// callers own citation and lifecycle state.
func SnapVoid(v Void, unitText string) bool {
	if len(v.AnchorKeys) == 0 || len(v.MissingKeys) == 0 {
		return false
	}
	keys := tokenSet(unitText)
	if len(keys) == 0 {
		return false
	}
	anchorHit, missingHit := false, false
	for _, k := range v.AnchorKeys {
		if _, ok := keys[k]; ok {
			anchorHit = true
			break
		}
	}
	for _, k := range v.MissingKeys {
		if _, ok := keys[k]; ok {
			missingHit = true
			break
		}
	}
	return anchorHit && missingHit
}

// tokenSet splits text into lowercased canonical subword keys with at least one
// letter or digit, deduplicated. The tokenzr tokenizer is the only token source.
func tokenSet(text string) map[string]struct{} {
	out := make(map[string]struct{}, 16)
	for _, piece := range tokenzr.SplitSubwords(strings.ToLower(text)) {
		piece = strings.TrimSpace(piece)
		if piece == "" || !hasAlnum(piece) {
			continue
		}
		out[piece] = struct{}{}
	}
	return out
}

func hasAlnum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// signature is a deterministic 64-bit digest of the sorted anchor/missing key
// sets. It carries identity only; it is never used as a score.
func signature(anchor, missing []string) uint64 {
	if len(anchor) == 0 && len(missing) == 0 {
		return 0
	}
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "a:%s\x00m:%s", strings.Join(anchor, "\x1f"), strings.Join(missing, "\x1f"))
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}
