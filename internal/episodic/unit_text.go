// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package episodic

import (
	"strings"

	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

// TokenBudget is the tokenz-r top rung (128/256/512/1024/2048/4096/8192): one
// unit never exceeds it unless a single sentence alone does. The rung itself is
// owned by the shared SDK chunker (`tokenzr.DefaultChunkTokens`), so every host
// that slices long text uses one definition.
//
// The unit text form lives in this package because three callers must agree on it
// byte for byte: the ingest unit builder renders it, the live pointer resolver
// re-renders it, and `SourceSnapshot` re-renders it from a frozen parse. A
// divergence would shift `#<k>` onto another segment.
const TokenBudget = tokenzr.DefaultChunkTokens

// Text renders the role tag that makes a unit self-describing.
func Text(role, speech string) string {
	if strings.TrimSpace(role) == "" {
		role = "message"
	}
	return "[" + role + "] " + speech
}

// Chunk splits one rendered unit into pieces of at most budget tokens, cutting only
// at sentence boundaries. A sentence that alone exceeds the budget is kept whole:
// a unit may be wider than the rung, never truncated. The split itself lives in the
// shared SDK chunker so the canonical, mirror and client trees cannot drift.
func Chunk(text string, budget int) []string {
	return tokenzr.ChunkBySentences(text, budget)
}
