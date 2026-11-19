// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"errors"
	"testing"
	"time"
)

type candidateTestStore struct {
	PlaintextStore
	missing map[string]bool
	cancel  context.CancelFunc
	loads   int
}

func (s *candidateTestStore) LoadLocalDocument(id string) (string, string, []string, error) {
	s.loads++
	if s.cancel != nil {
		s.cancel()
	}
	if s.missing[id] {
		return "", "", nil, errors.New("source unavailable")
	}
	return id, id, nil, nil
}

func TestLocalCandidateLimitPreservesGaps(t *testing.T) {
	hits := []LocalResolvedHit{
		{QueryHit: QueryHit{UnitID: "missing"}, SourceUnavailable: true},
		{QueryHit: QueryHit{UnitID: "first"}},
		{QueryHit: QueryHit{UnitID: "second"}},
	}
	got := limitHydratedLocalHits(hits, 1)
	if len(got) != 2 || got[0].UnitID != "missing" || got[1].UnitID != "first" {
		t.Fatalf("limited hits: %+v", got)
	}
}

func TestLocalCandidateHydrationContext(t *testing.T) {
	db := migrationTestDB(t)
	vault, err := NewVault(db, []byte("candidate-hydration-cancel"))
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.SaveLocalPlaintext("doc", 1, "quartz nebula", nil, IngestResponse{Layer: LayerEpisodic}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	store := &candidateTestStore{PlaintextStore: vault, cancel: cancel}
	client := NewClientWithStore(store, NewLocalVaultTransport(vault))
	_, err = client.Recall(ctx, "quartz nebula", 2)
	if !errors.Is(err, context.Canceled) || store.loads != 1 {
		t.Fatalf("loads=%d err=%v", store.loads, err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err = client.Recall(ctx, "quartz nebula", 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline err=%v", err)
	}
}

func TestLocalTransportCanceledContext(t *testing.T) {
	db := migrationTestDB(t)
	vault, err := NewVault(db, []byte("candidate-cancel"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewClient(vault, NewLocalVaultTransport(vault))
	_, err = client.Recall(ctx, "quartz", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("recall canceled: %v", err)
	}
}

func TestRecallWithOptionsSkipsDuplicateLocalCloudHits(t *testing.T) {
	db := migrationTestDB(t)
	vault, err := NewVault(db, []byte("candidate-duplicate-cloud"))
	if err != nil {
		t.Fatal(err)
	}
	vault.slotCacheMu.Lock()
	vault.snapRef.refreshing = true
	vault.slotCacheMu.Unlock()
	defer func() {
		vault.slotCacheMu.Lock()
		vault.snapRef.refreshing = false
		vault.slotCacheMu.Unlock()
	}()
	if err := vault.SaveLocalPlaintext("doc", 1, "quartz nebula", nil, IngestResponse{Layer: LayerEpisodic}); err != nil {
		t.Fatal(err)
	}
	snap, err := vault.buildCorpusSnapshot(context.Background())
	if err != nil || snap == nil || len(snap.docs) != 1 {
		t.Fatalf("snapshot: docs=%d err=%v", len(snap.docs), err)
	}
	vault.slotCacheMu.Lock()
	snap.docs = append(snap.docs, snap.docs[0])
	snap.inlineCount = len(snap.docs)
	vault.slotCacheMu.Unlock()

	transport := NewLocalVaultTransport(vault)
	qPkt, _ := vault.PackBlindPacket("QUERY", "quartz nebula", 0)
	cloudHits, err := transport.QueryBlindManifold(context.Background(), qPkt, 0)
	if err != nil || len(cloudHits) != 1 || cloudHits[0].UnitID != "doc" {
		t.Fatalf("duplicate transport fixture: hits=%+v err=%v", cloudHits, err)
	}
	client := NewClient(vault, transport)
	hits, err := client.RecallWithOptions(context.Background(), "quartz nebula", BlindQueryOptions{TopK: 10})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(hits) != 1 || hits[0].UnitID != "doc" {
		t.Fatalf("duplicate cloud UnitID was not suppressed: %+v", hits)
	}
}
