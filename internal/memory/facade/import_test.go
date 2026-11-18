// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/episodicunit"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/transcript"
	"github.com/rocareer/rolling-memory/internal/version"
)

func assertKeys(t *testing.T, label string, result map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(result))
	for key := range result {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s keys = %v, want %v", label, got, want)
	}
}

// writeTranscript drops one synthetic transcript in a temp dir.
func writeTranscript(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestImportFileTranscriptUsesTheAdapter verifies that any file that is not a
// rolling-memory JSON export is routed through the transcript importer.
func TestImportFileTranscriptUsesTheAdapter(t *testing.T) {
	memory := newMemory(t)
	path := writeTranscript(t, "chat.txt", "老张养了一只猫。\n老张住在北京。\n")

	result, err := memory.ImportFile(path, nil, nil, "shared", false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	assertKeys(t, "import", result,
		"ok", "path", "session", "messages", "inserted", "offset", "total_lines", "size")
	if result["ok"] != true || result["path"] != path {
		t.Fatalf("import = %#v", result)
	}
	if result["session"] != "chat" || result["messages"] != 2 || result["inserted"] != 2 {
		t.Fatalf("import = %#v", result)
	}
	if result["offset"] != 2 || result["total_lines"] != 2 {
		t.Fatalf("import = %#v", result)
	}
	if size, ok := result["size"].(int64); !ok || size != int64(len("老张养了一只猫。\n老张住在北京。\n")) {
		t.Fatalf("size = %#v", result["size"])
	}
	if _, ok := result["replay"]; ok {
		t.Fatal("replay must only appear when asked for")
	}

	var traces, pending int
	row := memory.Store.DB().QueryRow("SELECT count(*), sum(processed = 0) FROM trace")
	if err := row.Scan(&traces, &pending); err != nil {
		t.Fatalf("trace count: %v", err)
	}
	if traces != 2 || pending != 2 {
		t.Fatalf("traces = %d, pending = %d", traces, pending)
	}
	cursor, found, err := transcript.CursorFor(memory.Store, path, "chat")
	if err != nil || !found {
		t.Fatalf("cursor = %#v (%v)", cursor, err)
	}
	if cursor["offset"] != int64(2) {
		t.Fatalf("cursor = %#v", cursor)
	}
}

func TestImportFilePreservesOccurrenceAndRecordingTimes(t *testing.T) {
	memory := newMemory(t)
	path := writeTranscript(t, "times.jsonl", strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"quartz unknown time evidence"}}`,
		`{"type":"assistant","timestamp":"2026-11-18T00:00:00Z","message":{"role":"assistant","stop_reason":"end_turn","content":"sapphire dated evidence"}}`,
	}, "\n")+"\n")
	if _, err := memory.ImportFile(path, nil, nil, "shared", false); err != nil {
		t.Fatal(err)
	}
	// The per-segment occurrence times land on the canonical documents.
	for _, fixture := range []struct {
		text     string
		role     string
		occurred int64
	}{
		{"quartz unknown time evidence", "user", 0},
		{"sapphire dated evidence", "assistant", 1794960000},
	} {
		var vaultOccurred int64
		docID := episodicunit.DocID("mem:test", "["+fixture.role+"] "+fixture.text)
		err := memory.Store.DB().QueryRow(`SELECT occurred_at FROM thin_vault_docs WHERE doc_id = ?`, docID).Scan(&vaultOccurred)
		if err != nil || vaultOccurred != fixture.occurred {
			t.Fatalf("%q occurrence = %d, doc=%q err=%v", fixture.text, vaultOccurred, docID, err)
		}
	}
	// Recall delivers the turn with its truthful time (the dated assistant row).
	result, err := memory.Recall(facade.RecallRequest{Cue: "sapphire dated evidence", NoCrystallize: true})
	if err != nil || len(result.Bands) == 0 {
		t.Fatalf("recall: %+v %v", result, err)
	}
	found := false
	for _, band := range result.Bands {
		for _, node := range band["nodes"].([]map[string]any) {
			if text, _ := node["content"].(string); strings.Contains(text, "sapphire dated evidence") {
				found = true
				if node["occurred_at"] != int64(1794960000) || node["time"] != "2026-11-18 00:00:00 +00:00" {
					t.Fatalf("dated turn delivery: %+v", node)
				}
			}
		}
	}
	if !found {
		t.Fatalf("dated turn missing from %+v", result.Bands)
	}
	// A turn whose rows carry no timestamp must never gain one.
	undated := writeTranscript(t, "undated.jsonl", strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"garnet undated evidence"}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"topaz undated answer"}}`,
	}, "\n")+"\n")
	if _, err := memory.ImportFile(undated, nil, nil, "shared", false); err != nil {
		t.Fatal(err)
	}
	undatedID := episodicunit.DocID("mem:test", "[user] garnet undated evidence")
	result, err = memory.Recall(facade.RecallRequest{Cue: "garnet undated evidence", NoCrystallize: true})
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, band := range result.Bands {
		for _, node := range band["nodes"].([]map[string]any) {
			if node["id"] != undatedID {
				continue
			}
			found = true
			if node["occurred_at"] != int64(0) || node["time"] != "" {
				t.Fatalf("undated turn gained a time: %+v", node)
			}
		}
	}
	if !found {
		t.Fatalf("undated turn missing from recall: %+v", result.Bands)
	}
	recent, err := memory.Recall(facade.RecallRequest{Mode: "recent", NoCrystallize: true})
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, band := range recent.Bands {
		for _, node := range band["nodes"].([]map[string]any) {
			if node["id"] != undatedID {
				continue
			}
			found = true
			if node["time"] != "" || band["time_label"] != "" {
				t.Fatalf("recent assigned a time to unknown source: %+v", band)
			}
		}
	}
	if !found {
		t.Fatalf("recent omitted unknown-time document %q", undatedID)
	}
	const existingTime = int64(123456789)
	quartzID := episodicunit.DocID("mem:test", "[user] quartz unknown time evidence")
	if _, err := memory.Store.DB().Exec(`UPDATE thin_vault_docs SET occurred_at = ?, inserted_at = ? WHERE doc_id = ?`, existingTime, existingTime, quartzID); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ImportFile(path, nil, nil, "shared", false); err != nil {
		t.Fatal(err)
	}
	var traces, docs int
	if err := memory.Store.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM trace), (SELECT COUNT(*) FROM thin_vault_docs)`).Scan(&traces, &docs); err != nil || traces != 0 || docs != 4 {
		t.Fatalf("repeat import: traces=%d docs=%d err=%v", traces, docs, err)
	}
	var occurred, inserted int64
	if err := memory.Store.DB().QueryRow(`SELECT occurred_at, inserted_at FROM thin_vault_docs WHERE doc_id = ?`, quartzID).Scan(&occurred, &inserted); err != nil || occurred != existingTime || inserted != existingTime {
		t.Fatalf("repeat import changed existing vault row: occurred=%d inserted=%d err=%v", occurred, inserted, err)
	}
}

// TestImportFileTranscriptReplayCapsAtMessageCount verifies that replay on
// import caps max_traces at the message count of the imported batch.
func TestImportFileTranscriptReplayCapsAtMessageCount(t *testing.T) {
	memory := newMemory(t)
	first := writeTranscript(t, "one.txt", "老张养了一只猫。\n老张住在北京。\n")
	if _, err := memory.ImportFile(first, nil, nil, "shared", false); err != nil {
		t.Fatalf("first import: %v", err)
	}
	second := writeTranscript(t, "two.txt", "老张喜欢咖啡。\n老张喝咖啡。\n")

	result, err := memory.ImportFile(second, nil, nil, "shared", true)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	replayed, ok := result["replay"].(map[string]any)
	if !ok {
		t.Fatalf("import has no replay receipt: %#v", result)
	}
	assertKeys(t, "replay", replayed,
		"ok", "scope", "processed", "written", "skipped", "anchor_failed", "resolve_failed", "llm_calls", "warnings")
	if replayed["processed"] != 2 {
		t.Fatalf("replay processed = %v, want the message cap 2", replayed["processed"])
	}
	if replayed["scope"] != "mem:test" {
		t.Fatalf("replay scope = %#v", replayed["scope"])
	}
	var pending int
	if err := memory.Store.DB().QueryRow("SELECT count(*) FROM trace WHERE processed = 0").Scan(&pending); err != nil {
		t.Fatalf("pending count: %v", err)
	}
	if pending != 2 {
		t.Fatalf("pending = %d, want the second file's 2 traces still queued", pending)
	}
}

// TestImportFileTranscriptHonoursSessionAndAudience pins the two keyword
// arguments import_file forwards to the adapter.
func TestImportFileTranscriptHonoursSessionAndAudience(t *testing.T) {
	memory := newMemory(t)
	path := writeTranscript(t, "chat.txt", "老张养了一只猫。\n")
	session := "hand-over"
	result, err := memory.ImportFile(path, nil, &session, "public", false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result["session"] != "hand-over" {
		t.Fatalf("session = %#v", result["session"])
	}
	var audience, stored string
	if err := memory.Store.DB().QueryRow("SELECT audience, session FROM trace").Scan(&audience, &stored); err != nil {
		t.Fatalf("trace row: %v", err)
	}
	if audience != "public" || stored != "hand-over" {
		t.Fatalf("trace audience=%q session=%q", audience, stored)
	}
}

// TestImportFileTranscriptPaginates verifies that an explicit import consumes
// every batch and reports totals for the entire file.
func TestImportFileTranscriptPaginates(t *testing.T) {
	memory := newMemory(t)
	var builder strings.Builder
	for index := 0; index < 600; index++ {
		fmt.Fprintf(&builder, "合成消息 %d。\n", index)
	}
	path := writeTranscript(t, "bulk.txt", builder.String())

	result, err := memory.ImportFile(path, nil, nil, "shared", false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result["messages"] != 600 || result["inserted"] != 600 || result["offset"] != 600 || result["total_lines"] != 600 {
		t.Fatalf("import = %#v", result)
	}
	var count int
	if err := memory.Store.DB().QueryRow("SELECT count(*) FROM trace").Scan(&count); err != nil || count != 600 {
		t.Fatalf("trace count=%d err=%v", count, err)
	}
}

// TestImportFileFallsBackForPlainJSON verifies that a .json file that is not a
// scope export is handed to the transcript adapter rather than rejected.
func TestImportFileFallsBackForPlainJSON(t *testing.T) {
	memory := newMemory(t)
	path := writeTranscript(t, "notes.json", "{\"hello\": 1}\n")

	result, err := memory.ImportFile(path, nil, nil, "shared", false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	assertKeys(t, "import", result,
		"ok", "path", "session", "messages", "inserted", "offset", "total_lines", "size")
	if result["messages"] != 0 || result["inserted"] != 0 || result["total_lines"] != 1 {
		t.Fatalf("import = %#v", result)
	}
}

// TestImportFileJSONExportReplays verifies importing a full JSON scope export.
func TestImportFileJSONExportReplays(t *testing.T) {
	memory := newMemory(t)
	rememberOne(t, memory, "老张养了一只猫。", "老张", "养", map[string]any{"label": "猫"})
	path := filepath.Join(t.TempDir(), "export.json")
	payload, err := memory.Store.ExportScope("mem:test")
	if err != nil {
		t.Fatalf("legacy export fixture: %v", err)
	}
	payload["format"] = version.ExportFormat
	payload["scope"] = "mem:test"
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	fresh := newMemory(t)
	imported, err := fresh.ImportFile(path, nil, nil, "shared", true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	assertKeys(t, "import", imported, "ok", "scope", "inserted", "replay")
	replayed, ok := imported["replay"].(map[string]any)
	if !ok {
		t.Fatalf("import has no replay receipt: %#v", imported)
	}
	assertKeys(t, "replay", replayed,
		"ok", "scope", "processed", "written", "skipped", "anchor_failed", "resolve_failed", "llm_calls", "warnings")
	if imported["scope"] != "mem:test" {
		t.Fatalf("scope = %#v", imported["scope"])
	}
}
