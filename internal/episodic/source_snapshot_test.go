// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package episodic

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

// snapshotFixtureRows builds a transcript that exercises every enumeration shape the
// snapshot must agree with: multi-part user turns, filtered tool narration, an
// explicit completion row, and one segment far beyond the token rung.
func snapshotFixtureRows(body string) []string {
	return []string{
		`{"type":"user","timestamp":1794407222,"message":{"role":"user","content":[{"type":"text","text":"第一段，用户的话。"},{"type":"input_text","text":"第二段。"}]}}`,
		`{"type":"assistant","timestamp":1794407225,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"internal"},{"type":"text","text":"先取证。"}]}}`,
		`{"type":"assistant","timestamp":1794407226,"message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"text","text":"tool step narration"}]}}`,
		`{"type":"assistant","timestamp":1794407228,"message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"第一轮回答。"}]}}`,
		`{"type":"user","timestamp":1794407230,"message":{"role":"user","content":"第二轮问题。"}}`,
		`{"type":"assistant","timestamp":1794407235,"message":{"role":"assistant","content":[{"type":"text","text":"` + body + `"}]}}`,
		`{"type":"user","timestamp":1794407240,"message":{"role":"user","content":"第三轮问题。"}}`,
		`{"type":"assistant","timestamp":1794407241,"message":{"role":"assistant","stop_reason":"end_turn","content":"第三轮回答。"}}`,
	}
}

func encodeZSTD(t *testing.T, raw string) []byte {
	t.Helper()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	return encoder.EncodeAll([]byte(raw), nil)
}

// TestSourceSnapshotResolveAgreesWithStreamingScan pins the parity contract of the
// frozen parse: turn count, segment text, turn time, completion verdict and digest
// are exactly the streaming scan's, for plain and compressed sources alike, and
// every `#<k>` / `#<k>.<c>` pointer addresses the segment and chunk the ingest
// built — resolved in reverse order, so the parse is random-access.
func TestSourceSnapshotResolveAgreesWithStreamingScan(t *testing.T) {
	sentence := "先确认缓存失效断言并复跑全部包，确认没有回归之后再汇报结论。"
	body := strings.Repeat(sentence, 600)
	if tokens := tokenzr.Count(Text(RoleAssistant, body)); tokens <= TokenBudget {
		t.Fatalf("fixture body must outgrow the rung: %d tokens", tokens)
	}
	rows := snapshotFixtureRows(body)
	raw := strings.Join(append(rows, ""), "\n")
	dir := t.TempDir()
	plain := filepath.Join(dir, "rollout-7.jsonl")
	if err := os.WriteFile(plain, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	compressed := filepath.Join(dir, "rollout-7.jsonl.zst")
	if err := os.WriteFile(compressed, encodeZSTD(t, raw), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{plain, compressed} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			snap, err := NewSourceSnapshot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer snap.Close()
			wantTurns, err := JSONLTurns(path)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Turns() != len(wantTurns) {
				t.Fatalf("turns = %d, want %d", snap.Turns(), len(wantTurns))
			}
			rawBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wantDigest := sha256.Sum256(rawBytes)
			if digest, ok := snap.Digest(); !ok || digest != wantDigest {
				t.Fatalf("digest = %x ok=%v, want %x (raw bytes on disk)", digest, ok, wantDigest)
			}
			if session, ok := snap.SessionID(); !ok || session != "rollout-7" {
				t.Fatalf("session = %q ok=%v", session, ok)
			}
			var closed []bool
			if err := VisitJSONLTurns(context.Background(), path, func(_ JSONLTurn, complete bool) error {
				closed = append(closed, complete)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if len(closed) != len(wantTurns) {
				t.Fatalf("streaming scan turns = %d, want %d", len(closed), len(wantTurns))
			}
			// A returned segment slice is a copy: mutating it must not touch the parse.
			tamper, ok := snap.Segments(0)
			if !ok {
				t.Fatal("turn 0 unavailable")
			}
			tamper[0].Text = "tampered"
			again, ok := snap.Segments(0)
			if !ok || again[0].Text != wantTurns[0].Segments[0].Text {
				t.Fatalf("frozen parse was mutated: %+v", again)
			}

			chunkedSegments := 0
			for i := len(wantTurns) - 1; i >= 0; i-- {
				if snap.Completed(i) != closed[i] {
					t.Fatalf("turn %d completed = %v, want %v", i, snap.Completed(i), closed[i])
				}
				if at, ok := snap.OccurredAt(i); !ok || at != wantTurns[i].OccurredAt {
					t.Fatalf("turn %d occurred_at = %d ok=%v, want %d", i, at, ok, wantTurns[i].OccurredAt)
				}
				segments, ok := snap.Segments(i)
				if !ok || !reflect.DeepEqual(segments, wantTurns[i].Segments) {
					t.Fatalf("turn %d segments diverge: %+v", i, segments)
				}
				for k := len(segments) - 1; k >= 0; k-- {
					ref := fmt.Sprintf("@ref:jsonl:%s:%d#%d", path, i, k)
					rendered, ok := snap.Resolve(ref)
					want := Text(segments[k].Role, segments[k].Text)
					if !ok || rendered != want {
						t.Fatalf("%s = %q ok=%v, want %q", ref, rendered, ok, want)
					}
					chunks := Chunk(rendered, TokenBudget)
					if len(chunks) > 1 {
						chunkedSegments++
					}
					for c, chunk := range chunks {
						got, ok := snap.Resolve(fmt.Sprintf("%s.%d", ref, c))
						if !ok || got != chunk {
							t.Fatalf("%s.%d = %q ok=%v, want %q", ref, c, got, ok, chunk)
						}
					}
					if _, ok := snap.Resolve(fmt.Sprintf("%s.%d", ref, len(chunks))); ok {
						t.Fatalf("%s.%d resolved out of range", ref, len(chunks))
					}
				}
			}
			if chunkedSegments == 0 {
				t.Fatal("fixture must contain a segment above the token rung")
			}
			if text, ok := snap.Resolve(fmt.Sprintf("@ref:jsonl:%s:0#0", path)); !ok || text != "[user] 第一段，用户的话。" {
				t.Fatalf("first segment = %q ok=%v", text, ok)
			}
		})
	}
}

// TestSourceSnapshotCompletionParityAcrossHarnessShapes pins that the deferred
// completion verdict of the single pass equals the streaming scan's for the plain,
// Codex and DSH shapes, including the invalidated-completion cases.
func TestSourceSnapshotCompletionParityAcrossHarnessShapes(t *testing.T) {
	cases := map[string][]string{
		"plain": {
			`{"role":"user","content":"first"}`,
			`{"type":"turn/end","data":{"reason":{"kind":"aborted"}}}`,
			`{"role":"user","content":"second"}`,
			`{"type":"event_msg","payload":{"type":"task_complete"}}`,
			`{"role":"user","content":"third"}`,
		},
		"codex": {
			`{"type":"event_msg","payload":{"type":"task_started","turn_id":"a"}}`,
			`{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"a"},"content":[{"type":"input_text","text":"first"}]}}`,
			`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"a"}}`,
			`{"type":"response_item","payload":{"type":"function_call","name":"shell"}}`,
			`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"unfinished"}]}}`,
			`{"type":"event_msg","payload":{"type":"task_started","turn_id":"b"}}`,
			`{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"b"},"content":[{"type":"input_text","text":"second"}]}}`,
			`{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"b"}}`,
			`{"type":"event_msg","payload":{"type":"task_started","turn_id":"c"}}`,
			`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"third"}]}}`,
			`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"c"}}`,
		},
		"dsh": {
			`{"type":"turn/start","data":{"turn":1}}`,
			`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"first"}]}}`,
			`{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}`,
			`{"type":"turn/start","data":{"turn":2}}`,
			`{"type":"tool/call","data":{"turn":2}}`,
			`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"second"}]}}`,
			`{"type":"turn/end","data":{"turn":2,"reason":{"kind":"completed"}}}`,
			`{"type":"turn/start","data":{"turn":3}}`,
			`{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"third"}]}}`,
			`{"type":"turn/end","data":{"turn":3,"reason":{"kind":"aborted"}}}`,
		},
	}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			raw := strings.Join(append(rows, ""), "\n")
			dir := t.TempDir()
			plain := writeJSONL(t, rows)
			compressed := filepath.Join(dir, "session.jsonl.zstd")
			if err := os.WriteFile(compressed, encodeZSTD(t, raw), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{plain, compressed} {
				var closed []bool
				if err := VisitJSONLTurns(context.Background(), path, func(_ JSONLTurn, complete bool) error {
					closed = append(closed, complete)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				snap, err := NewSourceSnapshot(path)
				if err != nil {
					t.Fatal(err)
				}
				if snap.Turns() != len(closed) {
					t.Fatalf("%s: turns = %d, want %d", path, snap.Turns(), len(closed))
				}
				for i := range closed {
					if snap.Completed(i) != closed[i] {
						t.Fatalf("%s: turn %d completed = %v, want %v", path, i, snap.Completed(i), closed[i])
					}
				}
				snap.Close()
			}
		})
	}
	t.Run("unterminated tail", func(t *testing.T) {
		// A completion row without its newline is a partial write: neither path may
		// call the last turn complete, so an ingest gate cannot write it early.
		content := `{"role":"user","content":"question"}` + "\n" + `{"type":"event_msg","payload":{"type":"task_complete"}}`
		path := filepath.Join(t.TempDir(), "session.jsonl")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		var closed []bool
		if err := VisitJSONLTurns(context.Background(), path, func(_ JSONLTurn, complete bool) error {
			closed = append(closed, complete)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		snap, err := NewSourceSnapshot(path)
		if err != nil {
			t.Fatal(err)
		}
		defer snap.Close()
		if snap.Turns() != len(closed) || snap.Completed(0) != closed[0] {
			t.Fatalf("turns=%d completed=%v, streaming=%v", snap.Turns(), snap.Completed(0), closed)
		}
		if snap.Completed(0) {
			t.Fatal("unterminated completion row treated as a completed turn")
		}
		if text, ok := snap.Resolve("@ref:jsonl:" + path + ":0#0"); !ok || text != "[user] question" {
			t.Fatalf("frozen tail text = %q ok=%v", text, ok)
		}
	})
}

// TestSourceSnapshotRejectsForeignAndOutOfRangeRefs pins the fail-closed pointer
// surface: another path, another scheme, a whole-turn pointer, a malformed suffix
// and every out-of-range index report false instead of guessing.
func TestSourceSnapshotRejectsForeignAndOutOfRangeRefs(t *testing.T) {
	path := writeJSONL(t, []string{
		`{"role":"user","content":"one"}`,
		`{"role":"assistant","content":"two"}`,
	})
	snap, err := NewSourceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	other := filepath.Join(filepath.Dir(path), "other.jsonl")
	if err := os.WriteFile(other, []byte(`{"role":"user","content":"other"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{
		"",
		"plain text",
		"@ref:jsonl:" + other + ":0#0",
		"@ref:zcode:s1:m1#0",
		"@ref:jsonl:" + path + ":0",
		"@ref:jsonl:" + path + ":0#",
		"@ref:jsonl:" + path + ":0#x",
		"@ref:jsonl:" + path + ":0#-1",
		"@ref:jsonl:" + path + ":0#2",
		"@ref:jsonl:" + path + ":1#0",
		"@ref:jsonl:" + path + ":0#0.1",
		"@ref:jsonl:" + path + ":x#0",
		"@ref:jsonl::0#0",
	} {
		if text, ok := snap.Resolve(ref); ok {
			t.Fatalf("pointer %q resolved to %q", ref, text)
		}
	}
	// The `#<k>.` trailing-dot form is the ingest grammar's "no chunk suffix": it
	// resolves the whole segment, exactly as the live resolver reads it.
	if text, ok := snap.Resolve("@ref:jsonl:" + path + ":0#0."); !ok || text != "[user] one" {
		t.Fatalf("trailing-dot pointer = %q ok=%v", text, ok)
	}
}

// TestSourceSnapshotStaleAndFrozenSourceSemantics pins that a snapshot answers its
// construction-time state, while Stale reports every kind of source change —
// including an in-place rewrite that restored size and mtime, which no stat can
// see and only the digest catches.
func TestSourceSnapshotStaleAndFrozenSourceSemantics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-1.jsonl")
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
	snap, err := NewSourceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	if snap.Stale() {
		t.Fatal("fresh snapshot reported stale")
	}
	if !digestsMatch(t, snap, first) {
		t.Fatal("digest does not match the source bytes it froze")
	}
	if digestsMatch(t, snap, second) {
		t.Fatal("digest matched content that was never read")
	}
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if !snap.Stale() {
		t.Fatal("in-place rewrite with restored size and mtime must report stale")
	}
	if text, ok := snap.Resolve("@ref:jsonl:" + path + ":0#0"); !ok || text != "[user] alpha" {
		t.Fatalf("frozen text = %q ok=%v (a snapshot answers its construction-time state)", text, ok)
	}
	fresh, err := NewSourceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if fresh.Stale() {
		t.Fatal("snapshot of the rewritten source reported stale")
	}
	if text, ok := fresh.Resolve("@ref:jsonl:" + path + ":0#0"); !ok || text != "[user] bravo" {
		t.Fatalf("rewritten text = %q ok=%v", text, ok)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"role":"assistant","content":"three"}` + "\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	if !snap.Stale() {
		t.Fatal("appended source must report stale")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !snap.Stale() {
		t.Fatal("removed source must report stale")
	}
}

func digestsMatch(t *testing.T, snap *SourceSnapshot, raw string) bool {
	t.Helper()
	digest, ok := snap.Digest()
	if !ok {
		t.Fatal("digest unavailable")
	}
	return digest == sha256.Sum256([]byte(raw))
}

// TestSourceSnapshotRejectsCorruptOversizedAndCancelledSources pins the constructor
// failure classes the caller branches on: a corrupt compressed tail, a source above
// the decompressed bound (the fall-back trigger), a cancelled construction and a
// non-regular path all fail visibly and never return a snapshot.
func TestSourceSnapshotRejectsCorruptOversizedAndCancelledSources(t *testing.T) {
	dir := t.TempDir()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	good := encoder.EncodeAll([]byte(`{"role":"user","content":"question"}`+"\n"), nil)
	broken := filepath.Join(dir, "broken.jsonl.zst")
	if err := os.WriteFile(broken, good[:len(good)-2], 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := NewSourceSnapshot(broken)
	if err == nil || snap != nil {
		t.Fatalf("truncated compressed tail accepted: %v", err)
	}
	if errors.Is(err, ErrSourceSnapshotTooLarge) {
		t.Fatalf("truncated tail classified as oversize: %v", err)
	}
	t.Logf("truncated compressed tail error: %v", err)

	oversized := filepath.Join(dir, "oversized.jsonl")
	var builder strings.Builder
	for builder.Len() < 8*1024 {
		builder.WriteString(`{"role":"user","content":"padding padding padding"}` + "\n")
	}
	if err := os.WriteFile(oversized, []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newSourceSnapshot(context.Background(), oversized, 1<<10); !errors.Is(err, ErrSourceSnapshotTooLarge) {
		t.Fatalf("bounded construction err = %v, want ErrSourceSnapshotTooLarge", err)
	}
	if maxSourceSnapshotBytes != 256<<20 {
		t.Fatalf("documented bound changed: %d", maxSourceSnapshotBytes)
	}
	if snap, err := NewSourceSnapshot(oversized); err != nil {
		t.Fatalf("source under the default bound rejected: %v", err)
	} else {
		snap.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewSourceSnapshotContext(ctx, oversized); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled construction err = %v, want context.Canceled", err)
	}
	if _, err := NewSourceSnapshot(dir); err == nil {
		t.Fatal("non-regular path accepted")
	}
	if _, err := NewSourceSnapshot(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Fatal("missing path accepted")
	}
}

// TestSourceSnapshotZeroValueAndCloseReject pins the rejection surface: a zero value
// and a closed snapshot answer nothing — not 0 turns, not empty text, not a zero
// digest — and Close is idempotent.
func TestSourceSnapshotZeroValueAndCloseReject(t *testing.T) {
	path := writeJSONL(t, []string{`{"role":"user","content":"one"}`, `{"role":"assistant","content":"two"}`})
	ref := "@ref:jsonl:" + path + ":0#0"
	assertRejects := func(t *testing.T, snap *SourceSnapshot) {
		t.Helper()
		if snap.Turns() != -1 {
			t.Fatalf("Turns = %d, want -1", snap.Turns())
		}
		if text, ok := snap.Resolve(ref); ok {
			t.Fatalf("Resolve = %q", text)
		}
		if _, ok := snap.Segments(0); ok {
			t.Fatal("Segments answered")
		}
		if _, ok := snap.OccurredAt(0); ok {
			t.Fatal("OccurredAt answered")
		}
		if snap.Completed(0) {
			t.Fatal("Completed answered")
		}
		if _, ok := snap.Digest(); ok {
			t.Fatal("Digest answered")
		}
		if _, ok := snap.SessionID(); ok {
			t.Fatal("SessionID answered")
		}
		if !snap.Stale() {
			t.Fatal("Stale must report an unusable snapshot")
		}
		if err := snap.Close(); err != nil {
			t.Fatalf("Close = %v", err)
		}
	}
	var zero SourceSnapshot
	assertRejects(t, &zero)
	var nilSnapshot *SourceSnapshot
	assertRejects(t, nilSnapshot)

	snap, err := NewSourceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Turns() != 1 {
		t.Fatalf("turns = %d", snap.Turns())
	}
	if text, ok := snap.Resolve(ref); !ok || text != "[user] one" {
		t.Fatalf("live snapshot = %q ok=%v", text, ok)
	}
	if err := snap.Close(); err != nil {
		t.Fatal(err)
	}
	if err := snap.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	assertRejects(t, snap)
}

// TestSourceSnapshotTenThousandTurnScale records the construction cost of one
// 10k-turn compressed transcript (the scale the per-pointer cache stops covering).
// It logs readings instead of asserting a performance budget.
func TestSourceSnapshotTenThousandTurnScale(t *testing.T) {
	const turns = 10000
	var source strings.Builder
	for i := 0; i < turns; i++ {
		fmt.Fprintf(&source, `{"type":"turn/start","data":{"turn":%d}}`+"\n", i)
		fmt.Fprintf(&source, `{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"question %d"}]}}`+"\n", i)
		source.WriteString(`{"type":"assistant/message","data":{"message":{"role":"assistant","content":[{"type":"text","text":"answer"}]}}}` + "\n")
		fmt.Fprintf(&source, `{"type":"turn/end","data":{"turn":%d,"reason":{"kind":"completed"}}}`+"\n", i)
	}
	payload := encodeZSTD(t, source.String())
	path := filepath.Join(t.TempDir(), "session.jsonl.zstd")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	started := time.Now()
	snap, err := NewSourceSnapshot(path)
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	t.Logf("source snapshot scale: turns=%d compressed=%dB decompressed=%dB construct=%s heap_delta=%dB total_alloc=%dB",
		turns, len(payload), source.Len(), elapsed,
		int64(after.HeapAlloc)-int64(before.HeapAlloc), after.TotalAlloc-before.TotalAlloc)
	if snap.Turns() != turns {
		t.Fatalf("turns = %d, want %d", snap.Turns(), turns)
	}
	if digest, ok := snap.Digest(); !ok || digest != sha256.Sum256(payload) {
		t.Fatalf("digest = %x ok=%v, want the compressed bytes on disk", digest, ok)
	}
	if session, ok := snap.SessionID(); !ok || session != "session" {
		t.Fatalf("session = %q ok=%v", session, ok)
	}
	var closed []bool
	if err := VisitJSONLTurns(context.Background(), path, func(_ JSONLTurn, complete bool) error {
		closed = append(closed, complete)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(closed) != turns {
		t.Fatalf("streaming scan turns = %d, want %d", len(closed), turns)
	}
	for i := range closed {
		if !closed[i] || !snap.Completed(i) {
			t.Fatalf("turn %d completed = %v, want true (streaming %v)", i, snap.Completed(i), closed[i])
		}
	}
	for _, index := range []int{0, 5000, turns - 1} {
		ref := fmt.Sprintf("@ref:jsonl:%s:%d#0", path, index)
		if text, ok := snap.Resolve(ref); !ok || text != fmt.Sprintf("[user] question %d", index) {
			t.Fatalf("%s = %q ok=%v", ref, text, ok)
		}
	}
	// Query readings for the same run: resolving every pointer of a 10k-turn source
	// from the frozen parse, where the per-pointer streaming path re-reads the whole
	// transcript each time.
	const queries = 1000
	started = time.Now()
	for i := 0; i < queries; i++ {
		index := (i * 7919) % turns
		ref := fmt.Sprintf("@ref:jsonl:%s:%d#1", path, index)
		if text, ok := snap.Resolve(ref); !ok || text != "[assistant] answer" {
			t.Fatalf("%s = %q ok=%v", ref, text, ok)
		}
	}
	t.Logf("source snapshot queries: n=%d total=%s per_query=%s", queries, time.Since(started), time.Since(started)/queries)
}
