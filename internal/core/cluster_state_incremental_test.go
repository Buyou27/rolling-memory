// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// 本件钉住簇状态增量快路径：多批连续追加、删除与同 ID 覆盖都必须与一次性全量
// 构建逐位一致（含族根变化时的回退路径）。
package core

import (
	"math/rand"
	"testing"
)

func assertIncrementalFired(t *testing.T, eng *Engine, want bool) {
	t.Helper()
	st := eng.ensureClusterState()
	if st == nil || st.agg == nil {
		t.Fatalf("expected cluster aggregates, got %+v", st)
	}
	if st.agg.extended != want {
		t.Fatalf("incremental path extended=%v, want %v", st.agg.extended, want)
	}
}

func TestIncrementalAppendChainMatchesFullRebuild(t *testing.T) {
	docs := appendProbeDocs(400)
	full := NewEngine(BuildIndex(docs))
	full.ensureClusterState()

	inc := NewEngine(BuildIndex(docs[:100]))
	inc.ensureClusterState()
	for from := 100; from < len(docs); from += 37 {
		to := from + 37
		if to > len(docs) {
			to = len(docs)
		}
		inc.AppendDocs(docs[from:to])
		assertIncrementalFired(t, inc, true)
	}
	if want, got := clusterStateCanonicalDigest(full.ensureClusterState()), clusterStateCanonicalDigest(inc.ensureClusterState()); want != got {
		t.Fatalf("chained append digest mismatch:\n full=%s\n inc =%s", want, got)
	}
	if a, b := ComputeCorpusDigest(full.Index), ComputeCorpusDigest(inc.Index); a != b {
		t.Fatalf("chained append corpus digest mismatch: full=%x inc=%x", a, b)
	}
	assertSameQuery(t, full, inc, digestProbeQuery(docs))
}

// TestIncrementalAppendUpsertMatchesFullRebuild 验证同 ID 覆盖走增量扣减+并入
// （旧贡献按 prevDrops 扣减、新贡献按 delta 并入），结果与全量构建逐位一致。
func TestIncrementalAppendUpsertMatchesFullRebuild(t *testing.T) {
	docs := appendProbeDocs(240)
	replaced := docs[120]
	replaced.OccurredAt += 30
	replaced.Terms = map[uint64]Term{}
	replaced.Counts = map[uint64]int{}
	replaced.Positions = map[uint64][]TermPosition{}
	for k := 0; k < 6; k++ {
		h := uint64(7000 + (k*29)%211)
		replaced.Terms[h] = Term{Hash: h, Bits: 48, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: h, Sent: uint16(k / 3), Slot: uint16(k % 3)}
		replaced.Counts[h] = 1
		replaced.Positions[h] = []TermPosition{{Sentence: k / 3, Slot: k % 3}}
	}
	want := append([]Doc(nil), docs...)
	want[120] = replaced
	full := NewEngine(BuildIndex(want))
	full.ensureClusterState()

	inc := NewEngine(BuildIndex(docs[:200]))
	inc.ensureClusterState()
	inc.AppendDocs(append([]Doc{replaced}, docs[200:]...))
	assertIncrementalFired(t, inc, true)
	if a, b := clusterStateCanonicalDigest(full.ensureClusterState()), clusterStateCanonicalDigest(inc.ensureClusterState()); a != b {
		t.Fatalf("upsert incremental digest mismatch:\n full=%s\n inc =%s", a, b)
	}
	assertSameQuery(t, full, inc, digestProbeQuery(want))
}

// TestIncrementalAppendAfterDelete 验证删除后追加以删除重建后的状态为基座，
// 且与「删除 + 追加」一次性全量构建逐位一致。
func TestIncrementalAppendAfterDelete(t *testing.T) {
	docs := appendProbeDocs(300)
	const victim = "probe-00077"
	var wantDocs []Doc
	for _, d := range docs {
		if d.UnitID != victim {
			wantDocs = append(wantDocs, d)
		}
	}
	extra := appendProbeDocs(320)[300:]
	wantDocs = append(wantDocs, extra...)
	full := NewEngine(BuildIndex(wantDocs))
	full.ensureClusterState()

	inc := NewEngine(BuildIndex(docs))
	inc.ensureClusterState()
	if removed := inc.DeleteDocs(victim); removed != 1 {
		t.Fatalf("DeleteDocs removed %d, want 1", removed)
	}
	inc.AppendDocs(extra)
	assertIncrementalFired(t, inc, true)
	if a, b := clusterStateCanonicalDigest(full.ensureClusterState()), clusterStateCanonicalDigest(inc.ensureClusterState()); a != b {
		t.Fatalf("delete+append digest mismatch:\n full=%s\n inc =%s", a, b)
	}
	assertSameQuery(t, full, inc, digestProbeQuery(wantDocs))
}

// TestIncrementalDeleteMatchesFullRebuild 验证纯删除走增量扣减（族根稳定时），
// 结果与「从未入库」的全量构建逐位一致。
func TestIncrementalDeleteMatchesFullRebuild(t *testing.T) {
	docs := appendProbeDocs(240)
	const victim = "probe-00119"
	kept := make([]Doc, 0, len(docs)-1)
	for _, d := range docs {
		if d.UnitID != victim {
			kept = append(kept, d)
		}
	}
	full := NewEngine(BuildIndex(kept))
	full.ensureClusterState()
	inc := NewEngine(BuildIndex(docs))
	inc.ensureClusterState()
	if removed := inc.DeleteDocs(victim); removed != 1 {
		t.Fatalf("DeleteDocs removed %d, want 1", removed)
	}
	assertIncrementalFired(t, inc, true)
	if a, b := clusterStateCanonicalDigest(full.ensureClusterState()), clusterStateCanonicalDigest(inc.ensureClusterState()); a != b {
		t.Fatalf("incremental delete digest mismatch:\n full=%s\n inc =%s", a, b)
	}
	if a, b := ComputeCorpusDigest(full.Index), ComputeCorpusDigest(inc.Index); a != b {
		t.Fatalf("incremental delete corpus digest mismatch: full=%x inc=%x", a, b)
	}
	assertSameQuery(t, full, inc, digestProbeQuery(kept))
}

// TestIncrementalApplyDocsMixedMatchesFullRebuild 验证生产 drain 形态（同批删除+追加）
// 走增量扣减/并入，与一次性全量构建逐位一致。
func TestIncrementalApplyDocsMixedMatchesFullRebuild(t *testing.T) {
	docs := appendProbeDocs(260)
	const victim = "probe-00091"
	extra := appendProbeDocs(280)[260:]
	wantDocs := make([]Doc, 0, len(docs)-1+len(extra))
	for _, d := range docs {
		if d.UnitID != victim {
			wantDocs = append(wantDocs, d)
		}
	}
	wantDocs = append(wantDocs, extra...)
	full := NewEngine(BuildIndex(wantDocs))
	full.ensureClusterState()

	inc := NewEngine(BuildIndex(docs))
	inc.ensureClusterState()
	if removed := inc.ApplyDocs([]string{victim}, extra); removed != 1 {
		t.Fatalf("ApplyDocs removed %d, want 1", removed)
	}
	assertIncrementalFired(t, inc, true)
	if a, b := clusterStateCanonicalDigest(full.ensureClusterState()), clusterStateCanonicalDigest(inc.ensureClusterState()); a != b {
		t.Fatalf("mixed incremental digest mismatch:\n full=%s\n inc =%s", a, b)
	}
	assertSameQuery(t, full, inc, digestProbeQuery(wantDocs))
}

// mutateProbeDoc 返回改写词项/时间后的副本（共享词表，保持族根稳定）。
func mutateProbeDoc(d Doc, seed int) Doc {
	out := d
	out.OccurredAt += int64(seed) + 1
	out.Terms = make(map[uint64]Term, 6)
	out.Counts = make(map[uint64]int, 6)
	out.Positions = make(map[uint64][]TermPosition, 6)
	for k := 0; k < 6; k++ {
		h := uint64(1000 + (seed*7+k*13)%97)
		out.Terms[h] = Term{Hash: h, Bits: 40, Role: RolePrimary, ExtFlags: ExtFlagSeenPrimary, Group: h, Sent: uint16(k / 3), Slot: uint16(k % 3)}
		out.Counts[h] = 1
		out.Positions[h] = []TermPosition{{Sentence: k / 3, Slot: k % 3}}
	}
	return out
}

// TestIncrementalDeltaRandomChainMatchesFullRebuild 用随机追加/删除/覆盖序列逐步对拍
// 全量重建：任一步增量结果必须与一次性构建逐位一致（含回退路径）。
func TestIncrementalDeltaRandomChainMatchesFullRebuild(t *testing.T) {
	docs := appendProbeDocs(620)
	rng := rand.New(rand.NewSource(11))
	cur := append([]Doc(nil), docs[:300]...)
	eng := NewEngine(BuildIndex(cur))
	eng.ensureClusterState()
	next := 300
	for step := 0; step < 14; step++ {
		switch rng.Intn(3) {
		case 0, 1:
			n := 1 + rng.Intn(5)
			batch := append([]Doc(nil), docs[next:next+n]...)
			next += n
			cur = append(cur, batch...)
			eng.AppendDocs(batch)
		case 2:
			if rng.Intn(2) == 0 && len(cur) > 20 {
				n := 1 + rng.Intn(3)
				var victims []string
				drop := make(map[string]struct{}, n)
				for k := 0; k < n; k++ {
					victim := cur[rng.Intn(len(cur))]
					drop[victim.UnitID] = struct{}{}
					victims = append(victims, victim.UnitID)
				}
				var kept []Doc
				for _, d := range cur {
					if _, gone := drop[d.UnitID]; !gone {
						kept = append(kept, d)
					}
				}
				cur = kept
				eng.DeleteDocs(victims...)
			} else {
				at := rng.Intn(len(cur))
				replaced := mutateProbeDoc(cur[at], step*31+at)
				cur[at] = replaced
				eng.AppendDocs([]Doc{replaced})
			}
		}
		full := NewEngine(BuildIndex(cur))
		full.ensureClusterState()
		if a, b := clusterStateCanonicalDigest(full.ensureClusterState()), clusterStateCanonicalDigest(eng.ensureClusterState()); a != b {
			t.Fatalf("step %d digest mismatch:\n full=%s\n inc =%s", step, a, b)
		}
		if a, b := ComputeCorpusDigest(full.Index), ComputeCorpusDigest(eng.Index); a != b {
			t.Fatalf("step %d corpus digest mismatch: full=%x inc=%x", step, a, b)
		}
		assertSameQuery(t, full, eng, digestProbeQuery(cur))
	}
}
