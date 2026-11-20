// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	IngestQueuePending  = "pending"
	IngestQueueRunning  = "running"
	IngestQueueApplied  = "applied"
	IngestQueueFailed   = "failed"
	IngestQueueCanceled = "canceled"
)

var (
	// ErrIngestQueueLeaseLost reports that a queue item is no longer owned by the
	// supplied attempt and lease token. The row may have been reclaimed or ended.
	ErrIngestQueueLeaseLost = errors.New("localvault: ingest queue lease lost")
	// ErrIngestQueuePayloadConflict reports request-key reuse with a different
	// durable request. Idempotent retries must carry the same persisted fields.
	ErrIngestQueuePayloadConflict = errors.New("localvault: ingest queue request key conflict")
	// ErrIngestQueuePurgeEpochChanged reports that a destructive purge advanced
	// after enqueue, so the old request cannot claim application in the live epoch.
	ErrIngestQueuePurgeEpochChanged = errors.New("localvault: ingest queue purge epoch changed")
	// ErrIngestQueueBackpressure reports that pending queue payload bytes would
	// exceed the caller's declared budget. The whole batch is rolled back.
	ErrIngestQueueBackpressure = errors.New("localvault: ingest queue backpressure")
)

// IngestQueueBounds is the caller-declared capacity budget for one enqueue.
// MaxPendingBytes counts payload bytes of pending and running rows, which is the
// unapplied debt a worker still has to drain.
type IngestQueueBounds struct {
	MaxPendingBytes int64
}

// IngestQueueRequest is one opaque L0 ingest request. RawPayload is preserved
// byte-for-byte; interpretation belongs to the future worker and existing
// ingest adapters rather than this queue primitive.
type IngestQueueRequest struct {
	RequestKey string
	OccurredAt int64
	Identity   VaultIdentity
	RawPayload []byte
}

// IngestQueueItem is one durable queue row. Attempt and LeaseToken form the
// fencing identity held by a worker after ClaimIngestQueue.
type IngestQueueItem struct {
	QueueID          int64
	RequestKey       string
	Status           string
	Attempt          int64
	LeaseToken       string
	LeaseExpiresAtNS int64
	NextAttemptAtNS  int64
	PurgeEpoch       int64
	AppliedChangeSeq int64
	OccurredAt       int64
	Identity         VaultIdentity
	RawPayload       []byte
	ErrorText        string
}

// EnqueueIngestQueue persists the whole batch in one immediate transaction.
// Returning nil means COMMIT completed; a repeated request key is accepted only
// when every persisted request field is identical.
func (v *Vault) EnqueueIngestQueue(ctx context.Context, requests []IngestQueueRequest) ([]IngestQueueItem, error) {
	return v.EnqueueIngestQueueBounded(ctx, requests, nil)
}

// EnqueueIngestQueueBounded is EnqueueIngestQueue with an explicit pending-byte
// budget. Exceeding the budget rolls the whole batch back; idempotent retries of
// already queued requests do not grow the debt and therefore pass.
func (v *Vault) EnqueueIngestQueueBounded(ctx context.Context, requests []IngestQueueRequest, bounds *IngestQueueBounds) ([]IngestQueueItem, error) {
	if v == nil || v.db == nil {
		return nil, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return []IngestQueueItem{}, nil
	}
	if bounds != nil && bounds.MaxPendingBytes <= 0 {
		return nil, errors.New("localvault: ingest queue byte budget must be positive")
	}
	prepared := make([]IngestQueueRequest, len(requests))
	seen := make(map[string]IngestQueueRequest, len(requests))
	for index, request := range requests {
		request.RequestKey = strings.TrimSpace(request.RequestKey)
		request.Identity = normalizedVaultIdentity(request.Identity)
		if request.RawPayload == nil {
			request.RawPayload = []byte{}
		} else {
			request.RawPayload = append([]byte(nil), request.RawPayload...)
		}
		if request.RequestKey == "" {
			return nil, fmt.Errorf("localvault: ingest queue item %d requires a request key", index)
		}
		if previous, ok := seen[request.RequestKey]; ok && !sameIngestQueueRequest(previous, request) {
			return nil, fmt.Errorf("%w: %s", ErrIngestQueuePayloadConflict, request.RequestKey)
		}
		seen[request.RequestKey] = request
		prepared[index] = request
	}

	if bounds != nil {
		var incoming int64
		for _, request := range prepared {
			incoming += int64(len(request.RawPayload))
		}
		if incoming > bounds.MaxPendingBytes {
			return nil, fmt.Errorf("%w: incoming bytes %d exceed limit %d", ErrIngestQueueBackpressure, incoming, bounds.MaxPendingBytes)
		}
	}

	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var purgeEpoch int64
	if err := tx.QueryRowContext(ctx, `SELECT purge_epoch FROM rm_revision WHERE id = 1`).Scan(&purgeEpoch); err != nil {
		return nil, err
	}
	items := make([]IngestQueueItem, len(prepared))
	for index, request := range prepared {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO rm_ingest_queue(
				request_key, purge_epoch, occurred_at, assistant_id,
				incarnation_id, locus, raw_payload
			) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(request_key) DO NOTHING`,
			request.RequestKey, purgeEpoch, request.OccurredAt,
			request.Identity.AssistantID, request.Identity.IncarnationID,
			request.Identity.Locus, request.RawPayload,
		); err != nil {
			return nil, err
		}
		item, err := ingestQueueItemByRequestKey(ctx, tx, request.RequestKey)
		if err != nil {
			return nil, err
		}
		if !ingestQueueItemMatchesRequest(item, request) {
			return nil, fmt.Errorf("%w: %s", ErrIngestQueuePayloadConflict, request.RequestKey)
		}
		items[index] = item
	}
	if bounds != nil {
		var backlog int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(LENGTH(raw_payload)), 0) FROM rm_ingest_queue WHERE status IN ('pending','running')`).Scan(&backlog); err != nil {
			return nil, err
		}
		if backlog > bounds.MaxPendingBytes {
			return nil, fmt.Errorf("%w: pending bytes %d exceed limit %d", ErrIngestQueueBackpressure, backlog, bounds.MaxPendingBytes)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for index := range items {
		items[index].RawPayload = append([]byte(nil), items[index].RawPayload...)
	}
	return items, nil
}

// ClaimIngestQueue leases the oldest pending or expired-running item. The
// vault connection is configured for BEGIN IMMEDIATE, so selection and claim
// are serialized with every competing writer.
func (v *Vault) ClaimIngestQueue(ctx context.Context, now time.Time, leaseDuration time.Duration) (IngestQueueItem, bool, error) {
	if v == nil || v.db == nil {
		return IngestQueueItem{}, false, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return IngestQueueItem{}, false, err
	}
	if leaseDuration <= 0 {
		return IngestQueueItem{}, false, errors.New("localvault: ingest queue lease duration must be positive")
	}
	nowNS, leaseExpiresAtNS, err := ingestLeaseWindow(now, leaseDuration)
	if err != nil {
		return IngestQueueItem{}, false, err
	}

	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return IngestQueueItem{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Pending work is consumed in insertion order: the (status, queue_id) index
	// yields the oldest eligible row without scanning the backlog, and deferred
	// retries are few. Running rows are bounded by the worker count, so their
	// expired-lease minimum is cheap either way.
	var pendingID, runningID int64
	err = tx.QueryRowContext(ctx, `
		SELECT queue_id FROM rm_ingest_queue
		WHERE status = 'pending' AND next_attempt_at_ns <= ?
		ORDER BY queue_id LIMIT 1`, nowNS).Scan(&pendingID)
	if errors.Is(err, sql.ErrNoRows) {
		pendingID, err = 0, nil
	}
	if err != nil {
		return IngestQueueItem{}, false, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(queue_id), 0) FROM rm_ingest_queue
		WHERE status = 'running' AND lease_expires_at_ns <= ?`, nowNS).Scan(&runningID); err != nil {
		return IngestQueueItem{}, false, err
	}
	queueID := pendingID
	if queueID == 0 || (runningID != 0 && runningID < queueID) {
		queueID = runningID
	}
	if queueID == 0 {
		return IngestQueueItem{}, false, nil
	}
	leaseToken, err := newIngestQueueLeaseToken()
	if err != nil {
		return IngestQueueItem{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE rm_ingest_queue
		SET status = 'running',
		    attempt = attempt + 1,
		    lease_token = ?,
		    lease_expires_at_ns = ?,
		    error_text = ''
		WHERE queue_id = ?
		  AND ((status = 'pending' AND next_attempt_at_ns <= ?)
		       OR (status = 'running' AND lease_expires_at_ns <= ?))`,
		leaseToken, leaseExpiresAtNS, queueID, nowNS, nowNS,
	)
	if err != nil {
		return IngestQueueItem{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return IngestQueueItem{}, false, err
	}
	if affected != 1 {
		return IngestQueueItem{}, false, ErrIngestQueueLeaseLost
	}
	item, err := ingestQueueItemByID(ctx, tx, queueID)
	if err != nil {
		return IngestQueueItem{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return IngestQueueItem{}, false, err
	}
	item.RawPayload = append([]byte(nil), item.RawPayload...)
	return item, true, nil
}

// RenewIngestQueueLease extends the current running lease without changing its
// fencing identity. A reclaimed or terminal row rejects the old owner.
func (v *Vault) RenewIngestQueueLease(ctx context.Context, queueID, attempt int64, leaseToken string, now time.Time, leaseDuration time.Duration) (IngestQueueItem, error) {
	if v == nil || v.db == nil {
		return IngestQueueItem{}, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return IngestQueueItem{}, err
	}
	leaseToken = strings.TrimSpace(leaseToken)
	if queueID <= 0 || attempt <= 0 || leaseToken == "" {
		return IngestQueueItem{}, errors.New("localvault: ingest queue renew requires queue id, attempt, and lease token")
	}
	if leaseDuration <= 0 {
		return IngestQueueItem{}, errors.New("localvault: ingest queue lease duration must be positive")
	}
	nowNS, leaseExpiresAtNS, err := ingestLeaseWindow(now, leaseDuration)
	if err != nil {
		return IngestQueueItem{}, err
	}

	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return IngestQueueItem{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE rm_ingest_queue
		SET lease_expires_at_ns = CASE
		        WHEN lease_expires_at_ns > ? THEN lease_expires_at_ns
		        ELSE ?
		    END
		WHERE queue_id = ?
		  AND status = 'running'
		  AND attempt = ?
		  AND lease_token = ?
		  AND lease_expires_at_ns > ?`,
		leaseExpiresAtNS, leaseExpiresAtNS, queueID, attempt, leaseToken, nowNS,
	)
	if err != nil {
		return IngestQueueItem{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return IngestQueueItem{}, err
	}
	if affected != 1 {
		return IngestQueueItem{}, ErrIngestQueueLeaseLost
	}
	item, err := ingestQueueItemByID(ctx, tx, queueID)
	if err != nil {
		return IngestQueueItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return IngestQueueItem{}, err
	}
	item.RawPayload = append([]byte(nil), item.RawPayload...)
	return item, nil
}

// DeferIngestQueueAttempt releases a leased item back to pending with a fenced
// retry time, so one failing request cannot block newer items in the queue. The
// attempt count is kept for the next claim's fencing identity.
func (v *Vault) DeferIngestQueueAttempt(ctx context.Context, queueID, attempt int64, leaseToken string, retryAt time.Time, errorText string) (IngestQueueItem, error) {
	if v == nil || v.db == nil {
		return IngestQueueItem{}, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return IngestQueueItem{}, err
	}
	leaseToken = strings.TrimSpace(leaseToken)
	if queueID <= 0 || attempt <= 0 || leaseToken == "" {
		return IngestQueueItem{}, errors.New("localvault: ingest queue defer requires queue id, attempt, and lease token")
	}
	retryAtNS := retryAt.UnixNano()
	if retryAt.IsZero() || retryAtNS <= 0 {
		return IngestQueueItem{}, errors.New("localvault: ingest queue defer requires a valid retry time")
	}

	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return IngestQueueItem{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE rm_ingest_queue
		SET status = 'pending',
		    lease_token = '',
		    lease_expires_at_ns = 0,
		    next_attempt_at_ns = ?,
		    error_text = ?
		WHERE queue_id = ?
		  AND status = 'running'
		  AND attempt = ?
		  AND lease_token = ?`,
		retryAtNS, strings.TrimSpace(errorText), queueID, attempt, leaseToken,
	)
	if err != nil {
		return IngestQueueItem{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return IngestQueueItem{}, err
	}
	if affected != 1 {
		return IngestQueueItem{}, ErrIngestQueueLeaseLost
	}
	item, err := ingestQueueItemByID(ctx, tx, queueID)
	if err != nil {
		return IngestQueueItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return IngestQueueItem{}, err
	}
	item.RawPayload = append([]byte(nil), item.RawPayload...)
	return item, nil
}

// CurrentPurgeEpoch returns the live destructive-purge generation.
func (v *Vault) CurrentPurgeEpoch(ctx context.Context) (int64, error) {
	if v == nil || v.db == nil {
		return 0, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var purgeEpoch int64
	if err := v.db.QueryRowContext(ctx, `SELECT purge_epoch FROM rm_revision WHERE id = 1`).Scan(&purgeEpoch); err != nil {
		return 0, err
	}
	return purgeEpoch, nil
}

// IngestQueueStats is the durable backlog of one vault. It reads only counters,
// so a status probe never claims work.
type IngestQueueStats struct {
	Pending           int64
	Running           int64
	Applied           int64
	Failed            int64
	Canceled          int64
	PendingBytes      int64
	VisibleApplied    int64
	VisibleThroughSeq int64
	OldestPendingID   int64
	AcceptedID        int64
	AppliedID         int64
}

// IngestQueueVisibility derives what the published generation can already
// serve: the baseline watermark plus the change-log rows a reader replays,
// gated by the destructive-purge epoch the baseline was built from.
type IngestQueueVisibility struct {
	ChangeSeq         int64
	BuiltChangeSeq    int64
	BuiltKnown        bool
	DeltaRows         int64
	DeltaComplete     bool
	PurgeEpoch        int64
	BuiltPurgeEpoch   int64
	PurgeConsistent   bool
	VisibleThroughSeq int64
}

// Covers reports whether a change sequence is already visible to readers.
func (visibility IngestQueueVisibility) Covers(seq int64) bool {
	return seq > 0 && seq <= visibility.VisibleThroughSeq
}

// publishedSnapshotReady is the file probe used by visibility derivation; tests
// substitute it instead of materialising a full segment.
var publishedSnapshotReady = func(v *Vault) bool { return v.PublishedSnapshotReady() }

// IngestQueueVisibility reports the derived visible watermark of this vault.
func (v *Vault) IngestQueueVisibility(ctx context.Context) (IngestQueueVisibility, error) {
	var visibility IngestQueueVisibility
	if v == nil || v.db == nil {
		return visibility, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return visibility, err
	}
	if err := v.db.QueryRowContext(ctx, `SELECT change_seq, purge_epoch FROM rm_revision WHERE id = 1`).
		Scan(&visibility.ChangeSeq, &visibility.PurgeEpoch); err != nil {
		return visibility, err
	}
	built, builtKnown := v.snapshotBuiltChangeSeq(ctx)
	builtEpoch, builtEpochKnown := v.snapshotBuiltPurgeEpoch(ctx)
	visibility.BuiltChangeSeq = built
	visibility.BuiltKnown = builtKnown
	visibility.BuiltPurgeEpoch = builtEpoch
	visibility.PurgeConsistent = builtKnown && builtEpochKnown && builtEpoch == visibility.PurgeEpoch
	if !visibility.PurgeConsistent || !publishedSnapshotReady(v) {
		return visibility, nil
	}
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rm_change WHERE seq > ?`, built).Scan(&visibility.DeltaRows); err != nil {
		return visibility, err
	}
	visibility.DeltaComplete = visibility.DeltaRows <= int64(maxCSRDeltaDocs)
	visibility.VisibleThroughSeq = visibility.ChangeSeq
	if !visibility.DeltaComplete {
		visibility.VisibleThroughSeq = built
	}
	return visibility, nil
}

// IngestQueueStats reports queue depth by state.
func (v *Vault) IngestQueueStats(ctx context.Context) (IngestQueueStats, error) {
	var stats IngestQueueStats
	if v == nil || v.db == nil {
		return stats, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	rows, err := v.db.QueryContext(ctx, `
		SELECT status, COUNT(*), COALESCE(SUM(LENGTH(raw_payload)), 0)
		FROM rm_ingest_queue
		GROUP BY status`)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count, bytes int64
		if err := rows.Scan(&status, &count, &bytes); err != nil {
			return stats, err
		}
		switch status {
		case IngestQueuePending:
			stats.Pending = count
			stats.PendingBytes = bytes
		case IngestQueueRunning:
			stats.Running = count
		case IngestQueueApplied:
			stats.Applied = count
		case IngestQueueFailed:
			stats.Failed = count
		case IngestQueueCanceled:
			stats.Canceled = count
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}
	if err := v.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(queue_id), 0) FROM rm_ingest_queue`).Scan(&stats.AcceptedID); err != nil {
		return stats, err
	}
	if err := v.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(queue_id), 0) FROM rm_ingest_queue WHERE status = 'applied'`).Scan(&stats.AppliedID); err != nil {
		return stats, err
	}
	if err := v.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(queue_id), 0) FROM rm_ingest_queue WHERE status = 'pending'`).Scan(&stats.OldestPendingID); err != nil {
		return stats, err
	}
	visibility, err := v.IngestQueueVisibility(ctx)
	if err != nil {
		return stats, err
	}
	stats.VisibleThroughSeq = visibility.VisibleThroughSeq
	if err := v.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM rm_ingest_queue
		WHERE status = 'applied' AND applied_change_seq > 0 AND applied_change_seq <= ?`,
		stats.VisibleThroughSeq).Scan(&stats.VisibleApplied); err != nil {
		return stats, err
	}
	return stats, nil
}

// PruneIngestQueueApplied releases terminal rows whose evidence is already
// retired: applied rows covered by the published baseline, and canceled rows
// from a purge generation older than the live one. Pending, running and failed
// rows are always kept, so pruning can never erase debt or failure evidence.
func (v *Vault) PruneIngestQueueApplied(ctx context.Context) (int64, error) {
	if v == nil || v.db == nil {
		return 0, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	built, builtKnown := v.snapshotBuiltChangeSeq(ctx)
	if !builtKnown {
		built = 0
	}
	result, err := v.db.ExecContext(ctx, `
		DELETE FROM rm_ingest_queue
		WHERE (status = 'applied'
		       AND applied_change_seq > 0
		       AND applied_change_seq <= ?)
		   OR (status = 'canceled'
		       AND purge_epoch < (SELECT purge_epoch FROM rm_revision WHERE id = 1))`, built)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// FinishIngestQueueApplied marks the leased request written to the canonical
// store and captures that write-side change sequence in the same fenced update.
func (v *Vault) FinishIngestQueueApplied(ctx context.Context, queueID, attempt int64, leaseToken string) (IngestQueueItem, error) {
	return v.finishIngestQueue(ctx, queueID, attempt, leaseToken, IngestQueueApplied, "")
}

// FinishIngestQueueFailed marks the leased item failed.
func (v *Vault) FinishIngestQueueFailed(ctx context.Context, queueID, attempt int64, leaseToken, errorText string) (IngestQueueItem, error) {
	return v.finishIngestQueue(ctx, queueID, attempt, leaseToken, IngestQueueFailed, errorText)
}

// FinishIngestQueueCanceled marks the leased item canceled.
func (v *Vault) FinishIngestQueueCanceled(ctx context.Context, queueID, attempt int64, leaseToken string) (IngestQueueItem, error) {
	return v.finishIngestQueue(ctx, queueID, attempt, leaseToken, IngestQueueCanceled, "")
}

func (v *Vault) finishIngestQueue(ctx context.Context, queueID, attempt int64, leaseToken, status, errorText string) (IngestQueueItem, error) {
	if v == nil || v.db == nil {
		return IngestQueueItem{}, errors.New("localvault: nil Vault db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return IngestQueueItem{}, err
	}
	leaseToken = strings.TrimSpace(leaseToken)
	if queueID <= 0 || attempt <= 0 || leaseToken == "" {
		return IngestQueueItem{}, errors.New("localvault: ingest queue finish requires queue id, attempt, and lease token")
	}
	if status != IngestQueueApplied && status != IngestQueueFailed && status != IngestQueueCanceled {
		return IngestQueueItem{}, fmt.Errorf("localvault: invalid ingest queue terminal status %q", status)
	}

	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return IngestQueueItem{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE rm_ingest_queue
		SET status = ?,
		    lease_token = '',
		    lease_expires_at_ns = 0,
		    applied_change_seq = CASE
		        WHEN ? = 'applied' THEN COALESCE((SELECT change_seq FROM rm_revision WHERE id = 1), 0)
		        ELSE 0
		    END,
		    error_text = CASE WHEN ? = 'failed' THEN ? ELSE '' END
		WHERE queue_id = ?
		  AND status = 'running'
		  AND attempt = ?
		  AND lease_token = ?
		  AND (? <> 'applied' OR purge_epoch = (SELECT purge_epoch FROM rm_revision WHERE id = 1))`,
		status, status, status, errorText, queueID, attempt, leaseToken, status,
	)
	if err != nil {
		return IngestQueueItem{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return IngestQueueItem{}, err
	}
	if affected != 1 {
		if status == IngestQueueApplied {
			var storedStatus, storedToken string
			var storedAttempt, storedEpoch, liveEpoch int64
			err := tx.QueryRowContext(ctx, `
				SELECT q.status, q.attempt, q.lease_token, q.purge_epoch, r.purge_epoch
				FROM rm_ingest_queue q CROSS JOIN rm_revision r
				WHERE q.queue_id = ? AND r.id = 1`, queueID).
				Scan(&storedStatus, &storedAttempt, &storedToken, &storedEpoch, &liveEpoch)
			if err == nil && storedStatus == IngestQueueRunning && storedAttempt == attempt && storedToken == leaseToken && storedEpoch != liveEpoch {
				return IngestQueueItem{}, ErrIngestQueuePurgeEpochChanged
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return IngestQueueItem{}, err
			}
		}
		return IngestQueueItem{}, ErrIngestQueueLeaseLost
	}
	item, err := ingestQueueItemByID(ctx, tx, queueID)
	if err != nil {
		return IngestQueueItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return IngestQueueItem{}, err
	}
	item.RawPayload = append([]byte(nil), item.RawPayload...)
	return item, nil
}

func ingestQueueItemByRequestKey(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, requestKey string) (IngestQueueItem, error) {
	return scanIngestQueueItem(query.QueryRowContext(ctx, ingestQueueSelect+` WHERE request_key = ?`, requestKey))
}

func ingestQueueItemByID(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, queueID int64) (IngestQueueItem, error) {
	return scanIngestQueueItem(query.QueryRowContext(ctx, ingestQueueSelect+` WHERE queue_id = ?`, queueID))
}

const ingestQueueSelect = `
	SELECT queue_id, request_key, status, attempt, lease_token,
	       lease_expires_at_ns, next_attempt_at_ns, purge_epoch, applied_change_seq, occurred_at,
	       assistant_id, incarnation_id, locus, raw_payload, error_text
	FROM rm_ingest_queue`

func scanIngestQueueItem(row *sql.Row) (IngestQueueItem, error) {
	var item IngestQueueItem
	err := row.Scan(
		&item.QueueID,
		&item.RequestKey,
		&item.Status,
		&item.Attempt,
		&item.LeaseToken,
		&item.LeaseExpiresAtNS,
		&item.NextAttemptAtNS,
		&item.PurgeEpoch,
		&item.AppliedChangeSeq,
		&item.OccurredAt,
		&item.Identity.AssistantID,
		&item.Identity.IncarnationID,
		&item.Identity.Locus,
		&item.RawPayload,
		&item.ErrorText,
	)
	if err != nil {
		return IngestQueueItem{}, err
	}
	item.RawPayload = append([]byte(nil), item.RawPayload...)
	return item, nil
}

func normalizedVaultIdentity(identity VaultIdentity) VaultIdentity {
	return VaultIdentity{
		AssistantID:   strings.TrimSpace(identity.AssistantID),
		IncarnationID: strings.TrimSpace(identity.IncarnationID),
		Locus:         strings.TrimSpace(identity.Locus),
	}
}

func sameIngestQueueRequest(left, right IngestQueueRequest) bool {
	return left.RequestKey == right.RequestKey &&
		left.OccurredAt == right.OccurredAt &&
		left.Identity == right.Identity &&
		bytes.Equal(left.RawPayload, right.RawPayload)
}

func ingestQueueItemMatchesRequest(item IngestQueueItem, request IngestQueueRequest) bool {
	return item.RequestKey == request.RequestKey &&
		item.OccurredAt == request.OccurredAt &&
		item.Identity == request.Identity &&
		bytes.Equal(item.RawPayload, request.RawPayload)
}

func ingestLeaseWindow(now time.Time, leaseDuration time.Duration) (int64, int64, error) {
	if now.IsZero() {
		return 0, 0, errors.New("localvault: ingest queue claim requires a non-zero time")
	}
	nowNS := now.UnixNano()
	expires := now.Add(leaseDuration)
	leaseExpiresAtNS := expires.UnixNano()
	if !expires.After(now) || leaseExpiresAtNS <= nowNS {
		return 0, 0, errors.New("localvault: ingest queue lease expiry overflow")
	}
	return nowNS, leaseExpiresAtNS, nil
}

func newIngestQueueLeaseToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
