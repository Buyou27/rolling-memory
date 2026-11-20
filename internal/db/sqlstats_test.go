// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package db

import (
	"path/filepath"
	"testing"
)

// TestOpenCountingCountsStatements verifies the measurement wrapper observes the
// statements the pool sends without changing what the pool returns: inserts, queries and
// explicit prepares each land in exactly one counter.
func TestOpenCountingCountsStatements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "counting.db")
	connection, stats, err := OpenCounting(path, LocalProfile())
	if err != nil {
		t.Fatalf("OpenCounting: %v", err)
	}
	defer connection.Close()

	if _, err := connection.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY, body TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	base := stats.Snapshot()

	if _, err := connection.Exec(`INSERT INTO probe (body) VALUES (?)`, "one"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var body string
	if err := connection.QueryRow(`SELECT body FROM probe WHERE id = 1`).Scan(&body); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if body != "one" {
		t.Fatalf("body = %q, want one", body)
	}
	stmt, err := connection.Prepare(`SELECT COUNT(*) FROM probe`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	var count int
	if err := stmt.QueryRow().Scan(&count); err != nil {
		t.Fatalf("prepared query: %v", err)
	}
	if err := stmt.Close(); err != nil {
		t.Fatalf("close stmt: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	delta := stats.Snapshot().Since(base)
	if delta.Executes < 1 {
		t.Fatalf("executes = %d, want >= 1", delta.Executes)
	}
	if delta.Queries < 1 {
		t.Fatalf("queries = %d, want >= 1", delta.Queries)
	}
	if delta.Prepares < 1 {
		t.Fatalf("prepares = %d, want >= 1", delta.Prepares)
	}
	if delta.Statements() != delta.Executes+delta.Queries+delta.Prepares {
		t.Fatalf("statement total disagrees with its parts: %+v", delta)
	}
}
