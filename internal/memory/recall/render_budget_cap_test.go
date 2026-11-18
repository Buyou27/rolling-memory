// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package recall

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestRenderHonorsRequestedCap pins the delivery cap: the whole rendered block
// (envelope included) must stay inside the requested character budget. The
// earlier 200-rune floor made `caps.chars=300` deliver 357 runes.
func TestRenderHonorsRequestedCap(t *testing.T) {
	long := strings.Repeat("这条记忆句子用于占满交付预算。", 200)
	result := &Result{
		Scope: "mem:test",
		Bands: []map[string]any{{
			"title": "测试带",
			"kind":  "cell_band",
			"nodes": []map[string]any{{
				"id":      "u1",
				"content": long,
				"status":  "active",
			}},
		}},
	}
	for _, maxChars := range []int{300, 500, 1600} {
		rendered := result.Render(maxChars)
		if got := utf8.RuneCountInString(rendered); got > maxChars {
			t.Fatalf("Render(%d) delivered %d runes:\n%s", maxChars, got, rendered)
		}
		if !strings.Contains(rendered, "已截断") {
			t.Fatalf("Render(%d) dropped the truncation marker:\n%s", maxChars, rendered)
		}
	}
	// A budget that fits the whole body delivers it untruncated.
	full := result.Render(16000)
	if strings.Contains(full, "已截断") {
		t.Fatalf("Render(16000) truncated a body it can hold:\n%s", full)
	}
	if !strings.Contains(full, "这条记忆句子用于占满交付预算。") {
		t.Fatalf("Render(16000) lost the content:\n%s", full)
	}
}
