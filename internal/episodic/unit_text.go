// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package episodic

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

// TokenBudget is the tokenz-r top rung (128/256/512/1024/2048/4096/8192): one
// unit never exceeds it unless a single sentence alone does.
//
// The unit text form lives in this package because three callers must agree on it
// byte for byte: the ingest unit builder renders it, the live pointer resolver
// re-renders it, and `SourceSnapshot` re-renders it from a frozen parse. A
// divergence would shift `#<k>` onto another segment.
const TokenBudget = 8192

// Text renders the role tag that makes a unit self-describing.
func Text(role, speech string) string {
	if strings.TrimSpace(role) == "" {
		role = "message"
	}
	return "[" + role + "] " + speech
}

// Chunk splits one rendered unit into pieces of at most budget tokens, cutting only
// at sentence boundaries. A sentence that alone exceeds the budget is kept whole:
// a unit may be wider than the rung, never truncated.
func Chunk(text string, budget int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if budget <= 0 || len(trimmed) <= budget || tokenzr.Count(trimmed) <= budget {
		return []string{trimmed}
	}
	sentences := splitSentencesKeepingDelimiters(trimmed)
	if len(sentences) == 0 {
		return []string{trimmed}
	}
	chunks := make([]string, 0, 4)
	var current strings.Builder
	currentTokens := 0
	for _, sentence := range sentences {
		sentenceTokens := tokenzr.Count(sentence)
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

// splitSentencesKeepingDelimiters cuts one text into sentence pieces that keep
// their own boundary punctuation (the SDK's pulse splitter drops delimiters, which
// is fine for frames but would rewrite stored speech). The boundary class mirrors
// the shared language-agnostic set: line breaks plus terminal punctuation, with a
// dotted-letter guard so "a.b" and "e.g" stay inside one sentence.
func splitSentencesKeepingDelimiters(text string) []string {
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
				prevAlnum = isLetterOrNumber(prev)
			}
			if i+w < n {
				next, _ := utf8.DecodeRuneInString(text[i+w:])
				nextAlnum = isLetterOrNumber(next)
			}
			boundary = !(prevAlnum && nextAlnum)
		}
		i += w
		if !boundary {
			continue
		}
		// Keep the boundary rune and the whitespace that follows it, so joining the
		// pieces reproduces the original byte-for-byte.
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

func isLetterOrNumber(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}
