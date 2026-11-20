// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// TestRememberAsyncBecomesRecallableAfterDrain closes the async write loop: the
// unit is not retrievable while it sits in the queue, and becomes retrievable
// once a worker applies it through the same canonical path as a synchronous
// remember.
func TestRememberAsyncBecomesRecallableAfterDrain(t *testing.T) {
	memory := newVaultOnlyMemory(t)
	content := "Sydney harbour ferry terminal ticket office opens at seven."
	receipt, err := memory.RememberAsync(facade.RememberRequest{
		Content:    &content,
		Session:    "async-recall",
		OccurredAt: int64(1700000800),
	}, rememberQueueBounds())
	if err != nil {
		t.Fatalf("async remember: %v", err)
	}
	unitID := receipt["queued"].([]any)[0].(map[string]any)["id"].(string)

	before, err := memory.Recall(facade.RecallRequest{Cue: "Sydney harbour ferry ticket office", NoCrystallize: true})
	if err != nil {
		t.Fatalf("recall before drain: %v", err)
	}
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if containsUnit(beforeJSON, unitID) {
		t.Fatalf("queued unit was already recallable before apply: %s", beforeJSON)
	}

	processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1))
	if err != nil || processed != 1 {
		t.Fatalf("drain: processed=%d err=%v", processed, err)
	}

	after, err := memory.Recall(facade.RecallRequest{Cue: "Sydney harbour ferry ticket office", NoCrystallize: true})
	if err != nil {
		t.Fatalf("recall after drain: %v", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if !containsUnit(afterJSON, unitID) {
		t.Fatalf("applied unit is missing from recall: %s", afterJSON)
	}
}

func containsUnit(rendered []byte, unitID string) bool {
	return len(unitID) > 0 && strings.Contains(string(rendered), unitID)
}
