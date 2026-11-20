// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/memory/localvault"
	_ "modernc.org/sqlite"
)

// seedZCodeTurnFixture writes a minimal ZCode host database holding one turn
// (user question + assistant reply with a reasoning part and a text part) under
// `$HOME/.zcode/cli/db/db.sqlite`, the exact location the canonical resolver reads.
func seedZCodeTurnFixture(t *testing.T, home string) {
	t.Helper()
	dbDir := filepath.Join(home, ".zcode", "cli", "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir zcode fixture: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dbDir, "db.sqlite"))
	if err != nil {
		t.Fatalf("open zcode fixture: %v", err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, sequence INTEGER, time_created INTEGER, data TEXT)`,
		`CREATE TABLE part (message_id TEXT, sequence INTEGER, data TEXT)`,
		`INSERT INTO message (id, session_id, sequence, time_created, data) VALUES
			('msg_hydrate_u1', 'sess_hydrate_fixture', 1, 1700000000, '{"role":"user"}')`,
		`INSERT INTO part (message_id, sequence, data) VALUES
			('msg_hydrate_u1', 0, '{"type":"text","text":"fixture question"}')`,
		`INSERT INTO message (id, session_id, sequence, time_created, data) VALUES
			('msg_hydrate_a1', 'sess_hydrate_fixture', 2, 1700000001, '{"role":"assistant"}')`,
		`INSERT INTO part (message_id, sequence, data) VALUES
			('msg_hydrate_a1', 0, '{"type":"reasoning","text":"hidden"}'),
			('msg_hydrate_a1', 1, '{"type":"text","text":"fixture reply body"}')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed zcode fixture (%s): %v", stmt, err)
		}
	}
}

// TestUnixDaemonHydrateCanonicalSegments drives POST /v1/hydrate over the local Unix
// socket against a fixture host database: the endpoint must answer with the canonical
// per-segment text (the same resolver `recall` / `recent` use) and fail closed on a
// database mismatch or a non-POST method.
func TestUnixDaemonHydrateCanonicalSegments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedZCodeTurnFixture(t, home)

	memory := newMemory(t)
	tc, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	turn := "@ref:zcode:sess_hydrate_fixture:msg_hydrate_u1"
	for _, fixture := range []struct{ id, ref, text string }{
		{"ep_question", turn + "#0", "[user] fixture question"},
		{"ep_answer", turn + "#1", "[assistant] fixture reply body"},
	} {
		packet, _ := tc.Vault.PackBlindPacket(fixture.id, fixture.text, testNow)
		if err := tc.Vault.SaveLocalEpisodicPointerIndexed(fixture.id, testNow, fixture.ref, turn, fixture.text, packet, localvault.IngestResponse{Layer: localvault.LayerEpisodic}); err != nil {
			t.Fatalf("seed indexed pointer: %v", err)
		}
	}
	// Unix socket paths are capped at 104 bytes on BSD/macOS; a short /tmp base keeps
	// this test independent of the (long) t.TempDir prefix.
	sockDir, err := os.MkdirTemp("/tmp", "rm-hyd-")
	if err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "engine.sock")
	t.Setenv(DaemonSocketEnv, sockPath)

	done := make(chan error, 1)
	go func() {
		done <- ServeUnix(sockPath, memory, nil)
	}()
	t.Cleanup(func() {
		requestUnixDaemonShutdown(sockPath)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h, err := PingUnixDaemon(sockPath, 100*time.Millisecond); err == nil && h != nil && h.OK {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	client := newUnixHTTPClient(sockPath, 5*time.Second)
	post := func(payload UnixHydratePayload) (int, UnixHydrateResponse) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal hydrate payload: %v", err)
		}
		resp, err := client.Post("http://unix/v1/hydrate", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("hydrate post: %v", err)
		}
		defer resp.Body.Close()
		var out UnixHydrateResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// A valid document and pointer resolve the indexed segment, not the reasoning part.
	status, out := post(UnixHydratePayload{Items: []UnixHydrateItem{{DocID: "ep_answer", Ref: turn + "#1"}}})
	if status != http.StatusOK || !out.OK || len(out.Texts) != 1 || out.Texts[0].Text != "[assistant] fixture reply body" || out.Texts[0].ActualRef != turn+"#1" || !out.Texts[0].Verified || out.Texts[0].Error != "" {
		t.Fatalf("verified hydrate response: %d %+v", status, out)
	}

	// Missing identity, wrong identity, and a pointer into a different indexed unit
	// must never fall back to the live source, even when that source exists.
	status, out = post(UnixHydratePayload{Items: []UnixHydrateItem{
		{Ref: turn + "#1"},
		{DocID: "ep_missing", Ref: turn + "#0"},
		{DocID: "ep_question", Ref: turn + "#1"},
		{DocID: "ep_question", Ref: turn},
		{DocID: "ep_question", Ref: "   "},
		{DocID: "ep_question", Ref: turn + "#0"},
	}})
	if status != http.StatusOK || !out.OK || len(out.Texts) != 6 {
		t.Fatalf("unexpected hydrate response: %d %+v", status, out)
	}
	for i, item := range out.Texts[:5] {
		if item.Error == "" || item.Text != "" || item.Verified {
			t.Fatalf("unverified item %d returned content: %+v", i, item)
		}
	}
	if item := out.Texts[5]; item.Error != "" || !item.Verified || item.Text != "[user] fixture question" {
		t.Fatalf("valid item in mixed batch: %+v", item)
	}

	// The same content in a different turn is not enough to establish its audience
	// or source metadata, whether requested explicitly or reached as a fallback.
	alternatePath := filepath.Join(t.TempDir(), "alternate.jsonl")
	alternateBody := `{"type":"user","message":{"role":"user","content":"fixture question"}}` + "\n"
	if err := os.WriteFile(alternatePath, []byte(alternateBody), 0o600); err != nil {
		t.Fatal(err)
	}
	alternateTurn := "@ref:jsonl:" + alternatePath + ":0"
	packet, _ := tc.Vault.PackBlindPacket("ep_question", "[user] fixture question", testNow)
	if err := tc.Vault.SaveLocalEpisodicPointerIndexed("ep_question", testNow, alternateTurn+"#0", alternateTurn, "[user] fixture question", packet, localvault.IngestResponse{Layer: localvault.LayerEpisodic}); err != nil {
		t.Fatal(err)
	}
	status, out = post(UnixHydratePayload{Items: []UnixHydrateItem{{DocID: "ep_question", Ref: alternateTurn + "#0"}}})
	if status != http.StatusOK || !out.OK || len(out.Texts) != 1 || out.Texts[0].Error == "" || out.Texts[0].Text != "" || out.Texts[0].ActualRef != "" || out.Texts[0].Verified {
		t.Fatalf("explicit alternate source returned content: %d %+v", status, out)
	}
	sourcePath := filepath.Join(home, ".zcode", "cli", "db", "db.sqlite")
	sourceDB, err := sql.Open("sqlite", sourcePath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceDB.Exec(`UPDATE part SET data = '{"type":"text","text":"changed primary"}' WHERE message_id = 'msg_hydrate_u1'`); err != nil {
		_ = sourceDB.Close()
		t.Fatal(err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatal(err)
	}
	if got, actual, err := tc.Vault.LoadVerifiedEpisodicPointerSource("ep_question", turn+"#0"); err != nil || got != "[user] fixture question" || actual != alternateTurn+"#0" {
		t.Fatalf("alternate source control: %q %q %v", got, actual, err)
	}
	status, out = post(UnixHydratePayload{Items: []UnixHydrateItem{{DocID: "ep_question", Ref: turn + "#0"}}})
	if status != http.StatusOK || !out.OK || len(out.Texts) != 1 || out.Texts[0].Error == "" || out.Texts[0].Text != "" || out.Texts[0].ActualRef != "" || out.Texts[0].Verified {
		t.Fatalf("alternate source fallback returned content: %d %+v", status, out)
	}

	// A resegmented source returns the digest-matching text and its actual position.
	sourceDB, err = sql.Open("sqlite", sourcePath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceDB.Exec(`UPDATE part SET data = '{"type":"text","text":"fixture question"}' WHERE message_id = 'msg_hydrate_u1'; INSERT INTO part (message_id, sequence, data) VALUES ('msg_hydrate_u1', -1, '{"type":"text","text":"unindexed secret"}')`); err != nil {
		_ = sourceDB.Close()
		t.Fatal(err)
	}
	_ = sourceDB.Close()
	status, out = post(UnixHydratePayload{Items: []UnixHydrateItem{{DocID: "ep_question", Ref: turn + "#0"}}})
	if status != http.StatusOK || !out.OK || len(out.Texts) != 1 || out.Texts[0].Text != "[user] fixture question" || out.Texts[0].ActualRef != turn+"#1" || !out.Texts[0].Verified || out.Texts[0].Error != "" {
		t.Fatalf("relocated hydrate response: %d %+v", status, out)
	}

	// Corrupting the sealed index must prevent hydration despite a readable source.
	if _, err := memory.Store.DB().Exec(`UPDATE rm_episodic_index SET payload = x'00' WHERE doc_id = 'ep_question'`); err != nil {
		t.Fatal(err)
	}
	status, out = post(UnixHydratePayload{Items: []UnixHydrateItem{{DocID: "ep_question", Ref: turn + "#0"}}})
	if status != http.StatusOK || !out.OK || len(out.Texts) != 1 || out.Texts[0].Error == "" || out.Texts[0].Text != "" || out.Texts[0].Verified {
		t.Fatalf("corrupt index returned content: %d %+v", status, out)
	}

	// 4. Non-POST methods are rejected.
	resp, err := client.Get("http://unix/v1/hydrate")
	if err != nil {
		t.Fatalf("GET /v1/hydrate: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d", resp.StatusCode)
	}

	// 5. A vault-bound daemon refuses requests targeting another database.
	status, out = post(UnixHydratePayload{DB: filepath.Join(t.TempDir(), "other.db"), Items: []UnixHydrateItem{{Ref: "@ref:zcode:x:y#0"}}})
	if status != http.StatusConflict || out.OK || !strings.Contains(out.Error, "daemon bound to") {
		t.Fatalf("expected db mismatch conflict, got %d %+v", status, out)
	}
}
