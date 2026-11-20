// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func queueScalePercentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(q * float64(len(sorted)-1))
	return sorted[index]
}

// TestIngestQueueClaimScalesWithBacklog measures enqueue throughput and claim
// latency with a large pending backlog. It is a scale instrument and skips
// unless RM_QUEUE_SCALE_N is set.
func TestIngestQueueClaimScalesWithBacklog(t *testing.T) {
	raw := os.Getenv("RM_QUEUE_SCALE_N")
	if raw == "" {
		t.Skip("set RM_QUEUE_SCALE_N to measure claim latency at a large backlog")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("invalid RM_QUEUE_SCALE_N %q", raw)
	}
	vault, db := openIngestQueueVault(t)
	const batchSize = 1000
	enqueueStarted := time.Now()
	enqueued := 0
	for enqueued < n {
		size := batchSize
		if remaining := n - enqueued; remaining < size {
			size = remaining
		}
		batch := make([]IngestQueueRequest, size)
		for i := range batch {
			batch[i] = IngestQueueRequest{
				RequestKey: fmt.Sprintf("scale-%08d", enqueued+i),
				OccurredAt: 1700000000 + int64(enqueued+i),
				RawPayload: []byte(fmt.Sprintf(`{"version":1,"doc_id":"scale-%08d","plaintext":"x"}`, enqueued+i)),
			}
		}
		bounds := &IngestQueueBounds{MaxPendingBytes: int64(n)*64 + 1<<20}
		if _, err := vault.EnqueueIngestQueueBounded(context.Background(), batch, bounds); err != nil {
			t.Fatalf("enqueue batch at %d: %v", enqueued, err)
		}
		enqueued += size
	}
	enqueueElapsed := time.Since(enqueueStarted)

	now := time.Unix(1700000000, 0)
	if plan := explainQueuePlan(t, db, `
		SELECT queue_id FROM rm_ingest_queue
		WHERE status = 'pending' AND next_attempt_at_ns <= ?
		ORDER BY queue_id LIMIT 1`, now.UnixNano()); plan != "" {
		t.Logf("[queue-backlog] pending plan: %s", plan)
	}
	if plan := explainQueuePlan(t, db, `
		SELECT COALESCE(MIN(queue_id), 0) FROM rm_ingest_queue
		WHERE status = 'running' AND lease_expires_at_ns <= ?`, now.UnixNano()); plan != "" {
		t.Logf("[queue-backlog] running plan: %s", plan)
	}
	const claims = 100
	latencies := make([]float64, 0, claims)
	var firstID int64
	for i := 0; i < claims; i++ {
		start := time.Now()
		item, ok, err := vault.ClaimIngestQueue(context.Background(), now, time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim %d: ok=%v err=%v", i, ok, err)
		}
		latencies = append(latencies, float64(time.Since(start).Microseconds())/1000)
		if i == 0 {
			firstID = item.QueueID
		}
	}
	if firstID != 1 {
		t.Fatalf("first claimed queue id = %d, want 1 (oldest)", firstID)
	}
	stats, err := vault.IngestQueueStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pending != int64(n-claims) || stats.Running != claims {
		t.Fatalf("stats after claims = %+v", stats)
	}
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_ingest_queue`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != n {
		t.Fatalf("queue rows = %d, want %d", total, n)
	}
	t.Logf("[queue-backlog] n=%d enqueue_ms=%.1f enqueue_per_row_us=%.1f claim_p50=%.3fms claim_p95=%.3fms row_bytes=%.0f",
		n, float64(enqueueElapsed.Microseconds())/1000, float64(enqueueElapsed.Microseconds())/float64(n),
		queueScalePercentile(latencies, 0.50), queueScalePercentile(latencies, 0.95),
		float64(databaseBytes(t, db))/float64(n))
}

// TestIngestQueueQueriesUseIndexes pins the plan shape of every queue query
// that filters on status. With live-state partial indexes the byte budget, the
// applied watermark and the depth report all fell back to a full table scan,
// so these plans are regression gates rather than diagnostics.
func TestIngestQueueQueriesUseIndexes(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("plan-pending", "p")}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0).UnixNano()
	checks := []struct {
		label string
		query string
		args  []any
		index string
	}{
		{
			label: "claim pending head",
			query: `SELECT queue_id FROM rm_ingest_queue
				WHERE status = 'pending' AND next_attempt_at_ns <= ?
				ORDER BY queue_id LIMIT 1`,
			args:  []any{now},
			index: "idx_rm_ingest_queue_pending",
		},
		{
			label: "expired running lease",
			query: `SELECT COALESCE(MIN(queue_id), 0) FROM rm_ingest_queue
				WHERE status = 'running' AND lease_expires_at_ns <= ?`,
			args:  []any{now},
			index: "idx_rm_ingest_queue_running",
		},
		{
			label: "pending+running byte budget",
			query: `SELECT COALESCE(SUM(LENGTH(raw_payload)), 0) FROM rm_ingest_queue
				WHERE status IN ('pending','running')`,
			index: "idx_rm_ingest_queue_pending",
		},
		{
			label: "applied watermark",
			query: `SELECT COALESCE(MAX(queue_id), 0) FROM rm_ingest_queue WHERE status = 'applied'`,
			index: "idx_rm_ingest_queue_pending",
		},
		{
			label: "depth by state",
			query: `SELECT status, COUNT(*), COALESCE(SUM(LENGTH(raw_payload)), 0)
				FROM rm_ingest_queue GROUP BY status`,
			index: "idx_rm_ingest_queue_pending",
		},
		{
			label: "visible applied",
			query: `SELECT COUNT(*) FROM rm_ingest_queue
				WHERE status = 'applied' AND applied_change_seq > 0 AND applied_change_seq <= ?`,
			args:  []any{int64(1)},
			index: "idx_rm_ingest_queue_pending",
		},
		{
			label: "prune applied and retired generations",
			query: `DELETE FROM rm_ingest_queue
				WHERE (status = 'applied' AND applied_change_seq > 0 AND applied_change_seq <= ?)
				   OR (status = 'canceled' AND purge_epoch < (SELECT purge_epoch FROM rm_revision WHERE id = 1))`,
			args: []any{int64(1)},
		},
	}
	for _, check := range checks {
		plan := explainQueuePlan(t, db, check.query, check.args...)
		assertNoQueueTableScan(t, check.label, plan)
		if check.index != "" && !strings.Contains(plan, check.index) {
			t.Fatalf("%s does not use %s:\n%s", check.label, check.index, plan)
		}
	}
}

func assertNoQueueTableScan(t *testing.T, label, plan string) {
	t.Helper()
	for _, segment := range strings.Split(plan, " | ") {
		trimmed := strings.TrimSpace(segment)
		if strings.HasPrefix(trimmed, "SCAN rm_ingest_queue") &&
			!strings.Contains(trimmed, "USING INDEX") &&
			!strings.Contains(trimmed, "USING COVERING INDEX") {
			t.Fatalf("%s full-scans the queue table:\n%s", label, plan)
		}
	}
}

func explainQueuePlan(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.Query(`EXPLAIN QUERY PLAN `+query, args...)
	if err != nil {
		t.Fatalf("explain queue plan: %v", err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("explain queue plan scan: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain queue plan rows: %v", err)
	}
	return strings.Join(details, " | ")
}

func databaseBytes(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var pages, pageSize int64
	if err := db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	return pages * pageSize
}
