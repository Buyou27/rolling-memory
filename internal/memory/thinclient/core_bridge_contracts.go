// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"database/sql"
	"runtime"

	"github.com/rocareer/rolling-memory/internal/core"
	"github.com/rocareer/rolling-memory/sdk/go/public/closedset"
)

// EnsureCoreSQLiteSchema ensures the canonical Core/RM tables (`rm_docs` & `rm_terms`)
// are present in the client SQLite database.
func EnsureCoreSQLiteSchema(db *sql.DB) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(core.SQLiteSchema)
	return err
}

// CoreSchemaContracts forces the compiler/linker to retain the local SQLite
// schema and closed-set definitions in the binary.
func CoreSchemaContracts() {
	runtime.KeepAlive(core.SQLiteSchema)
	runtime.KeepAlive(closedset.StandardClosedSetTable)
	runtime.KeepAlive(closedset.UniversalSchemaClosedSetTable)
	var s *core.Store
	_ = s.DeleteDocs()
}
