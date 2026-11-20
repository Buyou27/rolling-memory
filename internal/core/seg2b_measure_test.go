// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"sort"
	"testing"
	"time"
)

// measurePhraseBlock is the append-order block model used by the 2b pre-implementation
// measurement: one immutable block per append group, keys sorted within the block and
// postings sorted by unit. A query binary-searches every block and merges the per-block
// unit lists, so no global rebuild is needed on append.
type measurePhraseBlock struct {
	keys      []rmDyad
	rowPtr    []uint32
	units     []int32
	prefixPtr []uint32 // 4097 entries: prefixPtr[b+1] counts keys with pair.a>>52 <= b
}

func buildMeasureBlocks(docDyads [][]rmDyad, k int) []measurePhraseBlock {
	if k <= 0 {
		k = 1
	}
	blocks := make([]measurePhraseBlock, 0, k)
	per := (len(docDyads) + k - 1) / k
	for start := 0; start < len(docDyads); start += per {
		end := start + per
		if end > len(docDyads) {
			end = len(docDyads)
		}
		postings := make([]dyadPosting, 0, (end-start)*64)
		for u := start; u < end; u++ {
			for _, p := range docDyads[u] {
				postings = append(postings, dyadPosting{pair: p, unit: int32(u)})
			}
		}
		sort.Sort(dyadPostingSort(postings))
		b := measurePhraseBlock{}
		for i, p := range postings {
			if i > 0 && p == postings[i-1] {
				continue
			}
			if len(b.keys) == 0 || b.keys[len(b.keys)-1] != p.pair {
				b.keys = append(b.keys, p.pair)
				b.rowPtr = append(b.rowPtr, uint32(len(b.units)))
			}
			b.units = append(b.units, p.unit)
		}
		b.rowPtr = append(b.rowPtr, uint32(len(b.units)))
		b.prefixPtr = make([]uint32, 4097)
		pi := 0
		for bucket := 0; bucket < 4096; bucket++ {
			for pi < len(b.keys) && int(b.keys[pi].a>>52) <= bucket {
				pi++
			}
			b.prefixPtr[bucket+1] = uint32(pi)
		}
		blocks = append(blocks, b)
	}
	return blocks
}

func measureBlockLookup(b *measurePhraseBlock, pair rmDyad) []int32 {
	lo, hi := 0, len(b.keys)
	if len(b.prefixPtr) == 4097 {
		bucket := pair.a >> 52
		lo, hi = int(b.prefixPtr[bucket]), int(b.prefixPtr[bucket+1])
	}
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if dyadLess(b.keys[mid], pair) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(b.keys) && b.keys[lo] == pair {
		return b.units[b.rowPtr[lo]:b.rowPtr[lo+1]]
	}
	return nil
}

// measureMergeUnitsInto performs the K-way unit merge a block-aware reader needs. Postings
// of the same pair from different blocks are disjoint for append-only corpora; the merge is
// still defensive about duplicates and keeps the canonical ascending-unit order.
func measureMergeUnitsInto(lists [][]int32, out []int32) []int32 {
	if len(lists) == 1 {
		return lists[0]
	}
	for {
		best, bestIdx := int32(0), -1
		for i, l := range lists {
			if len(l) == 0 {
				continue
			}
			if bestIdx < 0 || l[0] < best {
				best, bestIdx = l[0], i
			}
		}
		if bestIdx < 0 {
			return out
		}
		if len(out) == 0 || out[len(out)-1] != best {
			out = append(out, best)
		}
		lists[bestIdx] = lists[bestIdx][1:]
	}
}

// TestPhraseBlockFanoutMeasurement covers RFC §七 2b before implementation: it measures the
// append-order block model (K=1/2/4/8/11) against the current global phrase CSR, and checks
// that the K-way lookup reproduces the global postings exactly (order-preserving merge).
func TestPhraseBlockFanoutMeasurement(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement")
	}
	const docs = 1200
	const vocab = 20000
	corpus := make([]Doc, docs)
	for i := range corpus {
		corpus[i] = seg2aMeasureDoc(i)
	}
	eng := NewEngine(BuildIndex(corpus))
	st := eng.ensureClusterState()
	n := int(eng.Index.N)

	docDyads := make([][]rmDyad, n)
	occurrences := 0
	for u := 0; u < n; u++ {
		entries := st.docEntriesOf(int32(u))
		var ds []rmDyad
		if st.hasWidePos() {
			ds = st.extractDocDyadsExactInto(entries, nil)
		} else {
			ds, _ = st.extractDocDyadsInto(entries, nil, nil)
		}
		docDyads[u] = sortAndDedupRmDyads(ds)
		occurrences += len(docDyads[u])
	}
	globalKeys := st.phraseKeys
	t.Logf("corpus docs=%d globalKeys=%d perDocDyads=%d globalUnits=%d",
		n, len(globalKeys), occurrences, len(st.phraseUnits))
	sanity := buildMeasureBlocks(docDyads, 1)
	if len(sanity) != 1 {
		t.Fatalf("single-block model expected, got %d blocks", len(sanity))
	}
	if len(sanity[0].keys) != len(globalKeys) || len(sanity[0].units) != len(st.phraseUnits) {
		t.Fatalf("model sizes differ: keys %d/%d units %d/%d",
			len(sanity[0].keys), len(globalKeys), len(sanity[0].units), len(st.phraseUnits))
	}
	for i := range globalKeys {
		if sanity[0].keys[i] != globalKeys[i] {
			t.Fatalf("first key divergence at %d: model=(%d,%d) global=(%d,%d)",
				i, sanity[0].keys[i].a, sanity[0].keys[i].b, globalKeys[i].a, globalKeys[i].b)
		}
		if sanity[0].units[i] != st.phraseUnits[i] {
			t.Fatalf("first unit divergence at %d: model=%d global=%d", i, sanity[0].units[i], st.phraseUnits[i])
		}
	}

	// Sample query pairs: half existing keys (hits), half synthetic misses.
	const sample = 1000
	pairs := make([]rmDyad, 0, sample*2)
	for i := 0; i < sample && i < len(globalKeys); i++ {
		pairs = append(pairs, globalKeys[i*len(globalKeys)/sample])
	}
	missWant := sample
	state := uint64(0x9E3779B97F4A7C15)
	for len(pairs) < sample+missWant {
		state = state*6364136223846793005 + 1442695040888963407
		a := uint64(9000) + (state>>33)%vocab
		state = state*6364136223846793005 + 1442695040888963407
		b := uint64(9000) + (state>>33)%vocab
		if a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		pair := rmDyad{a: a, b: b}
		lo, hi := 0, len(globalKeys)
		for lo < hi {
			mid := int(uint(lo+hi) >> 1)
			if dyadLess(globalKeys[mid], pair) {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo < len(globalKeys) && globalKeys[lo] == pair {
			continue
		}
		pairs = append(pairs, pair)
	}

	// Baseline: current global CSR lookup. The measurement corpus is a full build, so
	// the base view is the global view (no append delta).
	start := time.Now()
	baselineHits := 0
	for _, pair := range pairs {
		baselineHits += len(st.phraseBasePostings(pair))
	}
	baselineCost := time.Since(start)
	t.Logf("global lookup pairs=%d hits=%d total=%s per-pair=%.3fus",
		len(pairs), baselineHits, baselineCost.Round(time.Microsecond),
		float64(baselineCost.Nanoseconds())/float64(len(pairs))/1000.0)

	var listsBuf [16][]int32
	mergeBuf := make([]int32, 0, 512)
	for _, k := range []int{1, 2, 4, 8, 11} {
		blocks := buildMeasureBlocks(docDyads, k)
		keysTotal := 0
		for i := range blocks {
			keysTotal += len(blocks[i].keys)
		}
		start = time.Now()
		lookupHits := 0
		for _, pair := range pairs {
			for i := range blocks {
				lookupHits += len(measureBlockLookup(&blocks[i], pair))
			}
		}
		lookupCost := time.Since(start)
		start = time.Now()
		hits, mismatches := 0, 0
		for _, pair := range pairs {
			lists := listsBuf[:0]
			for i := range blocks {
				if l := measureBlockLookup(&blocks[i], pair); len(l) > 0 {
					lists = append(lists, l)
				}
			}
			merged := measureMergeUnitsInto(lists, mergeBuf[:0])
			hits += len(merged)
			if want := st.phraseBasePostings(pair); len(merged) != len(want) {
				mismatches++
				if mismatches <= 3 {
					t.Logf("mismatch pair=(%d,%d) block=%d global=%d blockUnits=%v globalUnits=%v",
						pair.a, pair.b, len(merged), len(want), merged, want)
				}
			} else {
				for i := range merged {
					if merged[i] != want[i] {
						mismatches++
						break
					}
				}
			}
		}
		elapsed := time.Since(start)
		t.Logf("K=%2d blocks=%2d keys=%d dup=%.1f%% lookup=%s merge=%s per-pair=%.3fus hits=%d mismatches=%d",
			k, len(blocks), keysTotal, 100*float64(keysTotal-len(globalKeys))/float64(len(globalKeys)),
			lookupCost.Round(time.Microsecond), (elapsed - lookupCost).Round(time.Microsecond),
			float64(elapsed.Nanoseconds())/float64(len(pairs))/1000.0,
			hits, mismatches)
		if lookupHits != baselineHits {
			t.Fatalf("K=%d lookup hits=%d want %d", k, lookupHits, baselineHits)
		}
		if mismatches != 0 {
			t.Fatalf("K=%d block lookup differs from the global phrase CSR in %d pairs", k, mismatches)
		}
		if hits != baselineHits {
			t.Fatalf("K=%d hits=%d want %d", k, hits, baselineHits)
		}
	}

	// Append cost: one new document as a fresh immutable block versus the current global merge.
	newDoc := seg2aMeasureDoc(n)
	tmp := NewEngine(BuildIndex([]Doc{newDoc}))
	tmpState := tmp.ensureClusterState()
	var newDyads []rmDyad
	if tmpState.hasWidePos() {
		newDyads = tmpState.extractDocDyadsExactInto(tmpState.docEntriesOf(0), nil)
	} else {
		newDyads, _ = tmpState.extractDocDyadsInto(tmpState.docEntriesOf(0), nil, nil)
	}
	newDyads = sortAndDedupRmDyads(newDyads)
	delta := make([]dyadPosting, 0, len(newDyads))
	for _, p := range newDyads {
		delta = append(delta, dyadPosting{pair: p, unit: int32(n)})
	}
	sort.Sort(dyadPostingSort(delta))
	remap := make([]int32, n)
	for i := range remap {
		remap[i] = int32(i)
	}
	start = time.Now()
	_, _, _ = mergePhraseCSR(st, remap, delta)
	globalAppend := time.Since(start)

	start = time.Now()
	_ = buildMeasureBlocks([][]rmDyad{newDyads}, 1)
	blockAppend := time.Since(start)
	t.Logf("append one doc: newDyads=%d globalMerge=%s blockBuild=%s",
		len(newDyads), globalAppend.Round(time.Microsecond), blockAppend.Round(time.Microsecond))
	if globalAppend <= blockAppend {
		t.Logf("note: global merge not slower than one block build at this scale")
	}

	// The dual-order partner index is rebuilt by an O(P log P) sort inside buildPrefixTables
	// on every state materialization; measure it next to the main-order merge.
	start = time.Now()
	st.buildPrefixTables(n)
	prefixCost := time.Since(start)
	t.Logf("prefix tables rebuild (main buckets + dual sort + dayCos rows): %s", prefixCost.Round(time.Microsecond))
}
