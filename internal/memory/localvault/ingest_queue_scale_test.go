// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestIngestQueueApplyScalesAcrossSizes verifies the queued apply path stays
// correct and roughly linear across cold-start, growth and steady-state sizes.
// Timings are logged, not asserted: the gate is that every size applies exactly
// once, which is the property a linear representation must preserve.
func TestIngestQueueApplyScalesAcrossSizes(t *testing.T) {
	for _, size := range []int{1, 10, 100} {
		t.Run(fmt.Sprintf("n=%d", size), func(t *testing.T) {
			vault, db := openIngestQueueVault(t)
			requests := make([]IngestQueueRequest, size)
			for i := range requests {
				requests[i] = IngestQueueRequest{
					RequestKey: fmt.Sprintf("scale-%d-%03d", size, i),
					OccurredAt: 1700000000 + int64(i),
					Identity:   VaultIdentity{AssistantID: "scale", IncarnationID: "scale-1", Locus: "Rolling-Memory"},
					RawPayload: []byte(fmt.Sprintf(`{"version":1,"doc_id":"scale-doc-%03d","plaintext":"scale queued text %03d"}`, i, i)),
				}
			}
			if _, err := vault.EnqueueIngestQueue(context.Background(), requests); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			applied := 0
			for {
				item, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0).Add(time.Duration(applied)*time.Second), time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					break
				}
				docID := fmt.Sprintf("scale-doc-%03d", applied)
				worker := NewLocalClient(vault.CloneWithSharedSlotCache())
				worker.SetIdentity(item.Identity.AssistantID, item.Identity.IncarnationID, item.Identity.Locus)
				prepared, err := worker.PrepareQueuedRemember(context.Background(), docID,
					fmt.Sprintf("scale queued text %03d", applied), item.OccurredAt, LayerEpisodic)
				if err != nil {
					t.Fatalf("prepare %d: %v", applied, err)
				}
				if _, err := vault.ApplyPreparedQueuedIngest(context.Background(), item, prepared); err != nil {
					t.Fatalf("apply %d: %v", applied, err)
				}
				applied++
			}
			elapsed := time.Since(started)
			if applied != size {
				t.Fatalf("applied = %d, want %d", applied, size)
			}
			var canonical int
			if err := db.QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&canonical); err != nil {
				t.Fatal(err)
			}
			if canonical != size {
				t.Fatalf("canonical rows = %d, want %d", canonical, size)
			}
			stats, err := vault.IngestQueueStats(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if stats.Applied != int64(size) || stats.Pending != 0 || stats.Running != 0 {
				t.Fatalf("stats = %+v", stats)
			}
			t.Logf("[queue-scale] n=%d total_ms=%.1f per_item_us=%.1f", size,
				float64(elapsed.Microseconds())/1000, float64(elapsed.Microseconds())/float64(size))
		})
	}
}
