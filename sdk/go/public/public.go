// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package public provides the unified public tokenizer, character-geometry shaping,
// and closed-set homomorphic lookup primitives shared across all modes (local_plain,
// pulse_plain, pulse_cipher).
package public

import (
	"github.com/rocareer/rolling-memory/sdk/go/public/closedset"
	"github.com/rocareer/rolling-memory/sdk/go/public/shaping"
	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

type (
	SpaceElision = shaping.SpaceElision
	RunFoldRule  = shaping.RunFoldRule
	// LineChunk is one contiguous 1:1 line-range slice produced by ChunkLines.
	LineChunk = tokenzr.LineChunk
)

const (
	CanonicalPhraseJoint = shaping.CanonicalPhraseJoint

	// DefaultChunkTokens is the top rung of the seven-level tokenz-r ladder and
	// the default per-chunk token ceiling; callers may request a smaller ceiling
	// (e.g. 1024 for background-file chunks) through the chunk helpers.
	DefaultChunkTokens = tokenzr.DefaultChunkTokens
)

// ChunkLadder is the standard seven-level tokenz-r token budget ladder
// (128 / 256 / 512 / 1024 / 2048 / 4096 / 8192).
var ChunkLadder = tokenzr.ChunkLadder

var (
	StandardClosedSetTable        = closedset.StandardClosedSetTable
	UniversalSchemaClosedSetTable = closedset.UniversalSchemaClosedSetTable
	ClosedSetHarmonicEntries      = closedset.ClosedSetHarmonicEntries
)

func CountTokens(s string) int                    { return tokenzr.Count(s) }
func SplitBPEDirectPieces(w string) []string      { return tokenzr.SplitBPEDirectPieces(w) }
func SplitSubwords(s string) []string             { return tokenzr.SplitSubwords(s) }
func PreTokenize(s string) []string               { return tokenzr.PreTokenize(s) }
func CanonicalizeApostrophe(s string) string      { return shaping.CanonicalizeApostrophe(s) }
func FuseSpaceElision(s string) SpaceElision      { return shaping.FuseSpaceElision(s) }
func CanonicalPhrase(parts ...string) string      { return shaping.CanonicalPhrase(parts...) }
func CollapseDegenerateTokenRuns(s string) string { return shaping.CollapseDegenerateTokenRuns(s) }
func LookupFirstTag(tok string) string            { return closedset.LookupFirstTag(tok) }
func LookupHarmonics(tok string) []string         { return closedset.LookupHarmonics(tok) }
func LookupEverydayTimeClass(tok string) string   { return closedset.LookupEverydayTimeClass(tok) }

// NormalizeChunkTokens resolves a caller-supplied chunk token ceiling into
// (0, DefaultChunkTokens]: non-positive or oversized values fall back to
// DefaultChunkTokens, while smaller positive values are honored as requested.
func NormalizeChunkTokens(maxTokens int) int { return tokenzr.NormalizeChunkTokens(maxTokens) }

// ChunkBySentences splits text into lossless tokenz-r-budgeted chunks at
// sentence boundaries; a single sentence over budget stays whole.
func ChunkBySentences(text string, maxTokens int) []string {
	return tokenzr.ChunkBySentences(text, maxTokens)
}

// SplitSentencesKeepingDelimiters splits text at sentence boundaries while
// preserving terminal punctuation and trailing whitespace (lossless join).
func SplitSentencesKeepingDelimiters(text string) []string {
	return tokenzr.SplitSentencesKeepingDelimiters(text)
}

// ChunkLines groups source lines into contiguous chunks bounded by the
// normalized tokenz-r ceiling while preserving 1:1 line coordinates.
func ChunkLines(lines []string, maxTokens int, anchorPrefix string) []LineChunk {
	return tokenzr.ChunkLines(lines, maxTokens, anchorPrefix)
}
