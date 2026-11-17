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
)

const CanonicalPhraseJoint = shaping.CanonicalPhraseJoint

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
