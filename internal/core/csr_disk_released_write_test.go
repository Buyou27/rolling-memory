// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// 段头内容摘要必须对「已释放堆上倒排映射」的索引同样成立：服务层整建后先释放瞬态映射
// 再落盘派生段，若摘要在该状态下退化为 0，段自校验会与读者重算值不一致而永久不可读。
package core

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestWriteDiskClusterCSRAfterTransientReleaseRoundTrips 钉住「先 ReleaseTransientUnitMaps
// 再 WriteDiskClusterCSR」的落盘路径可被 OpenDiskClusterEngine 自校验通过，且段内摘要与
// 释放前索引的内容摘要逐位一致。
func TestWriteDiskClusterCSRAfterTransientReleaseRoundTrips(t *testing.T) {
	docs := make([]Doc, 0, 40)
	for i := 0; i < 40; i++ {
		h := uint64(1000 + i)
		docs = append(docs, Doc{
			UnitID:     fmt.Sprintf("u-%02d", i),
			Terms:      map[uint64]Term{h: {Hash: h, Bits: 40, Group: h, Sent: 1, Slot: 1}},
			Counts:     map[uint64]int{h: 1},
			OccurredAt: int64(1700000000 + i*60),
		})
	}
	want := ComputeCorpusDigest(BuildIndex(docs))

	eng := NewEngine(BuildIndex(docs))
	eng.MaterializeClusterState()
	eng.Index.ReleaseTransientUnitMaps()
	if eng.Index.Postings != nil || eng.Index.UnitPositions != nil {
		t.Fatal("fixture expected released transient maps")
	}

	path := filepath.Join(t.TempDir(), "released.zcsr")
	if err := WriteDiskClusterCSR(path, eng); err != nil {
		t.Fatalf("write after transient release: %v", err)
	}
	disk, err := OpenDiskClusterEngine(path)
	if err != nil {
		t.Fatalf("open segment written after transient release: %v", err)
	}
	if got := ComputeCorpusDigest(disk.Index); got != want {
		t.Fatalf("segment digest diverged: disk=%016x want=%016x", got, want)
	}
}
