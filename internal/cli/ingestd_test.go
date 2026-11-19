// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestIngestWorkerLeaseSingleFlight(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "lease.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, acquired, err := tryAcquireIngestWorkerLease(db, 20*time.Second)
	if err != nil || !acquired || first == nil {
		t.Fatalf("first acquire: acquired=%v err=%v", acquired, err)
	}
	if _, second, err := tryAcquireIngestWorkerLease(db, 20*time.Second); err != nil || second {
		t.Fatalf("second acquire must fail: acquired=%v err=%v", second, err)
	}
	if err := first.heartbeatOnce(); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	var current string
	if err := db.QueryRow(`SELECT CAST(v AS TEXT) FROM rm_meta WHERE k = ?`, ingestWorkerLeaseKey).Scan(&current); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if current != first.value {
		t.Fatalf("heartbeat did not publish its value: row=%q lease=%q", current, first.value)
	}
	first.release()
	third, acquired, err := tryAcquireIngestWorkerLease(db, 20*time.Second)
	if err != nil || !acquired || third == nil {
		t.Fatalf("acquire after release: acquired=%v err=%v", acquired, err)
	}
	third.release()
}

func TestIngestWorkerLeaseExpires(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "lease_expired.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE rm_meta (k TEXT PRIMARY KEY, v BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	stale := fmt.Sprintf("999999:%d:stale", time.Now().Add(-time.Minute).Unix())
	if _, err := db.Exec(`INSERT INTO rm_meta(k, v) VALUES (?, ?)`, ingestWorkerLeaseKey, []byte(stale)); err != nil {
		t.Fatal(err)
	}
	lease, acquired, err := tryAcquireIngestWorkerLease(db, 20*time.Second)
	if err != nil || !acquired || lease == nil {
		t.Fatalf("expired lease must be reclaimed: acquired=%v err=%v", acquired, err)
	}
	lease.release()
}
