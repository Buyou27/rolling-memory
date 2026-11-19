// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/episodicunit"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
)

// A turn band must show what the user asked even when retrieval only matched a later
// assistant segment: the turn's own stored segment `#0` (the opening user message) is
// hydrated from the verified index and prepended ahead of the delivered hit window.
func TestLocalRecallEpisodicTurnKeepsUserPromptOutsideHitWindow(t *testing.T) {
	memory := newMemory(t)
	client, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "turn.jsonl")
	lines := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"请只作答：用户首段标记 zebra-prompt"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":"quartz answer beta evidence 已按问题收口"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	turnRef := "@ref:jsonl:" + path + ":0"
	segments := []struct {
		id   string
		ptr  string
		text string
	}{
		{"ep_turn_prompt", turnRef + "#0", "[user] 请只作答：用户首段标记 zebra-prompt"},
		{"ep_turn_answer", turnRef + "#1", "[assistant] quartz answer beta evidence 已按问题收口"},
	}
	for index, segment := range segments {
		packet, _ := client.Vault.PackBlindPacket(segment.id, segment.text, testNow+int64(index))
		if err := client.Vault.SaveLocalEpisodicPointerIndexed(segment.id, testNow+int64(index), segment.ptr, turnRef, segment.text, packet, localvault.IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatalf("save %s: %v", segment.id, err)
		}
	}

	// The cue matches only the assistant segment; the prompt unit must stay outside the
	// hit window and still be delivered as the turn's provenance head.
	result, err := memory.Recall(facade.RecallRequest{Cue: "quartz answer beta evidence", NoCrystallize: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bands) == 0 {
		t.Fatalf("expected a turn band, got none (raw=%d)", len(result.RawHits))
	}
	nodes, _ := result.Bands[0]["nodes"].([]map[string]any)
	if len(nodes) == 0 {
		t.Fatalf("band without nodes: %+v", result.Bands[0])
	}
	content, _ := nodes[0]["content"].(string)
	if !strings.Contains(content, "zebra-prompt") {
		t.Fatalf("delivered band must carry the user prompt, got: %s", content)
	}
	window, _ := nodes[0]["segment_window"].([]int)
	if len(window) != 2 || window[0] == 0 {
		t.Fatalf("prompt must come from the turn head, not the hit window: %v", nodes[0]["segment_window"])
	}
	if rendered := result.Render(6000); !strings.Contains(rendered, "[user]") {
		t.Fatalf("rendered recall must show the user line: %s", rendered)
	}
}

// TestLocalRecallLongTurnFollowerChunkKeepsSpeakerAttribution pins the attribution
// rule for messages longer than one chunk: only the opening chunk of a segment
// carries the inline role tag, so a hit that lands on a later chunk must inherit
// the speaker named by the turn's first segment (read from verified source text,
// never inferred from content).
func TestLocalRecallLongTurnFollowerChunkKeepsSpeakerAttribution(t *testing.T) {
	memory := newMemory(t)
	client, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	filler := strings.Repeat("alpha 缓存断言复跑全部包，确认没有回归之后再汇报结论。", 800)
	speech := filler + "zebra quokka evidence 只出现在后片。"
	prompt, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": "请记录这次长回复的结论"},
	})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := json.Marshal(map[string]any{
		"type":    "assistant",
		"message": map[string]any{"role": "assistant", "content": speech},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "long-turn.jsonl")
	if err := os.WriteFile(path, append(append(prompt, '\n'), append(answer, '\n')...), 0o600); err != nil {
		t.Fatal(err)
	}
	turnRef := "@ref:jsonl:" + path + ":0"
	chunks := episodicunit.Chunk(episodicunit.Text("assistant", speech), episodicunit.TokenBudget)
	if len(chunks) < 2 {
		t.Fatalf("fixture did not split into chunks: %d", len(chunks))
	}
	for index, chunk := range chunks {
		id := fmt.Sprintf("ep_long_chunk_%d", index)
		pointer := fmt.Sprintf("%s#1.%d", turnRef, index)
		packet, _ := client.Vault.PackBlindPacket(id, chunk, testNow+int64(index))
		if err := client.Vault.SaveLocalEpisodicPointerIndexed(id, testNow+int64(index), pointer, turnRef, chunk, packet, localvault.IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatalf("save chunk %d: %v", index, err)
		}
	}

	result, err := memory.Recall(facade.RecallRequest{Cue: "zebra quokka evidence", NoCrystallize: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bands) == 0 {
		t.Fatalf("expected a delivered band, got none (raw=%d)", len(result.RawHits))
	}
	var authors []string
	for _, band := range result.Bands {
		nodes, _ := band["nodes"].([]map[string]any)
		for _, node := range nodes {
			if list, ok := node["authors"].([]string); ok {
				authors = append(authors, list...)
			}
		}
	}
	if len(authors) == 0 {
		t.Fatalf("follower-chunk hit delivered without speaker attribution: %+v", result.Bands)
	}
	found := false
	for _, author := range authors {
		if author == "assistant" {
			found = true
		}
	}
	if !found {
		t.Fatalf("speaker attribution = %v, want assistant from the turn's first segment", authors)
	}
}
