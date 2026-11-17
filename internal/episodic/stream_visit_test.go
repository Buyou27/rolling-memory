// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package episodic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestVisitJSONLTurnsParityAcrossScales(t *testing.T) {
	for _, count := range []int{0, 1, 100, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var source strings.Builder
			for i := 0; i < count; i++ {
				fmt.Fprintf(&source, `{"type":"user","message":{"role":"user","content":"你好 %d"}}`+"\n", i)
				source.WriteString(`{"type":"assistant","message":{"role":"assistant","content":"مرحبا"}}` + "\n")
			}
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(path, []byte(source.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			want, err := JSONLTurns(path)
			if err != nil {
				t.Fatal(err)
			}
			var got []JSONLTurn
			var closed []bool
			err = VisitJSONLTurns(context.Background(), path, func(turn JSONLTurn, complete bool) error {
				got = append(got, turn)
				closed = append(closed, complete)
				return nil
			})
			if err != nil || len(got) != len(want) {
				t.Fatalf("count=%d got=%d err=%v", count, len(got), err)
			}
			for i := range want {
				if !reflect.DeepEqual(got[i], want[i]) || closed[i] {
					t.Fatalf("turn %d: got=%+v want=%+v closed=%v", i, got[i], want[i], closed[i])
				}
			}
		})
	}
}

func TestVisitJSONLTurnsCancellationAndCallbackFailure(t *testing.T) {
	path := writeJSONL(t, []string{`{"role":"user","content":"one"}`, `{"role":"user","content":"two"}`})
	failure := errors.New("stop")
	calls := 0
	err := VisitJSONLTurns(context.Background(), path, func(JSONLTurn, bool) error {
		calls++
		return failure
	})
	if !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("callback calls=%d err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = VisitJSONLTurns(ctx, path, func(JSONLTurn, bool) error { t.Fatal("visited after cancellation"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scan: %v", err)
	}
}

func TestCompressedJSONLStreamingAndResolution(t *testing.T) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	var source strings.Builder
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&source, `{"type":"turn/start","data":{"turn":%d}}`+"\n", i)
		fmt.Fprintf(&source, `{"type":"user/message","time":1794407222466,"data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"question %d"}]}}`+"\n", i)
		source.WriteString(`{"type":"assistant/message","data":{"message":{"role":"assistant","content":[{"type":"text","text":"answer"}]}}}` + "\n")
		fmt.Fprintf(&source, `{"type":"turn/end","data":{"turn":%d,"reason":{"kind":"completed"}}}`+"\n", i)
	}
	compressed := encoder.EncodeAll([]byte(source.String()), nil)
	for _, suffix := range []string{".jsonl.zst", ".jsonl.zstd"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session"+suffix)
			if err := os.WriteFile(path, compressed, 0o600); err != nil {
				t.Fatal(err)
			}
			var seen int
			err := VisitJSONLTurns(context.Background(), path, func(turn JSONLTurn, complete bool) error {
				if turn.Index != seen || len(turn.Segments) != 2 || !complete {
					t.Fatalf("turn %d = %+v complete=%v", seen, turn, complete)
				}
				seen++
				return nil
			})
			if err != nil || seen != 10000 {
				t.Fatalf("seen=%d err=%v", seen, err)
			}
			for _, index := range []int{9999, 0, 5000} {
				segments, err := JSONLTurnSegments(path, index)
				if err != nil || len(segments) != 2 || segments[0].Text != fmt.Sprintf("question %d", index) || segments[1].Text != "answer" {
					t.Fatalf("resolved %d = %+v err=%v", index, segments, err)
				}
			}
		})
	}
}

func TestCompressedJSONLRejectsCorruptionAndLimits(t *testing.T) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	good := encoder.EncodeAll([]byte(`{"role":"user","content":"question"}`+"\n"), nil)
	for name, raw := range map[string][]byte{
		"truncated": good[:len(good)-2],
		"invalid":   []byte("not a zstd frame"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "broken.jsonl.zst")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := VisitJSONLTurns(context.Background(), path, func(JSONLTurn, bool) error { return nil }); err == nil {
				t.Fatal("corrupt stream accepted")
			}
			if _, err := JSONLTurnSegments(path, 0); err == nil {
				t.Fatal("corrupt stream resolved")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "long.jsonl.zstd")
	oversized := encoder.EncodeAll([]byte(strings.Repeat("x", MaxJSONLLineBytes+1)+"\n"), nil)
	if err := os.WriteFile(path, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VisitJSONLTurns(context.Background(), path, func(JSONLTurn, bool) error { return nil }); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized row err=%v", err)
	}
}

func TestCompletedTurnFollowedByActivityIsNotComplete(t *testing.T) {
	cases := map[string][]string{
		"codex": {
			`{"type":"event_msg","payload":{"type":"task_started","turn_id":"a"}}`,
			`{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"a"},"content":[{"type":"input_text","text":"question"}]}}`,
			`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"a"}}`,
			`{"type":"response_item","payload":{"type":"function_call","name":"shell"}}`,
			`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"unfinished"}]}}`,
		},
		"dsh": {
			`{"type":"turn/start","data":{"turn":1}}`,
			`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"question"}]}}`,
			`{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}`,
			`{"type":"tool/call","data":{"turn":1}}`,
			`{"type":"assistant/message","data":{"message":{"role":"assistant","content":[{"type":"text","text":"unfinished"}]}}}`,
		},
	}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeJSONL(t, rows)
			var states []bool
			if err := VisitJSONLTurns(context.Background(), path, func(_ JSONLTurn, complete bool) error {
				states = append(states, complete)
				return nil
			}); err != nil || !reflect.DeepEqual(states, []bool{false}) {
				t.Fatalf("states=%v err=%v", states, err)
			}
		})
	}
}

func TestDSHTurnCompletionCorrelationKeepsOrdinal(t *testing.T) {
	path := writeJSONL(t, []string{
		`{"type":"turn/start","data":{"turn":1}}`,
		`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"first"}]}}`,
		`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"second"}]}}`,
		`{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}`,
		`{"type":"turn/start","data":{"turn":2}}`,
		`{"type":"tool/call","data":{"turn":2}}`,
		`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"aborted"}]}}`,
		`{"type":"turn/end","data":{"turn":2,"reason":{"kind":"aborted"}}}`,
		`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"missing start"}]}}`,
		`{"type":"turn/start","data":{"turn":3}}`,
		`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"wrong id"}]}}`,
		`{"type":"turn/end","data":{"turn":4,"reason":{"kind":"completed"}}}`,
		`{"type":"turn/start","data":{"turn":5}}`,
		`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"duplicate"}]}}`,
		`{"type":"turn/end","data":{"turn":5,"reason":{"kind":"completed"}}}`,
		`{"type":"turn/start","data":{"turn":5}}`,
		`{"type":"turn/end","data":{"turn":5,"reason":{"kind":"completed"}}}`,
	})
	var states []bool
	var turns []JSONLTurn
	err := VisitJSONLTurns(context.Background(), path, func(turn JSONLTurn, complete bool) error {
		turns = append(turns, turn)
		states = append(states, complete)
		return nil
	})
	if err != nil || !reflect.DeepEqual(states, []bool{true, true, false, false, false, false}) {
		t.Fatalf("states=%v err=%v", states, err)
	}
	for i, want := range []string{"first", "second", "aborted", "missing start", "wrong id", "duplicate"} {
		segments, err := JSONLTurnSegments(path, i)
		if err != nil || len(segments) != 1 || segments[0].Text != want || !reflect.DeepEqual(segments, turns[i].Segments) {
			t.Fatalf("ordinal %d segments=%+v err=%v", i, segments, err)
		}
	}
}

func TestCodexTurnCompletionCorrelationKeepsOrdinal(t *testing.T) {
	path := writeJSONL(t, []string{
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"a"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"a"},"content":[{"type":"input_text","text":"first"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"a"},"content":[{"type":"input_text","text":"second"}]}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"a"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"b"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"shell"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"b"},"content":[{"type":"input_text","text":"third"}]}}`,
		`{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"b"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"c"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"missing id"}]}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"c"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"d"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"d"},"content":[{"type":"input_text","text":"wrong completion"}]}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"other"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"e"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"e"},"content":[{"type":"input_text","text":"duplicate"}]}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"e"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"e"}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"e"}}`,
	})
	var states []bool
	var turns []JSONLTurn
	err := VisitJSONLTurns(context.Background(), path, func(turn JSONLTurn, complete bool) error {
		turns = append(turns, turn)
		states = append(states, complete)
		return nil
	})
	if err != nil || !reflect.DeepEqual(states, []bool{true, true, false, false, false, false}) {
		t.Fatalf("states=%v err=%v", states, err)
	}
	for i, want := range []string{"first", "second", "third", "missing id", "wrong completion", "duplicate"} {
		segments, err := JSONLTurnSegments(path, i)
		if err != nil || len(segments) != 1 || segments[0].Text != want || !reflect.DeepEqual(segments, turns[i].Segments) {
			t.Fatalf("ordinal %d segments=%+v err=%v", i, segments, err)
		}
	}
}

func TestJSONLFollowingUserDoesNotProveSuccess(t *testing.T) {
	path := writeJSONL(t, []string{
		`{"role":"user","content":"first"}`,
		`{"type":"turn/end","data":{"reason":{"kind":"aborted"}}}`,
		`{"role":"user","content":"second"}`,
		`{"type":"event_msg","payload":{"type":"task_complete"}}`,
		`{"role":"user","content":"third"}`,
	})
	var states []bool
	if err := VisitJSONLTurns(context.Background(), path, func(_ JSONLTurn, complete bool) error {
		states = append(states, complete)
		return nil
	}); err != nil || !reflect.DeepEqual(states, []bool{false, true, false}) {
		t.Fatalf("states=%v err=%v", states, err)
	}
}

func TestJSONLUnterminatedCompletionWaitsForNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tail.jsonl")
	content := `{"role":"user","content":"question"}` + "\n" + `{"type":"event_msg","payload":{"type":"task_complete"}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var states []bool
	visit := func(_ JSONLTurn, complete bool) error { states = append(states, complete); return nil }
	if err := VisitJSONLTurns(context.Background(), path, visit); err != nil || !reflect.DeepEqual(states, []bool{false}) {
		t.Fatalf("incomplete states=%v err=%v", states, err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	states = nil
	if err := VisitJSONLTurns(context.Background(), path, visit); err != nil || !reflect.DeepEqual(states, []bool{true}) {
		t.Fatalf("completed states=%v err=%v", states, err)
	}
}

func TestVisitZCodeTurnsFromTransactionSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transaction.sqlite")
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	_, err = writer.Exec(`PRAGMA journal_mode=WAL;
		CREATE TABLE message(id TEXT, session_id TEXT, sequence INTEGER, time_created INTEGER, data TEXT);
		CREATE TABLE part(message_id TEXT, sequence INTEGER, data TEXT);
		INSERT INTO message VALUES ('m1','s',1,1,'{"role":"user"}'), ('a1','s',2,2,'{"role":"assistant","finish":"stop"}');
		INSERT INTO part VALUES ('m1',1,'{"type":"text","text":"question"}'), ('a1',1,'{"type":"text","text":"original"}');`)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var before string
	if err := tx.QueryRowContext(ctx, `SELECT json_extract(data, '$.finish') FROM message WHERE id = 'a1'`).Scan(&before); err != nil || before != "stop" {
		t.Fatalf("initial finish=%q err=%v", before, err)
	}
	if _, err := writer.Exec(`UPDATE message SET data = '{"role":"assistant","finish":"other"}' WHERE id = 'a1'; UPDATE part SET data = '{"type":"text","text":"changed"}' WHERE message_id = 'a1';`); err != nil {
		t.Fatal(err)
	}
	var turns []Turn
	err = VisitZCodeTurnsFrom(ctx, tx, "s", "m1", func(turn Turn, _ bool) error {
		turns = append(turns, turn)
		var finish string
		if err := tx.QueryRowContext(ctx, `SELECT json_extract(data, '$.finish') FROM message WHERE id = 'a1'`).Scan(&finish); err != nil {
			return err
		}
		if finish != "stop" {
			t.Errorf("snapshot finish=%q", finish)
		}
		return nil
	})
	if err != nil || len(turns) != 1 || len(turns[0].Segments) != 2 || turns[0].Segments[1].Text != "original" {
		t.Fatalf("transaction turns=%+v err=%v", turns, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	turns = nil
	err = VisitZCodeTurnsFrom(ctx, reader, "s", "m1", func(turn Turn, _ bool) error { turns = append(turns, turn); return nil })
	if err != nil || len(turns) != 1 || len(turns[0].Segments) != 2 || turns[0].Segments[1].Text != "changed" {
		t.Fatalf("fresh database turns=%+v err=%v", turns, err)
	}
}

func TestVisitZCodeTurnsParityAndWALSnapshot(t *testing.T) {
	db := openZCodeFixture(t)
	want, err := ZCodeSessionTurns(db, "s1")
	if err != nil {
		t.Fatal(err)
	}
	var got []Turn
	var closed []bool
	err = VisitZCodeTurns(context.Background(), db, "s1", func(turn Turn, complete bool) error {
		got = append(got, turn)
		closed = append(closed, complete)
		return nil
	})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%+v want=%+v err=%v", got, want, err)
	}
	for i := range closed {
		if closed[i] != (i+1 < len(closed)) {
			t.Fatalf("turn %d closed=%v", i, closed[i])
		}
	}

	path := filepath.Join(t.TempDir(), "wal.sqlite")
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	_, err = writer.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE message(id TEXT, session_id TEXT, sequence INTEGER, time_created INTEGER, data TEXT); CREATE TABLE part(message_id TEXT, sequence INTEGER, data TEXT); INSERT INTO message VALUES ('m1','s',1,1,'{"role":"user"}'), ('m2','s',2,2,'{"role":"user"}'); INSERT INTO part VALUES ('m1',1,'{"type":"text","text":"one"}'), ('m2',1,'{"type":"text","text":"two"}');`)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var ids []string
	err = VisitZCodeTurns(context.Background(), reader, "s", func(turn Turn, _ bool) error {
		ids = append(ids, turn.UserMsgID)
		if len(ids) == 1 {
			_, err := writer.Exec(`INSERT INTO message VALUES ('m3','s',3,3,'{"role":"user"}'); INSERT INTO part VALUES ('m3',1,'{"type":"text","text":"three"}');`)
			return err
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(ids, []string{"m1", "m2"}) {
		t.Fatalf("snapshot ids=%v err=%v", ids, err)
	}
	ids = nil
	err = VisitZCodeTurns(context.Background(), reader, "s", func(turn Turn, _ bool) error { ids = append(ids, turn.UserMsgID); return nil })
	if err != nil || !reflect.DeepEqual(ids, []string{"m1", "m2", "m3"}) {
		t.Fatalf("next snapshot ids=%v err=%v", ids, err)
	}
}
