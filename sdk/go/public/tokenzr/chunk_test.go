// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package tokenzr

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeChunkTokens(t *testing.T) {
	if got := NormalizeChunkTokens(0); got != DefaultChunkTokens {
		t.Fatalf("NormalizeChunkTokens(0) = %d, want %d", got, DefaultChunkTokens)
	}
	if got := NormalizeChunkTokens(-10); got != DefaultChunkTokens {
		t.Fatalf("NormalizeChunkTokens(-10) = %d, want %d", got, DefaultChunkTokens)
	}
	if got := NormalizeChunkTokens(16384); got != DefaultChunkTokens {
		t.Fatalf("NormalizeChunkTokens(16384) = %d, want %d", got, DefaultChunkTokens)
	}
	for _, custom := range []int{128, 512, 1024, 2048, 4096, 8192} {
		if got := NormalizeChunkTokens(custom); got != custom {
			t.Fatalf("NormalizeChunkTokens(%d) = %d, want %d", custom, got, custom)
		}
	}
}

func TestChunkBySentencesPreservesBoundariesAndCustomThreshold(t *testing.T) {
	sent := "Verification of cache invalidation and full-suite regression checks completed without error. "
	text := strings.Repeat(sent, 80)
	chunks := ChunkBySentences(text, 128)
	if len(chunks) < 4 {
		t.Fatalf("expected multiple chunks at 128-token threshold, got %d", len(chunks))
	}
	for i, c := range chunks {
		if tok := Count(c); tok > 128 {
			t.Fatalf("chunk %d token count %d exceeds custom threshold 128", i, tok)
		}
		if !strings.HasSuffix(strings.TrimSpace(c), ".") {
			t.Fatalf("chunk %d does not end at sentence boundary: %q", i, c)
		}
	}
	if got := strings.Join(chunks, ""); got != text {
		t.Fatalf("chunks do not concatenate back to the input (len got=%d want=%d)", len(got), len(text))
	}
	// Default 8192 threshold keeps medium text in a single chunk.
	single := ChunkBySentences(text, 0)
	if len(single) != 1 || single[0] != text {
		t.Fatalf("expected the input verbatim as one chunk under the default 8192 threshold, got %d chunks", len(single))
	}
}

func TestChunkerVersionPinned(t *testing.T) {
	// A bump changes every content-addressed unit derived from chunk text, so it
	// must be a deliberate re-index migration, never an incidental edit.
	if ChunkerVersion != 1 {
		t.Fatalf("ChunkerVersion = %d, want 1", ChunkerVersion)
	}
}

// Token counts are not additive across a join ("hqar. " + "uebl" costs one more
// token than the parts), so a per-sentence sum can undershoot the real size of
// the assembled chunk. The exact check must peel the run back to the budget.
func TestChunkBySentencesEnforcesExactBudgetOnSuperadditiveJoin(t *testing.T) {
	head, tail := "hqar. ", "uebl"
	joint := head + tail
	budget := Count(head) + Count(tail)
	if Count(joint) <= budget {
		t.Skipf("fixture is no longer superadditive: parts=%d joined=%d", budget, Count(joint))
	}
	if Count(head) > budget {
		t.Skipf("fixture head already exceeds the derived budget: %d > %d", Count(head), budget)
	}
	chunks := ChunkBySentences(joint, budget)
	if got := strings.Join(chunks, ""); got != joint {
		t.Fatalf("chunks do not concatenate back to the input: %q", got)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected the run to be split, got %d chunk(s) with %d tokens over budget %d", len(chunks), Count(joint), budget)
	}
	for i, c := range chunks {
		if Count(c) > budget && len(SplitSentencesKeepingDelimiters(c)) > 1 {
			t.Fatalf("multi-sentence chunk %d carries %d tokens over budget %d", i, Count(c), budget)
		}
	}
}

func TestChunkBySentencesIsByteExactAcrossBoundaries(t *testing.T) {
	cases := []string{
		"  leading and trailing whitespace.  ",
		"First sentence. Second sentence! Third?\nFourth;fifth",
		"汉字。汉字！汉字？汉字；",
		"line one\nline two\n\nline four\n",
		"no terminal punctuation at all",
		"a. ",
	}
	for _, text := range cases {
		for _, budget := range []int{1, 4, 8, 16, 128} {
			chunks := ChunkBySentences(text, budget)
			if got := strings.Join(chunks, ""); got != text {
				t.Fatalf("budget=%d text=%q: chunks concatenate to %q", budget, text, got)
			}
			for i, c := range chunks {
				if c == "" {
					t.Fatalf("budget=%d text=%q: chunk %d is empty", budget, text, i)
				}
				if Count(c) > budget && len(SplitSentencesKeepingDelimiters(c)) > 1 {
					t.Fatalf("budget=%d text=%q: multi-sentence chunk %d carries %d tokens", budget, text, i, Count(c))
				}
			}
		}
	}
}

func TestChunkBySentencesBudgetInvariantAcrossRungs(t *testing.T) {
	r := rand.New(rand.NewSource(20261118))
	words := make([]string, 320)
	for i := range words {
		n := 1 + r.Intn(8)
		b := make([]byte, n)
		for j := range b {
			b[j] = byte('a' + r.Intn(26))
		}
		words[i] = string(b)
	}
	var sb strings.Builder
	for i, w := range words {
		sb.WriteString(w)
		if i < len(words)-1 {
			switch i % 3 {
			case 0:
				sb.WriteString(". ")
			case 1:
				sb.WriteString("。")
			default:
				sb.WriteString("\n")
			}
		}
	}
	text := sb.String()
	for _, budget := range []int{128, 256, 512, 1024} {
		chunks := ChunkBySentences(text, budget)
		if len(chunks) < 2 {
			t.Fatalf("budget=%d produced %d chunk(s)", budget, len(chunks))
		}
		if got := strings.Join(chunks, ""); got != text {
			t.Fatalf("budget=%d chunks do not concatenate back to the input", budget)
		}
		for i, c := range chunks {
			if Count(c) > budget && len(SplitSentencesKeepingDelimiters(c)) > 1 {
				t.Fatalf("budget=%d multi-sentence chunk %d carries %d tokens", budget, i, Count(c))
			}
		}
	}
}

func TestChunkLinesPreservesVerbatimLinesAndExactBudget(t *testing.T) {
	lines := []string{
		"# Section",
		"    indented code line with several tokens",
		"\tTabbed line",
		"",
		"body text",
		"## Next",
		"    deeper indentation stays",
	}
	chunks := ChunkLines(lines, 8, "L")
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks at an 8-token budget, got %d", len(chunks))
	}
	var rebuilt []string
	prevEnd := 0
	for i, ch := range chunks {
		if ch.LineStart != prevEnd+1 {
			t.Fatalf("chunk %d LineStart = %d, want %d", i, ch.LineStart, prevEnd+1)
		}
		if ch.LineEnd-ch.LineStart+1 != len(ch.Lines) {
			t.Fatalf("chunk %d span %d..%d does not match len(Lines)=%d", i, ch.LineStart, ch.LineEnd, len(ch.Lines))
		}
		body := strings.Join(ch.Lines, "\n")
		if ch.TokenCount != Count(body) {
			t.Fatalf("chunk %d TokenCount = %d, want the exact joined count %d", i, ch.TokenCount, Count(body))
		}
		if ch.TokenCount > 8 && len(ch.Lines) > 1 {
			t.Fatalf("multi-line chunk %d carries %d tokens over budget 8", i, ch.TokenCount)
		}
		rebuilt = append(rebuilt, ch.Lines...)
		prevEnd = ch.LineEnd
	}
	if !reflect.DeepEqual(rebuilt, lines) {
		t.Fatalf("chunk lines are not verbatim: got %q want %q", rebuilt, lines)
	}
}

func TestChunkLinesPreservesOneToOneLineCoordinates(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "Line content with several tokens for deterministic chunk coordinate testing."
	}
	chunks := ChunkLines(lines, 128, "L")
	if len(chunks) < 2 {
		t.Fatalf("expected multiple line chunks at 128-token budget, got %d", len(chunks))
	}
	prevEnd := 0
	for i, ch := range chunks {
		if ch.LineStart != prevEnd+1 {
			t.Fatalf("chunk %d LineStart = %d, want %d", i, ch.LineStart, prevEnd+1)
		}
		if ch.LineEnd-ch.LineStart+1 != len(ch.Lines) {
			t.Fatalf("chunk %d span %d..%d does not match len(Lines)=%d", i, ch.LineStart, ch.LineEnd, len(ch.Lines))
		}
		prevEnd = ch.LineEnd
	}
	if prevEnd != len(lines) {
		t.Fatalf("final LineEnd = %d, want %d", prevEnd, len(lines))
	}
}

func TestFitLineRunMatchesChunkLinesOnPlainRuns(t *testing.T) {
	lines := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		lines = append(lines, "Row content with several tokens for deterministic chunk coordinate testing.")
	}
	for _, budget := range []int{32, 64, 128, 512} {
		var want []int
		for start := 0; start < len(lines); {
			end := FitLineRun(lines, start, budget)
			want = append(want, end)
			start = end
		}
		var got []int
		for _, chunk := range ChunkLines(lines, budget, "L") {
			got = append(got, chunk.LineEnd)
		}
		if len(got) != len(want) {
			t.Fatalf("budget=%d: ChunkLines produced %d chunks, FitLineRun %d", budget, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("budget=%d: chunk %d ends at %d, FitLineRun %d", budget, i, got[i], want[i])
			}
		}
	}
}

func TestFitLineRunEnforcesBudgetAndKeepsSingleOverlongLine(t *testing.T) {
	long := strings.Repeat("fixture payload token ", 40)
	lines := []string{"short line", "another short line", long, "tail line"}
	run := FitLineRun(lines, 0, 16)
	if run != 2 {
		t.Fatalf("run = %d, want the two short lines only", run)
	}
	if body := strings.Join(lines[:run], "\n"); Count(body) > 16 {
		t.Fatalf("run body carries %d tokens over budget 16", Count(body))
	}
	overlong := FitLineRun([]string{long}, 0, 16)
	if overlong != 1 {
		t.Fatalf("single overlong line must stay whole, got end=%d", overlong)
	}
}

func TestFitLineRunStartBoundsAndBudgetNormalization(t *testing.T) {
	lines := []string{"alpha line", "beta line", "gamma line"}
	if got := FitLineRun(lines, -5, 128); got < 1 || got > len(lines) {
		t.Fatalf("negative start returned %d", got)
	}
	if got := FitLineRun(lines, len(lines), 128); got != len(lines) {
		t.Fatalf("start at end returned %d, want %d", got, len(lines))
	}
	if got := FitLineRun(lines, len(lines)+3, 128); got != len(lines) {
		t.Fatalf("start past end returned %d, want %d", got, len(lines))
	}
	// A zero/oversized budget normalizes to the top rung, so the tiny fixture fits
	// in one run instead of tripping an empty budget.
	for _, budget := range []int{0, -1, DefaultChunkTokens * 3} {
		if got := FitLineRun(lines, 0, budget); got != len(lines) {
			t.Fatalf("budget=%d returned end=%d, want %d", budget, got, len(lines))
		}
	}
}
