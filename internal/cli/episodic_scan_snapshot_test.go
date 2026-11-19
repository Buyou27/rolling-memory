// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/episodic"
	"github.com/rocareer/rolling-memory/internal/episodicunit"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// scanSnapshotFixtureRows is one transcript that exercises the enumeration shapes
// the page loop must agree on between the streaming reader and the frozen parse:
// multi-segment turns, an explicitly completed turn, an unfinished turn, and one
// segment far beyond the token rung (the chunked `#<k>.<c>` pointers).
func scanSnapshotFixtureRows() []string {
	body := strings.Repeat("先确认缓存失效断言并复跑全部包，确认没有回归之后再汇报结论。", 600)
	return []string{
		`{"type":"user","timestamp":1794407222,"message":{"role":"user","content":[{"type":"text","text":"第一段，用户的话。"},{"type":"input_text","text":"第二段。"}]}}`,
		`{"type":"assistant","timestamp":1794407228,"message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"第一轮回答。"}]}}`,
		`{"type":"user","timestamp":1794407230,"message":{"role":"user","content":"第二轮问题。"}}`,
		`{"type":"assistant","timestamp":1794407235,"message":{"role":"assistant","content":[{"type":"text","text":"` + body + `"}]}}`,
		`{"type":"user","timestamp":1794407240,"message":{"role":"user","content":"第三轮问题。"}}`,
		`{"type":"assistant","timestamp":1794407241,"message":{"role":"assistant","content":"未完成的尾轮回答。"}}`,
	}
}

type scanUnitEmission struct {
	idx       int
	turnStart int
	turn      candidateEpisodicTurn
}

func collectScanUnits(t *testing.T, run func(visit func(int, int, candidateEpisodicTurn) error) (int, error)) ([]scanUnitEmission, int) {
	t.Helper()
	var emissions []scanUnitEmission
	total, err := run(func(idx int, turnStart int, turn candidateEpisodicTurn) error {
		emissions = append(emissions, scanUnitEmission{idx: idx, turnStart: turnStart, turn: turn})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return emissions, total
}

// TestEpisodicScanSnapshotUnitsMatchStreamingUnits pins the iteration parity of the
// two readers: identical unit ordinals, turn-start ordinals and candidates for a
// plain and a compressed source, and a resume point that restarts the frozen parse
// at the cursor's own turn with the ordinals the streaming reader would assign.
func TestEpisodicScanSnapshotUnitsMatchStreamingUnits(t *testing.T) {
	scope := "mem:scan-parity"
	rows := append(scanSnapshotFixtureRows(), "")
	raw := strings.Join(rows, "\n")
	dir := t.TempDir()
	plain := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(plain, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	compressed := filepath.Join(dir, "session.jsonl.zst")
	if err := os.WriteFile(compressed, encoder.EncodeAll([]byte(raw), nil), 0o600); err != nil {
		_ = encoder.Close()
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{plain, compressed} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			source := episodicScanSource{HarnessID: "claude-code", Kind: "jsonl", Path: path}
			streamed, streamedTotal := collectScanUnits(t, func(visit func(int, int, candidateEpisodicTurn) error) (int, error) {
				return streamEpisodicScanSourceFrom(context.Background(), source, scope, "", 0, visit)
			})
			snapshot, err := episodic.NewSourceSnapshot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			frozen, frozenTotal := collectScanUnits(t, func(visit func(int, int, candidateEpisodicTurn) error) (int, error) {
				return streamEpisodicScanSnapshotFrom(context.Background(), source, scope, snapshot, "", 0, visit)
			})
			if streamedTotal != frozenTotal || !reflect.DeepEqual(streamed, frozen) {
				t.Fatalf("frozen emissions diverge:\n--- streamed ---\n%+v\n--- frozen ---\n%+v", streamed, frozen)
			}
			if len(streamed) == 0 {
				t.Fatal("fixture produced no units")
			}
			// Resume at a mid-file turn: the frozen parse must re-emit the same unit
			// ordinals the full streaming enumeration assigned from that point on, so
			// the caller's `unitIdx < start` skip observes the identical remainder.
			resumeAt := -1
			for i, emission := range streamed {
				if strings.HasSuffix(emission.turn.TurnRef, ":2") {
					resumeAt = i
					break
				}
			}
			if resumeAt < 0 {
				t.Fatal("fixture has no third turn to resume at")
			}
			start := streamed[resumeAt]
			resumed, _ := collectScanUnits(t, func(visit func(int, int, candidateEpisodicTurn) error) (int, error) {
				return streamEpisodicScanSnapshotFrom(context.Background(), source, scope, snapshot, start.turn.TurnRef, start.turnStart, visit)
			})
			var want []scanUnitEmission
			for _, emission := range streamed {
				if emission.idx >= start.idx {
					want = append(want, emission)
				}
			}
			if !reflect.DeepEqual(want, resumed) {
				t.Fatalf("resumed emissions diverge:\n--- streamed ---\n%+v\n--- resumed ---\n%+v", want, resumed)
			}
		})
	}
}

// TestEpisodicScanSnapshotStalePageDoesNotAdvanceCursor pins the page contract of
// the frozen parse: when the source changes while the page runs, the page fails and
// the cursor does not advance past it — the same failure class as a source that
// changed mid-stream — and the next page re-observes the replacement content from
// the same checkpoint.
func TestEpisodicScanSnapshotStalePageDoesNotAdvanceCursor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeScanFixture(t, path, "original", 4)
	sources := []episodicScanSource{{HarnessID: "claude-code", Kind: "jsonl", Path: path}}
	seen := map[string]bool{}
	changed := false
	cursor, more, err := visitEpisodicSourcesVerified(context.Background(), sources, "mem:stale", episodicSourceCursor{}, 10, func(_ *episodic.SourceSnapshot, turn candidateEpisodicTurn) error {
		seen[turn.Text] = true
		if !changed {
			changed = true
			writeScanFixture(t, path, "replacement", 4)
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "changed during scan") || !more {
		t.Fatalf("stale page more=%v err=%v", more, err)
	}
	if cursor.Source != "" || !cursor.RetrySources {
		t.Fatalf("stale page advanced the cursor: %+v", cursor)
	}
	if !seen["[user] original 0"] {
		t.Fatalf("stale page visited nothing: %v", seen)
	}
	resumed := map[string]bool{}
	cursor, _, err = visitEpisodicSourcesVerified(context.Background(), sources, "mem:stale", cursor, 10, func(_ *episodic.SourceSnapshot, turn candidateEpisodicTurn) error {
		resumed[turn.Text] = true
		return nil
	})
	if err != nil || !resumed["[user] replacement 0"] || !resumed["[user] replacement 3"] {
		t.Fatalf("retry after stale page: cursor=%+v resumed=%v err=%v", cursor, resumed, err)
	}
}

// TestEpisodicScanSnapshotFallbackKeepsStreamingBehavior pins the fall-back the
// page loop applies when the frozen parse cannot be built (here: a corrupt
// compressed tail): the streaming reader produces the page, reports its error and
// leaves the cursor unadvanced, exactly as before the frozen parse existed.
func TestEpisodicScanSnapshotFallbackKeepsStreamingBehavior(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.jsonl.zst")
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	good := encoder.EncodeAll([]byte(`{"role":"user","message":{"role":"user","content":"question"}}`+"\n"), nil)
	_ = encoder.Close()
	if err := os.WriteFile(path, good[:len(good)-2], 0o600); err != nil {
		t.Fatal(err)
	}
	source := episodicScanSource{HarnessID: "claude-code", Kind: "jsonl", Path: path}
	if snapshot := episodicScanSourceSnapshot(context.Background(), source); snapshot != nil {
		snapshot.Close()
		t.Fatal("corrupt source produced a frozen parse")
	}
	_, streamErr := streamEpisodicScanSourceFrom(context.Background(), source, "mem:fallback", "", 0, func(int, int, candidateEpisodicTurn) error { return nil })
	if streamErr == nil {
		t.Fatal("streaming reader did not report the corrupt source")
	}
	visited := 0
	cursor, more, err := visitEpisodicSourcesVerified(context.Background(), []episodicScanSource{source}, "mem:fallback", episodicSourceCursor{}, 10, func(*episodic.SourceSnapshot, candidateEpisodicTurn) error {
		visited++
		return nil
	})
	if err == nil || !more || visited != 0 {
		t.Fatalf("fallback page more=%v visited=%d err=%v", more, visited, err)
	}
	if !strings.Contains(err.Error(), streamErr.Error()) {
		t.Fatalf("fallback page error %q does not carry the streaming error %q", err, streamErr)
	}
	if cursor.Source != "" || !cursor.RetrySources || !cursor.RetryCycle {
		t.Fatalf("fallback page advanced the cursor: %+v", cursor)
	}
}

// scanVaultRowDump renders every episodic row a scan save can touch, so the two
// ingest paths can be compared row by row.
func scanVaultRowDump(t *testing.T, db *sql.DB) string {
	t.Helper()
	dump := func(query string) string {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var builder strings.Builder
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			builder.WriteString(value)
			builder.WriteByte('\n')
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return builder.String()
	}
	return dump(`SELECT doc_id || '|' || layer || '|' || occurred_at || '|' || raw_text || '|' || assistant_id || '|' || incarnation_id || '|' || COALESCE(locus, '') FROM thin_vault_docs ORDER BY doc_id`) +
		dump(`SELECT doc_id || '|' || ref_pointer || '|' || hex(codec_key) || '|' || hex(payload) || '|' || hex(content_digest) || '|' || hex(payload_mac) FROM rm_episodic_index ORDER BY doc_id`) +
		dump(`SELECT turn_ref || '|' || doc_id || '|' || ref_pointer || '|' || hex(content_digest) || '|' || hex(source_mac) FROM rm_episodic_sources ORDER BY turn_ref, doc_id, ref_pointer`) +
		dump(`SELECT turn_ref || '|' || doc_id FROM rm_episodic_lineage ORDER BY turn_ref, doc_id`)
}

// TestEpisodicSourceScanSnapshotAndStreamingSaveIdenticalRows pins the end-to-end
// equivalence of the scan chain: the same source, ingested once through the
// snapshot-driven page with Verified saves and once through the streaming page with
// live saves, produces byte-identical documents, index rows, source records and
// lineage rows.
func TestEpisodicSourceScanSnapshotAndStreamingSaveIdenticalRows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
	scope := "mem:scan-save-parity"
	path := filepath.Join(home, "session.jsonl")
	raw := strings.Join(append(scanSnapshotFixtureRows(), ""), "\n")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := []episodicScanSource{{HarnessID: "claude-code", Kind: "jsonl", Path: path}}
	newMemory := func(name string) *facade.Memory {
		t.Helper()
		memory, err := facade.New(&config.Config{DB: filepath.Join(home, name), Scope: scope, Mode: "local"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { memory.Close() })
		// Both ingest runs share one vault key, so the keyed payload bytes, digests
		// and MACs are comparable row by row (a fresh database otherwise derives its
		// own random key, which would make every keyed byte differ by construction).
		if _, err := memory.Store.DB().Exec(`CREATE TABLE IF NOT EXISTS thin_vault_meta (k TEXT PRIMARY KEY, v BLOB NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		key := make([]byte, 32)
		for i := range key {
			key[i] = byte(i + 1)
		}
		if _, err := memory.Store.DB().Exec(`INSERT OR REPLACE INTO thin_vault_meta(k, v) VALUES ('user_secret_key', ?)`, key); err != nil {
			t.Fatal(err)
		}
		return memory
	}
	verifiedMemory := newMemory("verified.db")
	streamedMemory := newMemory("streamed.db")
	verifiedClient, err := verifiedMemory.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	streamedClient, err := streamedMemory.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	verifiedDeleted := verifiedClient.Vault.DeletedUnitSet("episodic")
	cursor, more, err := visitEpisodicSourcesVerified(context.Background(), sources, scope, episodicSourceCursor{}, 100, func(snapshot *episodic.SourceSnapshot, turn candidateEpisodicTurn) error {
		if snapshot == nil {
			t.Fatal("JSONL page produced a turn without its frozen parse")
		}
		return saveEpisodicSourceTurnVerified(context.Background(), verifiedMemory.Store.DB(), verifiedClient.Vault, scope, verifiedDeleted, turn, snapshot)
	})
	if err != nil || more {
		t.Fatalf("verified page cursor=%+v more=%v err=%v", cursor, more, err)
	}
	streamedDeleted := streamedClient.Vault.DeletedUnitSet("episodic")
	_, _, err = visitEpisodicSources(context.Background(), sources, scope, episodicSourceCursor{}, 100, func(turn candidateEpisodicTurn) error {
		return saveEpisodicSourceTurn(context.Background(), streamedMemory.Store.DB(), streamedClient.Vault, scope, streamedDeleted, turn)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := scanVaultRowDump(t, verifiedMemory.Store.DB()), scanVaultRowDump(t, streamedMemory.Store.DB()); got != want {
		t.Fatalf("scan rows diverge between the frozen-parse and streaming saves:\n--- verified ---\n%s\n--- streamed ---\n%s", got, want)
	}
	// The fixture's first turn is explicitly completed (three units), the tail turn
	// is not: only the completed units may land, and both paths must land the same.
	var count int
	if err := verifiedMemory.Store.DB().QueryRow(`SELECT COUNT(*) FROM thin_vault_docs WHERE layer = 'episodic'`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("verified scan wrote %d documents: %v", count, err)
	}
	t.Logf("scan parity: %d documents, rows identical between paths", count)
}

// TestEpisodicTurnSourceGrouping pins the reconcile save section's per-source
// bucketing: turns group by the JSONL path their pointer names, in first-appearance
// order and without disturbing the order inside a group, while non-JSONL pointers
// (zcode, empty) fall into the pathless group that keeps the live save path.
func TestEpisodicTurnSourceGrouping(t *testing.T) {
	turns := []candidateEpisodicTurn{
		{RefPointer: "@ref:jsonl:/a.jsonl:0#0"},
		{RefPointer: "@ref:zcode:s1:m1#0"},
		{RefPointer: "@ref:jsonl:/b.jsonl:2#1"},
		{RefPointer: ""},
		{RefPointer: "@ref:jsonl:/a.jsonl:1#0"},
		{RefPointer: "@ref:zcode:s1:m2#0"},
	}
	groups := groupEpisodicTurnsBySource(turns)
	got := make([]string, 0, len(groups))
	for _, group := range groups {
		got = append(got, group.path+"="+fmt.Sprint(len(group.turns)))
	}
	want := []string{"/a.jsonl=2", "=3", "/b.jsonl=1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("groups = %v, want %v", got, want)
	}
	first := groups[0].turns
	if first[0].RefPointer != "@ref:jsonl:/a.jsonl:0#0" || first[1].RefPointer != "@ref:jsonl:/a.jsonl:1#0" {
		t.Fatalf("group order changed: %+v", first)
	}
	for _, tc := range []struct{ ref, want string }{
		{"@ref:jsonl:/x.jsonl:3#0", "/x.jsonl"},
		{"@ref:jsonl:/x.jsonl:3", "/x.jsonl"},
		{"@ref:zcode:s:m#0", ""},
		{"", ""},
		{"inline text", ""},
	} {
		if got := episodicTurnSourcePath(tc.ref); got != tc.want {
			t.Fatalf("episodicTurnSourcePath(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

// TestEpisodicReconcileVerifiedSaveMatchesStreamingRows pins the reconcile chain's
// equivalence: the same transcripts, ingested once through executeReconcileSync with
// per-source Verified saves and once through the streaming reader with live saves,
// produce byte-identical documents, index rows, source records and lineage rows.
func TestEpisodicReconcileVerifiedSaveMatchesStreamingRows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
	t.Setenv("ZCODE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("ROLLING_MEMORY_HARNESS", "")
	scope := "mem:reconcile-parity"
	dir := filepath.Join(home, ".claude", "projects", "demo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("先确认缓存失效断言并复跑全部包，确认没有回归之后再汇报结论。", 600)
	newer := filepath.Join(dir, "newer.jsonl")
	if err := os.WriteFile(newer, []byte(strings.Join([]string{
		`{"type":"user","timestamp":1794407300,"message":{"role":"user","content":[{"type":"text","text":"新会话第一段。"},{"type":"input_text","text":"新会话第二段。"}]}}`,
		`{"type":"assistant","timestamp":1794407301,"message":{"role":"assistant","stop_reason":"end_turn","content":"新会话回答。"}}`,
		"",
	}, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(dir, "older.jsonl")
	if err := os.WriteFile(older, []byte(strings.Join([]string{
		`{"type":"user","timestamp":1794407000,"message":{"role":"user","content":"旧会话提问。"}}`,
		`{"type":"assistant","timestamp":1794407001,"message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"` + body + `"}]}}`,
		"",
	}, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	// The expected document count is derived from the frozen parse of the older
	// transcript alone: three units from the newer session plus every completed unit
	// of the older one (the long segment chunked under the shared rung).
	olderSnapshot, err := episodic.NewSourceSnapshot(older)
	if err != nil {
		t.Fatal(err)
	}
	expectedDocs := 3
	for turn := 0; turn < olderSnapshot.Turns(); turn++ {
		if !olderSnapshot.Completed(turn) {
			continue
		}
		segments, ok := olderSnapshot.Segments(turn)
		if !ok {
			t.Fatal("older turn unavailable")
		}
		for _, segment := range segments {
			expectedDocs += len(episodicunit.Chunk(episodicunit.Text(segment.Role, segment.Text), episodicunit.TokenBudget))
		}
	}
	if err := olderSnapshot.Close(); err != nil {
		t.Fatal(err)
	}
	newMemory := func(name string) *facade.Memory {
		t.Helper()
		memory, err := facade.New(&config.Config{DB: filepath.Join(home, name), Scope: scope, Mode: "local"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { memory.Close() })
		if _, err := memory.Store.DB().Exec(`CREATE TABLE IF NOT EXISTS thin_vault_meta (k TEXT PRIMARY KEY, v BLOB NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		key := make([]byte, 32)
		for i := range key {
			key[i] = byte(i + 1)
		}
		if _, err := memory.Store.DB().Exec(`INSERT OR REPLACE INTO thin_vault_meta(k, v) VALUES ('user_secret_key', ?)`, key); err != nil {
			t.Fatal(err)
		}
		return memory
	}
	reconciled := newMemory("reconciled.db")
	rep, err := executeReconcileSync(context.Background(), reconciled, "episodic", "claude-code", 100)
	if err != nil {
		t.Fatal(err)
	}
	if rep.EpisodicImportedFromHar != expectedDocs {
		t.Fatalf("reconcile imported %d units, want %d: %+v", rep.EpisodicImportedFromHar, expectedDocs, rep.Warnings)
	}
	streamed := newMemory("streamed.db")
	streamedClient, err := streamed.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	streamedDeleted := streamedClient.Vault.DeletedUnitSet("episodic")
	sources, err := discoverEpisodicScanSources(context.Background(), home, "claude-code")
	if err != nil || len(sources) != 2 {
		t.Fatalf("sources=%d err=%v", len(sources), err)
	}
	for _, source := range sources {
		_, _, err := visitEpisodicSources(context.Background(), []episodicScanSource{source}, scope, episodicSourceCursor{}, 100, func(turn candidateEpisodicTurn) error {
			return saveEpisodicSourceTurn(context.Background(), streamed.Store.DB(), streamedClient.Vault, scope, streamedDeleted, turn)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got, want := scanVaultRowDump(t, reconciled.Store.DB()), scanVaultRowDump(t, streamed.Store.DB()); got != want {
		t.Fatalf("reconcile rows diverge between the frozen-parse and streaming saves:\n--- reconcile ---\n%s\n--- streamed ---\n%s", got, want)
	}
	var count int
	if err := reconciled.Store.DB().QueryRow(`SELECT COUNT(*) FROM thin_vault_docs WHERE layer = 'episodic'`).Scan(&count); err != nil || count != expectedDocs {
		t.Fatalf("reconcile wrote %d documents: %v", count, err)
	}
	t.Logf("reconcile parity: %d documents, rows identical between paths", count)
}
