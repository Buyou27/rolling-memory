// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package store

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/config"
)

// TestLegacyGraphLivenessOnFreshDatabase pins how a brand-new database opened
// through the production open path (store.Open -> db.Migrate) initializes.
// Fresh databases bootstrap the vault schema (schema_vault.sql): the canonical
// corpus plus the shared bookkeeping tables only, so HasLegacyGraph() reports
// false and every caller takes the canonical fast path — recall serves from the
// local plaintext vault, and no legacy assertion graph, FTS shadow table or
// legacy extension table is created or maintained.
//
// The determination exists so any regression that reintroduces the legacy
// graph on fresh databases flips this test loudly and forces the recall
// routing fast paths and their comments to be revisited together.
func TestLegacyGraphLivenessOnFreshDatabase(t *testing.T) {
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
	st, err := Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	rows, err := st.DB().Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	t.Logf("fresh database tables (%d): %v", len(tables), tables)

	legacy := st.HasLegacyGraph()
	t.Logf("HasLegacyGraph on fresh database = %v", legacy)
	if legacy {
		t.Fatalf("determination changed: fresh databases create the legacy assertion graph again; the thin bootstrap schema and the recall routing fast paths must be revisited together")
	}
	for _, forbidden := range []string{"assertion", "node", "trace", "node_fts", "trace_fts", "assertion_ext", "noetic", "episode", "memory_hot"} {
		for _, name := range tables {
			if name == forbidden || strings.HasPrefix(name, forbidden+"_") {
				t.Fatalf("fresh thin database must not carry legacy table %q (tables: %v)", name, tables)
			}
		}
	}
	// The released user database is pinned to this exact minimal bootstrap
	// shape: the store open must not create anything beyond the bookkeeping
	// tables. The vault/core tables appear lazily on first vault use (pinned by
	// localvault.TestNewVaultBootstrapTableSet). Extra tables here mean the
	// "简洁" contract regressed.
	want := []string{
		"db_meta",
		"injection_ledger",
		"rm_meta",
		"sqlite_sequence",
		"transcript_cursor",
	}
	sort.Strings(tables)
	if strings.Join(tables, ",") != strings.Join(want, ",") {
		t.Fatalf("fresh database bootstrap table set changed:\n got: %v\nwant: %v", tables, want)
	}
}
