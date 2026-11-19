// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rocareer/rolling-memory/internal/episodicunit"
	_ "modernc.org/sqlite"
)

// TestEpisodicReanchorRelocatedUnitCrossTurn pins the cross-turn re-anchor contract:
// a row whose stored coordinate drifted away from its sealed content (enumeration
// drift between imports; cross-turn coordinates are recorded as alternate sources and
// never re-pointed in place) is re-pointed to the coordinate where its sealed digest
// still resolves, and nothing is written when the content identity does not agree.
func TestEpisodicReanchorRelocatedUnitCrossTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	writeTranscript := func(lines string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	line := func(role, text string) string {
		return `{"type":"` + role + `","message":{"role":"` + role + `","content":"` + text + `"}}` + "\n"
	}
	turn := func(user, assistant string) string { return line("user", user) + line("assistant", assistant) }
	ref := func(index int, segment int) string {
		return "@ref:jsonl:" + path + ":" + strconv.Itoa(index) + "#" + strconv.Itoa(segment)
	}
	turnRef := func(index int) string { return "@ref:jsonl:" + path + ":" + strconv.Itoa(index) }

	// Epoch 1: the unit is sealed at turn 1, segment 1.
	writeTranscript(turn("继续。", "回答二。") + turn("开始。", "回答一。"))
	sealedRef := ref(1, 1)
	sealedText, err := ResolveEpisodicPointerLive(sealedRef)
	if err != nil || sealedText != "[assistant] 回答一。" {
		t.Fatalf("sealed turn text = %q err=%v", sealedText, err)
	}

	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(dir, "vault.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("reanchor-cross-turn-secret"))
	if err != nil {
		t.Fatal(err)
	}
	scope := "mem:self"
	docID := episodicunit.DocID(scope, sealedText)
	const occurredAt = int64(7)
	pkt, _ := v.PackBlindPacket(docID, sealedText, occurredAt)
	if err := v.SaveLocalEpisodicPointerIndexed(docID, occurredAt, sealedRef, turnRef(1), sealedText, pkt, IngestResponse{}); err != nil {
		t.Fatal(err)
	}

	// Epoch 2: enumeration drift moves the same speech to turn 0. A cross-turn save is
	// recorded as an alternate source only, so the row keeps its stale coordinate.
	writeTranscript(turn("开始。", "回答一。") + turn("继续。", "回答二。"))
	movedRef := ref(0, 1)
	movedText, err := ResolveEpisodicPointerLive(movedRef)
	if err != nil || movedText != sealedText {
		t.Fatalf("moved turn text = %q err=%v", movedText, err)
	}
	if err := v.SaveLocalEpisodicPointerIndexed(docID, occurredAt, movedRef, turnRef(0), movedText, pkt, IngestResponse{}); err != nil {
		t.Fatal(err)
	}
	if raw := readRawText(t, db, docID); raw != sealedRef {
		t.Fatalf("cross-turn save must not re-point the row, raw_text = %q", raw)
	}

	// The stored coordinate now addresses other speech; the sealed content only
	// resolves from the recorded alternate coordinate.
	if got, err := ResolveEpisodicPointerLive(sealedRef); err != nil || got == sealedText {
		t.Fatalf("stale coordinate should address drifted speech, got %q err=%v", got, err)
	}
	if got, err := v.LoadVerifiedEpisodicPointer(docID, sealedRef); err == nil || got != "" {
		t.Fatalf("stale coordinate must fail closed, got %q err=%v", got, err)
	}
	text, actualRef, err := v.LoadVerifiedEpisodicPointerSource(docID, sealedRef)
	if err != nil || text != sealedText || actualRef != movedRef {
		t.Fatalf("relocated source = %q at %q err=%v, want %q at %q", text, actualRef, err, sealedText, movedRef)
	}

	// Probe: re-anchorability is reported without writing.
	probed, err := v.ReanchorRelocatedEpisodicUnit(scope, docID, sealedRef, occurredAt, false)
	if err != nil || probed != movedRef {
		t.Fatalf("probe = %q err=%v, want %q", probed, err, movedRef)
	}
	if raw := readRawText(t, db, docID); raw != sealedRef {
		t.Fatalf("probe wrote the row: raw_text = %q", raw)
	}

	// Apply: the row is re-pointed and re-sealed; both the primary reader and the
	// hydration path then resolve the same unit at its current coordinate.
	applied, err := v.ReanchorRelocatedEpisodicUnit(scope, docID, sealedRef, occurredAt, true)
	if err != nil || applied != movedRef {
		t.Fatalf("apply = %q err=%v, want %q", applied, err, movedRef)
	}
	if raw := readRawText(t, db, docID); raw != movedRef {
		t.Fatalf("raw_text = %q, want %q", raw, movedRef)
	}
	if got, err := v.LoadVerifiedEpisodicPointer(docID, movedRef); err != nil || got != sealedText {
		t.Fatalf("re-anchored pointer = %q err=%v", got, err)
	}
	settled, err := v.ReanchorRelocatedEpisodicUnit(scope, docID, movedRef, occurredAt, true)
	if err != nil || settled != "" {
		t.Fatalf("settled row must report no move, got %q err=%v", settled, err)
	}

	// Identity mismatch: the same relocation shape, but the row id does not match the
	// content identity — re-anchor must refuse instead of adopting the content.
	foreignID := "ep_000000000000000000000000"
	foreignPkt, _ := v.PackBlindPacket(foreignID, sealedText, occurredAt)
	writeTranscript(turn("继续。", "回答二。") + turn("开始。", "回答一。"))
	if err := v.SaveLocalEpisodicPointerIndexed(foreignID, occurredAt, sealedRef, turnRef(1), sealedText, foreignPkt, IngestResponse{}); err != nil {
		t.Fatal(err)
	}
	writeTranscript(turn("开始。", "回答一。") + turn("继续。", "回答二。"))
	if err := v.SaveLocalEpisodicPointerIndexed(foreignID, occurredAt, movedRef, turnRef(0), sealedText, foreignPkt, IngestResponse{}); err != nil {
		t.Fatal(err)
	}
	if raw := readRawText(t, db, foreignID); raw != sealedRef {
		t.Fatalf("foreign row raw_text = %q, want %q", raw, sealedRef)
	}
	if _, err := v.ReanchorRelocatedEpisodicUnit(scope, foreignID, sealedRef, occurredAt, false); err == nil {
		t.Fatal("re-anchor must refuse a row whose content identity does not match its id")
	}
	if raw := readRawText(t, db, foreignID); raw != sealedRef {
		t.Fatalf("refused re-anchor wrote the row: raw_text = %q", raw)
	}
}

func readRawText(t *testing.T, db *sql.DB, docID string) string {
	t.Helper()
	var raw string
	if err := db.QueryRow(`SELECT raw_text FROM rm_units WHERE doc_id = ?`, docID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
