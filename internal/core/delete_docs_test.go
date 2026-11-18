// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// 本件钉住 DeleteDocs 的无命中快速返回：请求 ID 全部不在库时不得触发
// ExportDocs + BuildIndex 全量重建；命中任一在库 ID 时仍走原重建路径。
package core

import "testing"

func TestDeleteDocsAbsentIsNoOp(t *testing.T) {
	eng := NewEngine(BuildIndex(appendProbeDocs(64)))
	eng.ensureClusterState()
	idxBefore, stBefore := eng.Index, eng.clusterState

	if got := eng.DeleteDocs("ghost-a", "ghost-b", ""); got != 0 {
		t.Fatalf("absent delete removed %d units, want 0", got)
	}
	if eng.Index != idxBefore || eng.clusterState != stBefore {
		t.Fatal("absent delete must not rebuild the index or cluster state")
	}

	if got := eng.DeleteDocs("probe-00003"); got != 1 {
		t.Fatalf("resident delete removed %d units, want 1", got)
	}
	if eng.Index == idxBefore {
		t.Fatal("resident delete must rebuild the index")
	}
	if _, ok := eng.Index.LookupUnit("probe-00003"); ok {
		t.Fatal("deleted unit still resident")
	}
	if got := eng.DeleteDocs("probe-00003"); got != 0 {
		t.Fatalf("second delete removed %d units, want 0", got)
	}
}
