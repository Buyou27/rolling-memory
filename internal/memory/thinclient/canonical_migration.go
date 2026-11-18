// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

const (
	// canonicalBackfillMarker records whether the one-time derivation from the
	// thin corpus into the canonical store has completed for this vault.
	canonicalBackfillMarker = "canonical_backfill_v1"
	canonicalBackfillDone   = "done"
	// canonicalBackfillClaimTTLSeconds bounds a claim: a process that dies mid
	// derivation leaves its timestamp behind, and the next open retries once the
	// claim is older than this window.
	canonicalBackfillClaimTTLSeconds = 3600
)

// ensureCanonicalCorpus derives the canonical retrieval documents from the thin
// corpus once per vault.
//
// A vault written before the canonical store existed keeps its source text and
// chunk rows in the thin tables, while the retrieval engine reads only rm_docs.
// The first reader of such a vault therefore derives the canonical documents
// from those sources before the snapshot is built; otherwise the vault would
// recall nothing. The rm_migration marker makes the pass one-time and the claim
// row keeps concurrent processes (daemon, CLI, console) from repeating it.
func (v *Vault) ensureCanonicalCorpus(ctx context.Context) {
	if v == nil || v.db == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	v.canonicalMigrationMu.Lock()
	defer v.canonicalMigrationMu.Unlock()
	if v.canonicalMigrationDone {
		return
	}
	done, err := v.canonicalBackfillMarked(ctx)
	if err != nil {
		return
	}
	if done {
		v.canonicalMigrationDone = true
		return
	}
	if !v.thinCorpusHasRows(ctx) {
		if err := v.recordCanonicalBackfill(ctx, canonicalBackfillDone); err == nil {
			v.canonicalMigrationDone = true
		}
		return
	}
	if !v.claimCanonicalBackfill(ctx) {
		// Another process owns the derivation; serve whatever the canonical store
		// already holds instead of repeating minutes of work.
		return
	}
	// A store that already holds documents was written by this build (or migrated
	// by an operator): only units still absent are derived, so a partial pass is
	// repaired without re-ingesting the whole corpus.
	onlyMissing := v.canonicalDocCount(ctx) > 0
	report := &CanonicalBackfillReport{}
	if err := v.BackfillCanonicalCorpus(ctx, report, onlyMissing); err != nil || report.Failed > 0 {
		_, _ = v.db.ExecContext(ctx, `DELETE FROM rm_migration WHERE key = ?`, canonicalBackfillMarker)
		return
	}
	if err := v.recordCanonicalBackfill(ctx, canonicalBackfillDone); err != nil {
		return
	}
	v.canonicalMigrationDone = true
}

// canonicalBackfillMarked reports whether this vault already completed the
// derivation.
func (v *Vault) canonicalBackfillMarked(ctx context.Context) (bool, error) {
	var value string
	err := v.db.QueryRowContext(ctx, `SELECT value FROM rm_migration WHERE key = ?`, canonicalBackfillMarker).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == canonicalBackfillDone, nil
}

// canonicalDocCount reports how many documents the canonical store already
// holds; a non-zero count means the store predates this derivation pass.
func (v *Vault) canonicalDocCount(ctx context.Context) int64 {
	var count int64
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rm_docs`).Scan(&count); err != nil {
		return 0
	}
	return count
}

// recordCanonicalBackfill writes the terminal marker for the derivation.
func (v *Vault) recordCanonicalBackfill(ctx context.Context, value string) error {
	_, err := v.db.ExecContext(ctx, `
		INSERT INTO rm_migration(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		canonicalBackfillMarker, value)
	return err
}

// claimCanonicalBackfill takes the derivation claim atomically. A completed or
// fresh claim held by another process leaves the row untouched and reports
// false.
func (v *Vault) claimCanonicalBackfill(ctx context.Context) bool {
	now := time.Now().Unix()
	res, err := v.db.ExecContext(ctx, `
		INSERT INTO rm_migration(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
		WHERE rm_migration.value <> ? AND CAST(rm_migration.value AS INTEGER) < ?`,
		canonicalBackfillMarker, strconv.FormatInt(now, 10), canonicalBackfillDone, now-canonicalBackfillClaimTTLSeconds)
	if err != nil {
		return false
	}
	affected, err := res.RowsAffected()
	return err == nil && affected == 1
}
