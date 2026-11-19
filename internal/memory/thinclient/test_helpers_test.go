// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// migrationTestDB opens an empty vault database for tests that exercise the
// canonical schema and its legacy-migration passes.
func migrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "vault.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
