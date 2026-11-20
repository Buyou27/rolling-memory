// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/db"

	_ "modernc.org/sqlite"
)

// occurred_at concentration is what the cluster pipeline pays for: units that
// share one second are paired inside the same bucket.
func TestReadTimeConcentration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concentration.sqlite")
	connection, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(`CREATE TABLE rm_docs(unit_id TEXT PRIMARY KEY, occurred_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	empty, err := db.ReadTimeConcentration(connection)
	if err != nil {
		t.Fatalf("empty read: %v", err)
	}
	if empty.Docs != 0 || empty.PairsPerDoc != 0 {
		t.Fatalf("empty database = %+v, want zero", empty)
	}

	for i, occurredAt := range []int64{100, 100, 100, 200, 200} {
		if _, err := connection.Exec(
			`INSERT INTO rm_docs(unit_id, occurred_at) VALUES (?, ?)`,
			string(rune('a'+i)), occurredAt,
		); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.ReadTimeConcentration(connection)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Docs != 5 || got.DistinctInstants != 2 || got.LargestGroup != 3 {
		t.Fatalf("counts = %+v, want 5 docs / 2 instants / largest 3", got)
	}
	// Three units on one second contribute 3 pairs, two on the other contribute 1.
	if got.SameInstantPairs != 4 {
		t.Fatalf("same-instant pairs = %d, want 4", got.SameInstantPairs)
	}
	if got.PairsPerDoc != 4.0/5.0 {
		t.Fatalf("pairs per doc = %v, want 0.8", got.PairsPerDoc)
	}
	if got.Collapsed() {
		t.Fatal("a small corpus must not be reported as collapsed")
	}
}

func TestTimeConcentrationCollapsedLine(t *testing.T) {
	cases := []struct {
		pairsPerDoc float64
		want        bool
	}{
		{99.9, false},
		{100, true},
		{250, true},
	}
	for _, testCase := range cases {
		got := (db.TimeConcentration{PairsPerDoc: testCase.pairsPerDoc}).Collapsed()
		if got != testCase.want {
			t.Fatalf("Collapsed(%v) = %v, want %v", testCase.pairsPerDoc, got, testCase.want)
		}
	}
}

// A vault without the canonical table (a fresh or legacy file) reads as zero
// rather than failing the caller.
func TestReadTimeConcentrationWithoutTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bare.sqlite")
	connection, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	got, err := db.ReadTimeConcentration(connection)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Docs != 0 {
		t.Fatalf("docs = %d, want 0", got.Docs)
	}
}
