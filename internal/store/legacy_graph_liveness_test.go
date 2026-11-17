// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package store

import (
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/config"
)

// TestLegacyGraphLivenessOnFreshDatabase pins how a brand-new database opened
// through the production open path (store.Open -> db.Migrate) initializes.
// The canonical schema still creates the legacy `assertion` graph, so
// HasLegacyGraph() reports true on fresh databases and callers cannot take a
// thin-only fast path: recall serves through the legacy body first and the
// local thin-vault fallback is load-bearing for empty graphs.
//
// The determination exists so a future schema split (fresh databases carrying
// only the thin tables) flips this test loudly and forces the recall routing
// fast paths and their comments to be revisited together.
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
	if !legacy {
		t.Fatalf("determination changed: fresh databases no longer create the legacy assertion graph; revisit recall routing fast paths and the api/store comments together")
	}
}
