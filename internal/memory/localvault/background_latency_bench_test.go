// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

// BenchmarkRecallWhileIngesting measures the read path while a separate
// connection keeps writing turns. The reader never builds: it opens the
// published slot, applies the bounded change log and searches.
func BenchmarkRecallWhileIngesting(b *testing.B) {
	temp := b.TempDir()
	dbPath := filepath.Join(temp, "recall_ingest.sqlite")
	writerDB, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		b.Fatal(err)
	}
	defer writerDB.Close()
	writer, err := NewVault(writerDB, []byte("recall-ingest-key"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		text := fmt.Sprintf("memory baseline unit %d: the alpha beta ledger records the harbour decision.", i)
		if err := writer.SaveLocalPlaintext(fmt.Sprintf("seed-%d", i), 1700000000+int64(i), text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := writer.RebuildCorpusSnapshot(context.Background()); err != nil {
		b.Fatal(err)
	}

	readerDB, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		b.Fatal(err)
	}
	defer readerDB.Close()
	reader, err := NewVault(readerDB, []byte("recall-ingest-key"))
	if err != nil {
		b.Fatal(err)
	}
	reader.SetBuildAllowed(false)
	if _, err := reader.loadCorpusSnapshot(context.Background()); err != nil {
		b.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			text := fmt.Sprintf("memory background unit %d: the alpha beta ingress keeps arriving.", i)
			_ = writer.SaveLocalPlaintext(fmt.Sprintf("bg-%d", i), 1700100000+int64(i), text, nil, IngestResponse{Layer: "episodic"})
			time.Sleep(100 * time.Millisecond)
		}
	}()

	latencies := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		started := time.Now()
		if _, err := reader.SearchClientSDKDefault("memory alpha beta ledger", 10); err != nil {
			b.Fatal(err)
		}
		latencies = append(latencies, time.Since(started))
	}
	b.StopTimer()
	close(stop)
	wg.Wait()

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)*50/100]
	p99 := latencies[len(latencies)*99/100]
	b.ReportMetric(float64(p50.Microseconds())/1000, "p50-ms")
	b.ReportMetric(float64(p99.Microseconds())/1000, "p99-ms")
}

// BenchmarkRecallIdle is the baseline used to compare the ingesting run.
func BenchmarkRecallIdle(b *testing.B) {
	temp := b.TempDir()
	dbPath := filepath.Join(temp, "recall_idle.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	vault, err := NewVault(db, []byte("recall-idle-key"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		text := fmt.Sprintf("memory baseline unit %d: the alpha beta ledger records the harbour decision.", i)
		if err := vault.SaveLocalPlaintext(fmt.Sprintf("seed-%d", i), 1700000000+int64(i), text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := vault.RebuildCorpusSnapshot(context.Background()); err != nil {
		b.Fatal(err)
	}
	vault.SetBuildAllowed(false)
	if _, err := vault.loadCorpusSnapshot(context.Background()); err != nil {
		b.Fatal(err)
	}
	latencies := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		started := time.Now()
		if _, err := vault.SearchClientSDKDefault("memory alpha beta ledger", 10); err != nil {
			b.Fatal(err)
		}
		latencies = append(latencies, time.Since(started))
	}
	b.StopTimer()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)*50/100]
	p99 := latencies[len(latencies)*99/100]
	b.ReportMetric(float64(p50.Microseconds())/1000, "p50-ms")
	b.ReportMetric(float64(p99.Microseconds())/1000, "p99-ms")
}
