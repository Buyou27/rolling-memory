// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package episodic

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJSONLStreamResolverAgreesAcrossScales(t *testing.T) {
	for _, count := range []int{1, 100, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var source strings.Builder
			for i := 0; i < count; i++ {
				fmt.Fprintf(&source, `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"你好 %d"},{"type":"input_text","text":"مرحبا 日本語"}]}}`+"\n", i)
				source.WriteString(`{"payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ответ Καλημέρα"},{"type":"tool","text":"ignored"}]}}` + "\n")
				source.WriteString(`{"isMeta":true,"type":"user","message":{"content":"ignored"}}` + "\n")
			}
			path := writeJSONL(t, []string{source.String()})
			turns, err := JSONLTurns(path)
			if err != nil || len(turns) != count {
				t.Fatalf("turns=%d err=%v", len(turns), err)
			}
			for _, index := range []int{0, count / 2, count - 1} {
				got, err := JSONLTurnSegments(path, index)
				if err != nil || !reflect.DeepEqual(got, turns[index].Segments) {
					t.Fatalf("turn %d: got=%+v want=%+v err=%v", index, got, turns[index].Segments, err)
				}
			}
			for _, index := range []int{-1, count} {
				if _, err := JSONLTurnSegments(path, index); err == nil {
					t.Fatalf("turn %d unexpectedly resolved", index)
				}
			}
		})
	}
}

func TestJSONLStreamResolverLiveChangesAndLongLine(t *testing.T) {
	long := strings.Repeat("世界مرحبا", 20000)
	path := writeJSONL(t, []string{`{"type":"user","message":{"role":"user","content":"` + long + `"}}`})
	first, err := JSONLTurnSegments(path, 0)
	if err != nil || len(first) != 1 || first[0].Text != long {
		t.Fatalf("long line failed: segments=%d err=%v", len(first), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"type":"assistant","message":{"role":"assistant","content":"追加"}}` + "\n" + `{"type":"user","message":{"role":"user","content":"next"}}` + "\n")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	all, err := JSONLTurns(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range all {
		got, err := JSONLTurnSegments(path, i)
		if err != nil || !reflect.DeepEqual(got, all[i].Segments) {
			t.Fatalf("turn %d after append: got=%+v err=%v", i, got, err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"replacement"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := JSONLTurnSegments(path, 0)
	if err != nil || len(got) != 1 || got[0].Text != "replacement" {
		t.Fatalf("replacement: %+v %v", got, err)
	}
	if _, err := JSONLTurnSegments(path, 1); err == nil {
		t.Fatal("truncated turn unexpectedly resolved")
	}
}

func TestJSONLResolverCacheRejectsSameSizeAndMtimeRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.jsonl")
	padding := strings.Repeat(" ", 300*1024)
	first := `{"role":"user","content":"alpha"}` + padding + "\n"
	second := `{"role":"user","content":"bravo"}` + padding + "\n"
	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := JSONLTurnSegments(path, 0)
	if err != nil || len(got) != 1 || got[0].Text != "alpha" {
		t.Fatalf("first=%+v err=%v", got, err)
	}
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	got, err = JSONLTurnSegments(path, 0)
	if err != nil || len(got) != 1 || got[0].Text != "bravo" {
		t.Fatalf("rewritten=%+v err=%v", got, err)
	}
}

func TestJSONLResolverDoesNotCacheOversizedDecodedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.jsonl")
	content := strings.Repeat("x", maxJSONLTurnsCacheEntryBytes+1)
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"`+content+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	segments, err := JSONLTurnSegments(path, 0)
	if err != nil || len(segments) != 1 || segments[0].Text != content {
		t.Fatalf("segments=%d err=%v", len(segments), err)
	}
	if _, ok := jsonlTurnsCache.Load(path); ok {
		t.Fatal("oversized decoded transcript retained in cache")
	}
}

func TestJSONLUnterminatedValidMessageResolves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"role":"user","content":"question"}` + "\n" + `{"role":"assistant","content":"answer"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	turns, err := JSONLTurns(path)
	if err != nil || len(turns) != 1 || len(turns[0].Segments) != 2 || turns[0].Segments[1].Text != "answer" {
		t.Fatalf("turns=%+v err=%v", turns, err)
	}
	resolved, err := JSONLTurnSegments(path, 0)
	if err != nil || !reflect.DeepEqual(resolved, turns[0].Segments) {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
}

func TestJSONLStreamResolverHarnessShapesAndInvalidRows(t *testing.T) {
	path := writeJSONL(t, []string{
		"",
		`not json`,
		`{"type":"user","message":`,
		`{"role":"human","content":"top level"}`,
		`{"role":"agent","content":"top level reply"}`,
		`{"type":"user","message":{"role":"user","content":"message envelope"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","text":"ignored"},{"type":"text","text":"reply"}]}}`,
		`{"payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"payload envelope"}]}}`,
		`{"payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"response"}]}}`,
		`{"isCompactSummary":true,"type":"user","message":{"content":"ignored"}}`,
		`{"type":"user","content":"type fallback"}`,
		`{"type":"assistant","content":"type response"}`,
	})
	turns, err := JSONLTurns(path)
	if err != nil || len(turns) != 4 {
		t.Fatalf("turns=%+v err=%v", turns, err)
	}
	for index, turn := range turns {
		got, err := JSONLTurnSegments(path, index)
		if err != nil || !reflect.DeepEqual(got, turn.Segments) {
			t.Fatalf("turn %d: got=%+v want=%+v err=%v", index, got, turn.Segments, err)
		}
	}
}

func TestZCodeResolverMultipleUserParts(t *testing.T) {
	db := openZCodeFixture(t)
	if _, err := db.Exec(`INSERT INTO part(id, message_id, session_id, sequence, data) VALUES ('extra', 'm1', 's1', 2, '{"type":"text","text":"مرحبا 日本語"}')`); err != nil {
		t.Fatal(err)
	}
	turns, err := ZCodeSessionTurns(db, "s1")
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range turns {
		got, err := ZCodeTurnSegments(db, "s1", turn.UserMsgID)
		if err != nil || !reflect.DeepEqual(got, turn.Segments) {
			t.Fatalf("turn %s: got=%+v want=%+v err=%v", turn.UserMsgID, got, turn.Segments, err)
		}
	}
}
