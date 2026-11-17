// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// B06 回归：CitedIDs 必须逐字往返（含重复、自引与空串），LoadIndex 与
// BuildIndex 的语料摘要逐位一致；旧 (unit_id, cited_id) 主键库经 OpenStore
// 自动迁移到 (unit_id, ord) 且遗留行无损。
func TestStoreCitationVerbatimRoundtripAdversarial(t *testing.T) {
	docs := []Doc{
		{UnitID: "ev_1", OccurredAt: 100, Terms: map[uint64]Term{11: {Hash: 11, Group: 11, Bits: 40, Flags: 1, Role: RolePrimary}}, Counts: map[uint64]int{11: 1}, Positions: map[uint64][]TermPosition{11: {{Sentence: 0, Slot: 1}}}},
		{
			UnitID:     "no_1",
			OccurredAt: 200,
			CitedIDs:   []string{"ev_1", "ev_1", "no_1", ""},
			Terms:      map[uint64]Term{22: {Hash: 22, Group: 22, Bits: 30, Flags: 1, Role: RolePrimary}},
			Counts:     map[uint64]int{22: 1},
			Positions:  map[uint64][]TermPosition{22: {{Sentence: 0, Slot: 2}}},
		},
	}
	mem := BuildIndex(docs)
	st, err := OpenStore(filepath.Join(t.TempDir(), "cites-verbatim.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Ingest(docs); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	u, ok := got.LookupUnit("no_1")
	if !ok {
		t.Fatal("no_1 missing after roundtrip")
	}
	want := []string{"ev_1", "ev_1", "no_1", ""}
	cited := got.UnitMeta[u].CitedIDs
	if len(cited) != len(want) {
		t.Fatalf("CitedIDs roundtrip wrong (want %v): %+v", want, cited)
	}
	for i := range want {
		if cited[i] != want[i] {
			t.Fatalf("CitedIDs roundtrip wrong (want %v): %+v", want, cited)
		}
	}
	if d, wantDigest := ComputeCorpusDigest(got), ComputeCorpusDigest(mem); d != wantDigest {
		t.Fatalf("corpus digest divergence: LoadIndex=%016x BuildIndex=%016x", d, wantDigest)
	}
}

// B06 迁移回归：旧 (unit_id, cited_id) 主键表在 OpenStore 时原位重建为
// (unit_id, ord)，遗留行逐行保留，且重建后可容纳重复引用。
func TestStoreLegacyCitationsSchemaMigrationAdversarial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-cites.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := `CREATE TABLE rm_citations (
    unit_id  TEXT NOT NULL,
    cited_id TEXT NOT NULL,
    ord      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (unit_id, cited_id)
);
INSERT INTO rm_citations(unit_id, cited_id, ord) VALUES ('u1','a',0), ('u1','b',1);`
	if _, err := db.Exec(legacy); err != nil {
		t.Fatal(err)
	}
	st, err := OpenStoreWithOptions(db, StoreOptions{OwnsDB: true})
	if err != nil {
		t.Fatalf("OpenStoreWithOptions: %v", err)
	}
	defer st.Close()
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'rm_citations'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "PRIMARY KEY (unit_id, ord)") {
		t.Fatalf("legacy citations table not rebuilt: %s", ddl)
	}
	rows, err := db.Query(`SELECT cited_id, ord FROM rm_citations WHERE unit_id = 'u1' ORDER BY ord`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var cid string
		var ord int
		if err := rows.Scan(&cid, &ord); err != nil {
			t.Fatal(err)
		}
		got = append(got, cid)
	}
	rows.Close()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("legacy citation rows not preserved: %v", got)
	}
	if _, err := db.Exec(`INSERT INTO rm_citations(unit_id, cited_id, ord) VALUES ('u1','a',2)`); err != nil {
		t.Fatalf("duplicate citation rejected after migration: %v", err)
	}
}
