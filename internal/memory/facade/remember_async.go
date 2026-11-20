// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/ids"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

const (
	rememberQueuePayloadVersion = 1
	rememberQueueRequestPrefix  = "remember:v1:"
)

// rememberQueuePayload is the only persisted facade payload schema. Identity
// and occurred_at use their dedicated queue columns; scope and session are not
// repeated because the bound vault and content-addressed doc id already fix them.
type rememberQueuePayload struct {
	Version   int    `json:"version"`
	DocID     string `json:"doc_id"`
	Plaintext string `json:"plaintext"`
}

type preparedRememberUnit struct {
	DocID      string
	Plaintext  string
	ChunkIndex int
}

type preparedRemember struct {
	Scope      string
	Session    string
	OccurredAt int64
	ChunkCount int
	Units      []preparedRememberUnit
}

func (m *Memory) prepareRemember(req RememberRequest) (preparedRemember, error) {
	scope := m.Config.Scope
	if req.Scope != nil && strings.TrimSpace(*req.Scope) != "" {
		scope = strings.TrimSpace(*req.Scope)
	}
	if err := m.checkLocalVaultScope(scope); err != nil {
		return preparedRemember{}, err
	}
	session := strings.TrimSpace(req.Session)
	if session == "" {
		session = "default"
	}
	var textParts []string
	if req.Content != nil && strings.TrimSpace(*req.Content) != "" {
		textParts = append(textParts, ids.StripInjected(strings.TrimSpace(*req.Content)))
	} else if req.SourceText != nil && strings.TrimSpace(*req.SourceText) != "" {
		textParts = append(textParts, ids.StripInjected(strings.TrimSpace(*req.SourceText)))
	}
	if len(textParts) == 0 && req.Assertion != nil {
		textParts = append(textParts, ids.StableJSON(req.Assertion))
	}
	if len(textParts) == 0 && len(req.Assertions) > 0 {
		textParts = append(textParts, ids.StableJSON(req.Assertions))
	}
	text := strings.Join(textParts, "\n")
	if strings.TrimSpace(text) == "" {
		return preparedRemember{}, fmt.Errorf("localvault remember requires non-empty content")
	}
	chunks := tokenzr.ChunkBySentences(text, tokenzr.DefaultChunkTokens)
	now := clock.Unix()
	plan := preparedRemember{
		Scope:      scope,
		Session:    session,
		OccurredAt: ids.ParseTime(req.OccurredAt, ids.ParseTimeOptions{TimeRef: &now, Default: now}),
		ChunkCount: len(chunks),
		Units:      make([]preparedRememberUnit, 0, len(chunks)),
	}
	seen := make(map[string]struct{}, len(chunks))
	for index, chunk := range chunks {
		docID := "rm_" + ids.SHA256Hex(scope + "\x00" + session + "\x00" + chunk)[:24]
		if _, duplicate := seen[docID]; duplicate {
			continue
		}
		seen[docID] = struct{}{}
		plan.Units = append(plan.Units, preparedRememberUnit{
			DocID:      docID,
			Plaintext:  chunk,
			ChunkIndex: index,
		})
	}
	if len(plan.Units) == 0 {
		return preparedRemember{}, fmt.Errorf("localvault remember produced no unit")
	}
	return plan, nil
}

func (p preparedRemember) dryReceipt() map[string]any {
	return map[string]any{
		"ok":        true,
		"dry":       true,
		"scope":     p.Scope,
		"extractor": localvault.EngineName,
		"chunks":    p.ChunkCount,
	}
}

// RememberAsync canonicalizes one remember request exactly like Remember, then
// commits the complete unit batch to the durable ingest queue without applying
// any canonical document writes on the caller's path. The caller must declare
// its pending-byte budget, so an async producer cannot grow the queue without a
// drain guarantee.
func (m *Memory) RememberAsync(req RememberRequest, bounds localvault.IngestQueueBounds) (map[string]any, error) {
	started := clock.MonotonicMS()
	plan, err := m.prepareRemember(req)
	if err != nil {
		return nil, err
	}
	if req.Dry {
		return plan.dryReceipt(), nil
	}
	client, err := m.LocalVault()
	if err != nil {
		return nil, err
	}
	if client == nil || client.Vault == nil {
		return nil, errors.New("remember queue requires an open local vault")
	}
	identity := client.Vault.Identity()
	requests := make([]localvault.IngestQueueRequest, len(plan.Units))
	for index, unit := range plan.Units {
		payload, err := json.Marshal(rememberQueuePayload{
			Version:   rememberQueuePayloadVersion,
			DocID:     unit.DocID,
			Plaintext: unit.Plaintext,
		})
		if err != nil {
			return nil, fmt.Errorf("remember queue payload: %w", err)
		}
		requests[index] = localvault.IngestQueueRequest{
			RequestKey: rememberQueueRequestKey(unit.DocID),
			OccurredAt: plan.OccurredAt,
			Identity:   identity,
			RawPayload: payload,
		}
	}
	items, err := client.Vault.EnqueueIngestQueueBounded(context.Background(), requests, &bounds)
	if err != nil {
		return nil, err
	}
	queued := make([]any, len(items))
	applied := len(items) > 0
	for index, item := range items {
		itemApplied := item.Status == localvault.IngestQueueApplied
		applied = applied && itemApplied
		queued[index] = map[string]any{
			"id":          plan.Units[index].DocID,
			"queue_id":    item.QueueID,
			"request_key": item.RequestKey,
			"status":      item.Status,
			"applied":     itemApplied,
			"chunk_index": plan.Units[index].ChunkIndex,
		}
	}
	return map[string]any{
		"ok":        true,
		"dry":       false,
		"scope":     plan.Scope,
		"extractor": localvault.EngineName,
		"accepted":  true,
		"durable":   true,
		"applied":   applied,
		"queued":    queued,
		"trace": map[string]any{
			"hash":   plan.Units[0].DocID,
			"chunks": len(plan.Units),
		},
		"meta": map[string]any{
			"engine":     localvault.EngineName,
			"chunks":     len(plan.Units),
			"elapsed_ms": int(clock.MonotonicMS() - started),
		},
	}, nil
}

func rememberQueueRequestKey(docID string) string {
	return rememberQueueRequestPrefix + docID
}

func decodeRememberQueuePayload(raw []byte) (rememberQueuePayload, error) {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return rememberQueuePayload{}, errors.New("remember queue payload is empty or invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload rememberQueuePayload
	if err := decoder.Decode(&payload); err != nil {
		return rememberQueuePayload{}, fmt.Errorf("decode remember queue payload: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return rememberQueuePayload{}, errors.New("remember queue payload has trailing data")
		}
		return rememberQueuePayload{}, fmt.Errorf("decode remember queue payload trailing data: %w", err)
	}
	if payload.Version != rememberQueuePayloadVersion {
		return rememberQueuePayload{}, fmt.Errorf("unsupported remember queue payload version %d", payload.Version)
	}
	if payload.DocID == "" || payload.DocID != strings.TrimSpace(payload.DocID) {
		return rememberQueuePayload{}, errors.New("remember queue payload requires a canonical doc id")
	}
	if strings.TrimSpace(payload.Plaintext) == "" {
		return rememberQueuePayload{}, errors.New("remember queue payload requires plaintext")
	}
	return payload, nil
}

// IngestQueueProcessOptions defines one bounded worker slice. HeartbeatInterval
// and RetryBackoff are supplied explicitly so the caller owns the lease safety
// margin and the retry cadence required by its deployment rather than relying
// on hidden timing ratios.
type IngestQueueProcessOptions struct {
	Limit             int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RetryBackoff      time.Duration
}

func (opts IngestQueueProcessOptions) validate() error {
	if opts.Limit <= 0 {
		return errors.New("remember queue process limit must be positive")
	}
	if opts.LeaseDuration <= 0 {
		return errors.New("remember queue lease duration must be positive")
	}
	if opts.HeartbeatInterval <= 0 {
		return errors.New("remember queue heartbeat interval must be positive")
	}
	if opts.HeartbeatInterval >= opts.LeaseDuration {
		return errors.New("remember queue heartbeat interval must be shorter than the lease duration")
	}
	if opts.RetryBackoff <= 0 {
		return errors.New("remember queue retry backoff must be positive")
	}
	return nil
}

// IngestQueueStats reports durable queue depth without claiming work.
func (m *Memory) IngestQueueStats(ctx context.Context) (localvault.IngestQueueStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := m.LocalVault()
	if err != nil {
		return localvault.IngestQueueStats{}, err
	}
	if client == nil || client.Vault == nil {
		return localvault.IngestQueueStats{}, errors.New("remember queue requires an open local vault")
	}
	return client.Vault.IngestQueueStats(ctx)
}

// IngestQueueVisibility reports the derived visible watermark of the vault.
func (m *Memory) IngestQueueVisibility(ctx context.Context) (localvault.IngestQueueVisibility, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := m.LocalVault()
	if err != nil {
		return localvault.IngestQueueVisibility{}, err
	}
	if client == nil || client.Vault == nil {
		return localvault.IngestQueueVisibility{}, errors.New("remember queue requires an open local vault")
	}
	return client.Vault.IngestQueueVisibility(ctx)
}

// PruneIngestQueueApplied releases applied queue rows already covered by the
// published snapshot watermark.
func (m *Memory) PruneIngestQueueApplied(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := m.LocalVault()
	if err != nil {
		return 0, err
	}
	if client == nil || client.Vault == nil {
		return 0, errors.New("remember queue requires an open local vault")
	}
	return client.Vault.PruneIngestQueueApplied(ctx)
}

// ProcessRememberQueue claims at most Limit oldest eligible items. The returned
// count is the number transitioned to a terminal state (applied, failed, or
// canceled). A failing item is deferred to a caller-declared retry time instead
// of blocking newer items, and the first failure is returned after the slice.
func (m *Memory) ProcessRememberQueue(ctx context.Context, opts IngestQueueProcessOptions) (int, error) {
	if err := opts.validate(); err != nil {
		return 0, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	client, err := m.LocalVault()
	if err != nil {
		return 0, err
	}
	if client == nil || client.Vault == nil {
		return 0, errors.New("remember queue requires an open local vault")
	}
	processed := 0
	var firstErr error
	for claimed := 0; claimed < opts.Limit; claimed++ {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		item, ok, err := client.Vault.ClaimIngestQueue(ctx, clock.Now(), opts.LeaseDuration)
		if err != nil {
			return processed, err
		}
		if !ok {
			break
		}
		terminal, itemErr := processRememberQueueItem(ctx, client.Vault, item, opts)
		if terminal {
			processed++
		}
		if itemErr != nil && firstErr == nil {
			firstErr = itemErr
		}
	}
	return processed, firstErr
}

func deferRememberQueueItem(ctx context.Context, vault *localvault.Vault, item localvault.IngestQueueItem, opts IngestQueueProcessOptions, cause error) error {
	_, err := vault.DeferIngestQueueAttempt(ctx, item.QueueID, item.Attempt, item.LeaseToken, clock.Now().Add(opts.RetryBackoff), cause.Error())
	return err
}

func processRememberQueueItem(ctx context.Context, vault *localvault.Vault, item localvault.IngestQueueItem, opts IngestQueueProcessOptions) (bool, error) {
	liveEpoch, err := vault.CurrentPurgeEpoch(ctx)
	if err != nil {
		return false, err
	}
	if liveEpoch != item.PurgeEpoch {
		if _, err := vault.FinishIngestQueueCanceled(ctx, item.QueueID, item.Attempt, item.LeaseToken); err != nil {
			return false, err
		}
		return true, nil
	}
	payload, err := decodeRememberQueuePayload(item.RawPayload)
	if err != nil || item.RequestKey != rememberQueueRequestKey(payload.DocID) {
		if _, finishErr := vault.FinishIngestQueueFailed(ctx, item.QueueID, item.Attempt, item.LeaseToken, "invalid remember queue payload"); finishErr != nil {
			return false, finishErr
		}
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	workerVault := vault.CloneWithSharedSlotCache()
	if workerVault == nil {
		return false, errors.New("remember queue could not clone the local vault")
	}
	worker := localvault.NewLocalClient(workerVault)
	worker.SetIdentity(item.Identity.AssistantID, item.Identity.IncarnationID, item.Identity.Locus)
	heartbeat := startRememberQueueHeartbeat(ctx, vault, item, opts)
	prepared, prepareErr := worker.PrepareQueuedRemember(ctx, payload.DocID, payload.Plaintext, item.OccurredAt, localvault.LayerEpisodic)
	// A lost lease is settled by the transaction fence below; a transient
	// renewal failure must not discard a valid preparation.
	_ = heartbeat.stop()
	if prepareErr != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if deferErr := deferRememberQueueItem(ctx, vault, item, opts, prepareErr); deferErr != nil {
			return false, deferErr
		}
		return false, fmt.Errorf("remember queue prepare %s: %w", payload.DocID, prepareErr)
	}
	if _, applyErr := vault.ApplyPreparedQueuedIngest(ctx, item, prepared); applyErr != nil {
		if errors.Is(applyErr, localvault.ErrIngestQueuePurgeEpochChanged) || errors.Is(applyErr, localvault.ErrEpisodicUnitDeleted) {
			if _, cancelErr := vault.FinishIngestQueueCanceled(ctx, item.QueueID, item.Attempt, item.LeaseToken); cancelErr != nil {
				return false, cancelErr
			}
			return true, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if deferErr := deferRememberQueueItem(ctx, vault, item, opts, applyErr); deferErr != nil {
			return false, deferErr
		}
		return false, fmt.Errorf("remember queue apply %s: %w", payload.DocID, applyErr)
	}
	return true, nil
}

type rememberQueueHeartbeat struct {
	cancel context.CancelFunc
	done   <-chan struct{}
	errs   <-chan error
}

func startRememberQueueHeartbeat(ctx context.Context, vault *localvault.Vault, item localvault.IngestQueueItem, opts IngestQueueProcessOptions) rememberQueueHeartbeat {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(opts.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if _, err := vault.RenewIngestQueueLease(
					heartbeatCtx, item.QueueID, item.Attempt, item.LeaseToken,
					clock.Now(), opts.LeaseDuration,
				); err != nil {
					errs <- err
					return
				}
			}
		}
	}()
	return rememberQueueHeartbeat{cancel: cancel, done: done, errs: errs}
}

func (heartbeat rememberQueueHeartbeat) stop() error {
	heartbeat.cancel()
	<-heartbeat.done
	select {
	case err := <-heartbeat.errs:
		return err
	default:
		return nil
	}
}
