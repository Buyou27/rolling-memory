// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math/rand"
	"sort"
	"testing"
)

// radixSortDyadPostingsReference is the previous out-of-place behaviour: a
// stable 1024-bucket MSB partition into a second array followed by full
// per-bucket sorts. It is the bit-exact oracle for the in-place variant.
func radixSortDyadPostingsReference(postings []dyadPosting) {
	const radixBits = 10
	const numBuckets = 1 << radixBits
	const shift = 64 - radixBits
	var bucketCounts [numBuckets]uint32
	for _, dp := range postings {
		bucketCounts[dp.pair.a>>shift]++
	}
	var bucketOffsets [numBuckets + 1]uint32
	for b := 0; b < numBuckets; b++ {
		bucketOffsets[b+1] = bucketOffsets[b] + bucketCounts[b]
	}
	var writeCur [numBuckets]uint32
	copy(writeCur[:], bucketOffsets[:numBuckets])
	partitioned := make([]dyadPosting, len(postings))
	for _, dp := range postings {
		b := dp.pair.a >> shift
		partitioned[writeCur[b]] = dp
		writeCur[b]++
	}
	copy(postings, partitioned)
	for b := 0; b < numBuckets; b++ {
		s, e := bucketOffsets[b], bucketOffsets[b+1]
		if e-s <= 1 {
			continue
		}
		sort.Sort(dyadPostingSort(postings[s:e]))
	}
}

func randomDyadPostings(rng *rand.Rand, n int, bucketMod uint64) []dyadPosting {
	out := make([]dyadPosting, n)
	for i := range out {
		a := rng.Uint64()
		if bucketMod > 0 {
			a = (a % bucketMod) << 54
		}
		out[i] = dyadPosting{pair: rmDyad{a: a, b: rng.Uint64()}, unit: int32(rng.Intn(1 << 20))}
	}
	return out
}

func equalDyadPostings(got, want []dyadPosting) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestRadixSortDyadPostingsInPlaceParity(t *testing.T) {
	cases := []struct {
		name      string
		n         int
		bucketMod uint64
	}{
		{"empty", 0, 0},
		{"single", 1, 0},
		{"small", 37, 0},
		{"single-bucket", 5000, 1},
		{"two-buckets", 5000, 2},
		{"sparse-buckets", 20000, 8},
		{"full-range", 20000, 0},
		{"just-over-insertion", 2000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(tc.n) + 1))
			got := randomDyadPostings(rng, tc.n, tc.bucketMod)
			want := append([]dyadPosting(nil), got...)
			radixSortDyadPostingsReference(want)
			radixSortDyadPostings(got)
			if !equalDyadPostings(got, want) {
				for i := range got {
					if i >= len(want) || got[i] != want[i] {
						t.Fatalf("element %d differs: got=%+v want=%+v", i, got[i], want[i])
					}
				}
				t.Fatalf("length mismatch: got=%d want=%d", len(got), len(want))
			}
		})
	}
}

func TestRadixSortDyadPostingsDuplicatesParity(t *testing.T) {
	// Same (a, b, unit) triples repeated: the total order makes the final
	// order unique, so both implementations must agree element-wise.
	rng := rand.New(rand.NewSource(7))
	base := randomDyadPostings(rng, 50, 4)
	got := make([]dyadPosting, 0, 5000)
	for i := 0; i < 100; i++ {
		got = append(got, base...)
	}
	want := append([]dyadPosting(nil), got...)
	radixSortDyadPostingsReference(want)
	radixSortDyadPostings(got)
	if !equalDyadPostings(got, want) {
		t.Fatal("duplicate-heavy input diverges from the out-of-place reference")
	}
}
