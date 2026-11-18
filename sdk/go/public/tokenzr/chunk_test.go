// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package tokenzr

import (
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
		if !strings.HasSuffix(c, ".") {
			t.Fatalf("chunk %d does not end at sentence boundary: %q", i, c)
		}
	}
	// Default 8192 threshold keeps medium text in a single chunk.
	single := ChunkBySentences(text, 0)
	if len(single) != 1 {
		t.Fatalf("expected 1 chunk under default 8192 threshold, got %d", len(single))
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
