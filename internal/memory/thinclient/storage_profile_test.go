// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// vaultTestDSN mirrors the storage profile the production runtime opens vault
// files with (internal/db.Connect): immediate write transactions, a 15s busy
// timeout, and WAL. A bare file DSN runs on the rollback journal with a zero
// busy timeout, so lock contention between a foreground writer and the
// vault's own background activity (debounced snapshot refresh, reindex
// passes) turns into an instant SQLITE_BUSY on a loaded machine - a failure
// mode the product's own connection settings rule out.
func vaultTestDSN(dbPath string) string {
	return dbPath + "?_txlock=immediate&_pragma=busy_timeout(15000)&_pragma=journal_mode(WAL)"
}

// TestVaultTestDSNLockPatience pins the two behaviours the profile buys: a
// bare connection fails a write instantly while another connection holds the
// write lock, while a vaultTestDSN connection waits the holder out.
func TestVaultTestDSNLockPatience(t *testing.T) {
	// holdWriteLock opens the database at path behind dsn, seeds one table and
	// returns while its open transaction owns the write lock; calling the
	// returned function commits and releases it.
	holdWriteLock := func(t *testing.T, path, dsn string) func() {
		t.Helper()
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatalf("open %s: %v", dsn, err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS lock_probe(v INTEGER)`); err != nil {
			t.Fatalf("create lock_probe: %v", err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if _, err := tx.Exec(`INSERT INTO lock_probe(v) VALUES (1)`); err != nil {
			t.Fatalf("hold write lock: %v", err)
		}
		return func() { _ = tx.Commit() }
	}

	t.Run("bare connection has no patience", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bare.sqlite")
		release := holdWriteLock(t, path, path)
		defer release()

		bare, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("open bare: %v", err)
		}
		defer bare.Close()

		start := time.Now()
		if _, err := bare.Exec(`INSERT INTO lock_probe(v) VALUES (2)`); err == nil {
			t.Fatal("expected SQLITE_BUSY from a zero-patience connection under a held write lock")
		} else if !strings.Contains(strings.ToLower(err.Error()), "locked") {
			t.Fatalf("expected a database-is-locked error, got %v", err)
		}
		if waited := time.Since(start); waited > time.Second {
			t.Fatalf("bare connection waited %v; expected an immediate failure", waited)
		}
	})

	t.Run("profile connection waits for the lock holder", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "profile.sqlite")
		patient, err := sql.Open("sqlite", vaultTestDSN(path))
		if err != nil {
			t.Fatalf("open profile dsn: %v", err)
		}
		defer patient.Close()
		if err := patient.Ping(); err != nil {
			t.Fatalf("ping profile dsn: %v", err)
		}
		release := holdWriteLock(t, path, path)
		releaseTimer := time.AfterFunc(200*time.Millisecond, release)
		defer releaseTimer.Stop()

		start := time.Now()
		if _, err := patient.Exec(`INSERT INTO lock_probe(v) VALUES (2)`); err != nil {
			t.Fatalf("profile connection should wait out the lock holder, got %v", err)
		}
		if waited := time.Since(start); waited < 100*time.Millisecond {
			t.Fatalf("write completed after %v without waiting for the lock holder", waited)
		}
	})
}
