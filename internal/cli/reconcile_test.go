// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLegacyGraphDatabaseIsRefused locks the clean-install boundary: a
// database that still carries the retired assertion graph is refused with a
// re-import message instead of being served through the legacy reader.
func TestLegacyGraphDatabaseIsRefused(t *testing.T) {
	env := newEnv(t)
	dbPath := filepath.Join(env.base, "legacy.db")
	ensureLegacyGraphDB(t, dbPath)
	res := run(t, "sync", "status", "--db", dbPath, "--json")
	if res.code != 1 || !strings.Contains(res.stdout, "older Rolling-Memory release") {
		t.Fatalf("legacy database refusal: exit=%d stdout=%s stderr=%s", res.code, res.stdout, res.stderr)
	}
}

// TestEpisodicRebuildDryRunThenApply verifies the estimate and the fail-closed apply.
func TestEpisodicRebuildDryRunThenApply(t *testing.T) {
	env := newEnv(t)
	dbPath := filepath.Join(env.base, "memory.db")

	// Initialise the vault through the normal CLI path.
	if res := run(t, "sync", "status", "--json"); res.code != 0 {
		t.Fatalf("sync status exit=%d stderr=%s", res.code, res.stderr)
	}

	home := os.Getenv("HOME")
	sessionDir := filepath.Join(home, ".claude", "projects", "demo")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	lines := strings.Join([]string{
		`{"type":"user","sessionId":"s1","timestamp":"2026-11-06T03:00:00","message":{"role":"user","content":"请检查缓存失效。"}}`,
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-11-06T03:00:10","message":{"role":"assistant","stop_reason":"end_turn","content":[` +
			`{"type":"text","text":"先确认断言。"},` +
			`{"type":"text","text":"结论：缓存按 docID 失效。"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(sessionDir, "s1.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO rm_units(doc_id, layer, occurred_at, raw_text, sentences_json)
		VALUES ('ep_legacy_turn', 'episodic', 1762398000, '[user] 请检查缓存失效。
[assistant] 先确认断言。
结论：缓存按 docID 失效。', '[]')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy unit: %v", err)
	}
	_ = db.Close()

	// 1. Dry run: no deletion, plan reported.
	res := run(t, "sync", "reconcile", "--rebuild-episodic", "--json")
	if res.code != 0 {
		t.Fatalf("dry-run exit=%d stdout=%s stderr=%s", res.code, res.stdout, res.stderr)
	}
	dry := jsonObject(t, res.stdout)
	if dry["mode"] != "dry_run" {
		t.Fatalf("expected dry_run mode, got %v", dry["mode"])
	}
	if dry["units_removed"].(float64) < 1 || dry["new_units"].(float64) < 3 {
		t.Fatalf("dry-run plan = %+v", dry)
	}
	db, _ = sql.Open("sqlite", "file:"+dbPath)
	var legacyAfterDry int
	_ = db.QueryRow(`SELECT count(*) FROM rm_units WHERE doc_id = 'ep_legacy_turn'`).Scan(&legacyAfterDry)
	_ = db.Close()
	if legacyAfterDry != 1 {
		t.Fatalf("dry-run must not delete anything, legacy row count = %d", legacyAfterDry)
	}

	// 2. Apply is refused because source completeness cannot be established.
	res = run(t, "sync", "reconcile", "--rebuild-episodic", "--yes", "--json")
	if res.code == 0 || !strings.Contains(res.stdout, "apply disabled") {
		t.Fatalf("apply exit=%d stdout=%s stderr=%s", res.code, res.stdout, res.stderr)
	}
	db, _ = sql.Open("sqlite", "file:"+dbPath)
	defer db.Close()
	var legacyAfterApply int
	if err := db.QueryRow(`SELECT count(*) FROM rm_units WHERE doc_id = 'ep_legacy_turn'`).Scan(&legacyAfterApply); err != nil {
		t.Fatalf("count legacy: %v", err)
	}
	if legacyAfterApply != 1 {
		t.Fatalf("apply must preserve legacy unit, count = %d", legacyAfterApply)
	}
}
