// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import "github.com/rocareer/rolling-memory/sdk/go/public/shaping"

// RunFoldRule is a pluggable document-ingestion run-folding rule: given an
// input text, it returns the folded text and whether any degenerate run was
// collapsed. Rules must remain 100% language-agnostic and dictionary-free.
type RunFoldRule = shaping.RunFoldRule

// NoopRunFoldRule is the identity rule that leaves the input text unchanged
// when run folding is disabled on a specific extractor instance.
func NoopRunFoldRule(text string) (string, bool) {
	return shaping.NoopRunFoldRule(text)
}

// CombineRunFoldRules chains multiple RunFoldRule functions sequentially into a
// single composite rule (dual to CombineHarmonicExpanders).
func CombineRunFoldRules(rules ...RunFoldRule) RunFoldRule {
	return shaping.CombineRunFoldRules(rules...)
}

// DefaultRunFoldRules returns the standard slice of orthogonal run-folding
// rules (identical token runs, digit-skeleton counter runs, and identical line runs).
func DefaultRunFoldRules() []RunFoldRule {
	return shaping.DefaultRunFoldRules()
}

// DefaultRunFoldRule is the fused single-pass default run-folding rule.
func DefaultRunFoldRule(text string) (string, bool) {
	return shaping.DefaultRunFoldRule(text)
}

// FoldIdenticalTokenRuns collapses single-line identical-token runs (> ceil(e^2) -> ceil(e)).
func FoldIdenticalTokenRuns(text string) (string, bool) {
	return shaping.FoldIdenticalTokenRuns(text)
}

// FoldDigitSkeletonTokenRuns collapses single-line digit-skeleton counter runs
// and period-2 table cell counter runs (> ceil(e^2) -> ceil(e)).
func FoldDigitSkeletonTokenRuns(text string) (string, bool) {
	return shaping.FoldDigitSkeletonTokenRuns(text)
}

// FoldIdenticalLineRuns collapses contiguous identical non-empty line runs (> ceil(e^2) -> ceil(e)).
func FoldIdenticalLineRuns(text string) (string, bool) {
	return shaping.FoldIdenticalLineRuns(text)
}

// CollapseDegenerateTokenRuns applies DefaultRunFoldRule and returns the folded text.
func CollapseDegenerateTokenRuns(text string) string {
	return shaping.CollapseDegenerateTokenRuns(text)
}

// foldDocDegenerateRuns applies the extractor's configured RunFolder plugin (or
// DefaultRunFoldRule when RunFolder is nil) to document text prior to sentence
// segmentation.
func (a *TermExtractor) foldDocDegenerateRuns(text string) string {
	if a != nil && a.enc != nil && a.enc.harmOpts.RunFolder != nil {
		folded, _ := a.enc.harmOpts.RunFolder(text)
		return folded
	}
	return shaping.CollapseDegenerateTokenRuns(text)
}
