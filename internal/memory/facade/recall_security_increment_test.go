// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
	"github.com/rocareer/rolling-memory/internal/store"
)

func TestSecurityScopeRejectedBeforeImportAndGraphRecall(t *testing.T) {
	memory := newMemory(t)
	other := "mem:other"
	path := writeTranscript(t, "scope.txt", "scope witness\n")
	var before int
	if err := memory.Store.DB().QueryRow(`SELECT count(*) FROM trace`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ImportFile(path, &other, nil, "shared", false); err == nil {
		t.Fatal("cross-scope import accepted")
	}
	var after int
	if err := memory.Store.DB().QueryRow(`SELECT count(*) FROM trace`).Scan(&after); err != nil || after != before {
		t.Fatalf("rejected import changed traces: before=%d after=%d err=%v", before, after, err)
	}
	if result, err := memory.Recall(facade.RecallRequest{Cue: "scope witness", Scope: &other}); err == nil || result != nil {
		t.Fatalf("cross-scope graph recall = %#v, %v", result, err)
	}
}

func TestSecurityRestrictedFileImportFailsBeforeWrites(t *testing.T) {
	memory := newMemory(t)
	path := writeTranscript(t, "private.txt", "private imported witness\n")
	for _, audience := range []string{"private", " PRIVATE ", "group", " GROUP ", "unknown", " ", "SHARED"} {
		result, err := memory.ImportFile(path, nil, nil, audience, false)
		if err == nil || result != nil || !strings.Contains(err.Error(), "background transcript scanning cannot enforce file privacy") {
			t.Fatalf("private import = %#v, %v", result, err)
		}
	}
	var traces, cursors, vaultTables int
	if err := memory.Store.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM trace), (SELECT COUNT(*) FROM transcript_cursor), (SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='thin_vault_docs')`).Scan(&traces, &cursors, &vaultTables); err != nil {
		t.Fatal(err)
	}
	if traces != 0 || cursors != 0 || vaultTables != 0 {
		t.Fatalf("private import wrote data: traces=%d cursors=%d vault_tables=%d", traces, cursors, vaultTables)
	}
	if _, err := memory.ImportFile(path, nil, nil, "shared", false); err != nil {
		t.Fatal(err)
	}
	var beforeTrace, beforeDocs, beforeCursors int
	if err := memory.Store.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM trace), (SELECT COUNT(*) FROM thin_vault_docs), (SELECT COUNT(*) FROM transcript_cursor)`).Scan(&beforeTrace, &beforeDocs, &beforeCursors); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ImportFile(path, nil, nil, "private", false); err == nil {
		t.Fatal("private import accepted an existing shared source")
	}
	var afterTrace, afterDocs, afterCursors int
	if err := memory.Store.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM trace), (SELECT COUNT(*) FROM thin_vault_docs), (SELECT COUNT(*) FROM transcript_cursor)`).Scan(&afterTrace, &afterDocs, &afterCursors); err != nil || beforeTrace != afterTrace || beforeDocs != afterDocs || beforeCursors != afterCursors || beforeDocs != 1 {
		t.Fatalf("private rejection altered existing data: before=%d/%d/%d after=%d/%d/%d err=%v", beforeTrace, beforeDocs, beforeCursors, afterTrace, afterDocs, afterCursors, err)
	}
}

func TestSecurityImportedTracesDoNotInheritCurrentPersona(t *testing.T) {
	t.Setenv("ROLLING_MEMORY_ASSISTANT_ID", "current-assistant")
	t.Setenv("ROLLING_MEMORY_INCARNATION_ID", "current-session")
	t.Setenv("ROLLING_MEMORY_LOCUS", "current-workspace")
	memory := newMemory(t)
	for _, fixture := range []struct {
		name, text, session, audience string
	}{
		{"first.txt", "quartz old source one", "old-session-one", "shared"},
		{"second.txt", "sapphire old source two", "old-session-two", "public"},
	} {
		path := writeTranscript(t, fixture.name, fixture.text+"\n")
		if _, err := memory.ImportFile(path, nil, &fixture.session, fixture.audience, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range []struct{ text, session string }{
		{"quartz old source one", "old-session-one"},
		{"sapphire old source two", "old-session-two"},
	} {
		var session, assistant, incarnation, locus string
		err := memory.Store.DB().QueryRow(`SELECT t.session, d.assistant_id, d.incarnation_id, d.locus
			FROM trace t JOIN thin_vault_docs d ON d.doc_id = 'ep_' || t.hash WHERE t.text = ?`, fixture.text).
			Scan(&session, &assistant, &incarnation, &locus)
		if err != nil || session != fixture.session || assistant != "" || incarnation != "" || locus != "" {
			t.Fatalf("import identity for %q: session=%q assistant=%q incarnation=%q locus=%q err=%v", fixture.text, session, assistant, incarnation, locus, err)
		}
	}
	privateHash, created, err := memory.Store.AddTrace(memory.Config.Scope, "private trace must stay private", store.TraceOptions{Session: "private-session", Audience: "private", Meta: map[string]any{"source": "stop_payload"}})
	if err != nil || !created {
		t.Fatalf("seed private payload: %v, %v", created, err)
	}
	if err := memory.MirrorSessionTracesToVault("private-session"); err != nil {
		t.Fatal(err)
	}
	var privateCount int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM thin_vault_docs WHERE doc_id = ?`, "ep_"+privateHash).Scan(&privateCount); err != nil || privateCount != 0 {
		t.Fatalf("private trace mirrored: count=%d err=%v", privateCount, err)
	}
	untrustedHash, created, err := memory.Store.AddTrace(memory.Config.Scope, "untrusted locus witness", store.TraceOptions{
		Session: "untrusted-session", Audience: "shared", Meta: map[string]any{
			"source": "/external/session.jsonl", "locus": "untrusted-workspace",
		},
	})
	if err != nil || !created {
		t.Fatalf("seed untrusted trace: %v, %v", created, err)
	}
	if err := memory.MirrorSessionTracesToVault("untrusted-session"); err != nil {
		t.Fatal(err)
	}
	var untrustedLocus string
	if err := memory.Store.DB().QueryRow(`SELECT locus FROM thin_vault_docs WHERE doc_id = ?`, "ep_"+untrustedHash).Scan(&untrustedLocus); err != nil || untrustedLocus != "" {
		t.Fatalf("untrusted locus attributed: %q %v", untrustedLocus, err)
	}
	for _, fixture := range []struct {
		source, session, locus string
	}{
		{"stop_payload", "hook-one", "hook-workspace-one"},
		{"user_prompt_submit", "hook-two", "hook-workspace-two"},
	} {
		hash, created, err := memory.Store.AddTrace(memory.Config.Scope, "hook witness "+fixture.session, store.TraceOptions{
			Session: fixture.session, Audience: "shared", Meta: map[string]any{
				"source": fixture.source, "locus": fixture.locus, "incarnation": "coding/harness/main",
			},
		})
		if err != nil || !created {
			t.Fatalf("seed hook trace: %v, %v", created, err)
		}
		if err := memory.MirrorSessionTracesToVault(fixture.session); err != nil {
			t.Fatal(err)
		}
		var assistant, incarnation, locus string
		if err := memory.Store.DB().QueryRow(`SELECT assistant_id, incarnation_id, locus FROM thin_vault_docs WHERE doc_id = ?`, "ep_"+hash).Scan(&assistant, &incarnation, &locus); err != nil || assistant != "" || incarnation != "" || locus != fixture.locus {
			t.Fatalf("hook source identity: assistant=%q incarnation=%q locus=%q err=%v", assistant, incarnation, locus, err)
		}
	}
	tc, err := memory.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	if tc.AssistantID != "current-assistant" || tc.IncarnationID != "current-session" || tc.Locus != "current-workspace" {
		t.Fatalf("current identity changed: %q %q %q", tc.AssistantID, tc.IncarnationID, tc.Locus)
	}
}

func TestSecurityWholeTurnPointerIsNotDelivered(t *testing.T) {
	memory := newMemory(t)
	client, err := memory.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source#metadata.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"quartz whole turn"}}` + "\n" + `{"type":"assistant","message":{"role":"assistant","content":"whole turn secret"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := "@ref:jsonl:" + path + ":0"
	text, err := thinclient.ResolveEpisodicPointerLive(ref)
	if err != nil {
		t.Fatal(err)
	}
	packet, _ := client.Vault.PackBlindPacket("ep_whole", text, testNow)
	if err := client.Vault.SaveLocalEpisodicPointerIndexed("ep_whole", testNow, ref, "", text, packet, thinclient.IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	thinclient.PurgeLegacyTables(memory.Store.DB())
	for _, mode := range []string{"full", "probe", "recent"} {
		result, err := memory.Recall(facade.RecallRequest{Cue: "quartz whole turn", Mode: mode, NoCrystallize: true})
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(result.ToDict())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "whole turn secret") || strings.Contains(string(payload), path) || strings.Contains(string(payload), "@ref:") {
			t.Errorf("%s delivered whole-turn source: %s", mode, payload)
		}
	}
}

func TestSecurityAlternateSourceRequiresProvenance(t *testing.T) {
	memory := newMemory(t)
	client, err := memory.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	primary := filepath.Join(dir, "primary.jsonl")
	alternate := filepath.Join(dir, "alternate.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"quartz provenance witness"}}` + "\n"
	for _, path := range []string{primary, alternate} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	primaryTurn := "@ref:jsonl:" + primary + ":0"
	alternateTurn := "@ref:jsonl:" + alternate + ":0"
	text := "[user] quartz provenance witness"
	const docID = "ep_provenance"
	packet, _ := client.Vault.PackBlindPacket(docID, text, testNow)
	client.SetIdentity("primary-assistant", "primary-session", "primary-locus")
	if err := client.Vault.SaveLocalEpisodicPointerIndexed(docID, testNow, primaryTurn+"#0", primaryTurn, text, packet, thinclient.IngestResponse{Layer: thinclient.LayerEpisodic}); err != nil {
		t.Fatal(err)
	}
	client.SetIdentity("alternate-assistant", "alternate-session", "alternate-locus")
	if err := client.Vault.SaveLocalEpisodicPointerIndexed(docID, testNow+100, alternateTurn+"#0", alternateTurn, text, packet, thinclient.IngestResponse{Layer: thinclient.LayerEpisodic}); err != nil {
		t.Fatal(err)
	}
	thinclient.PurgeLegacyTables(memory.Store.DB())
	req := facade.RecallRequest{Cue: "quartz provenance witness", NoCrystallize: true}
	result, err := memory.Recall(req)
	if err != nil || !strings.Contains(result.Render(10000), text) {
		t.Fatalf("primary source control: %+v %v", result, err)
	}
	if err := os.Remove(primary); err != nil {
		t.Fatal(err)
	}
	if got, actual, err := client.Vault.LoadVerifiedEpisodicPointerSource(docID, primaryTurn+"#0"); err != nil || got != text || actual != alternateTurn+"#0" {
		t.Fatalf("alternate-source control: %q %q %v", got, actual, err)
	}
	result, err = memory.Recall(req)
	if err != nil || len(result.Bands) != 0 || len(result.RawHits) != 0 || result.Gaps["source_unavailable"] != 1 || result.Absent {
		t.Fatalf("unattributed alternate delivered: %+v %v", result, err)
	}
	if err := os.WriteFile(primary, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = memory.Recall(req)
	if err != nil || !strings.Contains(result.Render(10000), text) || result.Gaps["source_unavailable"] != 0 {
		t.Fatalf("restored primary source control: %+v %v", result, err)
	}
}

func TestSecuritySameTurnRelocationUsesActualSegment(t *testing.T) {
	memory := newMemory(t)
	client, err := memory.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "turn.jsonl")
	original := `{"type":"user","message":{"role":"user","content":"quartz relocation witness"}}` + "\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	turn := "@ref:jsonl:" + path + ":0"
	text := "[user] quartz relocation witness"
	const docID = "ep_relocation"
	packet, _ := client.Vault.PackBlindPacket(docID, text, testNow)
	if err := client.Vault.SaveLocalEpisodicPointerIndexed(docID, testNow, turn+"#0", turn, text, packet, thinclient.IngestResponse{Layer: thinclient.LayerEpisodic}); err != nil {
		t.Fatal(err)
	}
	thinclient.PurgeLegacyTables(memory.Store.DB())
	changed := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"unindexed secret"},{"type":"text","text":"quartz relocation witness"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, actual, err := client.Vault.LoadVerifiedEpisodicPointerSource(docID, turn+"#0"); err != nil || got != text || actual != turn+"#1" {
		t.Fatalf("same-turn relocation control: %q %q %v", got, actual, err)
	}
	result, err := memory.Recall(facade.RecallRequest{Cue: "quartz relocation witness", NoCrystallize: true})
	if err != nil || len(result.Bands) != 1 || result.Gaps["source_unavailable"] != 0 {
		t.Fatalf("relocated recall: %+v %v", result, err)
	}
	nodes := result.Bands[0]["nodes"].([]map[string]any)
	window, _ := nodes[0]["segment_window"].([]int)
	if len(window) != 2 || window[0] != 1 || window[1] != 1 || nodes[0]["content"] != text {
		t.Fatalf("wrong source segment delivered: %+v", nodes[0])
	}
	if payload, err := json.Marshal(result.ToDict()); err != nil || strings.Contains(string(payload), "unindexed secret") || strings.Contains(string(payload), path) {
		t.Fatalf("unverified segment or pointer leaked: %s %v", payload, err)
	}
}

func TestSecurityLineageDoesNotAuthorizeAdjacentHit(t *testing.T) {
	memory := newMemory(t)
	client, err := memory.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sensitive.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"quartz nebula evidence"}}` + "\n" + `{"type":"assistant","message":{"role":"assistant","content":"unrelated confidential sibling"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	turn := "@ref:jsonl:" + path + ":0"
	for _, row := range []struct{ id, ref, text string }{
		{"ep_quartz", turn + "#0", "[user] quartz nebula evidence"},
		{"ep_secret", turn + "#1", "[assistant] unrelated confidential sibling"},
	} {
		packet, _ := client.Vault.PackBlindPacket(row.id, row.text, testNow)
		if err := client.Vault.SaveLocalEpisodicPointerIndexed(row.id, testNow, row.ref, turn, row.text, packet, thinclient.IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatal(err)
		}
	}
	thinclient.PurgeLegacyTables(memory.Store.DB())
	result, err := memory.Recall(facade.RecallRequest{Cue: "quartz nebula evidence", NoCrystallize: true})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(result.ToDict())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "confidential sibling") || strings.Contains(string(payload), path) || strings.Contains(string(payload), "@ref:") {
		t.Fatalf("unselected sibling or source pointer delivered: %s", payload)
	}
	if !strings.Contains(string(payload), "quartz nebula evidence") {
		t.Fatalf("selected evidence missing: %s", payload)
	}
}
