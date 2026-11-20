// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func ingestTxDoc(unitID string, hash uint64, text string) Doc {
	return Doc{
		UnitID:     unitID,
		OccurredAt: 1700000000,
		Terms:      map[uint64]Term{hash: {Hash: hash, Group: hash, Bits: 40, Flags: 1, Role: RolePrimary}},
		Counts:     map[uint64]int{hash: 1},
		Positions:  map[uint64][]TermPosition{hash: {{Sentence: 0, Slot: 1}}},
		RawText:    text,
	}
}

func openIngestTxStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "ingest-tx.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func countIngestTxRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestStoreIngestTxCommitsWithCallerTransaction(t *testing.T) {
	st := openIngestTxStore(t)
	docs := []Doc{
		ingestTxDoc("tx-a", 11, "first transaction document"),
		ingestTxDoc("tx-b", 22, "second transaction document"),
	}
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.IngestTx(tx, docs); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := countIngestTxRows(t, st.db, `SELECT COUNT(*) FROM rm_docs`); got != 2 {
		t.Fatalf("committed docs = %d, want 2", got)
	}
	if got := countIngestTxRows(t, st.db, `SELECT COUNT(*) FROM rm_terms`); got != 2 {
		t.Fatalf("committed terms = %d, want 2", got)
	}
	idx, err := st.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if idx.N != 2 {
		t.Fatalf("LoadIndex N = %d, want 2", idx.N)
	}
}

func TestStoreIngestTxRollsBackCanonicalAndBusinessRowsTogether(t *testing.T) {
	st := openIngestTxStore(t)
	if _, err := st.db.Exec(`CREATE TABLE rm_ingest_tx_probe(k TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.IngestTx(tx, []Doc{ingestTxDoc("rolled-back", 33, "rolled back document")}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO rm_ingest_tx_probe(k) VALUES('marker')`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"rm_docs", "rm_terms", "rm_ingest_tx_probe"} {
		if got := countIngestTxRows(t, st.db, `SELECT COUNT(*) FROM `+table); got != 0 {
			t.Fatalf("rolled-back %s rows = %d, want 0", table, got)
		}
	}
}

func TestStoreIngestTxKeepsLastDocumentPerUnit(t *testing.T) {
	st := openIngestTxStore(t)
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.IngestTx(tx, []Doc{ingestTxDoc("same-unit", 44, "older text")}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := st.IngestTx(tx, []Doc{ingestTxDoc("same-unit", 55, "newer text")}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := countIngestTxRows(t, st.db, `SELECT COUNT(*) FROM rm_docs`); got != 1 {
		t.Fatalf("docs after repeated unit = %d, want 1", got)
	}
	if got := countIngestTxRows(t, st.db, `SELECT COUNT(*) FROM rm_terms WHERE unit_id = 'same-unit' AND term_hash = 44`); got != 0 {
		t.Fatalf("stale term rows = %d, want 0", got)
	}
	if got := countIngestTxRows(t, st.db, `SELECT COUNT(*) FROM rm_terms WHERE unit_id = 'same-unit' AND term_hash = 55`); got != 1 {
		t.Fatalf("replacement term rows = %d, want 1", got)
	}
	var text string
	if err := st.db.QueryRow(`SELECT raw_text FROM rm_docs WHERE unit_id = 'same-unit'`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "newer text" {
		t.Fatalf("stored text = %q, want newer text", text)
	}
}

type ingestTxStubBackend struct{}

func (ingestTxStubBackend) Dialect() StoreDialect              { return DialectSQLite }
func (ingestTxStubBackend) TenantScope() (string, string)      { return "local", "default" }
func (ingestTxStubBackend) DeleteDocs(...string) error         { return nil }
func (ingestTxStubBackend) Ingest([]Doc) error                 { return nil }
func (ingestTxStubBackend) DocCount() (int, error)             { return 0, nil }
func (ingestTxStubBackend) LoadIndex() (*Index, error)         { return nil, nil }
func (ingestTxStubBackend) LoadRawText(string) (string, error) { return "", nil }
func (ingestTxStubBackend) Close() error                       { return nil }

func TestStoreIngestTxRejectsDegenerateInputs(t *testing.T) {
	st := openIngestTxStore(t)
	if err := st.IngestTx(nil, []Doc{ingestTxDoc("nil-tx", 66, "text")}); err == nil {
		t.Fatal("nil transaction was accepted")
	}
	if err := st.IngestTx(nil, nil); err == nil {
		t.Fatal("nil transaction with empty batch was accepted")
	}
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.IngestTx(tx, nil); err != nil {
		t.Fatalf("empty batch on a valid transaction: %v", err)
	}

	wrapped, err := WrapStoreBackend(ingestTxStubBackend{})
	if err != nil {
		t.Fatal(err)
	}
	if err := wrapped.IngestTx(tx, []Doc{ingestTxDoc("backend", 77, "text")}); err == nil {
		t.Fatal("wrapped backend accepted a caller-owned transaction")
	}
	var nilStore *Store
	if err := nilStore.IngestTx(tx, []Doc{ingestTxDoc("nil-store", 88, "text")}); err == nil {
		t.Fatal("nil store accepted a caller-owned transaction")
	}
}
