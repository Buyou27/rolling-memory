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
//
// The rung also guards the RMBP wire caps: the wire header stores distinct atom
// keys and sentence frames as uint16 (65535 each), and a chunk at this rung
// carries at most 8192 tokens, so sentence frames stay <= 8192 and distinct atom
// keys stay below the cap even when every word expands into stem/subword atoms.
// A single sentence that alone exceeds the rung breaks the bound by contract (it
// is never truncated); such a payload is rejected loud at wire encode instead of
// being silently clipped.
const DefaultChunkTokens = 8192

// ChunkLadder enumerates the standard seven-level tokenz-r token budget rungs.
var ChunkLadder = [7]int{128, 256, 512, 1024, 2048, 4096, 8192}

// ChunkerVersion pins the chunk-text contract: the sentence-boundary class, the
// exact budget check and the per-piece fidelity rules. Chunk text is an identity
// input for every content-addressed unit derived from it (episodic docIDs,
// background file bodies), so changing any of those rules changes stored
// identities. Hosts must treat a version bump as a re-index migration, never as a
// silent upgrade.
const ChunkerVersion = 1

// headingSoftBreakDivisor closes a line chunk that has already filled one third
// of its budget before a Markdown heading, so the heading opens the next chunk
// together with its body. It is a layout heuristic only: it moves a split point
// by at most one heading and never changes ranking or the token budget itself.
const headingSoftBreakDivisor = 3

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
// ChunkLines, preserving 1:1 line coordinates with the source document. Lines
// hold the source text verbatim (indentation included); TokenCount is the exact
// count of their newline-joined body.
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
// boundaries. The returned pieces concatenate back to text byte for byte: no
// piece is trimmed and no separator is dropped. Every assembled piece is checked
// against the budget exactly, because token counts are not additive across a
// boundary. A single sentence that alone exceeds the budget is kept intact so no
// sentence is ever truncated mid-way.
func ChunkBySentences(text string, maxTokens int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	budget := NormalizeChunkTokens(maxTokens)
	if Count(text) <= budget {
		return []string{text}
	}
	sentences := SplitSentencesKeepingDelimiters(text)
	if len(sentences) == 0 {
		return []string{text}
	}
	chunks := make([]string, 0, 4)
	for start := 0; start < len(sentences); {
		end := sentenceChunkEnd(sentences, start, budget)
		chunks = append(chunks, strings.Join(sentences[start:end], ""))
		start = end
	}
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// sentenceChunkEnd returns the exclusive end index of the chunk starting at
// start: the longest run of sentences whose per-sentence token sum stays within
// budget, peeled back until the exact joined text fits as well. The sum is only a
// fast bound; the assembled chunk is the judge, because a join can cost more
// tokens than its parts (e.g. "hqar. " + "uebl" = 5 + 1 -> 7). A single sentence
// that alone exceeds the budget is returned on its own.
func sentenceChunkEnd(sentences []string, start, budget int) int {
	estimate := 0
	end := start
	for end < len(sentences) {
		tokens := Count(sentences[end])
		if end > start && estimate+tokens > budget {
			break
		}
		estimate += tokens
		end++
	}
	for end > start+1 && Count(strings.Join(sentences[start:end], "")) > budget {
		end--
	}
	return end
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
		tail := text[start:]
		if strings.TrimSpace(tail) != "" || len(out) == 0 {
			out = append(out, tail)
		} else {
			// A whitespace-only tail belongs to the piece before it; keeping it
			// there is what makes the concatenation reproduce text exactly.
			out[len(out)-1] += tail
		}
	}
	return out
}

// ChunkLines groups source lines into contiguous chunks bounded by
// NormalizeChunkTokens(maxTokens) tokens while preserving 1:1 1-based line
// coordinates (LineStart..LineEnd). Lines are stored verbatim so indentation and
// intra-line whitespace survive, and the budget is enforced on the exact
// newline-joined body rather than on the per-line estimates. A Markdown heading
// opens a new chunk once the current one has filled one third of its budget, and
// a single line that alone exceeds the budget is kept whole instead of truncated.
func ChunkLines(lines []string, maxTokens int, anchorPrefix string) []LineChunk {
	if len(lines) == 0 {
		return nil
	}
	budget := NormalizeChunkTokens(maxTokens)
	if anchorPrefix == "" {
		anchorPrefix = "L"
	}
	out := make([]LineChunk, 0, 4)
	for start := 0; start < len(lines); {
		end := lineChunkEnd(lines, start, budget)
		chunkLines := append([]string(nil), lines[start:end]...)
		out = append(out, LineChunk{
			ChunkIndex: len(out),
			Anchor:     anchorPrefix + strconv.Itoa(start+1) + "-" + strconv.Itoa(end),
			Lines:      chunkLines,
			LineStart:  start + 1,
			LineEnd:    end,
			TokenCount: Count(strings.Join(chunkLines, "\n")),
		})
		start = end
	}
	return out
}

// FitLineRun returns the exclusive end index of the longest run of lines starting
// at start whose newline-joined body fits the normalized tokenz-r budget exactly.
// Per-line estimates bound the scan (counted on trimmed text, so indentation is
// layout rather than content) and the exact joined body decides, peeling trailing
// lines back when a join costs more than its parts; a single line that alone
// exceeds the budget is returned on its own. Producers that mint their own anchors
// (spreadsheet row ranges, for example) share this decision instead of
// re-implementing the budget.
func FitLineRun(lines []string, start int, maxTokens int) int {
	if start < 0 {
		start = 0
	}
	if start >= len(lines) {
		return len(lines)
	}
	budget := NormalizeChunkTokens(maxTokens)
	estimate := 0
	end := start
	for end < len(lines) {
		tokens := Count(strings.TrimSpace(lines[end]))
		if end > start && estimate+tokens > budget {
			break
		}
		estimate += tokens
		end++
	}
	return fitLineRunEnd(lines, start, end, budget)
}

// fitLineRunEnd peels end back until the exact joined body fits, keeping at least
// one line (the single-line exception).
func fitLineRunEnd(lines []string, start, end, budget int) int {
	for end > start+1 && Count(strings.Join(lines[start:end], "\n")) > budget {
		end--
	}
	return end
}

// lineChunkEnd returns the exclusive end index of the chunk starting at start. The
// run is the exact budget fit; a Markdown heading inside it opens a new chunk once
// the current one has filled one headingSoftBreakDivisor-th of the budget.
func lineChunkEnd(lines []string, start, budget int) int {
	end := FitLineRun(lines, start, budget)
	estimate := 0
	for i := start; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		tokens := Count(trimmed)
		if i > start && isMarkdownHeading(trimmed) && estimate >= budget/headingSoftBreakDivisor {
			return fitLineRunEnd(lines, start, i, budget)
		}
		estimate += tokens
	}
	return end
}

// isMarkdownHeading reports whether a trimmed line opens a Markdown ATX section at
// levels one to three. Deeper levels and setext underlines are treated as body
// text; that only moves a split point, never content.
func isMarkdownHeading(trimmed string) bool {
	return strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") ||
		strings.HasPrefix(trimmed, "### ")
}
