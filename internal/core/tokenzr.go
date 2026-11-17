// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"

// CountTokens returns the canonical tokenz-r 128k Byte-Level BPE token count for s.
func CountTokens(s string) int {
	return tokenzr.Count(s)
}

// SplitBPEDirectPieces splits w into deterministic subword pieces via the canonical BPE table.
func SplitBPEDirectPieces(w string) []string {
	return tokenzr.SplitBPEDirectPieces(w)
}

// SplitSubwords splits s into subword strings via the canonical BPE table.
func SplitSubwords(s string) []string {
	return tokenzr.SplitSubwords(s)
}

// PreTokenize splits s into Unicode script-transition pre-tokens.
func PreTokenize(s string) []string {
	return tokenzr.PreTokenize(s)
}
