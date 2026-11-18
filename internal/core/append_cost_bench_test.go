// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// 本件是 AppendDocs/DeleteDocs 成本分解基准器材：分别测量 ExportDocs、BuildIndex、
// ClusterState 与一次单篇 Append 的耗时/分配，供增量摄取架构批次复用。
package core

import (
	"fmt"
	"testing"
)

func appendProbeDocs(n int) []Doc {
	docs := make([]Doc, n)
	base := int64(1700000000)
	for i := 0; i < n; i++ {
		d := epTestDoc(fmt.Sprintf("probe-%05d", i), base+int64(i)*3600)
		for k := 0; k < 8; k++ {
			h := uint64(1000 + (i*7+k*13)%97)
			d.Terms[h] = Term{Hash: h, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: h, Sent: uint16(k / 4), Slot: uint16(k % 4)}
			d.Counts[h] = 1
			d.Positions[h] = []TermPosition{{Sentence: k / 4, Slot: k % 4}}
		}
		docs[i] = d
	}
	return docs
}

func BenchmarkAppendCostProbe(b *testing.B) {
	for _, n := range []int{1000, 3000} {
		docs := appendProbeDocs(n)
		key := fmt.Sprintf("%d", n)
		b.Run("ExportDocs/"+key, func(b *testing.B) {
			eng := NewEngine(BuildIndex(docs))
			eng.ensureClusterState()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = eng.ExportDocs()
			}
		})
		b.Run("BuildIndex/"+key, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = BuildIndex(docs)
			}
		})
		b.Run("ClusterState/"+key, func(b *testing.B) {
			idx := BuildIndex(docs)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				eng := NewEngine(idx)
				_ = eng.ensureClusterState()
			}
		})
		b.Run("AppendDocs1/"+key, func(b *testing.B) {
			extra := appendProbeDocs(n + 1)[n]
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				eng := NewEngine(BuildIndex(docs))
				eng.ensureClusterState()
				b.StartTimer()
				eng.AppendDocs([]Doc{extra})
			}
		})
		// Phase split of the incremental path: CSR-direct index merge alone and
		// cluster-state rebuild alone, so P2b slices target the dominant term.
		b.Run("DeltaRebuild/"+key, func(b *testing.B) {
			extra := appendProbeDocs(n + 1)[n]
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				eng := NewEngine(BuildIndex(docs))
				eng.ensureClusterState()
				b.StartTimer()
				_ = eng.buildIndexWithDelta([]Doc{extra})
			}
		})
		b.Run("ClusterRebuild/"+key, func(b *testing.B) {
			extra := appendProbeDocs(n + 1)[n]
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				eng := NewEngine(BuildIndex(docs))
				eng.ensureClusterState()
				idx := eng.buildIndexWithDelta([]Doc{extra})
				b.StartTimer()
				_ = NewEngine(idx).ensureClusterState()
			}
		})
	}
}

// BenchmarkDeleteCostProbe measures one resident deletion on a warm engine; the
// setup (fresh build + cluster state) is excluded so the reading isolates the
// delete path itself (CSR rebuild vs ExportDocs + BuildIndex).
func BenchmarkDeleteCostProbe(b *testing.B) {
	for _, n := range []int{1000, 3000} {
		docs := appendProbeDocs(n)
		key := fmt.Sprintf("%d", n)
		b.Run("DeleteOne/"+key, func(b *testing.B) {
			victim := fmt.Sprintf("probe-%05d", n/2)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				eng := NewEngine(BuildIndex(docs))
				eng.ensureClusterState()
				b.StartTimer()
				eng.DeleteDocs(victim)
			}
		})
	}
}
