// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"database/sql"
	"strings"
)

// updateEpisodicUnitSourceMeta refreshes one existing episodic unit's source
// attribution and occurrence time in place. The effective text identity and the
// tombstone guard match the canonical read model, so a removed or re-pointed
// unit is never rewritten by a stale scan page. Both tables move in one
// transaction; a missing metadata row still updates the document time.
func updateEpisodicUnitSourceMeta(ctx context.Context, db *sql.DB, docID, refPointer, harnessID, sessionID string, locusKnown bool, locus string, occurredAt int64) error {
	docID = strings.TrimSpace(docID)
	if db == nil || docID == "" {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var matches int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM rm_docs d
			LEFT JOIN rm_unit_meta m ON m.unit_id = d.unit_id
			WHERE d.unit_id = ?
			  AND COALESCE(NULLIF(m.ref_pointer, ''), d.raw_text) = ?
			  AND NOT EXISTS (
				SELECT 1 FROM rm_deleted_units t
				WHERE t.unit_id = d.unit_id AND (t.layer = 'episodic' OR t.layer = '')
			  )
		)`, docID, refPointer).Scan(&matches); err != nil {
		return err
	}
	if matches == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rm_docs SET occurred_at = ? WHERE unit_id = ?`, occurredAt, docID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE rm_unit_meta SET
			assistant_id = ?,
			incarnation_id = ?,
			locus = CASE WHEN ? THEN ? ELSE locus END
		WHERE unit_id = ?
		  AND (COALESCE(assistant_id, '') != ? OR COALESCE(incarnation_id, '') != ? OR (? AND COALESCE(locus, '') != ?))`,
		harnessID, sessionID, locusKnown, locus, docID, harnessID, sessionID, locusKnown, locus); err != nil {
		return err
	}
	return tx.Commit()
}
