// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package transcript_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/store"
	"github.com/rocareer/rolling-memory/internal/transcript"
)

const testNow = int64(1793102400) // 2026-10-27T12:00:00Z

func TestMain(m *testing.M) {
	// UTC stays for the whole process: restoring time.Local after m.Run would
	// write a global while goroutines outliving their test (vault snapshot
	// refreshes) may still read it, and the process exits immediately after.
	time.Local = time.UTC
	code := m.Run()
	os.Exit(code)
}

func freezeClock(t *testing.T) {
	t.Helper()
	restore := clock.SetForTest(time.Unix(testNow, 0), 1_000_000)
	t.Cleanup(restore)
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	freezeClock(t)
	directory := t.TempDir()
	dbPath := filepath.Join(directory, "memory.db")
	configPath := filepath.Join(directory, "config.toml")
	mode := "local"
	scope := "mem:test"
	cfg, err := config.Load(config.LoadOptions{
		Path: &configPath, DB: &dbPath, Mode: &mode, Scope: &scope,
		Env: map[string]string{},
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.Log = filepath.Join(directory, "state", "rolling-memory.log")
	st, err := store.Open(cfg)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func countRows(t *testing.T, st *store.Store, table string) int {
	t.Helper()
	var count int
	if err := st.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestJSONLRolesBlocksAndSkips(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	lines := []string{
		`{"type":"user","sessionId":"s1","message":{"role":"user","content":[{"type":"text","text":"我喜欢猫。"},{"type":"tool_use","name":"x"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"记住了。"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<rolling-memory-recall>old</rolling-memory-recall>"}]}}`,
		`{"role":"user","content":"hello"}`,
		`{"isMeta":true,"type":"user","message":{"role":"user","content":"meta"}}`,
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	writeFile(t, path, content)

	session := "s1"
	messages, nextOffset, total, _, _, err := transcript.ReadMessages(path, &session, 0, 500)
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want 3 (%v)", len(messages), messages)
	}
	if messages[0].Author != "user" || messages[0].Text != "我喜欢猫。" {
		t.Fatalf("first message = %+v", messages[0])
	}
	if messages[1].Author != "assistant" {
		t.Fatalf("second author = %q", messages[1].Author)
	}
	if messages[2].Text != "hello" {
		t.Fatalf("third text = %q", messages[2].Text)
	}
	if nextOffset != 6 || total != 6 {
		t.Fatalf("nextOffset/total = %d/%d, want 6/6", nextOffset, total)
	}
}

func TestImportIsIdempotentAndCursorMoves(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "s.jsonl")
	writeFile(t, path, `{"type":"user","message":{"role":"user","content":"你好。"}}`+"\n"+
		`{"type":"assistant","message":{"role":"assistant","content":"你好。"}}`+"\n")
	st := newStore(t)
	first, err := transcript.ImportTranscript(st, path, "mem:test", nil, "shared", 0, 500)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if first["inserted"] != 2 {
		t.Fatalf("first inserted = %v, want 2", first["inserted"])
	}
	second, err := transcript.ImportTranscript(st, path, "mem:test", nil, "shared", 0, 500)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second["inserted"] != 0 {
		t.Fatalf("second inserted = %v, want 0", second["inserted"])
	}
	offset, err := transcript.CurrentOffset(st, path, "s")
	if err != nil {
		t.Fatalf("current offset: %v", err)
	}
	if offset != 2 {
		t.Fatalf("offset = %d, want 2", offset)
	}
	writeFile(t, path, `{"role":"user","content":"短"}`+"\n")
	offset, err = transcript.CurrentOffset(st, path, "s")
	if err != nil {
		t.Fatalf("current offset after shrink: %v", err)
	}
	if offset != 0 {
		t.Fatalf("offset after shrink = %d, want 0", offset)
	}
}

func TestPlainText(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "notes.txt")
	writeFile(t, path, "第一行。\n\n第二行。\n")
	session := "notes"
	messages, nextOffset, total, _, _, err := transcript.ReadMessages(path, &session, 0, 500)
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Author != "user" {
		t.Fatalf("author = %q", messages[0].Author)
	}
	if nextOffset != 3 || total != 3 {
		t.Fatalf("nextOffset/total = %d/%d, want 3/3", nextOffset, total)
	}
}

func TestInvalidJSONLinesAreSkipped(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "bad.jsonl")
	writeFile(t, path, "{\"broken\"\n"+`{"role":"user","content":"ok"}`+"\n")
	session := "bad"
	messages, _, _, _, _, err := transcript.ReadMessages(path, &session, 0, 500)
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Text != "ok" {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestInjectedTagsAreStripped(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "x.jsonl")
	writeFile(t, path, `{"role":"user","content":"前文<rolling-memory-boot>旧记忆</rolling-memory-boot>后文"}`+"\n")
	session := "x"
	messages, _, _, _, _, err := transcript.ReadMessages(path, &session, 0, 500)
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Text != "前文 后文" {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestMissingTranscriptReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.jsonl")
	_, _, _, _, _, err := transcript.ReadMessages(path, nil, 0, 500)
	if err == nil {
		t.Fatal("expected an error for a missing transcript")
	}
	// The standard adapter raises FileNotFoundError(path); its message is the
	// path, not the os.Stat decoration.
	if err.Error() != path {
		t.Fatalf("missing transcript error = %q, want %q", err, path)
	}
}

func TestIncompleteJSONLLineDoesNotAdvanceCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.jsonl")
	first := `{"role":"user","content":"first"}` + "\n"
	partial := `{"role":"assistant","content":"sec`
	writeFile(t, path, first+partial)
	st := newStore(t)
	result, err := transcript.ImportTranscript(st, path, "mem:test", nil, "shared", 0, 500)
	if err != nil || result["messages"] != 1 || result["offset"] != 1 {
		t.Fatalf("partial import = %#v err=%v", result, err)
	}
	offset, err := transcript.CurrentOffset(st, path, "partial")
	if err != nil || offset != 1 {
		t.Fatalf("offset=%d err=%v", offset, err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`ond"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err = transcript.ImportTranscript(st, path, "mem:test", nil, "shared", offset, 500)
	if err != nil || result["messages"] != 1 || result["offset"] != 2 {
		t.Fatalf("completed import = %#v err=%v", result, err)
	}
}

func TestValidUnterminatedJSONLMessageIsImported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "complete.jsonl")
	writeFile(t, path, `{"role":"user","content":"one"}`+"\n"+`{"role":"assistant","content":"two"}`)
	messages, next, total, _, _, err := transcript.ReadMessages(path, nil, 0, 500)
	if err != nil || len(messages) != 2 || messages[1].Text != "two" || next != 2 || total != 2 {
		t.Fatalf("messages=%+v next=%d total=%d err=%v", messages, next, total, err)
	}
}

func TestJSONLToolUseNarrationExcluded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools.jsonl")
	writeFile(t, path, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"narration"},{"type":"tool_use","name":"shell"}]}}`+"\n"+
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer"}]}}`+"\n")
	messages, _, _, _, _, err := transcript.ReadMessages(path, nil, 0, 500)
	if err != nil || len(messages) != 1 || messages[0].Text != "answer" {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
}

func TestCompressedMessagesAndCorruptInput(t *testing.T) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	raw := `{"type":"user/message","time":1794407222466,"data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"question"}]}}` + "\n" +
		`{"type":"assistant/message","data":{"message":{"role":"assistant","content":[{"type":"text","text":"answer"}]}}}` + "\n"
	compressed := encoder.EncodeAll([]byte(raw), nil)
	path := filepath.Join(t.TempDir(), "session.jsonl.zstd")
	if err := os.WriteFile(path, compressed, 0o600); err != nil {
		t.Fatal(err)
	}
	messages, next, total, _, _, err := transcript.ReadMessages(path, nil, 0, 500)
	if err != nil || len(messages) != 2 || next != 2 || total != 2 || messages[0].Text != "question" || messages[0].OccurredAt != 1794407222 || messages[1].Text != "answer" {
		t.Fatalf("messages=%+v next=%d total=%d err=%v", messages, next, total, err)
	}
	if err := os.WriteFile(path, compressed[:len(compressed)-2], 0o600); err != nil {
		t.Fatal(err)
	}
	st := newStore(t)
	if _, err := transcript.ImportTranscript(st, path, "mem:test", nil, "shared", 0, 500); err == nil {
		t.Fatal("truncated compressed transcript accepted")
	}
	if count := countRows(t, st, "transcript_cursor"); count != 0 {
		t.Fatalf("corrupt transcript saved %d cursors", count)
	}
}

func TestUnknownAndKnownMessageTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "times.jsonl")
	writeFile(t, path, `{"role":"user","content":"unknown"}`+"\n"+
		`{"role":"assistant","created_at":"2026-11-18T00:00:00Z","content":"known"}`+"\n")
	messages, _, _, _, _, err := transcript.ReadMessages(path, nil, 0, 500)
	if err != nil || len(messages) != 2 || messages[0].OccurredAt != 0 || messages[1].OccurredAt != 1794960000 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	st := newStore(t)
	if _, err := transcript.ImportTranscript(st, path, "mem:test", nil, "shared", 0, 500); err != nil {
		t.Fatal(err)
	}
	var occurred int64
	if err := st.DB().QueryRow("SELECT occurred_at FROM trace WHERE text = ?", "unknown").Scan(&occurred); err != nil || occurred != 0 {
		t.Fatalf("unknown occurred_at=%d err=%v", occurred, err)
	}
}

func TestCursorColumnsRoundTrip(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "c.jsonl")
	writeFile(t, path, `{"role":"user","content":"cursor"}`+"\n")
	st := newStore(t)
	if _, err := transcript.ImportTranscript(st, path, "mem:test", nil, "shared", 0, 500); err != nil {
		t.Fatalf("import: %v", err)
	}
	cursor, found, err := transcript.CursorFor(st, path, "c")
	if err != nil || !found {
		t.Fatalf("cursor: found=%v err=%v", found, err)
	}
	if cursor["offset"] != int64(1) || cursor["session"] != "c" {
		t.Fatalf("cursor = %v", cursor)
	}
	if _, ok := cursor["mtime"].(float64); !ok {
		t.Fatalf("mtime is not a float: %T", cursor["mtime"])
	}
	if countRows(t, st, "transcript_cursor") != 1 {
		t.Fatalf("cursor rows = %d", countRows(t, st, "transcript_cursor"))
	}
}
