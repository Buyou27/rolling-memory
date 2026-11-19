// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

func TestCrystallizationQueueRoundTrip(t *testing.T) {
	memory := newMemory(t)
	memory.SetSnapshotOnly(true)
	request := facade.CrystallizationRequest{
		NoeticID:   "nt_queue_roundtrip",
		Query:      "queue probe query",
		Answer:     "queue probe answer",
		OccurredAt: testNow,
	}
	if err := memory.EnqueueCrystallization(request); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	dir := memory.Config.DB + ".crystallize.d"
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("queue directory: entries=%d err=%v", len(entries), err)
	}
	processed, err := memory.ProcessCrystallizationQueue(context.Background(), 1)
	if err != nil || processed != 1 {
		t.Fatalf("process: processed=%d err=%v", processed, err)
	}
	var count int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_noetic WHERE noetic_id = ?`, request.NoeticID).Scan(&count); err != nil {
		t.Fatalf("query noetic: %v", err)
	}
	if count != 1 {
		t.Fatalf("noetic rows = %d, want 1", count)
	}
	if processed, err := memory.ProcessCrystallizationQueue(context.Background(), 1); err != nil || processed != 0 {
		t.Fatalf("second drain: processed=%d err=%v", processed, err)
	}
	for i := 0; i < 3; i++ {
		if err := memory.EnqueueCrystallization(facade.CrystallizationRequest{
			NoeticID:   "nt_queue_batch_" + string(rune('a'+i)),
			Query:      "batch query",
			Answer:     "batch answer",
			OccurredAt: testNow + int64(i),
		}); err != nil {
			t.Fatalf("enqueue batch %d: %v", i, err)
		}
	}
	if processed, err := memory.ProcessCrystallizationQueue(context.Background(), 1); err != nil || processed != 1 {
		t.Fatalf("batch drain: processed=%d err=%v", processed, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 2 {
		t.Fatalf("remaining queue: entries=%d err=%v", len(entries), err)
	}
	if err := os.WriteFile(filepath.Join(dir, "poison.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ProcessCrystallizationQueue(context.Background(), 10); err != nil {
		t.Fatalf("poison drain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "poison.json")); !os.IsNotExist(err) {
		t.Fatalf("poison entry was not dropped: %v", err)
	}
}
