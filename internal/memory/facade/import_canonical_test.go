// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/episodicunit"
)

// completedJSONLTurn is one completed turn in the shape every harness writes: the
// assistant row carries the explicit completion evidence the shared reader needs.
const completedJSONLTurn = `{"type":"user","message":{"role":"user","content":"老张养了一只猫。"}}` + "\n" +
	`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"好的，记下了。"}}` + "\n"

// TestImportJSONLCanonicalReceiptShape locks the canonical JSONL import: the
// receipt keeps the transcript keys (plus the incomplete-tail counter), no trace
// rows or cursor are written, and the documents carry the shared
// content-addressed ids with empty persona fields.
func TestImportJSONLCanonicalReceiptShape(t *testing.T) {
	memory := newMemory(t)
	path := writeTranscript(t, "session.jsonl", completedJSONLTurn)
	result, err := memory.ImportFile(path, nil, nil, "shared", true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	assertKeys(t, "import", result,
		"ok", "path", "session", "messages", "inserted", "offset", "total_lines", "size", "skipped_incomplete_tail", "replay")
	if result["ok"] != true || result["path"] != path || result["session"] != "session" {
		t.Fatalf("import = %#v", result)
	}
	if result["messages"] != 2 || result["inserted"] != 2 || result["offset"] != 2 || result["total_lines"] != 2 {
		t.Fatalf("import counts = %#v", result)
	}
	if size, ok := result["size"].(int64); !ok || size != int64(len(completedJSONLTurn)) {
		t.Fatalf("size = %#v", result["size"])
	}
	if result["skipped_incomplete_tail"] != 0 {
		t.Fatalf("skipped_incomplete_tail = %#v", result["skipped_incomplete_tail"])
	}
	replayed, ok := result["replay"].(map[string]any)
	if !ok {
		t.Fatalf("import has no replay receipt: %#v", result)
	}
	assertKeys(t, "replay", replayed,
		"ok", "scope", "processed", "written", "skipped", "anchor_failed", "resolve_failed", "llm_calls", "warnings")
	if replayed["processed"] != 0 {
		t.Fatalf("canonical import must queue no traces: %#v", replayed)
	}
	var traces, cursors int
	if err := memory.Store.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM trace), (SELECT COUNT(*) FROM transcript_cursor)`).Scan(&traces, &cursors); err != nil {
		t.Fatal(err)
	}
	if traces != 0 || cursors != 0 {
		t.Fatalf("canonical import wrote trace rows: traces=%d cursors=%d", traces, cursors)
	}
	for _, fixture := range []struct{ text, role string }{
		{"老张养了一只猫。", "user"},
		{"好的，记下了。", "assistant"},
	} {
		docID := episodicunit.DocID("mem:test", "["+fixture.role+"] "+fixture.text)
		var pointer, assistant, incarnation, locus string
		if err := memory.Store.DB().QueryRow(`SELECT raw_text, assistant_id, incarnation_id, locus FROM thin_vault_docs WHERE doc_id = ?`, docID).
			Scan(&pointer, &assistant, &incarnation, &locus); err != nil {
			t.Fatalf("document %s: %v", docID, err)
		}
		if !strings.HasPrefix(pointer, "@ref:jsonl:"+path+":0#") {
			t.Fatalf("pointer = %q", pointer)
		}
		if assistant != "" || incarnation != "" || locus != "" {
			t.Fatalf("file import created a persona: %q/%q/%q", assistant, incarnation, locus)
		}
	}
	// A repeat import converges on the same two documents.
	second, err := memory.ImportFile(path, nil, nil, "shared", false)
	if err != nil {
		t.Fatalf("repeat import: %v", err)
	}
	if second["messages"] != 2 || second["inserted"] != 0 {
		t.Fatalf("repeat import = %#v", second)
	}
	var docs int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&docs); err != nil || docs != 2 {
		t.Fatalf("docs = %d err=%v", docs, err)
	}
}

// TestDoctorReportsLegacyEpisodicIdentityReadOnly locks the doctor diagnostic:
// inline episodic rows whose id predates the content-addressed derivation are
// counted and reported, and the audit never rewrites or deletes them.
func TestDoctorReportsLegacyEpisodicIdentityReadOnly(t *testing.T) {
	memory := newMemory(t)
	clean, err := memory.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range clean["warnings"].([]any) {
		if strings.Contains(fmt.Sprint(warning), "legacy-identity") {
			t.Fatalf("fresh database reported a legacy identity backlog: %v", warning)
		}
	}
	if _, err := memory.ImportFile(writeTranscript(t, "session.jsonl", completedJSONLTurn), nil, nil, "shared", false); err != nil {
		t.Fatal(err)
	}
	withUnits, err := memory.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range withUnits["warnings"].([]any) {
		if strings.Contains(fmt.Sprint(warning), "legacy-identity") {
			t.Fatalf("canonical units reported as legacy identity: %v", warning)
		}
	}
	legacyID := "ep_" + strings.Repeat("0", 32)
	if _, err := memory.Store.DB().Exec(`INSERT INTO rm_docs(unit_id, layer, occurred_at, ingested_at, raw_text) VALUES (?, 'episodic', 1, 1, 'legacy inline witness')`, legacyID); err != nil {
		t.Fatal(err)
	}
	report, err := memory.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range report["warnings"].([]any) {
		if strings.Contains(fmt.Sprint(warning), "1 legacy-identity episodic units") {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy identity backlog not reported: %#v", report["warnings"])
	}
	var raw string
	var occurred, inserted int64
	if err := memory.Store.DB().QueryRow(`SELECT raw_text, occurred_at, ingested_at FROM rm_docs WHERE unit_id = ?`, legacyID).Scan(&raw, &occurred, &inserted); err != nil {
		t.Fatal(err)
	}
	if raw != "legacy inline witness" || occurred != 1 || inserted != 1 {
		t.Fatalf("doctor rewrote history: raw=%q occurred=%d inserted=%d", raw, occurred, inserted)
	}
	var docs int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&docs); err != nil || docs != 3 {
		t.Fatalf("doctor deleted history: docs=%d err=%v", docs, err)
	}
}
