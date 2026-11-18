// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// 本件钉住 sortUint64Asc 的升序等价性，并保留大切片基数排序相对比较排序的
// 决策器材（该切换由 effWeight 伙伴列表排序热点驱动）。
package core

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

func TestSortUint64AscMatchesStdlib(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, n := range []int{0, 1, 2, 31, 32, 33, 64, 1000, 5000} {
		for _, mode := range []string{"random", "dups", "sorted", "reversed"} {
			a := make([]uint64, n)
			for i := range a {
				switch mode {
				case "random":
					a[i] = rng.Uint64()
				case "dups":
					a[i] = uint64(rng.Intn(17))
				case "sorted":
					a[i] = uint64(i)
				case "reversed":
					a[i] = uint64(n - i)
				}
			}
			b := append([]uint64(nil), a...)
			sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
			sortUint64Asc(a)
			for i := range a {
				if a[i] != b[i] {
					t.Fatalf("n=%d mode=%s index %d: %d != %d", n, mode, i, a[i], b[i])
				}
			}
		}
	}
}

func BenchmarkSortUint64AscVsStdlib(b *testing.B) {
	for _, n := range []int{64, 1000, 100000} {
		rng := rand.New(rand.NewSource(int64(n)))
		base := make([]uint64, n)
		for i := range base {
			base[i] = rng.Uint64()
		}
		key := fmt.Sprintf("%d", n)
		b.Run("Stdlib/"+key, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				a := append([]uint64(nil), base...)
				sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
			}
		})
		b.Run("Radix/"+key, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				a := append([]uint64(nil), base...)
				sortUint64Asc(a)
			}
		})
	}
}
