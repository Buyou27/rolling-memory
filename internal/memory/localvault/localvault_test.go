// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/localvault"
	_ "modernc.org/sqlite"
)

// vaultTestDSN mirrors the storage profile the production runtime opens vault
// files with (internal/db.Connect): immediate write transactions, a 15s busy
// timeout, and WAL. Harness connections carry the same lock patience and
// journal semantics, so ordinary contention with the vault's own background
// activity never surfaces as an instant SQLITE_BUSY. Twin of the
// internal-package helper in storage_profile_test.go.
func vaultTestDSN(dbPath string) string {
	return dbPath + "?_txlock=immediate&_pragma=busy_timeout(15000)&_pragma=journal_mode(WAL)"
}

type failingTransportStub struct {
	calls   int
	failErr error
}

func (f *failingTransportStub) IngestBlindPulse(_ context.Context, pkt localvault.BlindPulsePacket) (localvault.IngestResponse, error) {
	f.calls++
	if f.failErr != nil {
		return localvault.IngestResponse{}, f.failErr
	}
	return localvault.IngestResponse{UnitID: pkt.UnitID, Layer: pkt.Layer}, nil
}

func (f *failingTransportStub) QueryBlindManifold(_ context.Context, _ localvault.BlindPulsePacket, _ int) ([]localvault.QueryHit, error) {
	return nil, f.failErr
}

// TestRecallNoeticMatchesOriginalCueNotEvidence proves that a long answer or
// cited evidence cannot smuggle an unrelated Noetic record into a later query.
func TestRecallNoeticMatchesOriginalCueNotEvidence(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "noetic-cue.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := localvault.NewVault(db, []byte("noetic-cue-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	client := localvault.NewClient(vault, nil)
	const cue = "启动后台 admin 本地开发 vite 后端端口"
	if err := client.RememberNoetic("NT-CUE", cue, nil, nil, "原始检索句柄", 100); err != nil {
		t.Fatalf("RememberNoetic NT-CUE: %v", err)
	}
	if err := client.RememberNoetic(
		"NT-EVIDENCE-LEAK", "开箱默认账号 登录页", nil, nil,
		cue+" 以及几千字与当前检索无关的旧巡检答案", 101,
	); err != nil {
		t.Fatalf("RememberNoetic NT-EVIDENCE-LEAK: %v", err)
	}
	hits, err := client.RecallNoetic(cue, 10)
	if err != nil {
		t.Fatalf("RecallNoetic: %v", err)
	}
	if len(hits) != 1 || hits[0].NoeticID != "NT-CUE" || hits[0].MatchScore != 1 {
		t.Fatalf("Noetic cue-only matches = %+v, want only NT-CUE with Dice 1", hits)
	}
}

func TestRecallNoeticVerificationGainAndDAGClosure(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "noetic-verify.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := localvault.NewVault(db, []byte("noetic-verify-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	client := localvault.NewClient(vault, nil)
	const cue = "quartz harbor gate ledger"
	// Unverified handle (single write).
	if err := client.RememberNoetic("NT-FRESH", cue, []string{"ep_fresh"}, []string{"fresh"}, "ans", 100); err != nil {
		t.Fatalf("RememberNoetic NT-FRESH: %v", err)
	}
	// Verified handle: written once at t=100, then re-consolidated and cited by NT-CHILD.
	if err := client.RememberNoetic("NT-VERIFIED", cue, []string{"ep_root"}, []string{"root"}, "ans", 100); err != nil {
		t.Fatalf("RememberNoetic NT-VERIFIED initial: %v", err)
	}
	if err := client.RememberNoetic("NT-VERIFIED", cue, []string{"ep_root"}, []string{"root"}, "ans", 100); err != nil {
		t.Fatalf("RememberNoetic NT-VERIFIED re-consolidate: %v", err)
	}
	if err := client.RememberNoetic("NT-CHILD", "quartz harbor", []string{"ep_child", "NT-VERIFIED"}, []string{"child"}, "ans", 200); err != nil {
		t.Fatalf("RememberNoetic NT-CHILD: %v", err)
	}
	hits, err := client.RecallNoetic(cue, 10)
	if err != nil {
		t.Fatalf("RecallNoetic: %v", err)
	}
	// NT-FRESH and NT-VERIFIED answer the same cue with different evidence, so
	// the newer trace supersedes the older one; only one handle can win a cue.
	if len(hits) != 1 || hits[0].NoeticID != "NT-VERIFIED" {
		t.Fatalf("expected the newest trace for this cue, got %+v", hits)
	}
	if hits[0].ReuseCount < 2 || hits[0].VerificationGain() <= 1.0 {
		t.Fatalf("expected NT-VERIFIED ReuseCount>=2 and VerificationGain>1, got reuse=%d gain=%f", hits[0].ReuseCount, hits[0].VerificationGain())
	}
	childHits, err := client.RecallNoetic("quartz harbor", 10)
	if err != nil || len(childHits) == 0 {
		t.Fatalf("RecallNoetic child: %v %+v", err, childHits)
	}
	foundRootLeaf := false
	for _, cid := range childHits[0].CitedUnitIDs {
		if cid == "ep_root" {
			foundRootLeaf = true
			break
		}
	}
	if !foundRootLeaf {
		t.Fatalf("expected DAG closure to expand ep_root into NT-CHILD CitedUnitIDs, got %+v", childHits[0].CitedUnitIDs)
	}
}

// TestRememberPreservesVaultWhenCloudIngestFails proves the local-first write
// contract: transport failure cannot erase the only local plaintext copy.
func TestRememberPreservesVaultWhenCloudIngestFails(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "local-first.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := localvault.NewVault(db, []byte("local-first-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	client := localvault.NewClient(vault, &failingTransportStub{failErr: fmt.Errorf("engine unavailable")})
	resp, err := client.Remember(context.Background(), "DOC-LOCAL-FIRST", "plaintext survives a cloud ingest failure", 100)
	if err == nil {
		t.Fatalf("expected remote ingest error, got %+v", resp)
	}
	raw, distilled, _, err := vault.LoadLocalDocument("DOC-LOCAL-FIRST")
	if err != nil || raw != "plaintext survives a cloud ingest failure" || distilled != raw {
		t.Fatalf("local-first document = raw %q distilled %q err %v", raw, distilled, err)
	}
	ref := "@ref:jsonl:/tmp/no-such-episodic-file:0#0"
	if _, err := client.RememberEpisodicByRef(context.Background(), "EP-LOCAL-FIRST", ref, "@ref:jsonl:/tmp/no-such-episodic-file:0", "pointer survives too", 101); err == nil || !strings.Contains(err.Error(), "engine unavailable") {
		t.Fatalf("expected original remote ingest error, got %v", err)
	}
	raw, _, _, err = vault.LoadLocalDocument("EP-LOCAL-FIRST")
	if err != nil || raw != "pointer survives too" {
		t.Fatalf("local-first plaintext = %q, err %v", raw, err)
	}

	offline := localvault.NewClient(vault, nil)
	if _, err := offline.RememberWithLayer(context.Background(), "BG-OFFLINE", "offline background remains readable", 102, localvault.LayerBackground, nil); err != nil {
		t.Fatalf("nil-transport local write: %v", err)
	}
	if got := vault.LoadLocalDocumentLayer("BG-OFFLINE"); got != localvault.LayerBackground {
		t.Fatalf("offline layer = %q", got)
	}
}

func TestRememberEpisodicByRefInvalidSourcePreservesPlaintext(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(dir, "vault.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	vault, err := localvault.NewVault(db, []byte("invalid-source-secret"))
	if err != nil {
		t.Fatal(err)
	}
	client := localvault.NewClient(vault, nil)
	source := filepath.Join(dir, "source.jsonl")
	if err := os.WriteFile(source, []byte(`{"type":"user","message":{"role":"user","content":"different source text"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, ref string }{
		{"missing", "@ref:jsonl:" + filepath.Join(dir, "missing.jsonl") + ":0#0"},
		{"mismatch", "@ref:jsonl:" + source + ":0#0"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			const text = "the supplied user text"
			if _, err := client.RememberEpisodicByRef(context.Background(), tc.id, tc.ref, tc.ref, text, 1); err != nil {
				t.Fatal(err)
			}
			raw, distilled, _, err := vault.LoadLocalDocument(tc.id)
			if err != nil || raw != text || distilled != text {
				t.Fatalf("document = %q, %q, %v", raw, distilled, err)
			}
			var indexes, lineage int
			if err := db.QueryRow(`SELECT count(*) FROM rm_episodic_index WHERE doc_id = ?`, tc.id).Scan(&indexes); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM rm_episodic_lineage WHERE doc_id = ?`, tc.id).Scan(&lineage); err != nil {
				t.Fatal(err)
			}
			if indexes != 0 || lineage != 0 {
				t.Fatalf("unverified source indexed: index=%d lineage=%d", indexes, lineage)
			}
		})
	}
	if err := vault.SaveLocalPlaintext("existing", 1, "another user's data", []string{"another user's data"}, localvault.IngestResponse{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RememberEpisodicByRef(context.Background(), "existing", "@ref:jsonl:"+source+":0#0", "", "new user text", 2); err == nil {
		t.Fatal("expected source identity conflict")
	}
	raw, _, _, err := vault.LoadLocalDocument("existing")
	if err != nil || raw != "another user's data" {
		t.Fatalf("existing document overwritten: %q, %v", raw, err)
	}
	live, err := localvault.ResolveEpisodicPointerLive("@ref:jsonl:" + source + ":0#0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RememberEpisodicByRef(context.Background(), "mismatch", "@ref:jsonl:"+source+":0#0", "", live, 2); err == nil {
		t.Fatal("verified source replaced existing inline identity")
	}
	raw, _, _, err = vault.LoadLocalDocument("mismatch")
	if err != nil || raw != "the supplied user text" {
		t.Fatalf("inline identity overwritten: %q, %v", raw, err)
	}
}

func TestRememberEpisodicByRefPreservesIdenticalInline(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(dir, "vault.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	vault, err := localvault.NewVault(db, []byte("same-inline-secret"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.jsonl")
	if err := os.WriteFile(source, []byte(`{"type":"user","message":{"role":"user","content":"same text"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := "@ref:jsonl:" + source + ":0#0"
	text, err := localvault.ResolveEpisodicPointerLive(ref)
	if err != nil {
		t.Fatal(err)
	}
	_, sents := vault.PackBlindPacket("same", text, 1)
	if err := vault.SaveLocalPlaintext("same", 1, text, sents, localvault.IngestResponse{Layer: localvault.LayerEpisodic}); err != nil {
		t.Fatal(err)
	}
	errTr := &failingTransportStub{failErr: fmt.Errorf("engine unavailable")}
	client := localvault.NewClient(vault, errTr)
	if _, err := client.RememberEpisodicByRef(context.Background(), "same", ref, ref, text, 2); err == nil || !strings.Contains(err.Error(), "engine unavailable") {
		t.Fatalf("expected transport error after preserved inline, got %v", err)
	}
	var stored string
	var occurred int64
	var indexes int
	if err := db.QueryRow(`SELECT raw_text, occurred_at FROM rm_units WHERE doc_id = 'same'`).Scan(&stored, &occurred); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM rm_episodic_index WHERE doc_id = 'same'`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if stored != text || occurred != 1 || indexes != 0 || errTr.calls != 1 {
		t.Fatalf("inline changed or ingest skipped: text=%q occurred=%d indexes=%d requests=%d", stored, occurred, indexes, errTr.calls)
	}
	okTr := &failingTransportStub{}
	client = localvault.NewClient(vault, okTr)
	if _, err := client.RememberEpisodicByRef(context.Background(), "same", ref, ref, text, 3); err != nil {
		t.Fatalf("successful ingest with identical inline: %v", err)
	}
	if err := db.QueryRow(`SELECT raw_text, occurred_at FROM rm_units WHERE doc_id = 'same'`).Scan(&stored, &occurred); err != nil {
		t.Fatal(err)
	}
	if stored != text || occurred != 1 || okTr.calls != 1 {
		t.Fatalf("successful ingest modified inline: text=%q occurred=%d requests=%d", stored, occurred, okTr.calls)
	}
}

func TestLoadLocalDocumentRefPrefixIdentity(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(dir, "vault.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	vault, err := localvault.NewVault(db, []byte("ref-identity-secret"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.jsonl")
	if err := os.WriteFile(source, []byte(`{"type":"user","message":{"role":"user","content":"live text"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := "@ref:jsonl:" + source + ":0#0"
	live, err := localvault.ResolveEpisodicPointerLive(ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"@ref: literal user text", ref} {
		id := "inline-" + text
		_, sents := vault.PackBlindPacket(id, text, 1)
		if err := vault.SaveLocalPlaintext(id, 1, text, sents, localvault.IngestResponse{}); err != nil {
			t.Fatal(err)
		}
		if got, _, _, err := vault.LoadLocalDocument(id); err != nil || got != text {
			t.Fatalf("inline %q read as %q: %v", text, got, err)
		}
	}
	if err := vault.SaveLocalEpisodicPointer("legacy", 1, ref, "", localvault.IngestResponse{}); err != nil {
		t.Fatal(err)
	}
	if got, _, _, err := vault.LoadLocalDocument("legacy"); err == nil || got != "" {
		t.Fatalf("unverified legacy source delivered %q: %v", got, err)
	}
	if err := vault.SaveLocalEpisodicPointer("lineage", 1, ref, ref, localvault.IngestResponse{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE rm_units SET sentences_json = ? WHERE doc_id = 'lineage'`, `["@ref:jsonl:"]`); err != nil {
		t.Fatal(err)
	}
	if got, _, _, err := vault.LoadLocalDocument("lineage"); err == nil || got != "" {
		t.Fatalf("ambiguous lineage source delivered %q: %v", got, err)
	}
	client := localvault.NewClient(vault, nil)
	if err := os.WriteFile(source, []byte(`{"type":"user","message":{"role":"user","content":"changed text"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _, _, err := vault.LoadLocalDocument("legacy"); err == nil || got != "" {
		t.Fatalf("changed unverified legacy source delivered %q: %v", got, err)
	}
	if err := os.WriteFile(source, []byte(`{"type":"user","message":{"role":"user","content":"live text"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RememberEpisodicByRef(context.Background(), "indexed", ref, "", live, 1); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`{"type":"user","message":{"role":"user","content":"changed text"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE rm_units SET sentences_json = ? WHERE doc_id = 'indexed'`, `["@ref:jsonl:"]`); err != nil {
		t.Fatal(err)
	}
	if got, _, _, err := vault.LoadLocalDocument("indexed"); err == nil || got != "" {
		t.Fatalf("changed indexed source delivered %q: %v", got, err)
	}
}

// TestLocalVaultSQLiteAndHTTPBlindBrainEndToEnd verifies the full production flow of the local
// Local Vault Client without importing any core algorithm package in Rolling-Memory:
//
//	Local SQLite Vault -> Local Vault Client -> HTTP JSON Wire (0 Plaintext, 0 Floats) -> Cloud Endpoint -> Local Resolution.
func TestLocalVaultSQLiteAndHTTPBlindBrainEndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	vault, err := localvault.NewVault(db, []byte("user-local-master-key-2026"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	client := localvault.NewClient(vault, localvault.NewLocalVaultTransport(vault))
	ctx := context.Background()

	docs := []struct {
		id   string
		text string
	}{
		{"DOC1", "Your registration for the BMVC2024 conference in Glasgow starting November24 is confirmed."},
		{"DOC2", "The flight to Glasgow airport arrives on November24 at terminal two."},
		{
			"DOC3",
			"Date: 2024-11-16.\n" +
				"The Hilton Glasgow hotel stay checking in November24 costs 655.59 GBP in total.\n" +
				"A credit card deposit is needed upon check-in.\n" +
				"A credit card deposit is needed upon check-in.\n" +
				"A credit card deposit is needed upon check-in.",
		},
	}

	for _, d := range docs {
		if _, err := client.Remember(ctx, d.id, d.text, 1731794400); err != nil {
			t.Fatalf("Remember(%s): %v", d.id, err)
		}
	}

	hits, err := client.Recall(ctx, "BMVC2024 conference registration", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) == 0 || hits[0].UnitID != "DOC1" {
		t.Fatalf("expected DOC1 at #1, got %+v", hits)
	}

	err = client.RememberNoetic(
		"NOETIC-1",
		"How much does the Glasgow conference hotel cost?",
		[]string{"DOC1", "DOC3"},
		nil,
		"The Hilton Glasgow hotel for BMVC2024 costs 655.59 GBP in total.",
		1731795000,
	)
	if err != nil {
		t.Fatalf("RememberNoetic failed: %v", err)
	}

	noeticHits, err := client.RecallNoetic("Glasgow hotel cost GBP", 5)
	if err != nil {
		t.Fatalf("RecallNoetic failed: %v", err)
	}
	if len(noeticHits) != 1 || noeticHits[0].NoeticID != "NOETIC-1" || len(noeticHits[0].CitedSentences) < 2 {
		t.Fatalf("unexpected local Noetic hits: %+v", noeticHits)
	}

	affected, err := vault.InvalidateLocalNoeticByCitedDoc("DOC3")
	if err != nil || len(affected) != 1 || affected[0] != "NOETIC-1" {
		t.Fatalf("expected NOETIC-1 to be invalidated when DOC3 changes, got affected=%v err=%v", affected, err)
	}
	afterHits, err := client.RecallNoetic("Glasgow hotel cost GBP", 5)
	if err != nil || len(afterHits) != 0 {
		t.Fatalf("expected 0 active Noetic hits after invalidation, got %+v (err=%v)", afterHits, err)
	}
}

// TestClientSDKDefaultRecall_StandaloneAndOfflineFallback verifies that the Client SDK Default Recall
// (`SearchClientSDKDefault` / `RecallDefault` using the deterministic cluster kernel + integer self-information coverage + same-day continuity)
// works out of the box as a system default capability on the client without requiring an upstream connection.
// TestClientSDKDefaultRecallCacheInvalidatesChangedDocument prevents the
// shared blind-slot cache from serving stale atoms after a docID is rewritten.
func TestClientSDKDefaultRecallCacheInvalidatesChangedDocument(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "cache-invalidation.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := localvault.NewVault(db, []byte("cache-invalidation-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	save := func(text string) {
		_, sents := vault.PackBlindPacket("DOC-CHANGING", text, 100)
		if err := vault.SaveLocalPlaintext("DOC-CHANGING", 100, text, sents, localvault.IngestResponse{
			UnitID: "DOC-CHANGING", Layer: localvault.LayerEpisodic,
		}); err != nil {
			t.Fatalf("SaveLocalPlaintext: %v", err)
		}
	}
	client := localvault.NewClient(vault, nil)
	save("alpha original marker")
	if hits, err := client.RecallDefault("alpha original marker", 3); err != nil || len(hits) != 1 {
		t.Fatalf("initial recall = %+v, err %v", hits, err)
	}
	save("replacement zeta")
	if hits, err := client.RecallDefault("alpha original marker", 3); err != nil || len(hits) != 0 {
		t.Fatalf("stale atoms survived document replacement: %+v, err %v", hits, err)
	}
	if hits, err := client.RecallDefault("replacement zeta", 3); err != nil || len(hits) != 1 || hits[0].UnitID != "DOC-CHANGING" {
		t.Fatalf("updated document not recalled: %+v, err %v", hits, err)
	}
}

func TestClientSDKDefaultRecall_StandaloneAndOfflineFallback(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sdk_default.db")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	vault, err := localvault.NewVault(db, []byte("sdk-default-secret-2026"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	localClient := localvault.NewClient(vault, nil) // nil transport => 100% Client SDK Default Recall
	ctx := context.Background()

	seedDocs := []struct {
		id   string
		occ  int64
		text string
	}{
		{"BMVC-REG", 1732440000, "Your registration for the BMVC2024 conference in Glasgow starting November24 is confirmed."},
		{"BMVC-HOTEL", 1732443600, "The Hilton Glasgow hotel stay checking in November24 costs 655.59 GBP in total."},
		{"NOISE-1", 1730000000, "Routine grocery shopping receipt for milk and bread in London."},
	}
	for _, d := range seedDocs {
		_, sents := vault.PackBlindPacket(d.id, d.text, d.occ)
		if err := vault.SaveLocalPlaintext(d.id, d.occ, d.text, sents, localvault.IngestResponse{
			UnitID: d.id,
			Layer:  "episodic",
		}); err != nil {
			t.Fatalf("SaveLocalPlaintext(%s): %v", d.id, err)
		}
	}

	hits, err := localClient.Recall(ctx, "BMVC2024 Glasgow November24", 3)
	if err != nil {
		t.Fatalf("localClient.Recall: %v", err)
	}
	if len(hits) < 2 || hits[0].UnitID != "BMVC-REG" || hits[1].UnitID != "BMVC-HOTEL" {
		t.Fatalf("expected BMVC-REG (#1) and BMVC-HOTEL (#2) via Client SDK Default Recall, got %+v", hits)
	}

	// Verify Layer 3 (`noetic`: Agent retrieval handles + answer stored locally) handle-linked recall:
	if err := localClient.RememberNoetic(
		"NOETIC-ACCOM",
		"What was the accommodation cost for the Scottish vision symposium?",
		[]string{"BMVC-HOTEL"},
		nil,
		"The accommodation at Hilton cost 655.59 GBP.",
		1732450000,
	); err != nil {
		t.Fatalf("RememberNoetic: %v", err)
	}
	nHits, err := localClient.Recall(ctx, "Scottish vision symposium accommodation", 5)
	if err != nil {
		t.Fatalf("localClient.Recall (Noetic handle link): %v", err)
	}
	foundNoetic, foundLinkedHotel := false, false
	for _, nh := range nHits {
		if nh.UnitID == "NOETIC-ACCOM" && nh.Layer == localvault.LayerNoetic {
			foundNoetic = true
		}
		if nh.UnitID == "BMVC-HOTEL" && strings.Contains(nh.RawPlaintext, "655.59 GBP") {
			foundLinkedHotel = true
		}
	}
	if !foundNoetic || !foundLinkedHotel {
		t.Fatalf("expected both NOETIC-ACCOM (noetic) and handle-linked BMVC-HOTEL (episodic), got %+v", nHits)
	}
	t.Logf("✓ 3-Only Local Memory Model (background × episodic × noetic handle-linked recall) verified: #1=%s (%.1f), #2=%s (%.1f)",
		hits[0].UnitID, hits[0].FinalWeight, hits[1].UnitID, hits[1].FinalWeight)
}

// TestSearchClientSDKDefaultRanksRelevantDocFirst pins the kernel ranking contract:
// the default recall surfaces the doc carrying the matching evidence first, returns
// hits ordered by FinalWeight DESC, and is deterministic across repeated runs.
func TestSearchClientSDKDefaultRanksRelevantDocFirst(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "kernel-rank.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := localvault.NewVault(db, []byte("kernel-rank-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	docs := []struct {
		id   string
		occ  int64
		text string
	}{
		{"DOC-STRONG", 1732500000, "hotel booking confirmation for the BMVC 2024 conference in Glasgow with total cost 655.59 GBP"},
		{"DOC-WEAK", 1732500100, "invoice for 655.59 GBP"},
		{"DOC-NOISE", 1732500200, "grocery receipt for milk and bread"},
	}
	for _, d := range docs {
		_, sents := vault.PackBlindPacket(d.id, d.text, d.occ)
		if err := vault.SaveLocalPlaintext(d.id, d.occ, d.text, sents, localvault.IngestResponse{
			UnitID: d.id, Layer: localvault.LayerEpisodic,
		}); err != nil {
			t.Fatalf("SaveLocalPlaintext(%s): %v", d.id, err)
		}
	}

	hits, err := vault.SearchClientSDKDefault("BMVC 2024 Glasgow hotel cost", 3)
	if err != nil {
		t.Fatalf("SearchClientSDKDefault: %v", err)
	}
	if len(hits) == 0 || hits[0].UnitID != "DOC-STRONG" {
		t.Fatalf("expected DOC-STRONG first from the kernel ranking, got %+v", hits)
	}
	if again, err := vault.SearchClientSDKDefault("BMVC 2024 Glasgow hotel cost", 3); err != nil || len(again) != len(hits) || again[0].UnitID != hits[0].UnitID {
		t.Fatalf("SearchClientSDKDefault must be deterministic: first=%+v second=%+v err=%v", hits, again, err)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i-1].FinalWeight < hits[i].FinalWeight {
			t.Fatalf("hits must be ordered by FinalWeight DESC: %+v", hits)
		}
	}
}
