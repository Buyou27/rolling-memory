// Copyright 2026 Rocareer & Rolling PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEpisodicIndexMirrorsCanonical pins the write-side hand-over for the
// episodic index: every saved pointer must also land in rm_episodic_index and
// bump the canonical episodic revision counter, so readers can move to the
// canonical table before the thin table is dropped.
func TestEpisodicIndexMirrorsCanonical(t *testing.T) {
	db := migrationTestDB(t)
	v, err := NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "turn.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"alpha project note"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := "@ref:jsonl:" + path + ":0#0"
	text := "[user] alpha project note"
	pkt, _ := v.PackBlindPacket("doc_mirror", text, 1)
	if err := v.SaveLocalEpisodicPointerIndexed("doc_mirror", 1, ref, "", text, pkt, IngestResponse{}); err != nil {
		t.Fatalf("SaveLocalEpisodicPointerIndexed: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_episodic_index WHERE doc_id = 'doc_mirror'`).Scan(&count); err != nil {
		t.Fatalf("count canonical index: %v", err)
	}
	if count == 0 {
		t.Fatal("canonical episodic index row missing")
	}
	var rev int64
	if err := db.QueryRow(`SELECT episodic_rev FROM rm_revision WHERE id = 1`).Scan(&rev); err != nil {
		t.Fatalf("read episodic_rev: %v", err)
	}
	if rev <= 0 {
		t.Fatalf("canonical episodic_rev did not advance: %d", rev)
	}
}
