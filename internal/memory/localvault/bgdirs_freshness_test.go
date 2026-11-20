// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/memory/localvault"
)

// newFreshnessTestClient builds a local client whose transport writes into the same vault,
// so the background sync path runs end to end without a stub.
func newFreshnessTestClient(t *testing.T) (*localvault.Client, *localvault.Vault, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "freshness_test.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	vault, err := localvault.NewVault(db, []byte("freshness-test-secret"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	return localvault.NewClient(vault, localvault.NewLocalVaultTransport(vault)), vault, db
}

// TestBackgroundSyncRawHashShortCircuit verifies the pre-extraction content tier: a file
// whose mtime moved but whose bytes did not stops before parsing, and a real edit with the
// same stat shape still ingests.
func TestBackgroundSyncRawHashShortCircuit(t *testing.T) {
	root := newBackgroundTestDir(t)
	path := filepath.Join(root, "touched.md")
	body := "# 记录\n本地金库在编码前完成整形，明文不离开本机。\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	client, _, db := newFreshnessTestClient(t)
	ctx := context.Background()
	if _, err := client.AddBackgroundDir(root); err != nil {
		t.Fatalf("AddBackgroundDir: %v", err)
	}
	first, err := client.SyncBackgroundDirs(ctx)
	if err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if first.IngestedChunks == 0 {
		t.Fatalf("initial sync ingested no chunks: %+v", first)
	}

	var rawSHA string
	var firstMtime int64
	if err := db.QueryRow(`SELECT raw_sha256, mtime_ns FROM rm_bg_files WHERE abs_path = ?`, path).Scan(&rawSHA, &firstMtime); err != nil {
		t.Fatalf("load raw hash: %v", err)
	}
	if rawSHA == "" {
		t.Fatal("raw_sha256 must be recorded for the pre-extraction tier")
	}

	// Touch only: byte-identical file, later mtime.
	touched := time.Unix(0, firstMtime).Add(time.Hour)
	if err := os.Chtimes(path, touched, touched); err != nil {
		t.Fatalf("touch file: %v", err)
	}
	second, err := client.SyncBackgroundDirs(ctx)
	if err != nil {
		t.Fatalf("touch sync: %v", err)
	}
	if second.UnchangedByFileHash != 1 || second.IngestedFiles != 0 || second.IngestedChunks != 0 {
		t.Fatalf("touched-but-unedited file must stop before extraction: %+v", second)
	}
	var touchedSHA string
	var touchedMtime int64
	if err := db.QueryRow(`SELECT raw_sha256, mtime_ns FROM rm_bg_files WHERE abs_path = ?`, path).Scan(&touchedSHA, &touchedMtime); err != nil {
		t.Fatalf("reload raw hash: %v", err)
	}
	if touchedSHA != rawSHA || touchedMtime != touched.UnixNano() {
		t.Fatalf("receipt after touch = (%s, %d), want (%s, %d)", touchedSHA, touchedMtime, rawSHA, touched.UnixNano())
	}

	// A real edit with a forced mtime still has to ingest.
	if err := os.WriteFile(path, []byte(body+"新增一行。\n"), 0o600); err != nil {
		t.Fatalf("edit file: %v", err)
	}
	edited := touched.Add(time.Hour)
	if err := os.Chtimes(path, edited, edited); err != nil {
		t.Fatalf("touch edited file: %v", err)
	}
	third, err := client.SyncBackgroundDirs(ctx)
	if err != nil {
		t.Fatalf("edit sync: %v", err)
	}
	if third.IngestedFiles != 1 || third.IngestedChunks == 0 || third.UnchangedByFileHash != 0 {
		t.Fatalf("edited file must ingest: %+v", third)
	}
}

// TestBackgroundSyncReceiptsTrackPassAndFailure verifies the freshness receipt: a completed
// pass records last_success_at and counters, and a failed mount keeps that timestamp while
// exposing the error, so "nothing changed" and "never ran" never look alike.
func TestBackgroundSyncReceiptsTrackPassAndFailure(t *testing.T) {
	root := newBackgroundTestDir(t)
	path := filepath.Join(root, "mounted.md")
	if err := os.WriteFile(path, []byte("# 挂载\n固定目录的内容进入本地索引。\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	client, _, db := newFreshnessTestClient(t)
	ctx := context.Background()
	if _, err := client.AddBackgroundDir(root); err != nil {
		t.Fatalf("AddBackgroundDir: %v", err)
	}
	if _, err := client.SyncBackgroundDirs(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	states, err := localvault.ReadBackgroundSyncStates(db)
	if err != nil {
		t.Fatalf("read receipts: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("receipts = %d, want 1", len(states))
	}
	receipt := states[0]
	if receipt.LastSuccessAt <= 0 || receipt.LastError != "" {
		t.Fatalf("successful pass receipt = %+v", receipt)
	}
	if receipt.TrackedFiles != 1 {
		t.Fatalf("tracked files = %d, want 1", receipt.TrackedFiles)
	}
	if receipt.UnchangedFiles != 0 || receipt.IngestedFiles != 1 {
		t.Fatalf("ingest counters = %+v", receipt)
	}
	succeededAt := receipt.LastSuccessAt

	// Remove the mount root: the pass fails before it can scan.
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove mount: %v", err)
	}
	if _, err := client.SyncBackgroundDirs(ctx); err == nil {
		t.Fatal("sync over a removed mount root must fail")
	}
	states, err = localvault.ReadBackgroundSyncStates(db)
	if err != nil {
		t.Fatalf("read receipts after failure: %v", err)
	}
	if states[0].LastError == "" {
		t.Fatal("failed pass must record its error")
	}
	if states[0].LastSuccessAt != succeededAt {
		t.Fatalf("failed pass overwrote last success: %d, want %d", states[0].LastSuccessAt, succeededAt)
	}

	// A later successful pass clears the error again.
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("restore mount: %v", err)
	}
	if _, err := client.SyncBackgroundDirs(ctx); err != nil {
		t.Fatalf("recovery sync: %v", err)
	}
	states, err = localvault.ReadBackgroundSyncStates(db)
	if err != nil {
		t.Fatalf("read receipts after recovery: %v", err)
	}
	if states[0].LastError != "" || states[0].LastSuccessAt < succeededAt {
		t.Fatalf("recovered receipt = %+v", states[0])
	}
}
