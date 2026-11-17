// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func dayCosDemandDoc(i int, ts int64) Doc {
	doc := epTestDoc("dense-"+strconv.Itoa(i), ts)
	const shared = uint64(0xC05)
	doc.Terms[shared] = Term{Hash: shared, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: shared, Sent: 1, Slot: 2}
	doc.Counts[shared] = 1
	doc.Positions[shared] = []TermPosition{{Sentence: 0, Slot: 2}}
	return doc
}

func dayCosDemandDocs(n int) []Doc {
	docs := make([]Doc, n)
	for i := range docs {
		docs[i] = dayCosDemandDoc(i, 1700000000+int64(i)*3600)
	}
	return docs
}

func TestDayCosOnDemandParityDenseEpisode(t *testing.T) {
	for _, n := range []int{1, 2, 16, 64, 192} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			idx := BuildIndex(dayCosDemandDocs(n))
			eng := NewEngine(idx)
			st := eng.ensureClusterState()
			sc := eng.ensureQueryScratch(idx.N, st.clusterN)
			if n >= 16 {
				allPairs := n * (n - 1) / 2
				if got := len(st.dayCosKeys); got >= allPairs {
					t.Fatalf("eager all-pairs precompute remains: cached=%d all=%d", got, allPairs)
				}
				if st.episodeOf[0] < 0 || st.episodeOf[0] != st.episodeOf[n-1] {
					t.Fatalf("test corpus must form one dense episode: first=%d last=%d", st.episodeOf[0], st.episodeOf[n-1])
				}
				got := st.docSubjectCos(idx, sc, 0, int32(n-1))
				want := st.computeDocSubjectCos(idx, 0, int32(n-1))
				if math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("far co-episode pair mismatch: got=%.17g want=%.17g", got, want)
				}
				if len(sc.dayCos) != 1 {
					t.Fatalf("on-demand pair was not memoized in the engine scratch: entries=%d", len(sc.dayCos))
				}
				if again := st.docSubjectCos(idx, sc, 0, int32(n-1)); math.Float64bits(again) != math.Float64bits(want) || len(sc.dayCos) != 1 {
					t.Fatalf("memoized pair changed: got=%.17g want=%.17g entries=%d", again, want, len(sc.dayCos))
				}
			}
			if n <= 64 {
				for a := 0; a < n; a++ {
					for b := a + 1; b < n; b++ {
						got := st.docSubjectCos(idx, sc, int32(a), int32(b))
						want := st.computeDocSubjectCos(idx, int32(a), int32(b))
						if math.Float64bits(got) != math.Float64bits(want) {
							t.Fatalf("pair (%d,%d) mismatch: got=%.17g want=%.17g", a, b, got, want)
						}
					}
				}
			}
		})
	}
}

func TestDayCosOnDemandConcurrentParity(t *testing.T) {
	const n = 64
	idx := BuildIndex(dayCosDemandDocs(n))
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan string, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			localEng := *eng
			localEng.ResetScratch()
			sc := localEng.ensureQueryScratch(idx.N, st.clusterN)
			for a := 0; a < n; a++ {
				for b := a + 1; b < n; b++ {
					if (a+b)%workers != worker {
						continue
					}
					got := st.docSubjectCos(idx, sc, int32(a), int32(b))
					want := st.computeDocSubjectCos(idx, int32(a), int32(b))
					if math.Float64bits(got) != math.Float64bits(want) {
						errs <- fmt.Sprintf("pair (%d,%d): got=%.17g want=%.17g", a, b, got, want)
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestDayCosOnDemandDiskRoundtripParity(t *testing.T) {
	const n = 64
	idx := BuildIndex(dayCosDemandDocs(n))
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	first := filepath.Join(t.TempDir(), "first.zcsr")
	if err := WriteDiskClusterCSR(first, eng); err != nil {
		t.Fatalf("WriteDiskClusterCSR(first): %v", err)
	}
	disk, err := OpenDiskClusterEngine(first)
	if err != nil {
		t.Fatalf("OpenDiskClusterEngine(first): %v", err)
	}
	defer disk.Close()
	diskScratch := disk.ensureQueryScratch(disk.Index.N, disk.clusterState.clusterN)
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			got := disk.clusterState.docSubjectCos(disk.Index, diskScratch, int32(a), int32(b))
			want := st.computeDocSubjectCos(idx, int32(a), int32(b))
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("first disk pair (%d,%d): got=%.17g want=%.17g", a, b, got, want)
			}
		}
	}
	second := filepath.Join(t.TempDir(), "second.zcsr")
	if err := WriteDiskClusterCSR(second, disk); err != nil {
		t.Fatalf("WriteDiskClusterCSR(second): %v", err)
	}
	reopened, err := OpenDiskClusterEngine(second)
	if err != nil {
		t.Fatalf("OpenDiskClusterEngine(second): %v", err)
	}
	defer reopened.Close()
	reopenedScratch := reopened.ensureQueryScratch(reopened.Index.N, reopened.clusterState.clusterN)
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			got := reopened.clusterState.docSubjectCos(reopened.Index, reopenedScratch, int32(a), int32(b))
			want := st.computeDocSubjectCos(idx, int32(a), int32(b))
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("second disk pair (%d,%d): got=%.17g want=%.17g", a, b, got, want)
			}
		}
	}
}

func TestDayCosScratchInvalidatedOnAppend(t *testing.T) {
	eng := NewEngine(BuildIndex(dayCosDemandDocs(16)))
	st := eng.ensureClusterState()
	sc := eng.ensureQueryScratch(eng.Index.N, st.clusterN)
	_ = st.docSubjectCos(eng.Index, sc, 0, 15)
	if len(sc.dayCos) == 0 {
		t.Fatal("expected an on-demand cache entry before append")
	}
	eng.AppendDocs([]Doc{dayCosDemandDoc(16, 1700000000+16*3600)})
	if eng.queryScratch != nil {
		t.Fatal("append must invalidate the scratch cache derived from the old index")
	}
	st = eng.ensureClusterState()
	sc = eng.ensureQueryScratch(eng.Index.N, st.clusterN)
	got := st.docSubjectCos(eng.Index, sc, 0, 15)
	want := st.computeDocSubjectCos(eng.Index, 0, 15)
	if math.Float64bits(got) != math.Float64bits(want) {
		t.Fatalf("post-append pair mismatch: got=%.17g want=%.17g", got, want)
	}
}

func BenchmarkDenseEpisodeClusterState(b *testing.B) {
	for _, n := range []int{64, 256, 1024} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			docs := dayCosDemandDocs(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				eng := NewEngine(BuildIndex(docs))
				_ = eng.ensureClusterState()
			}
		})
	}
}
