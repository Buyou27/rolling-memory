// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package public

import "testing"

func TestPublicTokenizerAndShaping(t *testing.T) {
	if n := CountTokens("hello world"); n <= 0 {
		t.Fatalf("CountTokens = %d", n)
	}
	if got := CanonicalizeApostrophe("Ｆａｎｃｅｔｔ’ｓ"); got != "Fancett's" {
		t.Fatalf("CanonicalizeApostrophe = %q", got)
	}
	if cls := LookupEverydayTimeClass("yesterday"); cls == "" {
		t.Fatal("expected everyday time class for yesterday")
	}
}
