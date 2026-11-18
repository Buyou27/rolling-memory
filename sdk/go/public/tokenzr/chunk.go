// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package tokenzr

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DefaultChunkTokens is the top rung of the seven-level tokenz-r ladder
// (128 / 256 / 512 / 1024 / 2048 / 4096 / 8192). By default, a single memory
// chunk is bounded by 8192 tokens unless a single sentence alone exceeds it;
// callers may pass any positive threshold below DefaultChunkTokens (e.g. 1024
// for background file chunks) to slice at a finer granularity.
const DefaultChunkTokens = 8192

// ChunkLadder enumerates the standard seven-level tokenz-r token budget rungs.
var ChunkLadder = [7]int{128, 256, 512, 1024, 2048, 4096, 8192}

// NormalizeChunkTokens normalizes a caller-supplied chunk token threshold into
// the valid range (0, DefaultChunkTokens]: values <= 0 or > DefaultChunkTokens
// resolve to DefaultChunkTokens (8192), while any positive threshold below
// DefaultChunkTokens is preserved as requested by the client.
func NormalizeChunkTokens(maxTokens int) int {
	if maxTokens <= 0 || maxTokens > DefaultChunkTokens {
		return DefaultChunkTokens
	}
	return maxTokens
}

// LineChunk represents one contiguous 1-based line-range slice produced by
// ChunkLines, preserving 1:1 line coordinates with the source document.
type LineChunk struct {
	ChunkIndex int      `json:"chunk_index"`
	Anchor     string   `json:"anchor"`
	Lines      []string `json:"lines"`
	LineStart  int      `json:"line_start"`
	LineEnd    int      `json:"line_end"`
	TokenCount int      `json:"token_count"`
}

// ChunkBySentences splits text into lossless chunks of at most
// NormalizeChunkTokens(maxTokens) tokens, cutting strictly at sentence
// boundaries. A single sentence that alone exceeds the budget is kept intact
// so no sentence is ever truncated mid-way.
func ChunkBySentences(text string, maxTokens int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	budget := NormalizeChunkTokens(maxTokens)
	if len(trimmed) <= budget || Count(trimmed) <= budget {
		return []string{trimmed}
	}
	sentences := SplitSentencesKeepingDelimiters(trimmed)
	if len(sentences) == 0 {
		return []string{trimmed}
	}
	chunks := make([]string, 0, 4)
	var current strings.Builder
	currentTokens := 0
	for _, sentence := range sentences {
		sentenceTokens := Count(sentence)
		if currentTokens > 0 && currentTokens+sentenceTokens > budget {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
			currentTokens = 0
		}
		current.WriteString(sentence)
		currentTokens += sentenceTokens
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		chunks = append(chunks, tail)
	}
	if len(chunks) == 0 {
		return []string{trimmed}
	}
	return chunks
}

// SplitSentencesKeepingDelimiters segments text into sentence pieces while
// preserving each sentence's terminal punctuation and trailing whitespace so
// concatenating the returned slices reproduces the input without loss.
// Decimal and abbreviation dots between alphanumeric runes (e.g. "3.14", "a.b")
// remain inside the current sentence.
func SplitSentencesKeepingDelimiters(text string) []string {
	out := make([]string, 0, 8)
	start := 0
	for i, n := 0, len(text); i < n; {
		r, w := utf8.DecodeRuneInString(text[i:])
		boundary := r == '\n' || r == '\r' || r == '。' || r == '！' || r == '？' ||
			r == '；' || r == ';' || r == '!' || r == '?'
		if !boundary && r == '.' {
			prevAlnum, nextAlnum := false, false
			if i > 0 {
				prev, _ := utf8.DecodeLastRuneInString(text[:i])
				prevAlnum = unicode.IsLetter(prev) || unicode.IsNumber(prev)
			}
			if i+w < n {
				next, _ := utf8.DecodeRuneInString(text[i+w:])
				nextAlnum = unicode.IsLetter(next) || unicode.IsNumber(next)
			}
			boundary = !(prevAlnum && nextAlnum)
		}
		i += w
		if !boundary {
			continue
		}
		for i < n {
			next, nextWidth := utf8.DecodeRuneInString(text[i:])
			if next != ' ' && next != '\t' && next != '\n' && next != '\r' {
				break
			}
			i += nextWidth
		}
		if piece := text[start:i]; strings.TrimSpace(piece) != "" {
			out = append(out, piece)
		}
		start = i
	}
	if start < len(text) {
		if piece := text[start:]; strings.TrimSpace(piece) != "" {
			out = append(out, piece)
		}
	}
	return out
}

// ChunkLines groups source lines into contiguous chunks bounded by
// NormalizeChunkTokens(maxTokens) tokens while preserving 1:1 1-based line
// coordinates (LineStart..LineEnd) and soft-breaking at Markdown headings once
// at least one-third of the chunk budget is filled.
func ChunkLines(lines []string, maxTokens int, anchorPrefix string) []LineChunk {
	if len(lines) == 0 {
		return nil
	}
	budget := NormalizeChunkTokens(maxTokens)
	if anchorPrefix == "" {
		anchorPrefix = "L"
	}
	var out []LineChunk
	var curLines []string
	curTokens := 0
	startIdx := 1

	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		tokens := Count(trimmed)
		isHeading := strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ")
		if (curTokens+tokens > budget || (isHeading && curTokens >= budget/3)) && len(curLines) > 0 {
			endIdx := idx
			if endIdx < startIdx {
				endIdx = startIdx
			}
			out = append(out, LineChunk{
				ChunkIndex: len(out),
				Anchor:     anchorPrefix + strconv.Itoa(startIdx) + "-" + strconv.Itoa(idx),
				Lines:      curLines,
				LineStart:  startIdx,
				LineEnd:    endIdx,
				TokenCount: curTokens,
			})
			curLines = nil
			curTokens = 0
			startIdx = idx + 1
		}
		curLines = append(curLines, trimmed)
		curTokens += tokens
	}
	if len(curLines) > 0 {
		endIdx := len(lines)
		if endIdx < startIdx {
			endIdx = startIdx
		}
		out = append(out, LineChunk{
			ChunkIndex: len(out),
			Anchor:     anchorPrefix + strconv.Itoa(startIdx) + "-" + strconv.Itoa(len(lines)),
			Lines:      curLines,
			LineStart:  startIdx,
			LineEnd:    endIdx,
			TokenCount: curTokens,
		})
	}
	return out
}
