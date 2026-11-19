// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"sync/atomic"

	"github.com/rocareer/rolling-memory/internal/text"
)

// LegacyStemMode toggles legacy stem truncation for A/B falsification tests (default false).
// 本包仅保留该开关（rank.go::MatchTokenOrStem 读取）；分词 / 茎干实现唯一正本在 internal/text。
var LegacyStemMode atomic.Bool

// MaxQueryTokens is the hard cap on tokens per query (delegates to internal/text).
const MaxQueryTokens = text.MaxQueryTokens

// IsContinuousSyllabicOrIdeographic reports whether r belongs to a script without word spaces (delegates to internal/text).
func IsContinuousSyllabicOrIdeographic(r rune) bool {
	return text.IsContinuousSyllabicOrIdeographic(r)
}

// IsHighGainToken reports whether tk belongs to the Primary Gain Tier (delegates to internal/text).
func IsHighGainToken(tk string) bool { return text.IsHighGainToken(tk) }

// StemToken computes the language-agnostic Shannon-bit proportional prefix stem (delegates to internal/text).
func StemToken(tok string) string { return text.StemToken(tok) }

// Tokenize performs permissive zero-stopword-table Unicode + tokenz-r BPE tokenization (delegates to internal/text).
func Tokenize(q string) []string { return text.Tokenize(q) }
