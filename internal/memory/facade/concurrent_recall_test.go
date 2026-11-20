// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// TestConcurrentRecallSharesOneSnapshot pins the read-plane contract: several
// callers recall from one published snapshot at the same time, every call
// succeeds, and they all see the same evidence. The engine holds one query
// scratch per instance, so the check only passes when each caller works on a
// shallow copy with its own scratch; run with -race to catch a shared one.
func TestConcurrentRecallSharesOneSnapshot(t *testing.T) {
	const scope = "mem:concurrent-recall"
	memory := newVaultOnlyMemoryWithScope(t, scope)
	for i := 0; i < 24; i++ {
		content := fmt.Sprintf("Doc %d: Alice met Bob in Paris about topic %d on 2026-01-%02d.", i, i, i%28+1)
		if _, err := memory.Remember(facade.RememberRequest{
			Content:    &content,
			Session:    "s1",
			OccurredAt: 1700000000 + int64(i)*3600,
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if _, err := memory.RebuildSnapshot(context.Background()); err != nil {
		t.Fatalf("rebuild snapshot: %v", err)
	}
	if !memory.PublishedSnapshotReady() {
		t.Fatal("snapshot not published")
	}
	const cue = "Alice Paris topic"
	if _, err := memory.Recall(facade.RecallRequest{Cue: cue, NoCrystallize: true}); err != nil {
		t.Fatalf("warmup recall: %v", err)
	}

	const workers, each = 8, 8
	signatures := make([]string, workers)
	errs := make([]error, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		group.Add(1)
		go func() {
			defer group.Done()
			seen := make([]string, 0, each)
			for i := 0; i < each; i++ {
				result, err := memory.Recall(facade.RecallRequest{Cue: cue, NoCrystallize: true})
				if err != nil {
					errs[worker] = err
					return
				}
				seen = append(seen, fmt.Sprint(result.Bands))
			}
			signatures[worker] = strings.Join(seen, "\x00")
		}()
	}
	group.Wait()

	for worker, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", worker, err)
		}
	}
	for worker, signature := range signatures {
		if signature == "" {
			t.Fatalf("worker %d delivered nothing", worker)
		}
		if signature != signatures[0] {
			t.Fatalf("worker %d saw different evidence than worker 0", worker)
		}
	}
}
