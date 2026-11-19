// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package store

import (
	"strconv"

	"github.com/rocareer/rolling-memory/internal/db"
)

// Status returns the scope counters and runtime handles.
func (s *Store) Status(scope string) (map[string]any, error) {
	if scope == "" {
		scope = s.Config.Scope
	}
	var hasLegacy int
	_ = s.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'assertion'`).Scan(&hasLegacy)
	if hasLegacy == 0 {
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
	counts, err := db.TableCounts(s.conn, scope)
	if err != nil {
		return nil, err
	}
	active, err := queryCount(s.conn,
		"SELECT count(*) FROM assertion WHERE scope = ? AND status = 'active' AND bare = 0", scope)
	if err != nil {
		return nil, err
	}
	private, err := queryCount(s.conn,
		"SELECT count(*) FROM assertion WHERE scope = ? AND audience = 'private'", scope)
	if err != nil {
		return nil, err
	}
	retracted, err := queryCount(s.conn,
		"SELECT count(*) FROM assertion WHERE scope = ? AND status = 'retracted'", scope)
	if err != nil {
		return nil, err
	}
	superseded, err := queryCount(s.conn,
		"SELECT count(*) FROM assertion WHERE scope = ? AND status = 'superseded'", scope)
	if err != nil {
		return nil, err
	}
	pending, err := queryCount(s.conn,
		"SELECT count(*) FROM trace WHERE scope = ? AND processed = 0", scope)
	if err != nil {
		return nil, err
	}
	sessions, err := queryCount(s.conn,
		"SELECT count(DISTINCT session) FROM trace WHERE scope = ?", scope)
	if err != nil {
		return nil, err
	}
	conflicts, err := queryCount(s.conn,
		"SELECT count(*) FROM meta_edge WHERE scope = ? AND meta_kind = 'conflicts-with' AND status = 'proposed'", scope)
	if err != nil {
		return nil, err
	}
	replay, err := queryOneMap(s.conn, "SELECT * FROM replay_state WHERE scope = ?", scope)
	if err != nil {
		return nil, err
	}
	schemaRaw, err := db.GetMeta(s.conn, "schema_version", "0")
	if err != nil {
		return nil, err
	}
	schemaVersion, _ := strconv.ParseInt(schemaRaw, 10, 64)
	var lastReplay any
	if replay != nil {
		lastReplay = replay
	}
	return map[string]any{
		"scope":                 scope,
		"db":                    s.Config.DB,
		"db_size_bytes":         db.DBSize(s.Config.DB),
		"mode":                  s.Config.Mode,
		"egress":                "loopback-only",
		"counts":                counts,
		"active_assertions":     active,
		"private_assertions":    private,
		"retracted_assertions":  retracted,
		"superseded_assertions": superseded,
		"pending_traces":        pending,
		"sessions":              sessions,
		"conflicts_proposed":    conflicts,
		"last_replay":           lastReplay,
		"schema_version":        schemaVersion,
	}, nil
}
