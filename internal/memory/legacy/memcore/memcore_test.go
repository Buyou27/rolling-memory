// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import (
	"testing"
)

func TestLexicalStemMatching(t *testing.T) {
	if s := MatchTokenOrStem("she enjoys painting portraits", "painted"); s < 0.85 {
		t.Fatalf("expected stem match >= 0.85, got %v", s)
	}
}
