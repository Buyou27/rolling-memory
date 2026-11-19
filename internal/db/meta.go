// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package db

import (
	"database/sql"
	"fmt"
)

// MigrateVaultMetaTable renames the thin-era bookkeeping table in place. The
// vault key and scope binding live there; creating a second empty table would
// silently rebind the vault or invalidate every stored pointer index.
//
// The rename runs only when `rm_meta` is absent. It is idempotent and safe to
// call before any read that expects `rm_meta`.
func MigrateVaultMetaTable(connection *sql.DB) error {
	if connection == nil {
		return nil
	}
	var hasRM, hasLegacy int
	if err := connection.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN name = 'rm_meta' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN name = 'thin_vault_meta' THEN 1 ELSE 0 END), 0)
		  FROM sqlite_master
		 WHERE type = 'table' AND name IN ('rm_meta', 'thin_vault_meta')`).Scan(&hasRM, &hasLegacy); err != nil {
		return err
	}
	if hasRM == 0 && hasLegacy != 0 {
		if _, err := connection.Exec(`ALTER TABLE thin_vault_meta RENAME TO rm_meta`); err != nil {
			return fmt.Errorf("vault meta migration: %w", err)
		}
	}
	return nil
}
