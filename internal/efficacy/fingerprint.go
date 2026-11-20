// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package efficacy

import (
	"database/sql"
	"strings"

	"github.com/rocareer/rolling-memory/internal/ids"
)

// ComputeDBFingerprint derives a stable identity for one local vault: database
// creation metadata plus the oldest canonical unit. A vault that has not
// crystallized anything yet anchors on its oldest document.
func ComputeDBFingerprint(db *sql.DB, scope string) string {
	if db == nil {
		return ""
	}
	var createdAt, engine string
	_ = db.QueryRow(`SELECT coalesce(value, '') FROM db_meta WHERE key = 'created_at'`).Scan(&createdAt)
	_ = db.QueryRow(`SELECT coalesce(value, '') FROM db_meta WHERE key = 'engine'`).Scan(&engine)
	var firstDocID string
	_ = db.QueryRow(`SELECT coalesce(unit_id, '') FROM rm_docs ORDER BY occurred_at ASC, rowid ASC LIMIT 1`).Scan(&firstDocID)
	if strings.TrimSpace(firstDocID) == "" {
		_ = db.QueryRow(`SELECT coalesce(noetic_id, '') FROM rm_noetic ORDER BY occurred_at ASC, rowid ASC LIMIT 1`).Scan(&firstDocID)
	}
	if strings.TrimSpace(createdAt) == "" || strings.TrimSpace(firstDocID) == "" {
		return ""
	}
	raw := strings.Join([]string{
		strings.TrimSpace(createdAt),
		strings.TrimSpace(engine),
		strings.TrimSpace(firstDocID),
	}, "|")
	return ids.SHA256Hex(raw)[:24]
}
