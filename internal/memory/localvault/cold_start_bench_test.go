// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkColdStartFromSlot measures a fresh process opening the published
// generation and its docs sidecar. The corpus tables are not scanned.
func BenchmarkColdStartFromSlot(b *testing.B) {
	temp := b.TempDir()
	dbPath := filepath.Join(temp, "cold_start.sqlite")
	seedDB, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		b.Fatal(err)
	}
	seed, err := NewVault(seedDB, []byte("cold-start-key"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		text := fmt.Sprintf("memory cold start unit %d: alpha beta ledger harbour.", i)
		if err := seed.SaveLocalPlaintext(fmt.Sprintf("cold-%d", i), 1700000000+int64(i), text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := seed.RebuildCorpusSnapshot(context.Background()); err != nil {
		b.Fatal(err)
	}
	_ = seedDB.Close()

	b.ResetTimer()
	var totalWarmup time.Duration
	for i := 0; i < b.N; i++ {
		db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
		if err != nil {
			b.Fatal(err)
		}
		vault, err := NewVault(db, []byte("cold-start-key"))
		if err != nil {
			b.Fatal(err)
		}
		vault.SetBuildAllowed(false)
		started := time.Now()
		if err := vault.WarmupCorpusSnapshot(context.Background()); err != nil {
			b.Fatal(err)
		}
		totalWarmup += time.Since(started)
		_ = db.Close()
	}
	b.StopTimer()
	if b.N > 0 {
		b.ReportMetric(float64(totalWarmup.Microseconds())/float64(b.N)/1000, "warmup-ms")
	}
}
