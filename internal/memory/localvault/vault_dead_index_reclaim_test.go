// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	dbmigrate "github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
	_ "modernc.org/sqlite"
)

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
	if _, err := localvault.NewVault(db, []byte("bootstrap-shape-secret")); err != nil {
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
		"rm_bg_chunks",
		"rm_bg_dirs",
		"rm_bg_files",
		"rm_change",
		"rm_citations",
		"rm_deleted_units",
		"rm_docs",
		"rm_episodic_index",
		"rm_episodic_lineage",
		"rm_episodic_sources",
		"rm_meta",
		"rm_migration",
		"rm_noetic",
		"rm_revision",
		"rm_terms",
		"rm_unit_meta",
		"rm_units",
		"sqlite_sequence",
		"transcript_cursor",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("released user database table set changed:\n got: %v\nwant: %v", got, want)
	}
}
