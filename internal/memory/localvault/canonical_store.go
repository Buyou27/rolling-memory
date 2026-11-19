// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"database/sql"
	"errors"
	"strings"

	core "github.com/rocareer/rolling-memory/internal/core"
)

// canonicalBusinessDDL holds the RM-owned business tables that live beside the
// canonical core schema (`rm_docs`/`rm_terms`/`rm_citations`). The core schema
// is the single source of truth for retrieval terms; these tables only carry
// product metadata (lineage pointers, background-file bookkeeping, L3 records,
// tombstones and revision counters) keyed by unit id.
//
// Revision triggers are attached to the core tables so freshness checks no
// longer depend on rowids: `INSERT OR REPLACE` changes a rowid on SQLite, while
// the revision counters move monotonically with every write.
const canonicalBusinessDDL = `
CREATE TABLE IF NOT EXISTS rm_unit_meta (
	unit_id TEXT PRIMARY KEY,
	layer TEXT NOT NULL DEFAULT '',
	ref_pointer TEXT NOT NULL DEFAULT '',
	sentences_json TEXT NOT NULL DEFAULT '[]',
	kept_indices_json TEXT NOT NULL DEFAULT '[]',
	address_uri TEXT NOT NULL DEFAULT '',
	coord64 INTEGER NOT NULL DEFAULT 0,
	waveform_bar TEXT NOT NULL DEFAULT '',
	doc_macro_wave TEXT NOT NULL DEFAULT '',
	assistant_id TEXT NOT NULL DEFAULT '',
	incarnation_id TEXT NOT NULL DEFAULT '',
	locus TEXT NOT NULL DEFAULT '',
	inserted_at INTEGER NOT NULL DEFAULT 0,
	meta_rev INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS rm_bg_dirs (
	dir_id TEXT PRIMARY KEY,
	root_path TEXT NOT NULL UNIQUE,
	added_at INTEGER NOT NULL DEFAULT 0,
	include_exts_json TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS rm_bg_files (
	abs_path TEXT PRIMARY KEY,
	dir_id TEXT NOT NULL,
	rel_path TEXT NOT NULL,
	mtime_ns INTEGER NOT NULL DEFAULT 0,
	file_size INTEGER NOT NULL DEFAULT 0,
	content_sha256 TEXT NOT NULL DEFAULT '',
	chunk_count INTEGER NOT NULL DEFAULT 0,
	unit_ids_json TEXT NOT NULL DEFAULT '[]',
	chunk_hashes_json TEXT NOT NULL DEFAULT '[]',
	chunker_generation TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS rm_bg_chunks (
	unit_id TEXT PRIMARY KEY,
	abs_path TEXT NOT NULL,
	rel_path TEXT NOT NULL,
	chunk_index INTEGER NOT NULL DEFAULT 0,
	anchor TEXT NOT NULL DEFAULT '',
	occurred_at INTEGER NOT NULL DEFAULT 0,
	inserted_at INTEGER NOT NULL DEFAULT 0,
	address_uri TEXT NOT NULL DEFAULT '',
	coord64 INTEGER NOT NULL DEFAULT 0,
	waveform_bar TEXT NOT NULL DEFAULT '',
	kept_indices_json TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_rm_bg_chunks_path ON rm_bg_chunks(abs_path);
CREATE INDEX IF NOT EXISTS idx_rm_bg_chunks_occurred_at ON rm_bg_chunks(occurred_at DESC, unit_id ASC);

CREATE TABLE IF NOT EXISTS rm_episodic_index (
	doc_id TEXT NOT NULL,
	ref_pointer TEXT NOT NULL,
	codec_key BLOB NOT NULL,
	payload BLOB,
	content_digest BLOB,
	payload_mac BLOB,
	PRIMARY KEY (doc_id, codec_key)
);

CREATE TABLE IF NOT EXISTS rm_episodic_sources (
	turn_ref TEXT NOT NULL,
	doc_id TEXT NOT NULL,
	ref_pointer TEXT NOT NULL,
	content_digest BLOB,
	source_mac BLOB,
	PRIMARY KEY (turn_ref, doc_id, ref_pointer)
);
CREATE TABLE IF NOT EXISTS rm_episodic_lineage (
	turn_ref TEXT NOT NULL,
	doc_id TEXT NOT NULL,
	occurred_at INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (turn_ref, doc_id)
);
CREATE INDEX IF NOT EXISTS idx_rm_episodic_lineage_doc ON rm_episodic_lineage(doc_id);
CREATE TRIGGER IF NOT EXISTS rm_episodic_sources_delete AFTER DELETE ON rm_docs BEGIN
	DELETE FROM rm_episodic_sources WHERE doc_id = OLD.unit_id;
	DELETE FROM rm_episodic_lineage WHERE doc_id = OLD.unit_id;
END;

CREATE TABLE IF NOT EXISTS rm_noetic (
	noetic_id TEXT PRIMARY KEY,
	occurred_at INTEGER NOT NULL DEFAULT 0,
	inserted_at INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'active',
	query_text TEXT NOT NULL,
	answer_text TEXT NOT NULL,
	cited_unit_ids_json TEXT NOT NULL DEFAULT '[]',
	cited_sentences_json TEXT NOT NULL DEFAULT '[]',
	address_uri TEXT NOT NULL DEFAULT '',
	coord64 INTEGER NOT NULL DEFAULT 0,
	waveform_bar TEXT NOT NULL DEFAULT '',
	assistant_id TEXT NOT NULL DEFAULT '',
	incarnation_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_rm_noetic_status_ts ON rm_noetic(status, occurred_at DESC);

CREATE TABLE IF NOT EXISTS rm_deleted_units (
	unit_id TEXT PRIMARY KEY,
	layer TEXT NOT NULL DEFAULT '',
	deleted_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS rm_revision (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	docs_rev INTEGER NOT NULL DEFAULT 0,
	terms_rev INTEGER NOT NULL DEFAULT 0,
	meta_rev INTEGER NOT NULL DEFAULT 0,
	bg_rev INTEGER NOT NULL DEFAULT 0,
	episodic_rev INTEGER NOT NULL DEFAULT 0,
	docs_cnt INTEGER NOT NULL DEFAULT 0,
	terms_cnt INTEGER NOT NULL DEFAULT 0,
	change_seq INTEGER NOT NULL DEFAULT 0,
	purge_epoch INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO rm_revision(id) VALUES (1);

CREATE TABLE IF NOT EXISTS rm_migration (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rm_change (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	unit_id TEXT NOT NULL,
	op TEXT NOT NULL CHECK (op IN ('upsert','delete'))
);
CREATE INDEX IF NOT EXISTS idx_rm_change_unit ON rm_change(unit_id);

CREATE TRIGGER IF NOT EXISTS rm_rev_docs_ai AFTER INSERT ON rm_docs BEGIN
	UPDATE rm_revision SET docs_rev = docs_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_docs_au AFTER UPDATE ON rm_docs BEGIN
	UPDATE rm_revision SET docs_rev = docs_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_docs_ad AFTER DELETE ON rm_docs BEGIN
	UPDATE rm_revision SET docs_rev = docs_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_terms_ai AFTER INSERT ON rm_terms BEGIN
	UPDATE rm_revision SET terms_rev = terms_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_terms_au AFTER UPDATE ON rm_terms BEGIN
	UPDATE rm_revision SET terms_rev = terms_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_terms_ad AFTER DELETE ON rm_terms BEGIN
	UPDATE rm_revision SET terms_rev = terms_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_meta_ai AFTER INSERT ON rm_unit_meta BEGIN
	UPDATE rm_revision SET meta_rev = meta_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_meta_au AFTER UPDATE ON rm_unit_meta BEGIN
	UPDATE rm_revision SET meta_rev = meta_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_meta_ad AFTER DELETE ON rm_unit_meta BEGIN
	UPDATE rm_revision SET meta_rev = meta_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_bg_ai AFTER INSERT ON rm_bg_chunks BEGIN
	UPDATE rm_revision SET bg_rev = bg_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_bg_au AFTER UPDATE ON rm_bg_chunks BEGIN
	UPDATE rm_revision SET bg_rev = bg_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_bg_ad AFTER DELETE ON rm_bg_chunks BEGIN
	UPDATE rm_revision SET bg_rev = bg_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_epi_ai AFTER INSERT ON rm_episodic_index BEGIN
	UPDATE rm_revision SET episodic_rev = episodic_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_epi_au AFTER UPDATE ON rm_episodic_index BEGIN
	UPDATE rm_revision SET episodic_rev = episodic_rev + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_rev_epi_ad AFTER DELETE ON rm_episodic_index BEGIN
	UPDATE rm_revision SET episodic_rev = episodic_rev + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS rm_cnt_docs_ai AFTER INSERT ON rm_docs BEGIN
	UPDATE rm_revision SET docs_cnt = docs_cnt + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_cnt_docs_ad AFTER DELETE ON rm_docs BEGIN
	UPDATE rm_revision SET docs_cnt = docs_cnt - 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_cnt_terms_ai AFTER INSERT ON rm_terms BEGIN
	UPDATE rm_revision SET terms_cnt = terms_cnt + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_cnt_terms_ad AFTER DELETE ON rm_terms BEGIN
	UPDATE rm_revision SET terms_cnt = terms_cnt - 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS rm_change_docs_ai AFTER INSERT ON rm_docs BEGIN
	INSERT INTO rm_change(unit_id, op) VALUES (NEW.unit_id, 'upsert');
	UPDATE rm_revision SET change_seq = (SELECT seq FROM rm_change WHERE rowid = last_insert_rowid()) WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_change_docs_au AFTER UPDATE ON rm_docs BEGIN
	INSERT INTO rm_change(unit_id, op) VALUES (NEW.unit_id, 'upsert');
	UPDATE rm_revision SET change_seq = (SELECT seq FROM rm_change WHERE rowid = last_insert_rowid()) WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_change_docs_ad AFTER DELETE ON rm_docs BEGIN
	INSERT INTO rm_change(unit_id, op) VALUES (OLD.unit_id, 'delete');
	UPDATE rm_revision SET change_seq = (SELECT seq FROM rm_change WHERE rowid = last_insert_rowid()) WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_change_meta_ai AFTER INSERT ON rm_unit_meta BEGIN
	INSERT INTO rm_change(unit_id, op) VALUES (NEW.unit_id, 'upsert');
	UPDATE rm_revision SET change_seq = (SELECT seq FROM rm_change WHERE rowid = last_insert_rowid()) WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_change_meta_au AFTER UPDATE ON rm_unit_meta BEGIN
	INSERT INTO rm_change(unit_id, op) VALUES (NEW.unit_id, 'upsert');
	UPDATE rm_revision SET change_seq = (SELECT seq FROM rm_change WHERE rowid = last_insert_rowid()) WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS rm_change_meta_ad AFTER DELETE ON rm_unit_meta BEGIN
	INSERT INTO rm_change(unit_id, op) VALUES (OLD.unit_id, 'upsert');
	UPDATE rm_revision SET change_seq = (SELECT seq FROM rm_change WHERE rowid = last_insert_rowid()) WHERE id = 1;
END;
`

// canonicalUnitMeta carries the RM-owned metadata that accompanies one
// canonical document. Retrieval terms always come from the text extractor;
// none of these fields influence term identity.
type canonicalUnitMeta struct {
	Layer           string
	RefPointer      string
	SentencesJSON   string
	KeptIndicesJSON string
	AddressURI      string
	Coord64         int64
	WaveformBar     string
	DocMacroWave    string
	AssistantID     string
	IncarnationID   string
	Locus           string
	InsertedAt      int64
	DocHash         uint64
	BlockHash       uint64
	LineStart       int
	LineEnd         int
	CitedIDs        []string
	// StoreRawText persists the unit text into rm_docs.raw_text. Inline units
	// keep their text; pointer units resolve it from the source snapshot and
	// leave the column empty to avoid duplicating conversation transcripts.
	StoreRawText bool
}

// ensureCanonicalBusinessSchema creates the canonical core schema and the
// RM-owned metadata tables on the shared connection pool.
func ensureCanonicalBusinessSchema(db *sql.DB) error {
	if db == nil {
		return errors.New("localvault: nil sql.DB")
	}
	if _, err := core.OpenStoreWithOptions(db, core.StoreOptions{
		Dialect:    core.DialectSQLite,
		OwnsDB:     false,
		SkipSchema: false,
	}); err != nil {
		return err
	}
	// Vaults created before the background revision counter existed keep a
	// three-column rm_revision; the bg triggers in the DDL below would fail on
	// every chunk write without this additive migration. A fresh database has no
	// rm_revision yet, so both "duplicate column" and "no such table" are benign.
	for _, col := range []string{"bg_rev", "episodic_rev", "docs_cnt", "terms_cnt", "change_seq", "purge_epoch"} {
		if _, err := db.Exec(`ALTER TABLE rm_revision ADD COLUMN ` + col + ` INTEGER NOT NULL DEFAULT 0`); err != nil {
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "no such table") {
				return err
			}
		}
	}
	if _, err := db.Exec(canonicalBusinessDDL); err != nil {
		return err
	}
	// One-time backfill for vaults that predate the O(1) corpus counters. The
	// marker keeps later opens from scanning the tables again.
	var counted int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_migration WHERE key = 'canonical_counts_v1'`).Scan(&counted); err != nil {
		return err
	}
	if counted == 0 {
		if _, err := db.Exec(`UPDATE rm_revision SET docs_cnt = (SELECT COUNT(*) FROM rm_docs), terms_cnt = (SELECT COUNT(*) FROM rm_terms) WHERE id = 1`); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT OR REPLACE INTO rm_migration(key, value) VALUES ('canonical_counts_v1', '1')`); err != nil {
			return err
		}
	}
	return nil
}

// canonicalStore returns the shared core store handle wrapped around the vault
// connection. The handle is opened once per snapshot ref (shared by clones).
func (v *Vault) canonicalStore() (*core.Store, error) {
	if v == nil || v.db == nil {
		return nil, errors.New("localvault: nil Vault db")
	}
	ref := v.snapRef
	if ref == nil {
		return core.OpenStoreWithOptions(v.db, core.StoreOptions{
			Dialect: core.DialectSQLite, OwnsDB: false, SkipSchema: true,
		})
	}
	ref.canonicalOnce.Do(func() {
		ref.canonicalStore, ref.canonicalErr = core.OpenStoreWithOptions(v.db, core.StoreOptions{
			Dialect: core.DialectSQLite, OwnsDB: false, SkipSchema: true,
		})
	})
	return ref.canonicalStore, ref.canonicalErr
}

// ingestCanonicalText derives the retrieval document from the unit text with
// the canonical extractor and writes it to rm_docs/rm_terms/rm_citations.
// Retrieval terms are never reconstructed from packed atoms.
func (v *Vault) ingestCanonicalText(unitID string, occurredAt int64, text string, meta canonicalUnitMeta) error {
	if v == nil || v.db == nil {
		return nil
	}
	unitID = strings.TrimSpace(unitID)
	if unitID == "" {
		return nil
	}
	// L1 background chunks are static document slices, not temporal dialogue
	// events: their file mtime must not enter SpacetimeCluster exact-timestamp
	// buckets, where thousands of chunks collapse onto one timestamp and create
	// O(B^2) neighbour edges. Same-document slices couple via DocHash + lines.
	if meta.Layer == LayerBackground {
		occurredAt = 0
	}
	if v.db != nil {
		var existingOccurred int64
		var existingAssistant, existingIncarnation, existingLocus string
		if err := v.db.QueryRow(`
			SELECT d.occurred_at, COALESCE(m.assistant_id, ''), COALESCE(m.incarnation_id, ''), COALESCE(m.locus, '')
			  FROM rm_docs d LEFT JOIN rm_unit_meta m ON m.unit_id = d.unit_id
			 WHERE d.unit_id = ?`, unitID).Scan(&existingOccurred, &existingAssistant, &existingIncarnation, &existingLocus); err == nil {
			if occurredAt <= 0 && existingOccurred > 0 {
				occurredAt = existingOccurred
			}
			if existingAssistant != "" {
				meta.AssistantID = existingAssistant
			}
			if existingIncarnation != "" {
				meta.IncarnationID = existingIncarnation
			}
			if existingLocus != "" {
				meta.Locus = existingLocus
			}
		}
	}
	te := v.newVaultTermExtractor()
	doc := te.ExtractDoc(unitID, occurredAt, text)
	doc.Layer = meta.Layer
	doc.DocHash = meta.DocHash
	doc.BlockHash = meta.BlockHash
	doc.LineStart = meta.LineStart
	doc.LineEnd = meta.LineEnd
	doc.CitedIDs = meta.CitedIDs
	if meta.StoreRawText {
		doc.RawText = text
	}
	store, err := v.canonicalStore()
	if err != nil {
		return err
	}
	if err := store.Ingest([]core.Doc{doc}); err != nil {
		return err
	}
	sentences := meta.SentencesJSON
	if strings.TrimSpace(sentences) == "" {
		sentences = "[]"
	}
	kept := meta.KeptIndicesJSON
	if strings.TrimSpace(kept) == "" {
		kept = "[]"
	}
	_, err = v.db.Exec(`
		INSERT INTO rm_unit_meta(
			unit_id, layer, ref_pointer, sentences_json, kept_indices_json,
			address_uri, coord64, waveform_bar, doc_macro_wave,
			assistant_id, incarnation_id, locus, inserted_at, meta_rev
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(unit_id) DO UPDATE SET
			layer = excluded.layer,
			ref_pointer = excluded.ref_pointer,
			sentences_json = excluded.sentences_json,
			kept_indices_json = excluded.kept_indices_json,
			address_uri = excluded.address_uri,
			coord64 = excluded.coord64,
			waveform_bar = excluded.waveform_bar,
			doc_macro_wave = excluded.doc_macro_wave,
			assistant_id = CASE WHEN rm_unit_meta.assistant_id <> '' THEN rm_unit_meta.assistant_id ELSE excluded.assistant_id END,
			incarnation_id = CASE WHEN rm_unit_meta.incarnation_id <> '' THEN rm_unit_meta.incarnation_id ELSE excluded.incarnation_id END,
			locus = CASE WHEN rm_unit_meta.locus <> '' THEN rm_unit_meta.locus ELSE excluded.locus END,
			inserted_at = CASE WHEN rm_unit_meta.inserted_at > 0 THEN rm_unit_meta.inserted_at ELSE excluded.inserted_at END,
			meta_rev = rm_unit_meta.meta_rev + 1`,
		unitID, meta.Layer, meta.RefPointer, sentences, kept,
		meta.AddressURI, meta.Coord64, meta.WaveformBar, meta.DocMacroWave,
		meta.AssistantID, meta.IncarnationID, meta.Locus, meta.InsertedAt,
	)
	return err
}

// deleteCanonicalUnits removes the retrieval documents and their metadata rows.
func (v *Vault) deleteCanonicalUnits(unitIDs ...string) error {
	if v == nil || v.db == nil || len(unitIDs) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(unitIDs))
	placeholders := make([]string, 0, len(unitIDs))
	for _, id := range unitIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		filtered = append(filtered, id)
		placeholders = append(placeholders, "?")
	}
	if len(filtered) == 0 {
		return nil
	}
	store, err := v.canonicalStore()
	if err != nil {
		return err
	}
	if err := store.DeleteDocs(filtered...); err != nil {
		return err
	}
	_, err = v.db.Exec(`DELETE FROM rm_unit_meta WHERE unit_id IN (`+strings.Join(placeholders, ",")+`)`, toAnySlice(filtered)...)
	return err
}

func toAnySlice(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

// deleteCanonicalBackgroundFile removes every canonical unit bound to one
// background file, then the file's RM bookkeeping rows.
func (v *Vault) deleteCanonicalBackgroundFile(absPath string) error {
	if v == nil || v.db == nil || strings.TrimSpace(absPath) == "" {
		return nil
	}
	rows, err := v.db.Query(`SELECT unit_id FROM rm_bg_chunks WHERE abs_path = ?`, absPath)
	if err != nil {
		return err
	}
	var unitIDs []string
	for rows.Next() {
		var unitID string
		if err := rows.Scan(&unitID); err != nil {
			rows.Close()
			return err
		}
		unitIDs = append(unitIDs, unitID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if err := v.deleteCanonicalUnits(unitIDs...); err != nil {
		return err
	}
	if _, err := v.db.Exec(`DELETE FROM rm_bg_chunks WHERE abs_path = ?`, absPath); err != nil {
		return err
	}
	_, err = v.db.Exec(`DELETE FROM rm_bg_files WHERE abs_path = ?`, absPath)
	return err
}

// deleteCanonicalEpisodicIndex removes the episodic index/source rows mirrored
// from the thin tables for one document.
func (v *Vault) deleteCanonicalEpisodicIndex(docID string) error {
	if v == nil || v.db == nil || strings.TrimSpace(docID) == "" {
		return nil
	}
	if _, err := v.db.Exec(`DELETE FROM rm_episodic_index WHERE doc_id = ?`, docID); err != nil {
		return err
	}
	_, err := v.db.Exec(`DELETE FROM rm_episodic_sources WHERE doc_id = ?`, docID)
	return err
}
