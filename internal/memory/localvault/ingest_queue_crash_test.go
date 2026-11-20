// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	crashHelperModeEnv = "RM_QUEUE_CRASH_MODE"
	crashHelperDBEnv   = "RM_QUEUE_CRASH_DB"
	crashDocID         = "crash-queued-doc"
	crashDocText       = "crash queued text"
)

// TestIngestQueueCrashHelper is the child half of the crash tests. It performs
// one queue step and then blocks until the parent kills it, so the parent can
// observe the database exactly as a killed process left it.
func TestIngestQueueCrashHelper(t *testing.T) {
	mode := os.Getenv(crashHelperModeEnv)
	if mode == "" {
		t.Skip("crash helper")
	}
	dbPath := os.Getenv(crashHelperDBEnv)
	if dbPath == "" {
		t.Fatal("crash helper requires " + crashHelperDBEnv)
	}
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	vault, err := NewVault(db, []byte("crash-helper-key"))
	if err != nil {
		t.Fatal(err)
	}
	item, ok, err := vault.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0), time.Minute)
	if err != nil || !ok {
		t.Fatalf("crash helper claim: ok=%v err=%v", ok, err)
	}
	switch mode {
	case "claim":
		fmt.Println("claimed")
	case "apply":
		var payload struct {
			Version   int    `json:"version"`
			DocID     string `json:"doc_id"`
			Plaintext string `json:"plaintext"`
		}
		if err := json.Unmarshal(item.RawPayload, &payload); err != nil {
			t.Fatal(err)
		}
		worker := NewLocalClient(vault.CloneWithSharedSlotCache())
		worker.SetIdentity(item.Identity.AssistantID, item.Identity.IncarnationID, item.Identity.Locus)
		prepared, err := worker.PrepareQueuedRemember(context.Background(), payload.DocID, payload.Plaintext, item.OccurredAt, LayerEpisodic)
		if err != nil {
			t.Fatal(err)
		}
		applied, err := vault.ApplyPreparedQueuedIngest(context.Background(), item, prepared)
		if err != nil || applied.Status != IngestQueueApplied {
			t.Fatalf("crash helper apply: status=%q err=%v", applied.Status, err)
		}
		fmt.Println("applied")
	default:
		t.Fatalf("unknown crash helper mode %q", mode)
	}
	// Wait for the parent's SIGKILL. A timer keeps the runtime from treating the
	// idle process as a deadlock and exiting before the kill arrives.
	time.Sleep(10 * time.Minute)
}

func runCrashChild(t *testing.T, mode, dbPath string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestIngestQueueCrashHelper$", "-test.v=false")
	cmd.Env = append(os.Environ(), crashHelperModeEnv+"="+mode, crashHelperDBEnv+"="+dbPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	handshake := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "claimed" || line == "applied" {
				handshake <- nil
				return
			}
		}
		handshake <- fmt.Errorf("child exited before handshake: %v", scanner.Err())
	}()
	select {
	case err := <-handshake:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("crash helper handshake timed out")
	}
	return cmd
}

func openCrashVault(t *testing.T, dbPath string) (*Vault, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	vault, err := NewVault(db, []byte("crash-helper-key"))
	if err != nil {
		t.Fatal(err)
	}
	return vault, db
}

func enqueueCrashItem(t *testing.T, vault *Vault) IngestQueueItem {
	t.Helper()
	payload := []byte(`{"version":1,"doc_id":"` + crashDocID + `","plaintext":"` + crashDocText + `"}`)
	items, err := vault.EnqueueIngestQueue(context.Background(), []IngestQueueRequest{{
		RequestKey: "remember:v1:" + crashDocID,
		OccurredAt: 1700000000,
		Identity:   VaultIdentity{AssistantID: "crash", IncarnationID: "crash-1", Locus: "Rolling-Memory"},
		RawPayload: payload,
	}})
	if err != nil || len(items) != 1 {
		t.Fatalf("enqueue crash item: items=%d err=%v", len(items), err)
	}
	return items[0]
}

func TestIngestQueueClaimSurvivesKillBeforeApply(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "claim-kill.sqlite")
	vault, _ := openCrashVault(t, dbPath)
	enqueueCrashItem(t, vault)
	child := runCrashChild(t, "claim", dbPath)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()

	reopened, db := openCrashVault(t, dbPath)
	var status string
	var attempt int64
	if err := db.QueryRow(`SELECT status, attempt FROM rm_ingest_queue`).Scan(&status, &attempt); err != nil {
		t.Fatal(err)
	}
	if status != IngestQueueRunning || attempt != 1 {
		t.Fatalf("killed claim row = status=%q attempt=%d", status, attempt)
	}
	if got := countUnitRows(t, db, "rm_docs", crashDocID); got != 0 {
		t.Fatalf("killed claim wrote %d canonical rows", got)
	}
	claim, ok, err := reopened.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0).Add(2*time.Minute), time.Minute)
	if err != nil || !ok || claim.Attempt != 2 {
		t.Fatalf("reclaim after kill: ok=%v attempt=%d err=%v", ok, claim.Attempt, err)
	}
	prepared := prepareQueuedDoc(t, reopened, crashDocID, crashDocText)
	if _, err := reopened.ApplyPreparedQueuedIngest(context.Background(), claim, prepared); err != nil {
		t.Fatal(err)
	}
	if got := countUnitRows(t, db, "rm_docs", crashDocID); got != 1 {
		t.Fatalf("canonical rows after reclaim = %d, want 1", got)
	}
}

func TestIngestQueueApplyCommitSurvivesKill(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "apply-kill.sqlite")
	vault, _ := openCrashVault(t, dbPath)
	enqueueCrashItem(t, vault)
	child := runCrashChild(t, "apply", dbPath)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()

	reopened, db := openCrashVault(t, dbPath)
	var status string
	var attempt, appliedSeq int64
	if err := db.QueryRow(`SELECT status, attempt, applied_change_seq FROM rm_ingest_queue`).Scan(&status, &attempt, &appliedSeq); err != nil {
		t.Fatal(err)
	}
	if status != IngestQueueApplied || attempt != 1 || appliedSeq <= 0 {
		t.Fatalf("killed apply row = status=%q attempt=%d applied=%d", status, attempt, appliedSeq)
	}
	if got := countUnitRows(t, db, "rm_docs", crashDocID); got != 1 {
		t.Fatalf("canonical rows after killed apply = %d, want 1", got)
	}
	if got := countUnitRows(t, db, "rm_unit_meta", crashDocID); got != 1 {
		t.Fatalf("metadata rows after killed apply = %d, want 1", got)
	}
	if _, ok, err := reopened.ClaimIngestQueue(context.Background(), time.Unix(1700000000, 0).Add(24*time.Hour), time.Minute); err != nil || ok {
		t.Fatalf("applied row reclaimed after kill: ok=%v err=%v", ok, err)
	}
	first, err := reopened.IngestQueueStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Applied != 1 || first.Pending != 0 || first.Running != 0 {
		t.Fatalf("queue stats after killed apply = %+v", first)
	}
}
