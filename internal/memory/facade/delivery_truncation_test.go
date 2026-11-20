// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
)

// TestRecallDeliveryCapsReportDroppedCounts verifies the receipt carries how much a cap
// cut, not just that it cut: has_more is what lets a caller ask for the next page instead
// of guessing whether results remain.
func TestRecallDeliveryCapsReportDroppedCounts(t *testing.T) {
	memory := newVaultOnlyMemory(t)
	client, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cap.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"quartz nebula evidence"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text := "[user] quartz nebula evidence"
	packet, _ := client.Vault.PackBlindPacket("ep_cap", text, testNow)
	if err := client.Vault.SaveLocalEpisodicPointerIndexed("ep_cap", testNow, "@ref:jsonl:"+path+":0#0", "", text, packet, localvault.IngestResponse{Layer: localvault.LayerEpisodic}); err != nil {
		t.Fatal(err)
	}

	zero := 0
	result, err := memory.Recall(facade.RecallRequest{Cue: "quartz nebula evidence", NoCrystallize: true, CapsBands: &zero})
	if err != nil {
		t.Fatal(err)
	}
	truncated, ok := result.Meta["truncated"].(map[string]any)
	if !ok {
		t.Fatalf("capped recall left no truncation receipt: %#v", result.Meta)
	}
	if dropped, _ := truncated["dropped_bands"].(int); dropped < 1 {
		t.Fatalf("dropped_bands = %#v, want >= 1", truncated["dropped_bands"])
	}
	if more, _ := truncated["has_more"].(bool); !more {
		t.Fatalf("has_more = %#v, want true", truncated["has_more"])
	}
	if got := result.Gaps["budget_exceeded"]; got != 1 {
		t.Fatalf("budget_exceeded = %d, want 1", got)
	}
}
