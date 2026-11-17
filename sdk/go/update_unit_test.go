// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package ramesdk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	ramesdk "github.com/rocareer/rolling-memory/sdk/go"
)

func updateUnitSuccessBody(docID string) map[string]any {
	return map[string]any{
		"success":   true,
		"tenant_id": "user",
		"scope":     cloudTestScope,
		"id":        docID,
		"layer":     ramesdk.LayerEpisodic,
	}
}

// 远端 ingest 失败时 UpdateUnit 不得触碰本地旧原文。
func TestUpdateUnitRemoteFailurePreservesLocalPlaintext(t *testing.T) {
	for _, mode := range []string{ramesdk.ModePulsePlain, ramesdk.ModePulseCipher} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				http.Error(w, "ingest unavailable", http.StatusInternalServerError)
			}))
			defer server.Close()
			store := ramesdk.NewMemoryStore([]byte("update-unit-secret"))
			saveCloudTestDocument(t, store, "doc", "original text")
			client := newCloudTestClient(server, store)
			if err := client.SetPulseMode(mode); err != nil {
				t.Fatal(err)
			}
			if _, err := client.UpdateUnit(context.Background(), "doc", "updated text", 2, ramesdk.LayerEpisodic); err == nil {
				t.Fatal("UpdateUnit succeeded against HTTP 500 ingest")
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("ingest requests = %d, want 1", got)
			}
			raw, _, _, err := store.LoadLocalDocument("doc")
			if err != nil || raw != "original text" {
				t.Fatalf("local plaintext after failed update = %q, err=%v; want original text preserved", raw, err)
			}
		})
	}
}

// 远端成功后 SaveLocalPlaintext 按 docID 覆盖本地记录，且单次更新只发一次 ingest。
func TestUpdateUnitSuccessOverwritesLocalPlaintext(t *testing.T) {
	for _, mode := range []string{ramesdk.ModePulsePlain, ramesdk.ModePulseCipher} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				writeCloudTestJSON(t, w, updateUnitSuccessBody("doc"))
			}))
			defer server.Close()
			store := ramesdk.NewMemoryStore([]byte("update-unit-secret"))
			saveCloudTestDocument(t, store, "doc", "original text")
			client := newCloudTestClient(server, store)
			if err := client.SetPulseMode(mode); err != nil {
				t.Fatal(err)
			}
			resp, err := client.UpdateUnit(context.Background(), "doc", "updated text", 2, ramesdk.LayerEpisodic)
			if err != nil {
				t.Fatalf("UpdateUnit: %v", err)
			}
			if resp.UnitID != "doc" {
				t.Fatalf("UpdateUnit response UnitID = %q, want doc", resp.UnitID)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("ingest requests = %d, want 1", got)
			}
			raw, _, _, err := store.LoadLocalDocument("doc")
			if err != nil || raw != "updated text" {
				t.Fatalf("local plaintext after update = %q, err=%v; want updated text", raw, err)
			}
		})
	}
}

// 更新不是清除：引用被更新单元的 noetic 记录必须存活。
func TestUpdateUnitPreservesNoeticCitations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCloudTestJSON(t, w, updateUnitSuccessBody("doc"))
	}))
	defer server.Close()
	store := ramesdk.NewMemoryStore([]byte("update-unit-secret"))
	saveCloudTestDocument(t, store, "doc", "original text")
	if err := store.SaveLocalNoetic(ramesdk.LocalNoeticRecord{
		NoeticID: "noetic-1", OccurredAt: 1700000000,
		QueryText: "q", AnswerText: "a", CitedUnitIDs: []string{"doc"},
	}); err != nil {
		t.Fatal(err)
	}
	client := newCloudTestClient(server, store)
	if _, err := client.UpdateUnit(context.Background(), "doc", "updated text", 2, ramesdk.LayerEpisodic); err != nil {
		t.Fatalf("UpdateUnit: %v", err)
	}
	if _, ok := store.GetLocalNoetic("noetic-1"); !ok {
		t.Fatal("UpdateUnit deleted noetic record citing the updated unit")
	}
	raw, _, _, err := store.LoadLocalDocument("doc")
	if err != nil || raw != "updated text" {
		t.Fatalf("local plaintext after update = %q, err=%v; want updated text", raw, err)
	}
}

// 首次 ingest 失败可安全重试：两次请求携带同一单元 id（服务端按主键冲突覆盖，
// 不产生重复单元），重试成功后本地原文更新。
func TestUpdateUnitFailureRetryKeepsSingleUnitIdentity(t *testing.T) {
	var requests atomic.Int64
	var mu sync.Mutex
	var ingestedIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := requests.Add(1)
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode ingest payload: %v", err)
		}
		mu.Lock()
		ingestedIDs = append(ingestedIDs, payload.ID)
		mu.Unlock()
		if call == 1 {
			http.Error(w, "ingest unavailable", http.StatusInternalServerError)
			return
		}
		writeCloudTestJSON(t, w, updateUnitSuccessBody(payload.ID))
	}))
	defer server.Close()
	store := ramesdk.NewMemoryStore([]byte("update-unit-secret"))
	saveCloudTestDocument(t, store, "doc", "original text")
	client := newCloudTestClient(server, store)
	if _, err := client.UpdateUnit(context.Background(), "doc", "updated text", 2, ramesdk.LayerEpisodic); err == nil {
		t.Fatal("first UpdateUnit succeeded against HTTP 500 ingest")
	}
	raw, _, _, err := store.LoadLocalDocument("doc")
	if err != nil || raw != "original text" {
		t.Fatalf("local plaintext after failed update = %q, err=%v; want original text preserved", raw, err)
	}
	if _, err := client.UpdateUnit(context.Background(), "doc", "updated text", 2, ramesdk.LayerEpisodic); err != nil {
		t.Fatalf("retry UpdateUnit: %v", err)
	}
	raw, _, _, err = store.LoadLocalDocument("doc")
	if err != nil || raw != "updated text" {
		t.Fatalf("local plaintext after retry = %q, err=%v; want updated text", raw, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ingestedIDs) != 2 || ingestedIDs[0] != "doc" || ingestedIDs[1] != "doc" {
		t.Fatalf("ingested unit IDs = %q, want exactly two attempts on the same unit id \"doc\"", ingestedIDs)
	}
}
