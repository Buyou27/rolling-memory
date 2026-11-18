// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
	_ "modernc.org/sqlite"
)

// TestPreCanonicalVaultStillRecallsThinCorpus guards the upgrade path: a vault
// written by an earlier release carries its corpus in the thin tables while the
// canonical store is still empty. Opening the new build must keep recalling that
// corpus instead of serving an empty snapshot until a migration runs.
func TestPreCanonicalVaultStillRecallsThinCorpus(t *testing.T) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "precanonical.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := thinclient.NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO thin_vault_docs(doc_id, layer, occurred_at, raw_text, sentences_json) VALUES ('ep_legacy', 'episodic', 1700000000, 'alpha project graph memory units', '[]')`); err != nil {
		t.Fatalf("seed thin corpus: %v", err)
	}
	hits, err := vault.SearchClientSDKDefault("alpha project graph memory", 5)
	if err != nil {
		t.Fatalf("SearchClientSDKDefault: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("a vault written before the canonical store existed lost its corpus")
	}
	if hits[0].UnitID != "ep_legacy" {
		t.Fatalf("unexpected first hit: %+v", hits[0])
	}
}
