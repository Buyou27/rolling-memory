// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import "github.com/rocareer/rolling-memory/sdk/go/public/shaping"

// RunFoldRule is a pluggable document-ingestion run-folding rule: given an
// input text, it returns the folded text and whether any degenerate run was
// collapsed. Rules must remain 100% language-agnostic and dictionary-free.
type RunFoldRule = shaping.RunFoldRule

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
