// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package store

import (
	"database/sql"
	"fmt"

	"github.com/rocareer/rolling-memory/internal/db"
)

// Check runs the local vault integrity checks.
func (s *Store) Check() (map[string]any, error) {
	issues := []map[string]any{}
	integrity, err := db.IntegrityCheck(s.conn)
	if err != nil {
		return nil, err
	}
	for _, message := range integrity {
		issues = append(issues, map[string]any{
			"severity": "error",
			"code":     "integrity",
			"detail":   message,
		})
	}
	corpus, err := db.ReadCorpusCounts(s.conn)
	if err != nil {
		return nil, err
	}
	var hasCorpus int
	if err := s.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'rm_docs'`).Scan(&hasCorpus); err != nil {
		return nil, err
	}
	if hasCorpus == 0 {
		return map[string]any{"ok": true, "issues": issues, "corpus": corpus.Map()}, nil
	}
	for _, probe := range []struct{ code, query, detail string }{
		{"orphan_terms", `SELECT COUNT(*) FROM rm_terms t WHERE NOT EXISTS (SELECT 1 FROM rm_docs d WHERE d.unit_id = t.unit_id)`, "%d term postings point at a missing unit"},
		{"orphan_meta", `SELECT COUNT(*) FROM rm_unit_meta m WHERE NOT EXISTS (SELECT 1 FROM rm_docs d WHERE d.unit_id = m.unit_id)`, "%d metadata rows point at a missing unit"},
		{"orphan_citations", `SELECT COUNT(*) FROM rm_citations c WHERE NOT EXISTS (SELECT 1 FROM rm_docs d WHERE d.unit_id = c.unit_id)`, "%d citation rows point at a missing unit"},
	} {
		orphans, err := queryCount(s.conn, probe.query)
		if err != nil {
			return nil, err
		}
		if orphans > 0 {
			issues = append(issues, map[string]any{
				"severity": "error",
				"code":     probe.code,
				"detail":   fmt.Sprintf(probe.detail, orphans),
			})
		}
	}
	ok := true
	for _, issue := range issues {
		if mapString(issue, "severity") == "error" {
			ok = false
			break
		}
	}
	return map[string]any{"ok": ok, "issues": issues, "corpus": corpus.Map()}, nil
}

func queryCount(connection *sql.DB, query string, args ...any) (int, error) {
	var count int
	if err := connection.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func mapString(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}
