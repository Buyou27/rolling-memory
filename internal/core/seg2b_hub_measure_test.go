// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"fmt"
	"runtime"
	"sort"
	"testing"
	"time"
)

const hubTermHash = uint64(0xB0B0B0)

// hubPostingDocs builds a corpus where one term appears in every document (df=N) and each
// document carries only two unique terms, so a hub query's postings traversal dominates its
// allocation instead of being hidden by document size.
func hubPostingDocs(n int) []Doc {
	docs := make([]Doc, n)
	for i := 0; i < n; i++ {
		doc := epTestDoc(fmt.Sprintf("hub-%05d", i), 1700000000+int64(i)*3600)
		doc.Terms[hubTermHash] = Term{Hash: hubTermHash, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: hubTermHash, Sent: 0, Slot: 0}
		doc.Counts[hubTermHash] = 1
		doc.Positions[hubTermHash] = []TermPosition{{Sentence: 0, Slot: 0}}
		unique := uint64(90000 + i)
		doc.Terms[unique] = Term{Hash: unique, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: unique, Sent: 1, Slot: 0}
		doc.Counts[unique] = 1
		doc.Positions[unique] = []TermPosition{{Sentence: 1, Slot: 0}}
		docs[i] = doc
	}
	return docs
}

// TestHubPostingsQueryAllocationScaling measures RFC §六-10's baseline: what one query pays
// today for a df=N term, in bytes allocated and in wall time. The chunked design must keep
// the allocation flat in N by consuming postings as an iterator; this test records the
// linear baseline that gate will compare against.
func TestHubPostingsQueryAllocationScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement")
	}
	queryTerm := Term{Hash: hubTermHash, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: hubTermHash, Sent: 0, Slot: 0}
	qTerms := map[uint64]Term{hubTermHash: queryTerm}
	qPositions := map[uint64][]TermPosition{hubTermHash: {{Sentence: 0, Slot: 0}}}
	for _, n := range []int{2000, 8000} {
		eng := NewEngine(BuildIndex(hubPostingDocs(n)))
		eng.ensureClusterState()
		for warm := 0; warm < 3; warm++ {
			_ = eng.SearchWithPositions(qTerms, qPositions, 10)
		}
		alloc := medianQueryAlloc(func() { _ = eng.SearchWithPositions(qTerms, qPositions, 10) })
		elapsed := medianQueryDuration(func() { _ = eng.SearchWithPositions(qTerms, qPositions, 10) })
		t.Logf("df=N hub query: n=%6d alloc=%7d B (%5.1f B/unit) p50=%9s",
			n, alloc, float64(alloc)/float64(n), elapsed.Round(time.Microsecond))
	}
}

// medianQueryAlloc returns the median total bytes allocated per run. TotalAlloc counts every
// allocation regardless of collection, so a GC between samples cannot hide a spike.
func medianQueryAlloc(run func()) uint64 {
	samples := make([]uint64, 3)
	for i := range samples {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		run()
		runtime.ReadMemStats(&after)
		samples[i] = after.TotalAlloc - before.TotalAlloc
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2]
}

func medianQueryDuration(run func()) time.Duration {
	samples := make([]time.Duration, 3)
	for i := range samples {
		start := time.Now()
		run()
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2]
}
