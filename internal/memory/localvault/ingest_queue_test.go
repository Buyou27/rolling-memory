// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func openIngestQueueVault(t *testing.T) (*Vault, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "ingest_queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	vault, err := NewVault(db, []byte("ingest-queue-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	return vault, db
}

func ingestQueueRequest(key, payload string) IngestQueueRequest {
	return IngestQueueRequest{
		RequestKey: key,
		OccurredAt: 1700000000,
		Identity: VaultIdentity{
			AssistantID:   "zcode",
			IncarnationID: "rollout-1",
			Locus:         "Rolling-Memory",
		},
		RawPayload: []byte(payload),
	}
}

func ingestQueueCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_ingest_queue`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestEnqueueIngestQueueBatchAtomicAndDurable(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	if err := vault.BumpPurgeEpoch(context.Background()); err != nil {
		t.Fatal(err)
	}
	requests := []IngestQueueRequest{
		ingestQueueRequest("batch-a", `{"unit":"a"}`),
		ingestQueueRequest("batch-b", `{"unit":"b"}`),
	}
	items, err := vault.EnqueueIngestQueue(context.Background(), requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Status != IngestQueuePending || items[1].Status != IngestQueuePending {
		t.Fatalf("enqueued items = %+v", items)
	}
	for index, item := range items {
		if item.QueueID <= 0 || item.PurgeEpoch != 1 || item.Attempt != 0 || item.LeaseToken != "" || item.LeaseExpiresAtNS != 0 {
			t.Fatalf("item %d = %+v", index, item)
		}
		if item.Identity != requests[index].Identity || !bytes.Equal(item.RawPayload, requests[index].RawPayload) {
			t.Fatalf("item %d lost identity or payload: %+v", index, item)
		}
	}
	dbPath := itemsDBPath(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if count := ingestQueueCount(t, reopened); count != len(requests) {
		t.Fatalf("reopened queue count = %d, want %d", count, len(requests))
	}
	var payload []byte
	if err := reopened.QueryRow(`SELECT raw_payload FROM rm_ingest_queue WHERE request_key = 'batch-b'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, requests[1].RawPayload) {
		t.Fatalf("reopened payload = %q, want %q", payload, requests[1].RawPayload)
	}
}

func TestEnqueueIngestQueueBatchRollback(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	_, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{
		ingestQueueRequest("rollback-a", "one"),
		{RequestKey: "   ", RawPayload: []byte("invalid")},
	})
	if err == nil {
		t.Fatal("expected invalid batch error")
	}
	if count := ingestQueueCount(t, db); count != 0 {
		t.Fatalf("rolled back batch left %d rows", count)
	}

	if _, err := db.Exec(`
		CREATE TRIGGER fail_second_ingest_queue_row
		BEFORE INSERT ON rm_ingest_queue
		WHEN NEW.request_key = 'rollback-trigger-b'
		BEGIN
			SELECT RAISE(ABORT, 'queue fault');
		END`); err != nil {
		t.Fatal(err)
	}
	_, err = vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{
		ingestQueueRequest("rollback-trigger-a", "one"),
		ingestQueueRequest("rollback-trigger-b", "two"),
	})
	if err == nil || !strings.Contains(err.Error(), "queue fault") {
		t.Fatalf("trigger rollback error = %v", err)
	}
	if count := ingestQueueCount(t, db); count != 0 {
		t.Fatalf("failed transaction left %d rows", count)
	}
}

func TestEnqueueIngestQueueIdempotencyAndConflict(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	request := ingestQueueRequest("same-request", "payload")
	first, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{request})
	if err != nil {
		t.Fatal(err)
	}
	again, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{request, request})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 2 || again[0].QueueID != first[0].QueueID || again[1].QueueID != first[0].QueueID {
		t.Fatalf("idempotent rows = first %+v again %+v", first, again)
	}
	if count := ingestQueueCount(t, db); count != 1 {
		t.Fatalf("idempotent enqueue count = %d", count)
	}
	conflict := request
	conflict.RawPayload = []byte("different")
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{
		ingestQueueRequest("new-row-must-rollback", "new"),
		conflict,
	}); !errors.Is(err, ErrIngestQueuePayloadConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	if count := ingestQueueCount(t, db); count != 1 {
		t.Fatalf("conflicting batch count = %d, want 1", count)
	}
}

func TestClaimIngestQueueSingleOwnerAcrossConnections(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ingest_queue_race.sqlite")
	firstDB, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	firstDB.SetMaxOpenConns(1)
	defer firstDB.Close()
	first, err := NewVault(firstDB, []byte("ingest-queue-race-key"))
	if err != nil {
		t.Fatal(err)
	}
	secondDB, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	secondDB.SetMaxOpenConns(1)
	defer secondDB.Close()
	second, err := NewVault(secondDB, []byte("ingest-queue-race-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{
		ingestQueueRequest("single-owner", "payload"),
	}); err != nil {
		t.Fatal(err)
	}
	type claimResult struct {
		item IngestQueueItem
		ok   bool
		err  error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	now := time.Unix(1700000000, 0)
	for _, vault := range []*Vault{first, second} {
		go func(vault *Vault) {
			<-start
			item, ok, err := vault.ClaimIngestQueue(context.Background(), now, time.Minute)
			results <- claimResult{item: item, ok: ok, err: err}
		}(vault)
	}
	close(start)
	claimed := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.ok {
			claimed++
			if result.item.Attempt != 1 || result.item.Status != IngestQueueRunning {
				t.Fatalf("winning claim = %+v", result.item)
			}
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed owners = %d, want 1", claimed)
	}
}

func TestRenewIngestQueueLeaseFencesOwners(t *testing.T) {
	vault, _ := openIngestQueueVault(t)
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{
		ingestQueueRequest("lease-renew", "payload"),
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 123)
	first, claimed, err := vault.ClaimIngestQueue(context.Background(), now, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
	notShortened, err := vault.RenewIngestQueueLease(context.Background(), first.QueueID, first.Attempt, first.LeaseToken, now.Add(10*time.Second), 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if notShortened.LeaseExpiresAtNS != first.LeaseExpiresAtNS {
		t.Fatalf("renew shortened lease: first=%d renewed=%d", first.LeaseExpiresAtNS, notShortened.LeaseExpiresAtNS)
	}
	renewed, err := vault.RenewIngestQueueLease(context.Background(), first.QueueID, first.Attempt, first.LeaseToken, now.Add(30*time.Second), 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.QueueID != first.QueueID || renewed.Attempt != first.Attempt || renewed.LeaseToken != first.LeaseToken || renewed.LeaseExpiresAtNS != now.Add(150*time.Second).UnixNano() {
		t.Fatalf("renewed lease = first %+v renewed %+v", first, renewed)
	}
	if _, claimed, err := vault.ClaimIngestQueue(context.Background(), now.Add(time.Minute), time.Minute); err != nil || claimed {
		t.Fatalf("renewed lease was reclaimed: claimed=%v err=%v", claimed, err)
	}
	if _, err := vault.RenewIngestQueueLease(context.Background(), first.QueueID, first.Attempt, first.LeaseToken, now.Add(150*time.Second), time.Minute); !errors.Is(err, ErrIngestQueueLeaseLost) {
		t.Fatalf("expired owner renewed lease: %v", err)
	}
	second, claimed, err := vault.ClaimIngestQueue(context.Background(), now.Add(150*time.Second), time.Minute)
	if err != nil || !claimed {
		t.Fatalf("reclaim after renewed expiry: claimed=%v err=%v", claimed, err)
	}
	if second.Attempt != first.Attempt+1 || second.LeaseToken == first.LeaseToken {
		t.Fatalf("reclaimed lease = first %+v second %+v", first, second)
	}
	if _, err := vault.RenewIngestQueueLease(context.Background(), first.QueueID, first.Attempt, first.LeaseToken, now.Add(151*time.Second), time.Minute); !errors.Is(err, ErrIngestQueueLeaseLost) {
		t.Fatalf("old owner renew error = %v", err)
	}
	if _, err := vault.FinishIngestQueueCanceled(context.Background(), second.QueueID, second.Attempt, second.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.RenewIngestQueueLease(context.Background(), second.QueueID, second.Attempt, second.LeaseToken, now.Add(152*time.Second), time.Minute); !errors.Is(err, ErrIngestQueueLeaseLost) {
		t.Fatalf("terminal renew error = %v", err)
	}
}

func TestClaimIngestQueueReclaimsExpiredLeaseAndFencesOldOwner(t *testing.T) {
	vault, _ := openIngestQueueVault(t)
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{
		ingestQueueRequest("lease-reclaim", "payload"),
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 123)
	first, claimed, err := vault.ClaimIngestQueue(context.Background(), now, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	if first.Status != IngestQueueRunning || first.Attempt != 1 || first.LeaseToken == "" || first.LeaseExpiresAtNS != now.Add(time.Minute).UnixNano() {
		t.Fatalf("first lease = %+v", first)
	}
	if _, claimed, err := vault.ClaimIngestQueue(context.Background(), now.Add(time.Minute-time.Nanosecond), time.Minute); err != nil || claimed {
		t.Fatalf("live lease was reclaimed: claimed=%v err=%v", claimed, err)
	}
	second, claimed, err := vault.ClaimIngestQueue(context.Background(), now.Add(time.Minute), 2*time.Minute)
	if err != nil || !claimed {
		t.Fatalf("expired claim: claimed=%v err=%v", claimed, err)
	}
	if second.QueueID != first.QueueID || second.Attempt != 2 || second.LeaseToken == first.LeaseToken {
		t.Fatalf("reclaimed lease = first %+v second %+v", first, second)
	}
	if _, err := vault.FinishIngestQueueApplied(context.Background(), first.QueueID, first.Attempt, first.LeaseToken); !errors.Is(err, ErrIngestQueueLeaseLost) {
		t.Fatalf("old lease finish error = %v", err)
	}
	failed, err := vault.FinishIngestQueueFailed(context.Background(), second.QueueID, second.Attempt, second.LeaseToken, "worker failed")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != IngestQueueFailed || failed.ErrorText != "worker failed" || failed.LeaseToken != "" || failed.LeaseExpiresAtNS != 0 {
		t.Fatalf("failed item = %+v", failed)
	}
	if _, claimed, err := vault.ClaimIngestQueue(context.Background(), now.Add(24*time.Hour), time.Minute); err != nil || claimed {
		t.Fatalf("terminal row was reclaimed: claimed=%v err=%v", claimed, err)
	}
}

func TestFinishIngestQueueAppliedRejectsPurgedEpoch(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{
		ingestQueueRequest("purged-before-applied", "payload"),
	}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	before, err := vault.CurrentPurgeEpoch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if before != claim.PurgeEpoch {
		t.Fatalf("claim epoch = %d, live = %d", claim.PurgeEpoch, before)
	}
	if err := vault.SaveLocalPlaintext("purged-doc", 1700000000, "canonical write before purge", nil, IngestResponse{Layer: LayerEpisodic}); err != nil {
		t.Fatal(err)
	}
	if err := vault.BumpPurgeEpoch(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := vault.CurrentPurgeEpoch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("purge epoch = %d, want %d", after, before+1)
	}
	if _, err := vault.FinishIngestQueueApplied(context.Background(), claim.QueueID, claim.Attempt, "wrong-token"); !errors.Is(err, ErrIngestQueueLeaseLost) {
		t.Fatalf("wrong lease after purge error = %v", err)
	}
	if _, err := vault.FinishIngestQueueApplied(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken); !errors.Is(err, ErrIngestQueuePurgeEpochChanged) {
		t.Fatalf("applied after purge error = %v", err)
	}
	var status, token string
	var appliedSeq, leaseExpiresAtNS int64
	if err := db.QueryRow(`SELECT status, lease_token, lease_expires_at_ns, applied_change_seq FROM rm_ingest_queue WHERE queue_id = ?`, claim.QueueID).
		Scan(&status, &token, &leaseExpiresAtNS, &appliedSeq); err != nil {
		t.Fatal(err)
	}
	if status != IngestQueueRunning || token != claim.LeaseToken || leaseExpiresAtNS != claim.LeaseExpiresAtNS || appliedSeq != 0 {
		t.Fatalf("purge rejection mutated row: status=%s token=%s expiry=%d applied=%d", status, token, leaseExpiresAtNS, appliedSeq)
	}
	failed, err := vault.FinishIngestQueueFailed(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken, "purged")
	if err != nil || failed.Status != IngestQueueFailed {
		t.Fatalf("finish failed after purge: item=%+v err=%v", failed, err)
	}
}

func TestFinishIngestQueueTerminalStatesAndAppliedWatermark(t *testing.T) {
	for _, terminal := range []string{IngestQueueApplied, IngestQueueFailed, IngestQueueCanceled} {
		t.Run(terminal, func(t *testing.T) {
			vault, db := openIngestQueueVault(t)
			if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{
				ingestQueueRequest("terminal-"+terminal, terminal),
			}); err != nil {
				t.Fatal(err)
			}
			claim, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
			if err != nil || !ok {
				t.Fatalf("claim: ok=%v err=%v", ok, err)
			}
			if terminal == IngestQueueApplied {
				if err := vault.SaveLocalPlaintext("applied-doc", 1700000000, "applied payload", nil, IngestResponse{Layer: LayerEpisodic}); err != nil {
					t.Fatal(err)
				}
			}
			var item IngestQueueItem
			switch terminal {
			case IngestQueueApplied:
				item, err = vault.FinishIngestQueueApplied(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken)
			case IngestQueueFailed:
				item, err = vault.FinishIngestQueueFailed(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken, "permanent failure")
			case IngestQueueCanceled:
				item, err = vault.FinishIngestQueueCanceled(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken)
			}
			if err != nil {
				t.Fatal(err)
			}
			if item.Status != terminal || item.LeaseToken != "" || item.LeaseExpiresAtNS != 0 {
				t.Fatalf("terminal item = %+v", item)
			}
			if terminal == IngestQueueApplied {
				var changeSeq int64
				if err := db.QueryRow(`SELECT change_seq FROM rm_revision WHERE id = 1`).Scan(&changeSeq); err != nil {
					t.Fatal(err)
				}
				if changeSeq == 0 || item.AppliedChangeSeq != changeSeq {
					t.Fatalf("applied watermark = %d, want %d", item.AppliedChangeSeq, changeSeq)
				}
			} else if item.AppliedChangeSeq != 0 {
				t.Fatalf("%s applied watermark = %d", terminal, item.AppliedChangeSeq)
			}
			if terminal == IngestQueueFailed && item.ErrorText != "permanent failure" {
				t.Fatalf("failed error = %q", item.ErrorText)
			}
			if _, err := vault.FinishIngestQueueCanceled(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken); !errors.Is(err, ErrIngestQueueLeaseLost) {
				t.Fatalf("second terminal write error = %v", err)
			}
			if _, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1800000000, 0), time.Minute); err != nil || ok {
				t.Fatalf("terminal claim: ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestIngestQueueEmptyRollbackAndClosedDB(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	items, err := vault.EnqueueIngestQueue(context.Background(), nil)
	if err != nil || len(items) != 0 {
		t.Fatalf("empty enqueue = %+v, %v", items, err)
	}
	if _, ok, err := vault.ClaimIngestQueue(context.Background(), time.Now(), time.Minute); err != nil || ok {
		t.Fatalf("empty claim: ok=%v err=%v", ok, err)
	}
	if _, ok, err := vault.ClaimIngestQueue(context.Background(), time.Time{}, time.Minute); err == nil || ok {
		t.Fatalf("zero-time claim: ok=%v err=%v", ok, err)
	}
	if _, ok, err := vault.ClaimIngestQueue(context.Background(), time.Now(), 0); err == nil || ok {
		t.Fatalf("zero-duration claim: ok=%v err=%v", ok, err)
	}
	overflowNow := time.Unix(0, int64(^uint64(0)>>1))
	if _, ok, err := vault.ClaimIngestQueue(context.Background(), overflowNow, time.Nanosecond); err == nil || ok {
		t.Fatalf("overflow claim: ok=%v err=%v", ok, err)
	}
	if _, err := vault.FinishIngestQueueApplied(context.Background(), 0, 0, ""); err == nil {
		t.Fatal("invalid finish succeeded")
	}
	if _, err := vault.RenewIngestQueueLease(context.Background(), 0, 0, "", time.Now(), time.Minute); err == nil {
		t.Fatal("invalid renew succeeded")
	}
	if _, err := vault.RenewIngestQueueLease(context.Background(), 1, 1, "token", time.Time{}, time.Minute); err == nil {
		t.Fatal("zero-time renew succeeded")
	}
	if _, err := vault.RenewIngestQueueLease(context.Background(), 1, 1, "token", time.Now(), 0); err == nil {
		t.Fatal("zero-duration renew succeeded")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vault.EnqueueIngestQueue(canceled, []IngestQueueRequest{ingestQueueRequest("canceled", "payload")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled enqueue error = %v", err)
	}
	if count := ingestQueueCount(t, db); count != 0 {
		t.Fatalf("canceled enqueue count = %d", count)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("closed", "payload")}); err == nil {
		t.Fatal("enqueue on closed database succeeded")
	}
	if _, ok, err := vault.ClaimIngestQueue(context.Background(), time.Now(), time.Minute); err == nil || ok {
		t.Fatalf("claim on closed database: ok=%v err=%v", ok, err)
	}
	if _, err := vault.RenewIngestQueueLease(context.Background(), 1, 1, "token", time.Now(), time.Minute); err == nil {
		t.Fatal("renew on closed database succeeded")
	}
	if _, err := vault.CurrentPurgeEpoch(context.Background()); err == nil {
		t.Fatal("purge epoch read on closed database succeeded")
	}
}

func TestIngestQueueStatsReportsDepthByState(t *testing.T) {
	vault, _ := openIngestQueueVault(t)
	requests := []IngestQueueRequest{
		ingestQueueRequest("stats-a", "alpha"),
		ingestQueueRequest("stats-b", "bravo"),
	}
	items, err := vault.EnqueueIngestQueue(context.Background(), requests)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	stats, err := vault.IngestQueueStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantBytes := int64(len(requests[0].RawPayload) + len(requests[1].RawPayload))
	if stats.Pending != 1 || stats.Running != 1 || stats.PendingBytes != int64(len(requests[1].RawPayload)) || wantBytes == 0 {
		t.Fatalf("stats = %+v (want pending=1 running=1 pending_bytes=%d)", stats, len(requests[1].RawPayload))
	}
	if stats.AcceptedID != items[1].QueueID || stats.OldestPendingID != items[1].QueueID || stats.AppliedID != 0 {
		t.Fatalf("stats ids = %+v", stats)
	}
	if _, err := vault.IngestQueueStats(nil); err != nil {
		t.Fatalf("nil context stats: %v", err)
	}
}

func TestEnqueueIngestQueueBoundedRejectsGrowthAndAllowsRetry(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	first := ingestQueueRequest("bounded-a", "alpha")
	const budget = int64(5)
	if _, err := vault.EnqueueIngestQueueBounded(context.Background(), []IngestQueueRequest{first}, &IngestQueueBounds{MaxPendingBytes: budget}); err != nil {
		t.Fatalf("first bounded enqueue: %v", err)
	}
	_, err := vault.EnqueueIngestQueueBounded(context.Background(), []IngestQueueRequest{ingestQueueRequest("bounded-b", "bravo")}, &IngestQueueBounds{MaxPendingBytes: budget})
	if !errors.Is(err, ErrIngestQueueBackpressure) {
		t.Fatalf("over-budget enqueue error = %v", err)
	}
	if count := ingestQueueCount(t, db); count != 1 {
		t.Fatalf("rejected batch left %d rows, want 1", count)
	}
	if _, err := vault.EnqueueIngestQueueBounded(context.Background(), []IngestQueueRequest{first}, &IngestQueueBounds{MaxPendingBytes: budget}); err != nil {
		t.Fatalf("idempotent retry at budget: %v", err)
	}
	if _, err := vault.EnqueueIngestQueueBounded(context.Background(), []IngestQueueRequest{first}, &IngestQueueBounds{}); err == nil {
		t.Fatal("zero byte budget was accepted")
	}
	if _, err := vault.EnqueueIngestQueueBounded(context.Background(), []IngestQueueRequest{ingestQueueRequest("bounded-oversized", "oversized")}, &IngestQueueBounds{MaxPendingBytes: 5}); !errors.Is(err, ErrIngestQueueBackpressure) {
		t.Fatalf("oversized request error = %v", err)
	}
	if count := ingestQueueCount(t, db); count != 1 {
		t.Fatalf("oversized request left %d rows, want 1", count)
	}
}

func TestPruneIngestQueueAppliedKeepsUncoveredRows(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	requests := []IngestQueueRequest{
		ingestQueueRequest("prune-applied", "applied payload"),
		ingestQueueRequest("prune-pending", "pending payload"),
	}
	_, err := vault.EnqueueIngestQueue(context.Background(), requests)
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	prepared := prepareQueuedDoc(t, vault, "prune-applied-doc", "pruned applied text")
	applied, err := vault.ApplyPreparedQueuedIngest(context.Background(), claim, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if applied.AppliedChangeSeq <= 0 {
		t.Fatalf("applied watermark = %d", applied.AppliedChangeSeq)
	}
	if pruned, err := vault.PruneIngestQueueApplied(context.Background()); err != nil || pruned != 0 {
		t.Fatalf("prune before watermark: pruned=%d err=%v", pruned, err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO rm_meta(k, v) VALUES('snapshot_built_change_seq', ?)`, []byte(strconv.FormatInt(applied.AppliedChangeSeq, 10))); err != nil {
		t.Fatal(err)
	}
	if pruned, err := vault.PruneIngestQueueApplied(context.Background()); err != nil || pruned != 1 {
		t.Fatalf("prune at watermark: pruned=%d err=%v", pruned, err)
	}
	var remaining string
	if err := db.QueryRow(`SELECT request_key FROM rm_ingest_queue`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != requests[1].RequestKey {
		t.Fatalf("remaining queue row = %q, want %q", remaining, requests[1].RequestKey)
	}
	if pruned, err := vault.PruneIngestQueueApplied(context.Background()); err != nil || pruned != 0 {
		t.Fatalf("second prune: pruned=%d err=%v", pruned, err)
	}
}

func TestDeferIngestQueueAttemptSchedulesRetryAndFences(t *testing.T) {
	vault, _ := openIngestQueueVault(t)
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("defer-retry", "payload")}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	claim, ok, err := vault.ClaimIngestQueue(context.Background(), now, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	retryAt := now.Add(30 * time.Second)
	deferred, err := vault.DeferIngestQueueAttempt(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken, retryAt, "transient fault")
	if err != nil {
		t.Fatal(err)
	}
	if deferred.Status != IngestQueuePending || deferred.Attempt != claim.Attempt || deferred.LeaseToken != "" ||
		deferred.LeaseExpiresAtNS != 0 || deferred.NextAttemptAtNS != retryAt.UnixNano() || deferred.ErrorText != "transient fault" {
		t.Fatalf("deferred item = %+v", deferred)
	}
	if _, ok, err := vault.ClaimIngestQueue(context.Background(), retryAt.Add(-time.Nanosecond), time.Minute); err != nil || ok {
		t.Fatalf("claimed before retry time: ok=%v err=%v", ok, err)
	}
	reclaimed, ok, err := vault.ClaimIngestQueue(context.Background(), retryAt, time.Minute)
	if err != nil || !ok || reclaimed.Attempt != claim.Attempt+1 {
		t.Fatalf("claim at retry time: ok=%v attempt=%d err=%v", ok, reclaimed.Attempt, err)
	}
	if _, err := vault.DeferIngestQueueAttempt(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken, retryAt.Add(time.Minute), "stale"); !errors.Is(err, ErrIngestQueueLeaseLost) {
		t.Fatalf("stale defer error = %v", err)
	}
	if _, err := vault.DeferIngestQueueAttempt(context.Background(), claim.QueueID, reclaimed.Attempt, reclaimed.LeaseToken, time.Time{}, "bad time"); err == nil {
		t.Fatal("zero retry time was accepted")
	}
}

func TestIngestQueueSchemaAddsDeferredRetryColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-queue.sqlite")
	raw, err := sql.Open("sqlite", vaultTestDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE rm_ingest_queue (
		queue_id INTEGER PRIMARY KEY AUTOINCREMENT,
		request_key TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'pending',
		attempt INTEGER NOT NULL DEFAULT 0,
		lease_token TEXT NOT NULL DEFAULT '',
		lease_expires_at_ns INTEGER NOT NULL DEFAULT 0,
		purge_epoch INTEGER NOT NULL DEFAULT 0,
		applied_change_seq INTEGER NOT NULL DEFAULT 0,
		occurred_at INTEGER NOT NULL DEFAULT 0,
		assistant_id TEXT NOT NULL DEFAULT '',
		incarnation_id TEXT NOT NULL DEFAULT '',
		locus TEXT NOT NULL DEFAULT '',
		raw_payload BLOB NOT NULL,
		error_text TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE INDEX idx_rm_ingest_queue_pending ON rm_ingest_queue(status, queue_id)`,
		`CREATE INDEX idx_rm_ingest_queue_claim ON rm_ingest_queue(status, lease_expires_at_ns, queue_id)`,
	} {
		if _, err := raw.Exec(ddl); err != nil {
			_ = raw.Close()
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", vaultTestDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	vault, err := NewVault(db, []byte("legacy-queue-key"))
	if err != nil {
		t.Fatalf("open legacy queue vault: %v", err)
	}
	var pendingIndexSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_rm_ingest_queue_pending'`).Scan(&pendingIndexSQL); err != nil {
		t.Fatalf("legacy pending index missing: %v", err)
	}
	if strings.Contains(strings.ToUpper(pendingIndexSQL), "WHERE") {
		t.Fatalf("queue pending index must stay a full definition: %s", pendingIndexSQL)
	}
	for _, want := range []struct {
		name  string
		count int
	}{
		{"idx_rm_ingest_queue_running", 1},
		{"idx_rm_ingest_queue_claim", 0},
	} {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, want.name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want.count {
			t.Fatalf("index %s count = %d, want %d", want.name, got, want.count)
		}
	}
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("legacy-column", "payload")}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim on migrated queue: ok=%v err=%v", ok, err)
	}
	if _, err := vault.DeferIngestQueueAttempt(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken, time.Unix(1700000060, 0), "retry"); err != nil {
		t.Fatalf("defer on migrated queue: %v", err)
	}
}

// TestCanonicalOpenKeepsQueueSchemaStable guards the O(1) open path: the queue
// index probe must not DROP or CREATE anything, so PRAGMA schema_version cannot
// move when an existing vault is opened again. A per-open index rebuild shows
// up here as a version bump.
func TestCanonicalOpenKeepsQueueSchemaStable(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("schema-stable", "payload")}); err != nil {
		t.Fatal(err)
	}
	readVersion := func() int64 {
		t.Helper()
		var version int64
		if err := db.QueryRow(`PRAGMA schema_version`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		return version
	}
	before := readVersion()
	for reopen := 1; reopen <= 2; reopen++ {
		if _, err := NewVault(db, []byte("ingest-queue-test-key")); err != nil {
			t.Fatalf("reopen %d: %v", reopen, err)
		}
		if after := readVersion(); after != before {
			t.Fatalf("reopen %d rewrote the queue schema: version %d -> %d", reopen, before, after)
		}
	}
}

func overridePublishedSnapshotReady(t *testing.T, ready bool) {
	t.Helper()
	previous := publishedSnapshotReady
	publishedSnapshotReady = func(*Vault) bool { return ready }
	t.Cleanup(func() { publishedSnapshotReady = previous })
}

func applyOneQueuedItem(t *testing.T, vault *Vault) IngestQueueItem {
	t.Helper()
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("visibility", "payload")}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	prepared := prepareQueuedDoc(t, vault, "visibility-doc", "visibility text")
	applied, err := vault.ApplyPreparedQueuedIngest(context.Background(), claim, prepared)
	if err != nil {
		t.Fatal(err)
	}
	return applied
}

func TestIngestQueueVisibilityDerivesBaselineAndPurge(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	visibility, err := vault.IngestQueueVisibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if visibility.BuiltKnown || visibility.VisibleThroughSeq != 0 || visibility.Covers(1) {
		t.Fatalf("visibility before baseline = %+v", visibility)
	}
	applied := applyOneQueuedItem(t, vault)
	overridePublishedSnapshotReady(t, true)
	if _, err := db.Exec(`INSERT OR REPLACE INTO rm_meta(k, v) VALUES('snapshot_built_change_seq', ?)`, []byte(strconv.FormatInt(applied.AppliedChangeSeq, 10))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO rm_meta(k, v) VALUES('snapshot_built_purge_epoch', ?)`, []byte("0")); err != nil {
		t.Fatal(err)
	}
	visibility, err = vault.IngestQueueVisibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !visibility.PurgeConsistent || visibility.VisibleThroughSeq != visibility.ChangeSeq || !visibility.Covers(applied.AppliedChangeSeq) {
		t.Fatalf("visibility with baseline = %+v", visibility)
	}
	stats, err := vault.IngestQueueStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.VisibleApplied != 1 || stats.VisibleThroughSeq != visibility.ChangeSeq {
		t.Fatalf("visibility stats = %+v", stats)
	}
	if err := vault.BumpPurgeEpoch(context.Background()); err != nil {
		t.Fatal(err)
	}
	visibility, err = vault.IngestQueueVisibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if visibility.PurgeConsistent || visibility.VisibleThroughSeq != 0 {
		t.Fatalf("visibility after purge = %+v", visibility)
	}
	stats, err = vault.IngestQueueStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.VisibleApplied != 0 {
		t.Fatalf("visible applied after purge = %d", stats.VisibleApplied)
	}
}

func TestIngestQueueVisibilityStopsAtBaselineWhenDeltaExceedsCap(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	overridePublishedSnapshotReady(t, true)
	if _, err := db.Exec(`INSERT OR REPLACE INTO rm_meta(k, v) VALUES('snapshot_built_change_seq', ?)`, []byte("0")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO rm_meta(k, v) VALUES('snapshot_built_purge_epoch', ?)`, []byte("0")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`WITH RECURSIVE c(value) AS (SELECT 1 UNION ALL SELECT value + 1 FROM c WHERE value < ?)
		INSERT INTO rm_change(unit_id, op) SELECT 'bulk-' || value, 'upsert' FROM c`, maxCSRDeltaDocs+1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE rm_revision SET change_seq = (SELECT MAX(seq) FROM rm_change) WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	visibility, err := vault.IngestQueueVisibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if visibility.DeltaRows <= int64(maxCSRDeltaDocs) || visibility.DeltaComplete {
		t.Fatalf("delta rows = %d complete=%v", visibility.DeltaRows, visibility.DeltaComplete)
	}
	if visibility.VisibleThroughSeq != visibility.BuiltChangeSeq {
		t.Fatalf("visible through = %d, want baseline %d", visibility.VisibleThroughSeq, visibility.BuiltChangeSeq)
	}
}

func TestPruneIngestQueueReleasesRetiredCanceledGenerations(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("canceled-retired", "retired")}); err != nil {
		t.Fatal(err)
	}
	if err := vault.BumpPurgeEpoch(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim retired: ok=%v err=%v", ok, err)
	}
	if _, err := vault.FinishIngestQueueCanceled(context.Background(), claim.QueueID, claim.Attempt, claim.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("canceled-current", "current")}); err != nil {
		t.Fatal(err)
	}
	current, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim current: ok=%v err=%v", ok, err)
	}
	if _, err := vault.FinishIngestQueueCanceled(context.Background(), current.QueueID, current.Attempt, current.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if pruned, err := vault.PruneIngestQueueApplied(context.Background()); err != nil || pruned != 1 {
		t.Fatalf("prune canceled generations: pruned=%d err=%v", pruned, err)
	}
	var remaining string
	if err := db.QueryRow(`SELECT request_key FROM rm_ingest_queue`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != "canceled-current" {
		t.Fatalf("remaining canceled row = %q, want current generation", remaining)
	}
}

func itemsDBPath(t *testing.T, db *sql.DB) string {
	t.Helper()
	var path string
	if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	return path
}

func countUnitRows(t *testing.T, db *sql.DB, table, unitID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE unit_id = ?`, unitID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func prepareQueuedDoc(t *testing.T, vault *Vault, docID, text string) PreparedCanonicalIngest {
	t.Helper()
	worker := NewLocalClient(vault.CloneWithSharedSlotCache())
	prepared, err := worker.PrepareQueuedRemember(context.Background(), docID, text, 1700000000, LayerEpisodic)
	if err != nil {
		t.Fatalf("prepare queued remember: %v", err)
	}
	return prepared
}

func TestApplyPreparedQueuedIngestRollsBackCanonicalRowsWithTerminalFailure(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("atomic-rollback", "payload")}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER fail_ingest_queue_terminal
		AFTER UPDATE OF status ON rm_ingest_queue
		WHEN NEW.status = 'applied'
		BEGIN
			SELECT RAISE(ABORT, 'queue terminal fault');
		END`); err != nil {
		t.Fatal(err)
	}
	prepared := prepareQueuedDoc(t, vault, "atomic-rollback-doc", "atomic rollback text")
	if _, err := vault.ApplyPreparedQueuedIngest(context.Background(), claim, prepared); err == nil || !strings.Contains(err.Error(), "queue terminal fault") {
		t.Fatalf("apply error = %v, want terminal fault", err)
	}
	for _, table := range []string{"rm_docs", "rm_terms", "rm_unit_meta"} {
		if got := countUnitRows(t, db, table, "atomic-rollback-doc"); got != 0 {
			t.Fatalf("%s rows after rollback = %d, want 0", table, got)
		}
	}
	var status, leaseToken string
	var attempt, appliedSeq int64
	if err := db.QueryRow(`SELECT status, attempt, lease_token, applied_change_seq FROM rm_ingest_queue WHERE queue_id = ?`, claim.QueueID).
		Scan(&status, &attempt, &leaseToken, &appliedSeq); err != nil {
		t.Fatal(err)
	}
	if status != IngestQueueRunning || attempt != claim.Attempt || leaseToken != claim.LeaseToken || appliedSeq != 0 {
		t.Fatalf("queue row after rollback = status=%s attempt=%d token=%s applied=%d", status, attempt, leaseToken, appliedSeq)
	}
}

func TestApplyPreparedQueuedIngestRejectsStalePurgeAndLease(t *testing.T) {
	t.Run("purge epoch", func(t *testing.T) {
		vault, db := openIngestQueueVault(t)
		if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("atomic-purge", "payload")}); err != nil {
			t.Fatal(err)
		}
		claim, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim: ok=%v err=%v", ok, err)
		}
		if err := vault.BumpPurgeEpoch(context.Background()); err != nil {
			t.Fatal(err)
		}
		prepared := prepareQueuedDoc(t, vault, "atomic-purge-doc", "purged text")
		if _, err := vault.ApplyPreparedQueuedIngest(context.Background(), claim, prepared); !errors.Is(err, ErrIngestQueuePurgeEpochChanged) {
			t.Fatalf("purged apply error = %v", err)
		}
		for _, table := range []string{"rm_docs", "rm_terms", "rm_unit_meta"} {
			if got := countUnitRows(t, db, table, "atomic-purge-doc"); got != 0 {
				t.Fatalf("%s rows after purge rejection = %d, want 0", table, got)
			}
		}
	})
	t.Run("reclaimed lease", func(t *testing.T) {
		vault, db := openIngestQueueVault(t)
		if _, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{ingestQueueRequest("atomic-lease", "payload")}); err != nil {
			t.Fatal(err)
		}
		now := time.Unix(1700000000, 0)
		first, ok, err := vault.ClaimIngestQueue(context.Background(), now, time.Minute)
		if err != nil || !ok {
			t.Fatalf("first claim: ok=%v err=%v", ok, err)
		}
		if _, ok, err := vault.ClaimIngestQueue(context.Background(), now.Add(time.Minute), time.Minute); err != nil || !ok {
			t.Fatalf("reclaim: ok=%v err=%v", ok, err)
		}
		prepared := prepareQueuedDoc(t, vault, "atomic-lease-doc", "stale lease text")
		if _, err := vault.ApplyPreparedQueuedIngest(context.Background(), first, prepared); !errors.Is(err, ErrIngestQueueLeaseLost) {
			t.Fatalf("stale lease apply error = %v", err)
		}
		for _, table := range []string{"rm_docs", "rm_terms", "rm_unit_meta"} {
			if got := countUnitRows(t, db, table, "atomic-lease-doc"); got != 0 {
				t.Fatalf("%s rows after fence rejection = %d, want 0", table, got)
			}
		}
	})
}
