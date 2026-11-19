// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package e2e_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestIntegrityCheckAfterWrites writes through the CLI 现役写侧（import 转录）与
// recall 钩子，然后要求沙箱库 PRAGMA integrity_check 为 ok 且 CLI 自身 check 一致。
func TestIntegrityCheckAfterWrites(t *testing.T) {
	sb := newSandbox(t)
	transcript := writeTranscript(t, filepath.Join(sb.base, "integrity.jsonl"))
	seeded := sb.run("import", transcript, "--json")
	assertExit(t, seeded, 0, "import")

	hook := sb.runInput(`{"session_id":"e2e","prompt":"老张 猫"}`, "hook", "recall", "--json")
	assertExit(t, hook, 0, "hook recall")

	connection, err := sql.Open("sqlite", sb.db)
	if err != nil {
		t.Fatalf("open %s: %v", sb.db, err)
	}
	defer connection.Close()

	var integrity string
	if err := connection.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatalf("PRAGMA integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("PRAGMA integrity_check = %q", integrity)
	}

	// The import landed in the sandbox file: two content-addressed vault units
	// and no legacy graph at all — fresh databases bootstrap thin, so the
	// canonical JSONL import never creates trace/event/assertion tables and
	// the CLI writes no assertions.
	var units, legacyTables int
	if err := connection.QueryRow("SELECT count(*) FROM rm_units").Scan(&units); err != nil {
		t.Fatalf("count rm_units: %v", err)
	}
	if units != 2 {
		t.Fatalf("vault units = %d, want 2", units)
	}
	if err := connection.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('trace','event','assertion','node','node_fts','trace_fts')",
	).Scan(&legacyTables); err != nil {
		t.Fatalf("count legacy tables: %v", err)
	}
	if legacyTables != 0 {
		t.Fatalf("thin import must not create legacy graph tables: found %d", legacyTables)
	}

	check := sb.run("check", "--json")
	assertExit(t, check, 0, "check")
	if checked := jsonObject(t, check.stdout); checked["ok"] != true {
		t.Fatalf("check = %s", check.stdout)
	}
}
