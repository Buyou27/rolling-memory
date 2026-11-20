// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"
)

func applyScalePercentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(q * float64(len(sorted)-1))
	return sorted[index]
}

// TestIngestQueueApplyScales measures end-to-end queued apply throughput and
// heap growth at a production-sized batch. It skips unless RM_QUEUE_APPLY_N is
// set.
func TestIngestQueueApplyScales(t *testing.T) {
	raw := os.Getenv("RM_QUEUE_APPLY_N")
	if raw == "" {
		t.Skip("set RM_QUEUE_APPLY_N to measure queued apply at scale")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("invalid RM_QUEUE_APPLY_N %q", raw)
	}
	vault, db := openIngestQueueVault(t)
	requests := make([]IngestQueueRequest, n)
	for i := range requests {
		text := fmt.Sprintf("queued scale document %06d t%04d t%04d t%04d t%04d t%04d t%04d t%04d t%04d",
			i, i%97, (i*7)%97, (i*13)%97, (i*29)%97, (i*31)%97, (i*37)%97, (i*41)%97, (i*43)%97)
		requests[i] = IngestQueueRequest{
			RequestKey: fmt.Sprintf("apply-%08d", i),
			OccurredAt: 1700000000 + int64(i)*60,
			Identity:   VaultIdentity{AssistantID: "scale", IncarnationID: "apply-1", Locus: "Rolling-Memory"},
			RawPayload: []byte(fmt.Sprintf(`{"version":1,"doc_id":"apply-%08d","plaintext":%q}`, i, text)),
		}
	}
	runtime.GC()
	var heapBefore runtime.MemStats
	runtime.ReadMemStats(&heapBefore)
	enqueueStarted := time.Now()
	const batchSize = 1000
	for start := 0; start < n; start += batchSize {
		end := start + batchSize
		if end > n {
			end = n
		}
		bounds := &IngestQueueBounds{MaxPendingBytes: int64(n) * 256}
		if _, err := vault.EnqueueIngestQueueBounded(context.Background(), requests[start:end], bounds); err != nil {
			t.Fatalf("enqueue %d..%d: %v", start, end, err)
		}
	}
	enqueueElapsed := time.Since(enqueueStarted)

	now := time.Unix(1700000000, 0)
	latencies := make([]float64, 0, n)
	applyStarted := time.Now()
	for i := 0; i < n; i++ {
		item, ok, err := vault.ClaimIngestQueue(context.Background(), now.Add(time.Duration(i)*time.Second), 30*time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim %d: ok=%v err=%v", i, ok, err)
		}
		var payload struct {
			DocID     string `json:"doc_id"`
			Plaintext string `json:"plaintext"`
		}
		if err := json.Unmarshal(item.RawPayload, &payload); err != nil {
			t.Fatalf("decode payload %d: %v", i, err)
		}
		worker := NewLocalClient(vault.CloneWithSharedSlotCache())
		worker.SetIdentity(item.Identity.AssistantID, item.Identity.IncarnationID, item.Identity.Locus)
		start := time.Now()
		prepared, err := worker.PrepareQueuedRemember(context.Background(), payload.DocID, payload.Plaintext, item.OccurredAt, LayerEpisodic)
		if err != nil {
			t.Fatalf("prepare %d: %v", i, err)
		}
		if _, err := vault.ApplyPreparedQueuedIngest(context.Background(), item, prepared); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
		latencies = append(latencies, float64(time.Since(start).Microseconds())/1000)
	}
	applyElapsed := time.Since(applyStarted)
	runtime.GC()
	var heapAfter runtime.MemStats
	runtime.ReadMemStats(&heapAfter)

	var canonical int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	if canonical != n {
		t.Fatalf("canonical rows = %d, want %d", canonical, n)
	}
	stats, err := vault.IngestQueueStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Applied != int64(n) || stats.Pending != 0 || stats.Running != 0 {
		t.Fatalf("queue stats = %+v", stats)
	}
	t.Logf("[queue-apply] n=%d enqueue_s=%.2f apply_s=%.2f docs_per_s=%.1f apply_p50=%.2fms apply_p95=%.2fms db_MB=%.1f heap_delta_MB=%.1f heap_sys_delta_MB=%.1f",
		n, enqueueElapsed.Seconds(), applyElapsed.Seconds(), float64(n)/applyElapsed.Seconds(),
		applyScalePercentile(latencies, 0.50), applyScalePercentile(latencies, 0.95),
		float64(databaseBytes(t, db))/(1<<20),
		float64(int64(heapAfter.HeapAlloc)-int64(heapBefore.HeapAlloc))/(1<<20),
		float64(int64(heapAfter.HeapSys)-int64(heapBefore.HeapSys))/(1<<20))
}
