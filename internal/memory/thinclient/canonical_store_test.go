// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"database/sql"
	"path/filepath"
	"testing"

	core "github.com/rocareer/rolling-memory/internal/core"
)

// The canonical store is the retrieval truth: text is tokenised once with the
// standard extractor, terms (including their slot roles) land in rm_terms, and
// the RM metadata side table carries the product fields.
func TestCanonicalStoreIngestsPlaintextWithRoles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "canonical_store.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	v, err := NewVault(db, []byte("canonical-store-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	const text = "Alice met Bob in Paris on 2026-01-02."
	if err := v.SaveLocalPlaintext("doc-1", 1700000000, text, nil, IngestResponse{
		Layer:      "episodic",
		AddressURI: "addr://one",
	}); err != nil {
		t.Fatal(err)
	}

	var raw, layer, address string
	var termCount int
	if err := db.QueryRow(`SELECT raw_text, layer, term_count FROM rm_docs WHERE unit_id = 'doc-1'`).Scan(&raw, &layer, &termCount); err != nil {
		t.Fatalf("rm_docs row: %v", err)
	}
	if raw != text || layer != "episodic" || termCount == 0 {
		t.Fatalf("unexpected rm_docs row: raw=%q layer=%q terms=%d", raw, layer, termCount)
	}

	var storedTerms int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_terms WHERE unit_id = 'doc-1'`).Scan(&storedTerms); err != nil {
		t.Fatal(err)
	}
	if storedTerms == 0 {
		t.Fatal("rm_terms must carry the extracted terms")
	}

	rows, err := db.Query(`SELECT class_flags FROM rm_terms WHERE unit_id = 'doc-1'`)
	if err != nil {
		t.Fatal(err)
	}
	sawPrimary := false
	for rows.Next() {
		var packed int64
		if err := rows.Scan(&packed); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		role, _ := core.UnpackTermRoleExt(uint8((packed >> 8) & 0xff))
		if role == core.RolePrimary {
			sawPrimary = true
		}
	}
	rows.Close()
	if !sawPrimary {
		t.Fatal("canonical terms must keep at least one primary role")
	}

	var metaRev, coord int64
	if err := db.QueryRow(`SELECT meta_rev, coord64 FROM rm_unit_meta WHERE unit_id = 'doc-1'`).Scan(&metaRev, &coord); err != nil {
		t.Fatalf("rm_unit_meta row: %v", err)
	}
	if err := db.QueryRow(`SELECT address_uri FROM rm_unit_meta WHERE unit_id = 'doc-1'`).Scan(&address); err != nil {
		t.Fatal(err)
	}
	if metaRev < 1 || address != "addr://one" {
		t.Fatalf("unexpected meta row: rev=%d addr=%q", metaRev, address)
	}

	var docsRev, termsRev int64
	if err := db.QueryRow(`SELECT docs_rev, terms_rev FROM rm_revision WHERE id = 1`).Scan(&docsRev, &termsRev); err != nil {
		t.Fatal(err)
	}
	if docsRev == 0 || termsRev == 0 {
		t.Fatalf("revision triggers must advance: docs=%d terms=%d", docsRev, termsRev)
	}

	if err := v.deleteCanonicalUnits("doc-1"); err != nil {
		t.Fatal(err)
	}
	var docs, terms, metas int
	_ = db.QueryRow(`SELECT COUNT(*) FROM rm_docs WHERE unit_id = 'doc-1'`).Scan(&docs)
	_ = db.QueryRow(`SELECT COUNT(*) FROM rm_terms WHERE unit_id = 'doc-1'`).Scan(&terms)
	_ = db.QueryRow(`SELECT COUNT(*) FROM rm_unit_meta WHERE unit_id = 'doc-1'`).Scan(&metas)
	if docs != 0 || terms != 0 || metas != 0 {
		t.Fatalf("delete must clear canonical rows: docs=%d terms=%d meta=%d", docs, terms, metas)
	}
}
