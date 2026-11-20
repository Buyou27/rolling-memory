// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"strings"
	"testing"
)

// TestSaveLocalPlaintextCommitsDocumentAndMetadataTogether pins the single
// transaction contract: when the metadata row cannot be written, the canonical
// document and its term rows never commit either.
func TestSaveLocalPlaintextCommitsDocumentAndMetadataTogether(t *testing.T) {
	vault, db := openIngestQueueVault(t)
	if _, err := db.Exec(`
		CREATE TRIGGER fail_unit_meta_write
		BEFORE INSERT ON rm_unit_meta
		BEGIN
			SELECT RAISE(ABORT, 'meta fault');
		END`); err != nil {
		t.Fatal(err)
	}
	err := vault.SaveLocalPlaintext("atomic-doc", 1700000000, "atomic single transaction text", nil, IngestResponse{Layer: LayerEpisodic})
	if err == nil || !strings.Contains(err.Error(), "meta fault") {
		t.Fatalf("save error = %v, want meta fault", err)
	}
	for _, table := range []string{"rm_docs", "rm_terms", "rm_unit_meta"} {
		if got := countUnitRows(t, db, table, "atomic-doc"); got != 0 {
			t.Fatalf("%s rows after failed metadata write = %d, want 0", table, got)
		}
	}
	if _, err := db.Exec(`DROP TRIGGER fail_unit_meta_write`); err != nil {
		t.Fatal(err)
	}
	if err := vault.SaveLocalPlaintext("atomic-doc", 1700000000, "atomic single transaction text", nil, IngestResponse{Layer: LayerEpisodic}); err != nil {
		t.Fatalf("retry after dropping the trigger: %v", err)
	}
	for _, table := range []string{"rm_docs", "rm_unit_meta"} {
		if got := countUnitRows(t, db, table, "atomic-doc"); got != 1 {
			t.Fatalf("%s rows after retry = %d, want 1", table, got)
		}
	}
	if got := countUnitRows(t, db, "rm_terms", "atomic-doc"); got == 0 {
		t.Fatal("term rows after retry = 0")
	}
}
