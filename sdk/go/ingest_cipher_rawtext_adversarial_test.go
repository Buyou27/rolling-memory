// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package ramesdk_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	ramesdk "github.com/rocareer/rolling-memory/sdk/go"
)

// pulse_cipher 下任何分支都不得把 RawText 送上云端线：
// ① slots+RawText → 剥离后 POST（线上体不得含 raw_text）；
// ② raw-text-only → 在发起任何网络请求之前显式拒绝。
func TestIngestPacketCipherRawTextNeverLeavesClientAdversarial(t *testing.T) {
	var requests atomic.Int64
	var lastBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, _ := io.ReadAll(r.Body)
		lastBody = body
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tenant_id":  "user",
			"scope":      cloudTestScope,
			"id":         "u1",
			"layer":      ramesdk.LayerEpisodic,
			"pulse_mode": ramesdk.ModePulseCipher,
		})
	}))
	defer server.Close()
	client := newCloudTestClient(server, ramesdk.NewMemoryStore([]byte("cipher-leak-secret")))
	if err := client.SetPulseMode(ramesdk.ModePulseCipher); err != nil {
		t.Fatal(err)
	}

	// ① slots + RawText → POST 体不得携带明文。
	if _, err := client.IngestPacket(context.Background(), ramesdk.BlindPulsePacket{
		UnitID:  "u1",
		RawText: "secret plaintext must not leave",
		Slots:   []ramesdk.BlindTokenAtom{{H: 7, RawBitsX10: 40, ClassFlags: 1}},
	}); err != nil {
		t.Fatalf("cipher ingest with slots: %v", err)
	}
	if strings.Contains(string(lastBody), "raw_text") || strings.Contains(string(lastBody), "secret plaintext") {
		t.Fatalf("pulse_cipher ingest leaked raw text on the wire: %s", lastBody)
	}

	// ② raw-text-only → 零额外请求、显式失败。
	before := requests.Load()
	if _, err := client.IngestPacket(context.Background(), ramesdk.BlindPulsePacket{
		UnitID:  "u2",
		RawText: "raw-only secret",
	}); err == nil {
		t.Fatal("raw-text-only pulse_cipher ingest must fail closed")
	}
	if got := requests.Load(); got != before {
		t.Fatalf("raw-text-only pulse_cipher ingest issued %d extra request(s)", got-before)
	}
}

type rawTextPopulatingStore struct {
	ramesdk.PlaintextStore
}

func (s rawTextPopulatingStore) PackBlindPacket(docID, rawText string, occurredAt int64) (ramesdk.BlindPulsePacket, []string) {
	pkt, sents := s.PlaintextStore.PackBlindPacket(docID, rawText, occurredAt)
	pkt.RawText = rawText
	return pkt, sents
}

// pulse_cipher 下 QueryHits 即便遇到自定义 PlaintextStore.PackBlindPacket 在返回包上预填了
// RawText，也必须在发往 /query 前剥离 wireQuery.RawText，确保查询侧同样零明文离端。
func TestQueryHitsCipherRawTextNeverLeavesClientAdversarial(t *testing.T) {
	var lastBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastBody = body
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":    true,
			"tenant_id":  "user",
			"scope":      cloudTestScope,
			"pulse_mode": ramesdk.ModePulseCipher,
			"engine":     "autonoetic",
			"hits":       []any{},
			"count":      0,
		})
	}))
	defer server.Close()
	baseStore := ramesdk.NewMemoryStore([]byte("cipher-query-leak-secret"))
	client := newCloudTestClient(server, rawTextPopulatingStore{PlaintextStore: baseStore})
	if err := client.SetPulseMode(ramesdk.ModePulseCipher); err != nil {
		t.Fatal(err)
	}
	if _, err := client.QueryHits(context.Background(), "top secret query phrase", 5); err != nil {
		t.Fatalf("QueryHits error: %v", err)
	}
	if strings.Contains(string(lastBody), "raw_text") || strings.Contains(string(lastBody), "top secret query phrase") {
		t.Fatalf("pulse_cipher QueryHits leaked raw_text on the wire: %s", lastBody)
	}
}
