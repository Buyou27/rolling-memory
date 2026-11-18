// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package text

import "strings"

// FirstRunes returns at most limit leading runes of value. Budgets are counted
// in runes, never bytes, so a multi-byte script is truncated at a character
// boundary (limit below zero is caller error and yields an empty string).
func FirstRunes(value string, limit int) string {
	if limit < 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// FirstTrimmedNonEmpty returns the first value whose Python-style trimmed form
// is non-empty, or "" when every candidate is blank. It never invents a value.
func FirstTrimmedNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
