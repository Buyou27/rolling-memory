// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/episodic"
	"github.com/rocareer/rolling-memory/internal/episodicunit"
)

// snapshotVault builds one vault with a fixed key and identity so two vaults can be
// compared row by row after ingesting the same source through different paths.
func snapshotVault(t *testing.T, key string) (*Vault, *sql.DB) {
	t.Helper()
	db := migrationTestDB(t)
	vault, err := NewVault(db, []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	vault.SetIdentity(VaultIdentity{AssistantID: "claude-code", IncarnationID: "rollout-1", Locus: "Repo"})
	return vault, db
}

// snapshotSourceRows writes one transcript that exercises every enumeration shape:
// plain turns, a tool-narration turn that carries no completion, and one segment
// far beyond the token rung (the chunked `#<k>.<c>` pointers).
func snapshotSourceRows(body string) []string {
	return []string{
		`{"type":"user","timestamp":1794407222,"message":{"role":"user","content":[{"type":"text","text":"第一段，用户的话。"},{"type":"input_text","text":"第二段。"}]}}`,
		`{"type":"assistant","timestamp":1794407225,"message":{"role":"assistant","content":[{"type":"text","text":"先取证。"}]}}`,
		`{"type":"assistant","timestamp":1794407226,"message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"text","text":"tool step narration"}]}}`,
		`{"type":"assistant","timestamp":1794407228,"message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"第一轮回答。"}]}}`,
		`{"type":"user","timestamp":1794407230,"message":{"role":"user","content":"第二轮问题。"}}`,
		`{"type":"assistant","timestamp":1794407235,"message":{"role":"assistant","content":[{"type":"text","text":"` + body + `"}]}}`,
	}
}

// snapshotSourceUnits derives the ingest units of one source the way the CLI scan
// and the canonical import do, so the tests feed the saves the exact units a page
// would emit.
func snapshotSourceUnits(t *testing.T, snapshot *episodic.SourceSnapshot, scope, path string) []episodicunit.Unit {
	t.Helper()
	var units []episodicunit.Unit
	for turn := 0; turn < snapshot.Turns(); turn++ {
		segments, ok := snapshot.Segments(turn)
		if !ok {
			t.Fatalf("turn %d unavailable", turn)
		}
		occurredAt, _ := snapshot.OccurredAt(turn)
		completed := snapshot.Completed(turn)
		prefix := fmt.Sprintf("@ref:jsonl:%s:%d", path, turn)
		for _, segment := range segments {
			units = append(units, episodicunit.SegmentUnits(scope, prefix, segment, occurredAt, completed)...)
		}
	}
	return units
}

func scalarStrings(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

// vaultRowDump renders every episodic row a save can touch — document, index
// (payload/digest/MAC as hex), source record and lineage — so a parity failure
// names the first diverging row instead of just a count.
func vaultRowDump(t *testing.T, db *sql.DB) string {
	t.Helper()
	docs := scalarStrings(t, db, `SELECT doc_id || '|' || layer || '|' || occurred_at || '|' || raw_text || '|' || sentences_json || '|' || address_uri || '|' || coord64 || '|' || waveform_bar || '|' || doc_macro_wave || '|' || COALESCE(kept_indices_json, '') || '|' || assistant_id || '|' || incarnation_id || '|' || COALESCE(locus, '') FROM thin_vault_docs ORDER BY doc_id`)
	indexes := scalarStrings(t, db, `SELECT doc_id || '|' || ref_pointer || '|' || hex(codec_key) || '|' || hex(payload) || '|' || hex(content_digest) || '|' || hex(payload_mac) FROM thin_vault_episodic_index ORDER BY doc_id`)
	sources := scalarStrings(t, db, `SELECT turn_ref || '|' || doc_id || '|' || ref_pointer || '|' || hex(content_digest) || '|' || hex(source_mac) FROM thin_vault_episodic_sources ORDER BY turn_ref, doc_id, ref_pointer`)
	lineage := scalarStrings(t, db, `SELECT turn_ref || '|' || doc_id FROM thin_vault_episodic_lineage ORDER BY turn_ref, doc_id`)
	return strings.Join(append(append(append(docs, indexes...), sources...), lineage...), "\n")
}

// TestEpisodicVerifiedSaveMatchesLiveSave pins the equivalence contract of the
// frozen-parse save: for one source and one unit list, the Verified path produces
// byte-identical documents, index rows (payload, digest and MAC), source records
// and lineage rows to the live path, including the chunked `#<k>.<c>` pointers of a
// segment above the token rung.
func TestEpisodicVerifiedSaveMatchesLiveSave(t *testing.T) {
	scope := "mem:verified"
	body := strings.Repeat("先确认缓存失效断言并复跑全部包，确认没有回归之后再汇报结论。", 600)
	path := filepath.Join(t.TempDir(), "rollout-1.jsonl")
	raw := strings.Join(append(snapshotSourceRows(body), ""), "\n")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := episodic.NewSourceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	units := snapshotSourceUnits(t, snapshot, scope, path)
	if len(units) < 6 {
		t.Fatalf("fixture produced %d units", len(units))
	}
	chunked := 0
	for _, unit := range units {
		if strings.Contains(unit.RefPointer, ".") {
			chunked++
		}
	}
	if chunked == 0 {
		t.Fatal("fixture must carry a chunked pointer")
	}

	liveVault, liveDB := snapshotVault(t, "verified-parity-key")
	verifiedVault, verifiedDB := snapshotVault(t, "verified-parity-key")
	for _, unit := range units {
		pkt, _ := liveVault.PackBlindPacket(unit.DocID, unit.Text, unit.OccurredAt)
		if err := liveVault.SaveLocalEpisodicPointerIndexed(unit.DocID, unit.OccurredAt, unit.RefPointer, unit.TurnRef, unit.Text, pkt, IngestResponse{}); err != nil {
			t.Fatalf("live save %s: %v", unit.RefPointer, err)
		}
		if err := verifiedVault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, unit.DocID, unit.OccurredAt, unit.RefPointer, unit.TurnRef, unit.Text, IngestResponse{}); err != nil {
			t.Fatalf("verified save %s: %v", unit.RefPointer, err)
		}
	}
	if got, want := vaultRowDump(t, verifiedDB), vaultRowDump(t, liveDB); got != want {
		t.Fatalf("verified rows diverge from the live rows:\n--- verified ---\n%s\n--- live ---\n%s", got, want)
	}
	// The verified path must also converge on the live path for the second write of
	// one unit: the update branches (existing identity, repoint, source record) are
	// shared and must stay equivalent.
	pkt, _ := liveVault.PackBlindPacket(units[0].DocID, units[0].Text, units[0].OccurredAt)
	if err := liveVault.SaveLocalEpisodicPointerIndexed(units[0].DocID, units[0].OccurredAt, units[0].RefPointer, units[0].TurnRef, units[0].Text, pkt, IngestResponse{}); err != nil {
		t.Fatalf("live repeat save: %v", err)
	}
	if err := verifiedVault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, units[0].DocID, units[0].OccurredAt, units[0].RefPointer, units[0].TurnRef, units[0].Text, IngestResponse{}); err != nil {
		t.Fatalf("verified repeat save: %v", err)
	}
	if got, want := vaultRowDump(t, verifiedDB), vaultRowDump(t, liveDB); got != want {
		t.Fatalf("repeat-save rows diverge:\n--- verified ---\n%s\n--- live ---\n%s", got, want)
	}
}

// TestEpisodicVerifiedSaveFailsClosedWithoutSnapshotAnswer pins the rejection
// surface of the frozen-parse save: a ref the snapshot cannot address is an
// explicit source error even when the live resolver could answer it — the snapshot
// path never falls back to re-reading the transcript — while a nil snapshot keeps
// the live behavior byte for byte.
func TestEpisodicVerifiedSaveFailsClosedWithoutSnapshotAnswer(t *testing.T) {
	scope := "mem:verified-fail"
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jsonl")
	second := filepath.Join(dir, "second.jsonl")
	body := `{"role":"user","content":"q"}` + "\n" + `{"role":"assistant","content":"alpha"}` + "\n"
	if err := os.WriteFile(first, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(`{"role":"user","content":"q"}`+"\n"+`{"role":"assistant","content":"alpha"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := episodic.NewSourceSnapshot(first)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	vault, db := snapshotVault(t, "verified-fail-key")
	text := "[assistant] alpha"
	docID := episodicunit.DocID(scope, text)

	foreign := "@ref:jsonl:" + second + ":0#1"
	if err := vault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, docID, 1, foreign, "@ref:jsonl:"+second+":0", text, IngestResponse{}); !errors.Is(err, ErrEpisodicIndexItem) {
		t.Fatalf("foreign ref err = %v, want ErrEpisodicIndexItem (no live fallback)", err)
	}
	// The same ref would resolve live, so a silent fallback would have written it.
	if resolved, err := ResolveEpisodicPointerLive(foreign); err != nil || resolved != text {
		t.Fatalf("fixture ref must resolve live: %q err=%v", resolved, err)
	}
	if err := vault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, docID, 1, "@ref:zcode:s:m#0", "", text, IngestResponse{}); !errors.Is(err, ErrEpisodicIndexItem) {
		t.Fatalf("foreign scheme err = %v, want ErrEpisodicIndexItem", err)
	}
	own := "@ref:jsonl:" + first + ":0#1"
	if err := vault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, docID, 1, own, "@ref:jsonl:"+first+":0", "[assistant] other", IngestResponse{}); !errors.Is(err, ErrEpisodicIndexItem) {
		t.Fatalf("mismatched text err = %v, want ErrEpisodicIndexItem", err)
	}
	if err := vault.SaveLocalEpisodicPointerIndexedVerified("mem:another-scope", snapshot, docID, 1, own, "@ref:jsonl:"+first+":0", text, IngestResponse{}); !errors.Is(err, ErrEpisodicIndexItem) {
		t.Fatalf("scope identity mismatch err = %v, want ErrEpisodicIndexItem", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM thin_vault_docs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected saves wrote %d documents: %v", count, err)
	}
	closed, cErr := episodic.NewSourceSnapshot(first)
	if cErr != nil {
		t.Fatal(cErr)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := vault.SaveLocalEpisodicPointerIndexedVerified(scope, closed, docID, 1, own, "@ref:jsonl:"+first+":0", text, IngestResponse{}); !errors.Is(err, ErrEpisodicIndexItem) {
		t.Fatalf("closed snapshot err = %v, want ErrEpisodicIndexItem", err)
	}
	// A nil snapshot is the documented live behavior: the same save succeeds and
	// produces the same rows as the live API.
	pkt, _ := vault.PackBlindPacket(docID, text, 1)
	if err := vault.SaveLocalEpisodicPointerIndexed(docID, 1, own, "@ref:jsonl:"+first+":0", text, pkt, IngestResponse{}); err != nil {
		t.Fatal(err)
	}
	baseline := vaultRowDump(t, db)
	otherVault, otherDB := snapshotVault(t, "verified-fail-key")
	if err := otherVault.SaveLocalEpisodicPointerIndexedVerified(scope, nil, docID, 1, own, "@ref:jsonl:"+first+":0", text, IngestResponse{}); err != nil {
		t.Fatalf("nil snapshot save: %v", err)
	}
	if got := vaultRowDump(t, otherDB); got != baseline {
		t.Fatalf("nil-snapshot rows diverge from the live API:\n--- nil ---\n%s\n--- live ---\n%s", got, baseline)
	}
}

// TestEpisodicVerifiedSaveHonorsTombstones pins that the frozen-parse save keeps
// the tombstone transaction check: a unit deleted by user intent (or concurrently
// deleted between the page's parse and the save) is never resurrected.
func TestEpisodicVerifiedSaveHonorsTombstones(t *testing.T) {
	scope := "mem:verified-tombstone"
	path := filepath.Join(t.TempDir(), "source.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"tombstone fixture"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := episodic.NewSourceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	vault, db := snapshotVault(t, "verified-tombstone-key")
	text := "[user] tombstone fixture"
	docID := episodicunit.DocID(scope, text)
	ref := "@ref:jsonl:" + path + ":0#0"
	if err := vault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, docID, 1, ref, "@ref:jsonl:"+path+":0", text, IngestResponse{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO thin_vault_deleted_units(unit_id, layer, deleted_at) VALUES (?, 'episodic', 1)`, docID); err != nil {
		t.Fatal(err)
	}
	if err := vault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, docID, 1, ref, "@ref:jsonl:"+path+":0", text, IngestResponse{}); !errors.Is(err, ErrEpisodicUnitDeleted) {
		t.Fatalf("tombstoned verified save err = %v, want ErrEpisodicUnitDeleted", err)
	}
	// The ensure verification runs only when the index row is (re)built: with the
	// row present the read-only fast path answers without a source check.
	if _, err := db.Exec(`DELETE FROM thin_vault_episodic_index WHERE doc_id = ?`, docID); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.EnsureLocalEpisodicIndexVerified(scope, snapshot, docID, ref, text); !errors.Is(err, ErrEpisodicUnitDeleted) {
		t.Fatalf("tombstoned verified ensure err = %v, want ErrEpisodicUnitDeleted", err)
	}
}

// TestEpisodicEnsureVerifiedMatchesLiveEnsure pins the ensure seam: the frozen
// parse answers when it owns the ref, a ref it cannot address (another source's
// pointer) keeps the live check, and both paths converge on the same index rows.
func TestEpisodicEnsureVerifiedMatchesLiveEnsure(t *testing.T) {
	scope := "mem:verified-ensure"
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	other := filepath.Join(dir, "other.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"ensure fixture"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte(`{"type":"user","message":{"role":"user","content":"other fixture"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := episodic.NewSourceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	text := "[user] ensure fixture"
	docID := episodicunit.DocID(scope, text)
	ownRef := "@ref:jsonl:" + path + ":0#0"

	seed := func(t *testing.T, pointer string) (*Vault, *sql.DB) {
		t.Helper()
		vault, db := snapshotVault(t, "verified-ensure-key")
		if err := vault.SaveLocalEpisodicPointer(docID, 1, pointer, "", IngestResponse{}); err != nil {
			t.Fatal(err)
		}
		return vault, db
	}
	liveVault, liveDB := seed(t, ownRef)
	verifiedVault, verifiedDB := seed(t, ownRef)
	created, err := liveVault.EnsureLocalEpisodicIndex(scope, docID, ownRef, text)
	if err != nil || !created {
		t.Fatalf("live ensure: created=%v err=%v", created, err)
	}
	created, err = verifiedVault.EnsureLocalEpisodicIndexVerified(scope, snapshot, docID, ownRef, text)
	if err != nil || !created {
		t.Fatalf("verified ensure: created=%v err=%v", created, err)
	}
	if got, want := vaultRowDump(t, verifiedDB), vaultRowDump(t, liveDB); got != want {
		t.Fatalf("ensure rows diverge:\n--- verified ---\n%s\n--- live ---\n%s", got, want)
	}
	// A pointer of another source cannot be answered by this snapshot: the seam
	// falls back to the live resolver, exactly like the old ensure path, and both
	// agree on the outcome (including the identity mismatch refusal for foreign
	// speech).
	otherText := "[user] other fixture"
	otherID := episodicunit.DocID(scope, otherText)
	foreignRef := "@ref:jsonl:" + other + ":0#0"
	foreignLive, _ := seed(t, foreignRef)
	foreignVerified, _ := seed(t, foreignRef)
	_, liveErr := foreignLive.EnsureLocalEpisodicIndex(scope, otherID, foreignRef, otherText)
	_, verifiedErr := foreignVerified.EnsureLocalEpisodicIndexVerified(scope, snapshot, otherID, foreignRef, otherText)
	if (liveErr == nil) != (verifiedErr == nil) || (liveErr != nil && liveErr.Error() != verifiedErr.Error()) {
		t.Fatalf("foreign-source ensure diverges: live=%v verified=%v", liveErr, verifiedErr)
	}
}

// TestEpisodicVerifiedSaveScaleAgainstLiveSave records the batched-save cost of the
// frozen-parse path against the live path on a source larger than the per-pointer
// parse cache (the historical degradation range: every unit re-read and re-parsed
// the whole transcript). The readings are the evidence; the assertion only needs to
// catch a regression back to per-unit re-reads, with a wide margin for shared
// machines.
func TestEpisodicVerifiedSaveScaleAgainstLiveSave(t *testing.T) {
	if testing.Short() {
		t.Skip("scale reading")
	}
	scope := "mem:verified-scale"
	turns := 7000
	filler := strings.Repeat("cache boundary ", 80)
	path := filepath.Join(t.TempDir(), "scale.jsonl")
	var source strings.Builder
	for i := 0; i < turns; i++ {
		fmt.Fprintf(&source, `{"type":"user","timestamp":1794407222,"message":{"role":"user","content":"question %d %s"}}`+"\n", i, filler)
		fmt.Fprintf(&source, `{"type":"assistant","timestamp":1794407223,"message":{"role":"assistant","stop_reason":"end_turn","content":"answer %d %s"}}`+"\n", i, filler)
	}
	if err := os.WriteFile(path, []byte(source.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 8<<20 {
		t.Fatalf("fixture must exceed the per-pointer cache scale: %d bytes", info.Size())
	}
	const saves = 6
	// The saved text is the canonical rendering of the assistant segment, derived
	// through the shared turn reader once (outside the measured legs) so both legs
	// verify the same unit text.
	textFor := func(turn int) string {
		t.Helper()
		segments, err := episodic.JSONLTurnSegments(path, turn)
		if err != nil || len(segments) < 2 {
			t.Fatalf("turn %d segments: %+v err=%v", turn, segments, err)
		}
		return episodic.Text(segments[1].Role, segments[1].Text)
	}
	texts := make([]string, saves)
	for i := range texts {
		texts[i] = textFor(11 * (i + 1))
	}
	liveVault, _ := snapshotVault(t, "verified-scale-key")
	verifiedVault, _ := snapshotVault(t, "verified-scale-key")

	liveElapsed := time.Duration(0)
	for i := 0; i < saves; i++ {
		turn := 11 * (i + 1)
		text := texts[i]
		docID := episodicunit.DocID(scope, text)
		ref := fmt.Sprintf("@ref:jsonl:%s:%d#1", path, turn)
		turnRef := fmt.Sprintf("@ref:jsonl:%s:%d", path, turn)
		pkt, _ := liveVault.PackBlindPacket(docID, text, 1794407223)
		unitStart := time.Now()
		if err := liveVault.SaveLocalEpisodicPointerIndexed(docID, 1794407223, ref, turnRef, text, pkt, IngestResponse{}); err != nil {
			t.Fatalf("live save %d: %v", i, err)
		}
		liveElapsed += time.Since(unitStart)
	}

	snapshotStart := time.Now()
	snapshot, err := episodic.NewSourceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	snapshotElapsed := time.Since(snapshotStart)
	verifiedElapsed := time.Duration(0)
	for i := 0; i < saves; i++ {
		turn := 11 * (i + 1)
		text := texts[i]
		docID := episodicunit.DocID(scope, text)
		ref := fmt.Sprintf("@ref:jsonl:%s:%d#1", path, turn)
		turnRef := fmt.Sprintf("@ref:jsonl:%s:%d", path, turn)
		unitStart := time.Now()
		if err := verifiedVault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, docID, 1794407223, ref, turnRef, text, IngestResponse{}); err != nil {
			t.Fatalf("verified save %d: %v", i, err)
		}
		verifiedElapsed += time.Since(unitStart)
	}
	t.Logf("verified save scale: source=%dB turns=%d saves=%d live_total=%s live_per_save=%s snapshot_construct=%s verified_total=%s verified_per_save=%s speedup=%.1fx",
		info.Size(), turns, saves, liveElapsed, liveElapsed/saves, snapshotElapsed, verifiedElapsed, verifiedElapsed/saves,
		float64(liveElapsed)/float64(verifiedElapsed))
	if verifiedElapsed*5 > liveElapsed {
		t.Fatalf("verified path lost its order-of-magnitude margin: live=%s verified=%s", liveElapsed, verifiedElapsed)
	}
}
