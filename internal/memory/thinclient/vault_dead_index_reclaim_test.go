// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	dbmigrate "github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
	_ "modernc.org/sqlite"
)

// TestNewVaultReclaimsDeadFTSIndexes pins the reclamation contract: a vault
// opened over a database that still carries the retired local FTS5 lexical
// index (`thin_vault_docs_fts` + `thin_vault_fts_map`) drops both tables on
// open, and the vault keeps serving from the plaintext tables afterwards.
func TestNewVaultReclaimsDeadFTSIndexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reclaim.db")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	secret := []byte("reclaim-dead-index-secret")
	vault, err := thinclient.NewVault(db, secret)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	if err := vault.SaveLocalPlaintext("doc-a", 1700000000, "老张养了一只猫，叫小白。", thinclient.SplitSentences("老张养了一只猫，叫小白。"), thinclient.IngestResponse{UnitID: "doc-a", Layer: thinclient.LayerEpisodic}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Simulate the pre-retirement shape: the dead FTS index plus its rowid map,
	// and the legacy trace bookkeeping table.
	for _, ddl := range []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS thin_vault_docs_fts USING fts5(raw_text, doc_id UNINDEXED, content='', contentless_delete=1, tokenize = 'trigram')`,
		`CREATE TABLE IF NOT EXISTS thin_vault_fts_map(rowid INTEGER PRIMARY KEY AUTOINCREMENT, doc_id TEXT NOT NULL UNIQUE)`,
		`INSERT OR IGNORE INTO thin_vault_fts_map(rowid, doc_id) VALUES (1, 'doc-a')`,
		`INSERT INTO thin_vault_docs_fts(rowid, raw_text) VALUES (1, '老张养了一只猫，叫小白。')`,
		`CREATE TABLE IF NOT EXISTS session_cursor (scope TEXT NOT NULL, session TEXT NOT NULL, turn_done INTEGER NOT NULL DEFAULT -1, updated_at INTEGER NOT NULL, PRIMARY KEY (scope, session))`,
		`INSERT OR IGNORE INTO session_cursor(scope, session, turn_done, updated_at) VALUES ('mem:test', 'legacy', 3, 1)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("seed dead index (%s): %v", ddl[:40], err)
		}
	}

	reopened, err := thinclient.NewVault(db, secret)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('thin_vault_docs_fts', 'thin_vault_fts_map', 'session_cursor')`).Scan(&remaining); err != nil {
		t.Fatalf("count dead index tables: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("dead index/bookkeeping tables survived the open: %d", remaining)
	}
	hits, err := reopened.SearchClientSDKDefault("小白 猫", 5)
	if err != nil {
		t.Fatalf("search after reclamation: %v", err)
	}
	if len(hits) == 0 || hits[0].UnitID != "doc-a" {
		t.Fatalf("vault must keep serving from the plaintext tables, got %+v", hits)
	}
}

// TestNewVaultBootstrapTableSet pins the released user database shape: the
// production open (bootstrap migration + vault) of a fresh database carries
// exactly the bookkeeping tables, the vault tables and the embedded core's
// relational tables — nothing else.
func TestNewVaultBootstrapTableSet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bootstrap.db")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := dbmigrate.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := thinclient.NewVault(db, []byte("bootstrap-shape-secret")); err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type IN ('table','view') ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		got = append(got, name)
	}
	want := []string{
		"db_meta",
		"injection_ledger",
		"rm_citations",
		"rm_docs",
		"rm_terms",
		"sqlite_sequence",
		"thin_vault_corpus_revision",
		"thin_vault_deleted_units",
		"thin_vault_docs",
		"thin_vault_episodic_index",
		"thin_vault_episodic_lineage",
		"thin_vault_episodic_revision",
		"thin_vault_episodic_sources",
		"thin_vault_meta",
		"thin_vault_noetic",
		"transcript_cursor",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("released user database table set changed:\n got: %v\nwant: %v", got, want)
	}
}
