// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	phaseVocabSize     = 5000
	phaseSentencesPer  = 24
	phaseTokensPerSent = 14
	phaseEpochBase     = int64(1700000000)
)

func phaseCorpus(n int) []string {
	cumulative := make([]float64, phaseVocabSize)
	sum := 0.0
	for i := 1; i <= phaseVocabSize; i++ {
		sum += 1.0 / float64(i)
		cumulative[i-1] = sum
	}
	for i := range cumulative {
		cumulative[i] /= sum
	}
	docs := make([]string, n)
	for i := 0; i < n; i++ {
		rng := rand.New(rand.NewSource(int64(i)*2654435761 + 17))
		var builder strings.Builder
		for s := 0; s < phaseSentencesPer; s++ {
			for t := 0; t < phaseTokensPerSent; t++ {
				if t > 0 {
					builder.WriteByte(' ')
				}
				index := sort.SearchFloat64s(cumulative, rng.Float64())
				if index >= phaseVocabSize {
					index = phaseVocabSize - 1
				}
				fmt.Fprintf(&builder, "t%04d", index+1)
			}
			builder.WriteString(". ")
		}
		docs[i] = builder.String()
	}
	return docs
}

func phaseP50(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

// TestRecallPhaseBreakdown separates engine search from SDK hydration so a
// latency regression can be attributed to the shared kernel or the adapter.
// It is a measurement instrument: without RM_RECALL_PHASES it skips.
func TestRecallPhaseBreakdown(t *testing.T) {
	if os.Getenv("RM_RECALL_PHASES") != "1" {
		t.Skip("set RM_RECALL_PHASES=1 to measure recall phases")
	}
	scales := []int{300, 1000, 3000}
	if raw := strings.TrimSpace(os.Getenv("RM_RECALL_PHASE_SCALES")); raw != "" {
		scales = nil
		for _, part := range strings.Split(raw, ",") {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &n); err != nil || n <= 0 {
				t.Fatalf("invalid scale %q", part)
			}
			scales = append(scales, n)
		}
	}
	corpus := phaseCorpus(scales[len(scales)-1] + 2048)
	phaseTopK := 14
	if raw := strings.TrimSpace(os.Getenv("RM_RECALL_PHASE_TOPK")); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &phaseTopK); err != nil || phaseTopK <= 0 {
			t.Fatalf("invalid phase top-k %q", raw)
		}
	}
	phaseDelta := 0
	if raw := strings.TrimSpace(os.Getenv("RM_RECALL_PHASE_DELTA")); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &phaseDelta); err != nil || phaseDelta < 0 {
			t.Fatalf("invalid phase delta %q", raw)
		}
	}
	for _, n := range scales {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(t.TempDir(), "phase.sqlite")))
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = db.Close() })
			vault, err := NewVault(db, []byte("phase-breakdown-key"))
			if err != nil {
				t.Fatal(err)
			}
			client := NewLocalClient(vault)
			for i := 0; i < n; i++ {
				if _, err := client.Remember(context.Background(), fmt.Sprintf("phase-doc-%06d", i), corpus[i], phaseEpochBase+int64(i)*3600); err != nil {
					t.Fatalf("ingest %d: %v", i, err)
				}
			}
			cue := "t0001 t0002 t0003 t0004"
			if _, err := client.RecallWithOptions(context.Background(), cue, BlindQueryOptions{TopK: phaseTopK}); err != nil {
				t.Fatalf("warmup: %v", err)
			}
			for i := 0; i < phaseDelta; i++ {
				if _, err := client.Remember(context.Background(), fmt.Sprintf("phase-delta-%06d", i), corpus[n+i], phaseEpochBase+int64(n+i)*3600); err != nil {
					t.Fatalf("delta ingest %d: %v", i, err)
				}
			}
			snap, err := vault.loadCorpusSnapshot(context.Background())
			if err != nil || snap == nil {
				t.Fatalf("snapshot: %v", err)
			}
			var engineMS, sdkMS, productMS []float64
			for i := 0; i < 11; i++ {
				start := time.Now()
				_ = snap.searchSnapshotCoreEngines(vault, cue, nil, phaseTopK)
				engineMS = append(engineMS, float64(time.Since(start).Microseconds())/1000)
			}
			for i := 0; i < 11; i++ {
				start := time.Now()
				if _, err := vault.SearchClientSDKDefault(cue, phaseTopK); err != nil {
					t.Fatal(err)
				}
				sdkMS = append(sdkMS, float64(time.Since(start).Microseconds())/1000)
			}
			for i := 0; i < 11; i++ {
				start := time.Now()
				if _, err := client.RecallWithOptions(context.Background(), cue, BlindQueryOptions{TopK: phaseTopK}); err != nil {
					t.Fatal(err)
				}
				productMS = append(productMS, float64(time.Since(start).Microseconds())/1000)
			}
			deltaDocs := 0
			if snap.coreEngine != nil {
				deltaDocs = snap.coreEngine.DeltaDocCount()
			}
			t.Logf("[recall-phases] n=%d engine_p50=%.2fms sdk_p50=%.2fms product_p50=%.2fms delta_docs=%d",
				n, phaseP50(engineMS), phaseP50(sdkMS), phaseP50(productMS), deltaDocs)
		})
	}
}
