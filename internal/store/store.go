// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package store owns the SQLite connection for the local plaintext vault.
//
// A clean install carries only the canonical rm_* vault. Databases that still
// contain the retired assertion graph are refused at open; the golden corpus
// used to be the only reader of that graph and has been retired with it.
package store

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/db"
)

// Store is one open local vault connection.
type Store struct {
	Config *config.Config

	conn    *sql.DB
	profile db.StorageProfile
}

// Open opens (and migrates) the configured database.
func Open(cfg *config.Config) (*Store, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if err := db.VaultScopePreflight(cfg.DB, cfg.Scope); err != nil {
		return nil, err
	}
	if err := cfg.Ensure(); err != nil {
		return nil, err
	}
	profile := db.LocalProfile()
	switch strings.ToLower(cfg.Storage.Profile) {
	case "usb":
		profile = db.USBProfile()
	case "", "local", "auto":
		profile = db.LocalProfile()
	}
	connection, err := db.Connect(cfg.DB, profile)
	if err != nil {
		return nil, err
	}
	if err := db.BindVaultScope(connection, cfg.Scope); err != nil {
		connection.Close()
		return nil, err
	}
	var legacyTables int
	if err := connection.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('node', 'assertion', 'trace')`,
	).Scan(&legacyTables); err != nil {
		connection.Close()
		return nil, err
	}
	if legacyTables > 0 {
		connection.Close()
		return nil, fmt.Errorf("database %s was created by an older Rolling-Memory release and is no longer supported; download the latest release and re-import your transcripts into a new vault (https://github.com/rocareer/rolling-memory/releases/latest)", cfg.DB)
	}
	if _, err := db.Migrate(connection); err != nil {
		connection.Close()
		return nil, err
	}
	return &Store{Config: cfg, conn: connection, profile: profile}, nil
}

// Close closes the connection; the usb profile truncates the WAL first, and the
// local profile runs a non-blocking PASSIVE checkpoint so concurrent multi-instance
// MCP daemons do not let the WAL file grow unboundedly.
func (s *Store) Close() error {
	if s.conn == nil {
		return nil
	}
	if s.profile.Name == "usb" {
		_, _ = s.conn.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	} else {
		_, _ = s.conn.Exec("PRAGMA wal_checkpoint(PASSIVE)")
	}
	_ = s.conn.Close()
	return nil
}

// DB exposes the vault connection.
func (s *Store) DB() *sql.DB {
	return s.conn
}
