// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package episodicunit turns enumerated speech segments into L2 ingest units.
//
// One segment = one unit. A harness "turn" mixes dozens of tool steps into one
// blob: encoding it as a single unit averages the wave address over heterogeneous
// topics and forces delivery to slice it arbitrarily. Per-segment units keep every
// spoken word (nothing is deleted —低价值 content simply stops matching cues), and
// only a segment that alone outgrows the tokenz-r top rung is split at sentence
// boundaries (never mid-sentence, never truncated).
//
// The package owns the deterministic docID derivation so every ingest path
// (Stop hook, reconcile sweep, rebuild) addresses the same speech to the same unit.
package episodicunit

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/rocareer/rolling-memory/internal/episodic"
	"github.com/rocareer/rolling-memory/internal/ids"
)

// TokenBudget is the tokenz-r top rung (128/256/512/1024/2048/4096/8192): one
// unit never exceeds it unless a single sentence alone does. The rung is owned by
// `internal/episodic` together with the unit text form, so the ingest render and
// the zero-copy resolvers cannot drift; it is re-declared here for the unit
// builder's callers.
const TokenBudget = episodic.TokenBudget

// Unit is one L2 ingest unit derived from a speech segment.
type Unit struct {
	// DocID content-addresses the unit inside a scope.
	DocID string
	// RefPointer is the zero-copy harness pointer (`@ref:...#<k>` for one segment,
	// `@ref:...#<k>.<c>` for chunk c of a long segment), empty when the harness
	// cannot address the source turn (its text is then stored in the local vault).
	RefPointer string
	// TurnRef is the provenance of the source turn (pointer without `#<k>`), kept
	// for the lineage table when several turns share one content-addressed unit.
	TurnRef string
	// SegmentIndex is the segment ordinal inside the turn (`#<k>`).
	SegmentIndex int
	// ChunkIndex is the chunk ordinal inside the segment (`#<k>.<c>`; 0 for the
	// single-chunk case, where the pointer carries no chunk suffix).
	ChunkIndex int
	// Text is the rendered unit (`[role] speech`).
	Text string
	// OccurredAt is the source timestamp in Unix seconds (0 when unknown).
	OccurredAt int64
	// Completed reports whether the source turn can no longer grow.
	Completed bool
}

// DocID content-addresses one unit inside a scope: identical speech in any session
// or turn collapses onto one unit (the lineage table keeps every provenance).
func DocID(scope, text string) string {
	return "ep_" + ids.SHA256Hex(scope + "\x00" + text)[:24]
}

// Text renders the role tag that makes a unit self-describing.
func Text(role, speech string) string {
	return episodic.Text(role, speech)
}

// Chunk splits one rendered unit into pieces of at most budget tokens, cutting only
// at sentence boundaries; see `episodic.Chunk`, which owns the split every
// zero-copy chunk pointer (`#<k>.<c>`) is re-resolved against.
func Chunk(text string, budget int) []string {
	return episodic.Chunk(text, budget)
}

// SegmentUnits turns one segment into units under scope. pointerPrefix is the
// turn-level pointer without the `#<k>` suffix (empty when the harness cannot
// address a turn). Every unit stays zero-copy: a segment that fits one unit
// carries `#<k>`, a long segment carries `#<k>.<c>` per chunk, and both resolve
// live from the same enumerated segment (the chunk split is deterministic).
func SegmentUnits(scope, pointerPrefix string, segment episodic.Segment, occurredAt int64, completed bool) []Unit {
	rendered := Text(segment.Role, segment.Text)
	chunks := Chunk(rendered, TokenBudget)
	if occurredAt <= 0 {
		occurredAt = segment.OccurredAt
	}
	if occurredAt < 0 {
		occurredAt = 0
	}
	units := make([]Unit, 0, len(chunks))
	turnRef := strings.TrimSpace(pointerPrefix)
	for index, chunk := range chunks {
		refPointer := ""
		if turnRef != "" {
			if len(chunks) == 1 {
				refPointer = fmt.Sprintf("%s#%d", turnRef, segment.Index)
			} else {
				refPointer = fmt.Sprintf("%s#%d.%d", turnRef, segment.Index, index)
			}
		}
		units = append(units, Unit{
			DocID:        DocID(scope, chunk),
			RefPointer:   refPointer,
			TurnRef:      turnRef,
			SegmentIndex: segment.Index,
			ChunkIndex:   index,
			Text:         chunk,
			OccurredAt:   occurredAt,
			Completed:    completed,
		})
	}
	return units
}

// HMACSHA256 computes a 32-byte HMAC-SHA256 over parts for episodic index integrity.
func HMACSHA256(key []byte, parts ...[]byte) []byte {
	mac := hmac.New(sha256.New, key)
	for _, part := range parts {
		_, _ = mac.Write(part)
	}
	return mac.Sum(nil)
}
