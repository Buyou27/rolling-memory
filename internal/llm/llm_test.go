// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package llm

import "testing"

func TestParseJSONObjectFences(t *testing.T) {
	parsed := ParseJSONObject("```json\n{\"assertions\":[]}\n```")
	if parsed == nil || len(parsed) != 1 {
		t.Fatalf("parsed = %#v", parsed)
	}
	parsed = ParseJSONObject("prefix {\"s\":\"x\"} suffix")
	if parsed["s"] != "x" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if ParseJSONObject("not json") != nil {
		t.Fatal("non-JSON should return nil")
	}
}
