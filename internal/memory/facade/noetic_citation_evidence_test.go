// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade

import (
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/localvault"
)

// TestNoeticCitationEvidenceDropsBareNarrationMarker 钉住结晶引证的清洗契约：交付渲染
// 已剥掉的裸旁白标记行（`[assistant] 思考过程：` / `[assistant] Thinking Process:`）
// 不得单独充当引证句——ZCode 把推理流进 text 通道的回答步会以这类标记行开头，若不清洗，
// L3 引证就坍缩成一枚零信息标签行（ROI 面板 A 列只显示该标记的成因）。
func TestNoeticCitationEvidenceDropsBareNarrationMarker(t *testing.T) {
	h := localvault.LocalResolvedHit{
		DistilledPlaintext: "[assistant] 思考过程：\n\n1. **理解用户的终极核心意图**：\n    *   用户明确指出：rm 能力应与 rb 同。",
	}
	got := noeticCitationEvidence(h)
	if strings.Contains(got, "思考过程：") {
		t.Fatalf("citation evidence kept the bare narration marker: %q", got)
	}
	if !strings.Contains(got, "rm 能力应与 rb 同") {
		t.Fatalf("citation evidence lost the real content: %q", got)
	}
	only := noeticCitationEvidence(localvault.LocalResolvedHit{DistilledPlaintext: "[assistant] 思考过程："})
	if strings.TrimSpace(only) != "" {
		t.Fatalf("marker-only evidence must collapse to empty, got %q", only)
	}
	raw := noeticCitationEvidence(localvault.LocalResolvedHit{RawPlaintext: "[assistant] Thinking Process:\nPlatform X settled at 42 nodes."})
	if strings.Contains(raw, "Thinking Process:") || !strings.Contains(raw, "42 nodes") {
		t.Fatalf("raw fallback not cleaned: %q", raw)
	}
}

// TestRoleTaggedBlocksKeepSpeakerContinuationLines 钉住引证的结构兜底契约：角色块 = 角色行 +
// 其后续续行（到下一角色行或预算截断）。这样无论标签是什么语言、在不在净尾表里，引证都不会
// 坍缩成一行裸标签而丢掉标签下面的正文。
func TestRoleTaggedBlocksKeepSpeakerContinuationLines(t *testing.T) {
	text := "[user] 为什么 rm 客户端要做标签净尾？\n\n[assistant] 思考过程：\n\n1. 标签开头是开放集。\n2. 词表追不上且会误杀。\n\n[user] 继续"
	blocks := roleTaggedBlocks(text, 4)
	if len(blocks) != 3 {
		t.Fatalf("blocks=%d, want 3: %q", len(blocks), blocks)
	}
	if !strings.HasPrefix(blocks[1], "[assistant] 思考过程：") {
		t.Fatalf("assistant block lost its role prefix: %q", blocks[1])
	}
	if !strings.Contains(blocks[1], "1. 标签开头是开放集。") || !strings.Contains(blocks[1], "2. 词表追不上且会误杀。") {
		t.Fatalf("assistant block lost its continuation lines: %q", blocks[1])
	}
	// 纯角色壳（无正文）不算语音：其后的续行不得并入任何块。
	bare := roleTaggedBlocks("[assistant]\n游离内容", 4)
	if len(bare) != 0 {
		t.Fatalf("bare role tag produced a block: %q", bare)
	}
	// maxBlocks 限制块数。
	if capped := roleTaggedBlocks(text, 2); len(capped) != 2 {
		t.Fatalf("capped blocks=%d, want 2", len(capped))
	}
}
