// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type backgroundCloudRequest struct {
	header http.Header
	body   map[string]any
	raw    string
}

type backgroundCloudStub struct {
	mu          sync.Mutex
	server      *httptest.Server
	ingests     []backgroundCloudRequest
	purges      []backgroundCloudRequest
	ingestFails bool
	purgeFails  bool
}

func newBackgroundCloudStub(t *testing.T) *backgroundCloudStub {
	t.Helper()
	stub := &backgroundCloudStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		request := backgroundCloudRequest{header: r.Header.Clone(), body: body, raw: string(raw)}
		w.Header().Set("Content-Type", "application/json")

		stub.mu.Lock()
		defer stub.mu.Unlock()
		switch r.URL.Path {
		case "/open/memory/v1/ingest":
			stub.ingests = append(stub.ingests, request)
			if stub.ingestFails {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"ingest unavailable"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                    body["id"],
				"layer":                 body["layer"],
				"address_uri":           "C6:L1:B001#00",
				"address_coord64":       42,
				"waveform_bar":          "▃▆█▅▂",
				"waveform_hex":          "36AF52",
				"kept_sentence_indices": []int{0},
			})
		case "/open/memory/v1/purge":
			stub.purges = append(stub.purges, request)
			if stub.purgeFails {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"purge unavailable"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "mode": "units", "deleted_units": 1, "remaining_units": 0,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *backgroundCloudStub) requests() ([]backgroundCloudRequest, []backgroundCloudRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]backgroundCloudRequest(nil), s.ingests...), append([]backgroundCloudRequest(nil), s.purges...)
}

func TestBackgroundCLILocalModeNeverContactsBrain(t *testing.T) {
	env := newEnv(t)
	stub := newBackgroundCloudStub(t)
	t.Setenv("ROLLING_MEMORY_BRAIN_URL", stub.server.URL)
	t.Setenv("ROLLING_MEMORY_BRAIN_KEY", "test-memory-key")
	root := writeFile(t, filepath.Join(env.base, "mounted", "note.md"), "local background sentinel")
	result := run(t, "background", "add", "--path", filepath.Dir(root), "--json")
	if result.code != 0 {
		t.Fatalf("background add exit %d: %s%s", result.code, result.stdout, result.stderr)
	}
	result = run(t, "recall", "--q", "local background sentinel", "--json")
	if result.code != 0 {
		t.Fatalf("recall exit %d: %s%s", result.code, result.stdout, result.stderr)
	}
	ingests, purges := stub.requests()
	if len(ingests) != 0 || len(purges) != 0 {
		t.Fatalf("local mode sent %d ingests and %d purges", len(ingests), len(purges))
	}
}

func TestBackgroundCLIReadsBlindlySyncsAndPurges(t *testing.T) {
	env := newEnv(t)
	const plaintext = "confidential-background-body-must-stay-local"
	root := writeFile(t, filepath.Join(env.base, "mounted", "nested", "note.md"), "# Private runbook\n"+plaintext)
	dbPath := filepath.Join(env.base, "background.db")

	added := run(t, "background", "add", "--db", dbPath, "--path", filepath.Dir(root), "--json")
	if added.code != 0 {
		t.Fatalf("background add exit %d: %s%s", added.code, added.stdout, added.stderr)
	}
	addedPayload := jsonObject(t, added.stdout)
	syncPayload, _ := addedPayload["sync"].(map[string]any)
	if syncPayload["ingested_files"] != float64(1) || syncPayload["ingested_chunks"] != float64(1) {
		t.Fatalf("unexpected add sync report: %s", added.stdout)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open background db: %v", err)
	}
	defer db.Close()
	var addressedChunks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_bg_chunks WHERE address_uri != '' AND waveform_bar != ''`).Scan(&addressedChunks); err != nil {
		t.Fatalf("count addressed chunks: %v", err)
	}
	if addressedChunks != 1 {
		t.Fatalf("addressed chunks=%d, want 1", addressedChunks)
	}

	synced := run(t, "background", "sync", "--db", dbPath, "--json")
	if synced.code != 0 || jsonObject(t, synced.stdout)["unchanged_files"] != float64(1) {
		t.Fatalf("second sync = %s%s", synced.stdout, synced.stderr)
	}

	removed := run(t, "background", "remove", "--db", dbPath, "--path", filepath.Dir(root), "--json")
	if removed.code != 0 {
		t.Fatalf("background remove exit %d: %s%s", removed.code, removed.stdout, removed.stderr)
	}
}

func TestBackgroundRemoveDryRunPreviewsWithoutWrites(t *testing.T) {
	env := newEnv(t)
	root := writeFile(t, filepath.Join(env.base, "preview", "note.md"), "dry run preview document")
	dbPath := filepath.Join(env.base, "preview.db")

	added := run(t, "background", "add", "--db", dbPath, "--path", filepath.Dir(root), "--json")
	if added.code != 0 {
		t.Fatalf("background add exit %d: %s%s", added.code, added.stdout, added.stderr)
	}

	previewed := run(t, "background", "remove", "--db", dbPath, "--path", filepath.Dir(root), "--dry-run", "--json")
	if previewed.code != 0 {
		t.Fatalf("background remove --dry-run exit %d: %s%s", previewed.code, previewed.stdout, previewed.stderr)
	}
	obj := jsonObject(t, previewed.stdout)
	if obj["dry_run"] != true || obj["purged_files"] != float64(1) || obj["kept_files"] != float64(0) {
		t.Fatalf("unexpected dry-run receipt: %s", previewed.stdout)
	}
	preview, _ := obj["preview"].(map[string]any)
	if preview == nil || preview["purged_chunks"] == float64(0) || !strings.HasSuffix(preview["root_path"].(string), "preview") {
		t.Fatalf("unexpected dry-run preview payload: %s", previewed.stdout)
	}

	// A preview must write nothing: mount and file rows intact.
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open background db: %v", err)
	}
	defer db.Close()
	var mountCount, fileCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_bg_dirs`).Scan(&mountCount); err != nil {
		t.Fatalf("count mounts: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_bg_files`).Scan(&fileCount); err != nil {
		t.Fatalf("count files: %v", err)
	}
	if mountCount != 1 || fileCount != 1 {
		t.Fatalf("dry run wrote to the vault: mounts=%d files=%d", mountCount, fileCount)
	}

	// The real removal still lands afterwards and purges the previewed file.
	removed := run(t, "background", "remove", "--db", dbPath, "--path", filepath.Dir(root), "--json")
	if removed.code != 0 {
		t.Fatalf("background remove exit %d: %s%s", removed.code, removed.stdout, removed.stderr)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_bg_dirs`).Scan(&mountCount); err != nil {
		t.Fatalf("count mounts after removal: %v", err)
	}
	if mountCount != 0 {
		t.Fatalf("mount remains after successful remove")
	}
}

func TestBackgroundCLIExtensionFilterAndUpdate(t *testing.T) {
	env := newEnv(t)
	dir := filepath.Join(env.base, "filtered-docs")
	writeFile(t, filepath.Join(dir, "readme.md"), "# Markdown Doc\nKept when filtering by .md.")
	writeFile(t, filepath.Join(dir, "notes.txt"), "Plaintext note that is only included when .txt is selected.")
	writeFile(t, filepath.Join(dir, "dump.json"), `{"noise":"excluded"}`)
	dbPath := filepath.Join(env.base, "filtered.db")

	// Mount with --ext .md: only readme.md should be scanned and ingested.
	added := run(t, "background", "add", "--db", dbPath, "--path", dir, "--ext", ".md", "--json")
	if added.code != 0 {
		t.Fatalf("background add --ext .md failed (%d): %s%s", added.code, added.stdout, added.stderr)
	}
	addedObj := jsonObject(t, added.stdout)
	syncObj, _ := addedObj["sync"].(map[string]any)
	if syncObj["scanned_files"] != float64(1) || syncObj["ingested_files"] != float64(1) {
		t.Fatalf("expected only 1 .md file scanned/ingested, got %#v", syncObj)
	}

	// Re-mount the same directory expanding filter to .md,.txt: notes.txt is now ingested too.
	expanded := run(t, "background", "add", "--db", dbPath, "--path", dir, "--ext", ".md,.txt", "--json")
	if expanded.code != 0 {
		t.Fatalf("background add --ext .md,.txt failed (%d): %s%s", expanded.code, expanded.stdout, expanded.stderr)
	}
	expandedSync, _ := jsonObject(t, expanded.stdout)["sync"].(map[string]any)
	if expandedSync["scanned_files"] != float64(2) || expandedSync["ingested_files"] != float64(1) {
		t.Fatalf("expected 2 scanned and 1 newly ingested file, got %#v", expandedSync)
	}

	// Tighten filter back to .md: notes.txt is automatically purged.
	tightened := run(t, "background", "add", "--db", dbPath, "--path", dir, "--ext", ".md", "--json")
	if tightened.code != 0 {
		t.Fatalf("background add tighten --ext .md failed (%d): %s%s", tightened.code, tightened.stdout, tightened.stderr)
	}
	tightSync, _ := jsonObject(t, tightened.stdout)["sync"].(map[string]any)
	if tightSync["scanned_files"] != float64(1) || tightSync["deleted_files"] != float64(1) {
		t.Fatalf("expected 1 scanned and 1 deleted file after tightening filter, got %#v", tightSync)
	}
}
