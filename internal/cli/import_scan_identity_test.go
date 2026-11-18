// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/episodicunit"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// importScanCompletedTurn is one completed claude-code style turn: the assistant
// row carries the explicit completion evidence the shared turn reader requires.
const importScanCompletedTurn = `{"cwd":%q,"type":"user","message":{"role":"user","content":"老张养了一只猫。"}}` + "\n" +
	`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"好的，记下了。"}}` + "\n"

// importScanHome isolates one temporary HOME whose claude-code transcript root
// holds the file under test.
func importScanHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
	t.Setenv("ZCODE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("ROLLING_MEMORY_HARNESS", "")
	return home
}

func writeScannedSession(t *testing.T, home, body string) string {
	t.Helper()
	path := filepath.Join(home, ".claude", "projects", "demo", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newImportScanMemory(t *testing.T, scope string) *facade.Memory {
	t.Helper()
	memory, err := facade.New(&config.Config{DB: filepath.Join(t.TempDir(), "memory.db"), Scope: scope, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { memory.Close() })
	return memory
}

func episodicDocIDs(t *testing.T, memory *facade.Memory) []string {
	t.Helper()
	rows, err := memory.Store.DB().Query(`SELECT doc_id FROM thin_vault_docs WHERE layer = 'episodic' OR layer = '' ORDER BY doc_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(ids)
	return ids
}

func countEpisodicDocs(t *testing.T, memory *facade.Memory) int {
	t.Helper()
	var count int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM thin_vault_docs WHERE layer = 'episodic' OR layer = ''`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func repoHome(t *testing.T, home string) string {
	t.Helper()
	repo := filepath.Join(home, "Repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return repo
}

// TestImportScanCanonicalIdentityBothOrders locks the single-identity convergence
// between `import` and the background scan: whichever order they run in, one JSONL
// transcript must yield exactly the same two content-addressed documents, and a
// second pass must not add any.
func TestImportScanCanonicalIdentityBothOrders(t *testing.T) {
	for _, order := range []string{"import-then-scan", "scan-then-import"} {
		t.Run(order, func(t *testing.T) {
			home := importScanHome(t)
			path := writeScannedSession(t, home, fmt.Sprintf(importScanCompletedTurn, repoHome(t, home)))
			memory := newImportScanMemory(t, "mem:importscan")
			runImport := func() map[string]any {
				t.Helper()
				result, err := memory.ImportFile(path, nil, nil, "shared", false)
				if err != nil {
					t.Fatalf("import: %v", err)
				}
				return result
			}
			runScan := func() {
				t.Helper()
				if err := runEpisodicSourceScanTick(context.Background(), memory, home); err != nil {
					t.Fatalf("scan: %v", err)
				}
			}
			if order == "import-then-scan" {
				runImport()
				runScan()
			} else {
				runScan()
				runImport()
			}
			runImport()
			runScan()
			want := []string{
				episodicunit.DocID("mem:importscan", "[user] 老张养了一只猫。"),
				episodicunit.DocID("mem:importscan", "[assistant] 好的，记下了。"),
			}
			sort.Strings(want)
			got := episodicDocIDs(t, memory)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("docs = %v, want %v", got, want)
			}
			for _, id := range got {
				var pointer, assistant, session, locus string
				if err := memory.Store.DB().QueryRow(`SELECT raw_text, assistant_id, incarnation_id, locus FROM thin_vault_docs WHERE doc_id = ?`, id).
					Scan(&pointer, &assistant, &session, &locus); err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(pointer, "@ref:jsonl:"+path+":0#") {
					t.Fatalf("pointer = %q", pointer)
				}
				if assistant != "claude-code" || session != "session" || locus != "Repo" {
					t.Fatalf("identity = %q/%q/%q", assistant, session, locus)
				}
			}
		})
	}
}

// TestImportScanTombstoneBlocksResurrection locks the tombstone across both
// write paths: once the canonical document is deleted, neither `import` nor the
// scanner may bring it back.
func TestImportScanTombstoneBlocksResurrection(t *testing.T) {
	home := importScanHome(t)
	path := writeScannedSession(t, home, fmt.Sprintf(importScanCompletedTurn, repoHome(t, home)))
	memory := newImportScanMemory(t, "mem:tombstone")
	if _, err := memory.ImportFile(path, nil, nil, "shared", false); err != nil {
		t.Fatal(err)
	}
	docID := episodicunit.DocID("mem:tombstone", "[user] 老张养了一只猫。")
	client, err := memory.ThinClient()
	if err != nil {
		t.Fatal(err)
	}
	var pointer string
	if err := memory.Store.DB().QueryRow(`SELECT raw_text FROM thin_vault_docs WHERE doc_id = ?`, docID).Scan(&pointer); err != nil {
		t.Fatal(err)
	}
	client.Vault.RecordDeletedUnits("episodic", docID)
	deleted, err := client.Vault.DeleteLocalEpisodicUnit(docID, pointer)
	if err != nil || !deleted {
		t.Fatalf("delete: deleted=%v err=%v", deleted, err)
	}
	result, err := memory.ImportFile(path, nil, nil, "shared", false)
	if err != nil {
		t.Fatal(err)
	}
	if inserted, _ := result["inserted"].(int); inserted != 0 {
		t.Fatalf("tombstoned import inserted=%v: %#v", result["inserted"], result)
	}
	if err := runEpisodicSourceScanTick(context.Background(), memory, home); err != nil {
		t.Fatal(err)
	}
	want := episodicunit.DocID("mem:tombstone", "[assistant] 好的，记下了。")
	if got := episodicDocIDs(t, memory); len(got) != 1 || got[0] != want {
		t.Fatalf("docs = %v, want [%s]", got, want)
	}
}

// TestImportJSONLIncompleteTailSkippedAndCounted locks the shared completion
// semantics for the manual import path: completed turns land, the unfinished
// tail turn is counted and never written.
func TestImportJSONLIncompleteTailSkippedAndCounted(t *testing.T) {
	home := importScanHome(t)
	body := fmt.Sprintf(importScanCompletedTurn, repoHome(t, home)) +
		`{"type":"user","message":{"role":"user","content":"未完成的尾轮提问。"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":"尾轮回答。"}}` + "\n"
	path := writeScannedSession(t, home, body)
	memory := newImportScanMemory(t, "mem:incomplete")
	result, err := memory.ImportFile(path, nil, nil, "shared", false)
	if err != nil {
		t.Fatal(err)
	}
	if result["skipped_incomplete_tail"] != 1 {
		t.Fatalf("skipped_incomplete_tail = %#v: %#v", result["skipped_incomplete_tail"], result)
	}
	if result["messages"] != 2 || result["inserted"] != 2 {
		t.Fatalf("receipt = %#v", result)
	}
	var tail int
	if err := memory.Store.DB().QueryRow(`SELECT COUNT(*) FROM thin_vault_docs WHERE raw_text LIKE '%尾轮%'`).Scan(&tail); err != nil || tail != 0 {
		t.Fatalf("incomplete tail written: tail=%d err=%v", tail, err)
	}
	if got := countEpisodicDocs(t, memory); got != 2 {
		t.Fatalf("docs = %d, want 2", got)
	}
}
