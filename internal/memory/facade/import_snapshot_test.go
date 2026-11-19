// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/rocareer/rolling-memory/internal/episodicunit"
)

// TestImportJSONLCompressedSourceUsesFrozenParse locks the canonical import on a
// compressed transcript: the frozen parse drives the import, the receipt keeps the
// transcript shape, and the stored pointers name the compressed file.
func TestImportJSONLCompressedSourceUsesFrozenParse(t *testing.T) {
	memory := newMemory(t)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	payload := encoder.EncodeAll([]byte(completedJSONLTurn), nil)
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl.zst")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := memory.ImportFile(path, nil, nil, "shared", false)
	if err != nil {
		t.Fatal(err)
	}
	if result["messages"] != 2 || result["inserted"] != 2 || result["offset"] != 2 || result["total_lines"] != 2 {
		t.Fatalf("compressed import receipt = %#v", result)
	}
	for _, fixture := range []struct{ text, role string }{
		{"老张养了一只猫。", "user"},
		{"好的，记下了。", "assistant"},
	} {
		docID := episodicunit.DocID("mem:test", "["+fixture.role+"] "+fixture.text)
		var pointer string
		if err := memory.Store.DB().QueryRow(`SELECT raw_text FROM rm_units WHERE doc_id = ?`, docID).Scan(&pointer); err != nil {
			t.Fatalf("document %s: %v", docID, err)
		}
		if !strings.HasPrefix(pointer, "@ref:jsonl:"+path+":0#") {
			t.Fatalf("pointer = %q", pointer)
		}
	}
}

// TestImportJSONLSourceChangedDuringImportIsReported locks the frozen-parse import
// contract for a file that changes while the import runs: the import reports the
// change instead of pretending it read a stable file, the units already written
// (one transaction per unit) stay in the vault, and a retry against the settled
// file converges — the late incomplete tail is counted, not written.
func TestImportJSONLSourceChangedDuringImportIsReported(t *testing.T) {
	memory := newMemory(t)
	var source strings.Builder
	const turns = 60
	for i := 0; i < turns; i++ {
		fmt.Fprintf(&source, `{"type":"user","message":{"role":"user","content":"question %d"}}`+"\n", i)
		fmt.Fprintf(&source, `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"answer %d"}}`+"\n", i)
	}
	path := writeTranscript(t, "session.jsonl", source.String())
	appended := make(chan struct{})
	go func() {
		defer close(appended)
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			var count int
			if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_units WHERE layer = 'episodic'`).Scan(&count); err == nil && count > 0 {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return
		}
		_, _ = file.WriteString(`{"type":"user","message":{"role":"user","content":"late question"}}` + "\n")
		_ = file.Close()
	}()
	result, err := memory.ImportFile(path, nil, nil, "shared", false)
	<-appended
	if err == nil || !strings.Contains(err.Error(), "changed during import") {
		t.Fatalf("import of a changing source err = %v receipt = %#v", err, result)
	}
	var written int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_units WHERE layer = 'episodic'`).Scan(&written); err != nil {
		t.Fatal(err)
	}
	if written != turns*2 {
		t.Fatalf("units written before the change were lost: %d of %d", written, turns*2)
	}
	retry, err := memory.ImportFile(path, nil, nil, "shared", false)
	if err != nil {
		t.Fatalf("retry against the settled file: %v", err)
	}
	if retry["messages"] != turns*2 || retry["inserted"] != 0 || retry["skipped_incomplete_tail"] != 1 {
		t.Fatalf("retry receipt = %#v", retry)
	}
}

// TestImportJSONLCorruptCompressedSourceFailsClosed locks the fall-back the import
// keeps for sources the frozen parse cannot build: the streaming reader reports the
// corruption and the import fails without writing a partial set.
func TestImportJSONLCorruptCompressedSourceFailsClosed(t *testing.T) {
	memory := newMemory(t)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	good := encoder.EncodeAll([]byte(completedJSONLTurn), nil)
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "broken.jsonl.zst")
	if err := os.WriteFile(path, good[:len(good)-2], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ImportFile(path, nil, nil, "shared", false); err == nil {
		t.Fatal("corrupt compressed source imported without an error")
	}
	var docs int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM rm_units`).Scan(&docs); err != nil || docs != 0 {
		t.Fatalf("corrupt import wrote %d documents: %v", docs, err)
	}
}
