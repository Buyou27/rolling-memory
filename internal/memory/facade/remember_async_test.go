// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

func rememberProcessOptions(limit int) facade.IngestQueueProcessOptions {
	return facade.IngestQueueProcessOptions{
		Limit:             limit,
		LeaseDuration:     time.Minute,
		HeartbeatInterval: 20 * time.Second,
		RetryBackoff:      10 * time.Second,
	}
}

func rememberQueueBounds() localvault.IngestQueueBounds {
	return localvault.IngestQueueBounds{MaxPendingBytes: 8 << 20}
}

type canonicalRememberRow struct {
	UnitID          string
	OccurredAt      int64
	Layer           string
	TermCount       int
	RawText         string
	SentencesJSON   string
	KeptIndicesJSON string
	AssistantID     string
	IncarnationID   string
	Locus           string
	Terms           int
	Occurrences     int
}

func readCanonicalRememberRow(t *testing.T, memory *facade.Memory, unitID string) canonicalRememberRow {
	t.Helper()
	var row canonicalRememberRow
	if err := memory.Store.DB().QueryRow(`
		SELECT d.unit_id, d.occurred_at, d.layer, d.term_count, d.raw_text,
		       m.sentences_json, m.kept_indices_json,
		       m.assistant_id, m.incarnation_id, m.locus
		FROM rm_docs d JOIN rm_unit_meta m ON m.unit_id = d.unit_id
		WHERE d.unit_id = ?`, unitID).Scan(
		&row.UnitID, &row.OccurredAt, &row.Layer, &row.TermCount, &row.RawText,
		&row.SentencesJSON, &row.KeptIndicesJSON,
		&row.AssistantID, &row.IncarnationID, &row.Locus,
	); err != nil {
		t.Fatalf("read canonical remember row %s: %v", unitID, err)
	}
	if err := memory.Store.DB().QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(occurrences), 0)
		FROM rm_terms WHERE unit_id = ?`, unitID).Scan(&row.Terms, &row.Occurrences); err != nil {
		t.Fatalf("read canonical remember terms %s: %v", unitID, err)
	}
	return row
}

func queueCount(t *testing.T, memory *facade.Memory) int {
	t.Helper()
	var count int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_ingest_queue`).Scan(&count); err != nil {
		t.Fatalf("count ingest queue: %v", err)
	}
	return count
}

func canonicalCount(t *testing.T, memory *facade.Memory) int {
	t.Helper()
	var count int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&count); err != nil {
		t.Fatalf("count canonical rows: %v", err)
	}
	return count
}

func TestRememberAsyncDryRetryAndSynchronousParity(t *testing.T) {
	t.Setenv("ROLLING_MEMORY_ASSISTANT_ID", "assistant-original")
	t.Setenv("ROLLING_MEMORY_INCARNATION_ID", "incarnation-original")
	t.Setenv("ROLLING_MEMORY_LOCUS", "Rolling-Memory")
	asyncMemory := newMemory(t)
	syncMemory := newMemory(t)
	content := "[thinking] inspect the queue\n[tool_use Bash] {}\n[tool_result] hidden\n\nDurable async remembers preserve one canonical unit."
	request := facade.RememberRequest{
		Content:    &content,
		Session:    "async-session",
		OccurredAt: int64(1700000123),
	}

	dryRequest := request
	dryRequest.Dry = true
	asyncDry, err := asyncMemory.RememberAsync(dryRequest, rememberQueueBounds())
	if err != nil {
		t.Fatalf("async dry: %v", err)
	}
	syncDry, err := syncMemory.Remember(dryRequest)
	if err != nil {
		t.Fatalf("sync dry: %v", err)
	}
	if !reflect.DeepEqual(asyncDry, syncDry) {
		t.Fatalf("dry receipt drifted: async=%+v sync=%+v", asyncDry, syncDry)
	}
	var queueTables int
	if err := asyncMemory.Store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'rm_ingest_queue'`).Scan(&queueTables); err != nil {
		t.Fatal(err)
	}
	if queueTables != 0 {
		t.Fatalf("dry remember initialized %d queue tables", queueTables)
	}

	receipt, err := asyncMemory.RememberAsync(request, rememberQueueBounds())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if receipt["accepted"] != true || receipt["durable"] != true || receipt["applied"] != false {
		t.Fatalf("enqueue receipt = %+v", receipt)
	}
	for _, forbidden := range []string{"visible", "compacted"} {
		if _, ok := receipt[forbidden]; ok {
			t.Fatalf("enqueue receipt claimed %s: %+v", forbidden, receipt)
		}
	}
	if got := canonicalCount(t, asyncMemory); got != 0 {
		t.Fatalf("enqueue wrote %d canonical rows", got)
	}
	queued := receipt["queued"].([]any)
	if len(queued) != 1 {
		t.Fatalf("queued units = %+v", queued)
	}
	unitID := queued[0].(map[string]any)["id"].(string)
	queueID := queued[0].(map[string]any)["queue_id"].(int64)
	var raw []byte
	var storedQueueID, occurredAt int64
	var status, requestKey, assistantID, incarnationID, locus string
	if err := asyncMemory.Store.DB().QueryRow(`
		SELECT queue_id, request_key, status, occurred_at,
		       assistant_id, incarnation_id, locus, raw_payload
		FROM rm_ingest_queue`).Scan(
		&storedQueueID, &requestKey, &status, &occurredAt,
		&assistantID, &incarnationID, &locus, &raw,
	); err != nil {
		t.Fatal(err)
	}
	if storedQueueID != queueID || status != localvault.IngestQueuePending || occurredAt != 1700000123 {
		t.Fatalf("queued row = id=%d key=%q status=%q occurred=%d", storedQueueID, requestKey, status, occurredAt)
	}
	if assistantID != "assistant-original" || incarnationID != "incarnation-original" || locus != "Rolling-Memory" {
		t.Fatalf("queued identity = (%q,%q,%q)", assistantID, incarnationID, locus)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode queued payload: %v", err)
	}
	if len(payload) != 3 || payload["version"] != float64(1) || payload["doc_id"] != unitID {
		t.Fatalf("queued payload schema = %+v", payload)
	}
	plaintext, _ := payload["plaintext"].(string)
	if !strings.Contains(plaintext, "Durable async remembers") {
		t.Fatalf("queued plaintext lost content: %q", plaintext)
	}
	for _, forbidden := range []string{"scope", "session", "occurred_at"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("queued payload redundantly stored %s: %+v", forbidden, payload)
		}
	}

	syncReceipt, err := syncMemory.Remember(request)
	if err != nil {
		t.Fatalf("sync remember: %v", err)
	}
	syncUnitID := syncReceipt["written"].([]any)[0].(map[string]any)["id"].(string)
	if unitID != syncUnitID {
		t.Fatalf("async doc id = %s, sync doc id = %s", unitID, syncUnitID)
	}

	retry, err := asyncMemory.RememberAsync(request, rememberQueueBounds())
	if err != nil {
		t.Fatalf("retry enqueue: %v", err)
	}
	if queueCount(t, asyncMemory) != 1 {
		t.Fatalf("retry created another queue row")
	}
	retryQueueID := retry["queued"].([]any)[0].(map[string]any)["queue_id"].(int64)
	if retryQueueID != queueID {
		t.Fatalf("retry queue id = %d, want %d", retryQueueID, queueID)
	}

	processed, err := asyncMemory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1))
	if err != nil || processed != 1 {
		t.Fatalf("process: processed=%d err=%v", processed, err)
	}
	postApply, err := asyncMemory.RememberAsync(request, rememberQueueBounds())
	if err != nil {
		t.Fatalf("post-apply retry: %v", err)
	}
	if postApply["applied"] != true {
		t.Fatalf("post-apply receipt = %+v", postApply)
	}

	asyncRow := readCanonicalRememberRow(t, asyncMemory, unitID)
	syncRow := readCanonicalRememberRow(t, syncMemory, syncUnitID)
	if !reflect.DeepEqual(asyncRow, syncRow) {
		t.Fatalf("canonical rows differ:\nasync=%+v\nsync=%+v", asyncRow, syncRow)
	}
}

func TestRememberAsyncCollapsesDuplicateChunks(t *testing.T) {
	memory := newMemory(t)
	sentence := strings.Repeat("alpha ", 4500) + "."
	content := sentence + sentence
	if tokenzr.Count(sentence) > tokenzr.DefaultChunkTokens || tokenzr.Count(content) <= tokenzr.DefaultChunkTokens {
		t.Fatalf("invalid duplicate-chunk fixture: sentence=%d content=%d", tokenzr.Count(sentence), tokenzr.Count(content))
	}
	pieces := tokenzr.ChunkBySentences(content, tokenzr.DefaultChunkTokens)
	if len(pieces) != 2 || pieces[0] != pieces[1] {
		t.Fatalf("fixture did not produce two identical chunks: count=%d", len(pieces))
	}
	request := facade.RememberRequest{Content: &content, Session: "duplicate", OccurredAt: int64(1700000200)}
	dryRequest := request
	dryRequest.Dry = true
	dry, err := memory.RememberAsync(dryRequest, rememberQueueBounds())
	if err != nil {
		t.Fatal(err)
	}
	if dry["chunks"] != 2 {
		t.Fatalf("dry chunks = %v, want 2", dry["chunks"])
	}
	receipt, err := memory.RememberAsync(request, rememberQueueBounds())
	if err != nil {
		t.Fatal(err)
	}
	if got := receipt["trace"].(map[string]any)["chunks"]; got != 1 {
		t.Fatalf("collapsed chunks = %v, want 1", got)
	}
	if got := len(receipt["queued"].([]any)); got != 1 || queueCount(t, memory) != 1 {
		t.Fatalf("queued duplicate chunks = %d rows=%d", got, queueCount(t, memory))
	}
	if canonicalCount(t, memory) != 0 {
		t.Fatal("enqueue wrote a canonical row")
	}
}

func TestProcessRememberQueuePreservesIdentityHonorsCancelAndLimit(t *testing.T) {
	t.Setenv("ROLLING_MEMORY_ASSISTANT_ID", "assistant-at-enqueue")
	t.Setenv("ROLLING_MEMORY_INCARNATION_ID", "incarnation-at-enqueue")
	t.Setenv("ROLLING_MEMORY_LOCUS", "locus-at-enqueue")
	memory := newMemory(t)
	unitIDs := make([]string, 0, 3)
	for _, content := range []string{"queue limit alpha", "queue limit beta", "queue limit gamma"} {
		request := facade.RememberRequest{Content: &content, Session: "limit", OccurredAt: int64(1700000300)}
		receipt, err := memory.RememberAsync(request, rememberQueueBounds())
		if err != nil {
			t.Fatal(err)
		}
		unitIDs = append(unitIDs, receipt["queued"].([]any)[0].(map[string]any)["id"].(string))
	}
	t.Setenv("ROLLING_MEMORY_ASSISTANT_ID", "assistant-after-enqueue")
	t.Setenv("ROLLING_MEMORY_INCARNATION_ID", "incarnation-after-enqueue")
	t.Setenv("ROLLING_MEMORY_LOCUS", "locus-after-enqueue")

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if processed, err := memory.ProcessRememberQueue(canceled, rememberProcessOptions(3)); processed != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled process: processed=%d err=%v", processed, err)
	}
	var pending int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_ingest_queue WHERE status = 'pending'`).Scan(&pending); err != nil || pending != 3 {
		t.Fatalf("pending after cancel = %d err=%v", pending, err)
	}

	if processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1)); err != nil || processed != 1 {
		t.Fatalf("limit one: processed=%d err=%v", processed, err)
	}
	var applied int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_ingest_queue WHERE status = 'applied'`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("applied after limit one = %d err=%v", applied, err)
	}
	if processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(2)); err != nil || processed != 2 {
		t.Fatalf("limit two: processed=%d err=%v", processed, err)
	}
	if canonicalCount(t, memory) != 3 {
		t.Fatalf("canonical rows = %d, want 3", canonicalCount(t, memory))
	}
	for _, unitID := range unitIDs {
		row := readCanonicalRememberRow(t, memory, unitID)
		if row.AssistantID != "assistant-at-enqueue" || row.IncarnationID != "incarnation-at-enqueue" || row.Locus != "locus-at-enqueue" {
			t.Fatalf("%s identity = (%q,%q,%q)", unitID, row.AssistantID, row.IncarnationID, row.Locus)
		}
	}
}

func TestProcessRememberQueuePoisonsAndPurgedItemsAreTerminal(t *testing.T) {
	t.Run("poison", func(t *testing.T) {
		memory := newMemory(t)
		client, err := memory.LocalVault()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Vault.EnqueueIngestQueue(context.Background(), []localvault.IngestQueueRequest{{
			RequestKey: "remember:v1:rm_poison",
			OccurredAt: 1700000400,
			RawPayload: []byte(`{"version":2,"doc_id":"rm_poison","plaintext":"poison"}`),
		}}); err != nil {
			t.Fatal(err)
		}
		processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1))
		if err != nil || processed != 1 {
			t.Fatalf("process poison: processed=%d err=%v", processed, err)
		}
		var status, errorText string
		if err := memory.Store.DB().QueryRow(`SELECT status, error_text FROM rm_ingest_queue`).Scan(&status, &errorText); err != nil {
			t.Fatal(err)
		}
		if status != localvault.IngestQueueFailed || errorText == "" || canonicalCount(t, memory) != 0 {
			t.Fatalf("poison result: status=%q error=%q canonical=%d", status, errorText, canonicalCount(t, memory))
		}
	})

	t.Run("purged", func(t *testing.T) {
		memory := newMemory(t)
		content := "old epoch item"
		if _, err := memory.RememberAsync(facade.RememberRequest{Content: &content, Session: "purge", OccurredAt: int64(1700000401)}, rememberQueueBounds()); err != nil {
			t.Fatal(err)
		}
		client, err := memory.LocalVault()
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Vault.BumpPurgeEpoch(context.Background()); err != nil {
			t.Fatal(err)
		}
		processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1))
		if err != nil || processed != 1 {
			t.Fatalf("process purged: processed=%d err=%v", processed, err)
		}
		var status string
		if err := memory.Store.DB().QueryRow(`SELECT status FROM rm_ingest_queue`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != localvault.IngestQueueCanceled || canonicalCount(t, memory) != 0 {
			t.Fatalf("purged result: status=%q canonical=%d", status, canonicalCount(t, memory))
		}
	})
}

func TestProcessRememberQueueDefersApplyFailure(t *testing.T) {
	memory := newMemory(t)
	content := "apply failure must remain retryable"
	if _, err := memory.RememberAsync(facade.RememberRequest{Content: &content, Session: "retry", OccurredAt: int64(1700000500)}, rememberQueueBounds()); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Store.DB().Exec(`
		CREATE TRIGGER fail_remember_queue_apply
		BEFORE INSERT ON rm_docs
		BEGIN
			SELECT RAISE(ABORT, 'remember apply fault');
		END`); err != nil {
		t.Fatal(err)
	}
	processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1))
	if processed != 0 || err == nil || !strings.Contains(err.Error(), "remember apply fault") {
		t.Fatalf("failed apply: processed=%d err=%v", processed, err)
	}
	var status, leaseToken, errorText string
	var attempt, nextAttempt int64
	if err := memory.Store.DB().QueryRow(`SELECT status, attempt, lease_token, next_attempt_at_ns, error_text FROM rm_ingest_queue`).
		Scan(&status, &attempt, &leaseToken, &nextAttempt, &errorText); err != nil {
		t.Fatal(err)
	}
	if status != localvault.IngestQueuePending || attempt != 1 || leaseToken != "" || nextAttempt == 0 || errorText == "" {
		t.Fatalf("deferred row: status=%q attempt=%d token=%q next=%d error=%q", status, attempt, leaseToken, nextAttempt, errorText)
	}
	if processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1)); err != nil || processed != 0 {
		t.Fatalf("deferred item reclaimed early: processed=%d err=%v", processed, err)
	}
	if _, err := memory.Store.DB().Exec(`DROP TRIGGER fail_remember_queue_apply`); err != nil {
		t.Fatal(err)
	}
	now := clock.Now()
	restore := clock.SetForTest(now.Add(time.Minute), 1_000_000_001)
	defer restore()
	processed, err = memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1))
	if err != nil || processed != 1 {
		t.Fatalf("retry after backoff: processed=%d err=%v", processed, err)
	}
	if err := memory.Store.DB().QueryRow(`SELECT status, attempt FROM rm_ingest_queue`).Scan(&status, &attempt); err != nil {
		t.Fatal(err)
	}
	if status != localvault.IngestQueueApplied || attempt != 2 || canonicalCount(t, memory) != 1 {
		t.Fatalf("recovered row: status=%q attempt=%d canonical=%d", status, attempt, canonicalCount(t, memory))
	}
}

func TestProcessRememberQueueFailureDoesNotBlockLaterItems(t *testing.T) {
	memory := newMemory(t)
	for _, content := range []string{"blocked queued text", "healthy queued text"} {
		if _, err := memory.RememberAsync(facade.RememberRequest{Content: &content, Session: "head-of-line", OccurredAt: int64(1700000550)}, rememberQueueBounds()); err != nil {
			t.Fatal(err)
		}
	}
	var raw []byte
	if err := memory.Store.DB().QueryRow(`SELECT raw_payload FROM rm_ingest_queue ORDER BY queue_id LIMIT 1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DocID string `json:"doc_id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Store.DB().Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_first_remember_queue_apply
		BEFORE INSERT ON rm_docs
		WHEN NEW.unit_id = '%s'
		BEGIN
			SELECT RAISE(ABORT, 'first item fault');
		END`, payload.DocID)); err != nil {
		t.Fatal(err)
	}
	processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(2))
	if processed != 1 || err == nil || !strings.Contains(err.Error(), "first item fault") {
		t.Fatalf("head-of-line slice: processed=%d err=%v", processed, err)
	}
	var pending, applied int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_ingest_queue WHERE status = 'pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_ingest_queue WHERE status = 'applied'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if pending != 1 || applied != 1 || canonicalCount(t, memory) != 1 {
		t.Fatalf("after slice: pending=%d applied=%d canonical=%d", pending, applied, canonicalCount(t, memory))
	}
	if _, err := memory.Store.DB().Exec(`DROP TRIGGER fail_first_remember_queue_apply`); err != nil {
		t.Fatal(err)
	}
	now := clock.Now()
	restore := clock.SetForTest(now.Add(time.Minute), 1_000_000_004)
	defer restore()
	if processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1)); err != nil || processed != 1 {
		t.Fatalf("deferred retry: processed=%d err=%v", processed, err)
	}
	if canonicalCount(t, memory) != 2 {
		t.Fatalf("canonical rows after retry = %d, want 2", canonicalCount(t, memory))
	}
}

func TestProcessRememberQueueAtLeastOnceReplayIsIdempotent(t *testing.T) {
	memory := newMemory(t)
	content := "at least once replay keeps one canonical unit"
	receipt, err := memory.RememberAsync(facade.RememberRequest{Content: &content, Session: "replay", OccurredAt: int64(1700000600)}, rememberQueueBounds())
	if err != nil {
		t.Fatal(err)
	}
	unitID := receipt["queued"].([]any)[0].(map[string]any)["id"].(string)
	client, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	now := clock.Now()
	claim, ok, err := client.Vault.ClaimIngestQueue(context.Background(), now, time.Minute)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	var payload struct {
		Version   int    `json:"version"`
		DocID     string `json:"doc_id"`
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal(claim.RawPayload, &payload); err != nil {
		t.Fatal(err)
	}
	workerVault := client.Vault.CloneWithSharedSlotCache()
	worker := localvault.NewLocalClient(workerVault)
	worker.SetIdentity(claim.Identity.AssistantID, claim.Identity.IncarnationID, claim.Identity.Locus)
	if _, err := worker.Remember(context.Background(), payload.DocID, payload.Plaintext, claim.OccurredAt); err != nil {
		t.Fatal(err)
	}
	if canonicalCount(t, memory) != 1 {
		t.Fatalf("first apply rows = %d", canonicalCount(t, memory))
	}

	restore := clock.SetForTest(now.Add(time.Minute), 1_000_000_002)
	defer restore()
	processed, err := memory.ProcessRememberQueue(context.Background(), rememberProcessOptions(1))
	if err != nil || processed != 1 {
		t.Fatalf("replay process: processed=%d err=%v", processed, err)
	}
	if canonicalCount(t, memory) != 1 {
		t.Fatalf("replay duplicated canonical rows: %d", canonicalCount(t, memory))
	}
	var status string
	var attempt int64
	if err := memory.Store.DB().QueryRow(`SELECT status, attempt FROM rm_ingest_queue`).Scan(&status, &attempt); err != nil {
		t.Fatal(err)
	}
	if status != localvault.IngestQueueApplied || attempt != 2 {
		t.Fatalf("replayed queue row: status=%q attempt=%d", status, attempt)
	}
	if got := readCanonicalRememberRow(t, memory, unitID).RawText; got != content {
		t.Fatalf("replayed text = %q", got)
	}
}

func TestProcessRememberQueueRejectsDegenerateOptions(t *testing.T) {
	memory := newMemory(t)
	valid := rememberProcessOptions(1)
	tests := []struct {
		name string
		opts facade.IngestQueueProcessOptions
	}{
		{name: "zero limit", opts: facade.IngestQueueProcessOptions{LeaseDuration: valid.LeaseDuration, HeartbeatInterval: valid.HeartbeatInterval, RetryBackoff: valid.RetryBackoff}},
		{name: "zero lease", opts: facade.IngestQueueProcessOptions{Limit: 1, HeartbeatInterval: valid.HeartbeatInterval, RetryBackoff: valid.RetryBackoff}},
		{name: "zero heartbeat", opts: facade.IngestQueueProcessOptions{Limit: 1, LeaseDuration: valid.LeaseDuration, RetryBackoff: valid.RetryBackoff}},
		{name: "heartbeat equals lease", opts: facade.IngestQueueProcessOptions{Limit: 1, LeaseDuration: time.Minute, HeartbeatInterval: time.Minute, RetryBackoff: valid.RetryBackoff}},
		{name: "heartbeat exceeds lease", opts: facade.IngestQueueProcessOptions{Limit: 1, LeaseDuration: time.Minute, HeartbeatInterval: 2 * time.Minute, RetryBackoff: valid.RetryBackoff}},
		{name: "zero retry backoff", opts: facade.IngestQueueProcessOptions{Limit: 1, LeaseDuration: valid.LeaseDuration, HeartbeatInterval: valid.HeartbeatInterval}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if processed, err := memory.ProcessRememberQueue(context.Background(), test.opts); err == nil || processed != 0 {
				t.Fatalf("degenerate options: processed=%d err=%v", processed, err)
			}
		})
	}
}

func TestRememberAsyncEnforcesDeclaredQueueBudget(t *testing.T) {
	memory := newMemory(t)
	content := "bounded async remember"
	request := facade.RememberRequest{Content: &content, Session: "bounded", OccurredAt: int64(1700000700)}
	if _, err := memory.RememberAsync(request, localvault.IngestQueueBounds{MaxPendingBytes: 4}); !errors.Is(err, localvault.ErrIngestQueueBackpressure) {
		t.Fatalf("over-budget enqueue error = %v", err)
	}
	if got := queueCount(t, memory); got != 0 {
		t.Fatalf("rejected enqueue left %d queue rows", got)
	}
	if got := canonicalCount(t, memory); got != 0 {
		t.Fatalf("rejected enqueue left %d canonical rows", got)
	}
}
