// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package store

import (
	"strconv"

	"github.com/rocareer/rolling-memory/internal/db"
)

// Status returns the canonical vault counters and runtime handles.
func (s *Store) Status(scope string) (map[string]any, error) {
	if scope == "" {
		scope = s.Config.Scope
	}
	corpus, err := db.ReadCorpusCounts(s.conn)
	if err != nil {
		return nil, err
	}
	schemaRaw, _ := db.GetMeta(s.conn, "schema_version", "1")
	schemaVersion, _ := strconv.ParseInt(schemaRaw, 10, 64)
	return map[string]any{
		"scope":          scope,
		"db":             s.Config.DB,
		"db_size_bytes":  db.DBSize(s.Config.DB),
		"mode":           s.Config.Mode,
		"egress":         "loopback-only",
		"corpus":         corpus.Map(),
		"schema_version": schemaVersion,
	}, nil
}
