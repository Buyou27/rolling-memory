// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package recall

import (
	"strings"
	"testing"
)

// TestRenderRecordsCharacterTruncation verifies the render-time cut is reported even when
// no delivery cap created the truncation receipt: the block and the structured meta must
// agree that the payload was shortened.
func TestRenderRecordsCharacterTruncation(t *testing.T) {
	result := &Result{
		Scope: "mem:test",
		Bands: []map[string]any{{
			"kind": "source_item",
			"nodes": []map[string]any{
				{"id": "u1", "content": strings.Repeat("本地记忆的交付内容需要按预算裁剪", 6)},
				{"id": "u2", "content": strings.Repeat("第二段内容同样按预算裁剪", 6)},
			},
		}},
		Meta: map[string]any{},
	}
	block := result.Render(160)
	if !strings.Contains(block, "已截断") {
		t.Fatalf("render did not truncate: %q", block)
	}
	truncated, ok := result.Meta["truncated"].(map[string]any)
	if !ok {
		t.Fatalf("render truncation left no receipt: %#v", result.Meta)
	}
	if flagged, _ := truncated["chars"].(bool); !flagged {
		t.Fatalf("truncated.chars = %#v, want true", truncated["chars"])
	}
}
