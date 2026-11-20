// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"reflect"
	"testing"
)

func TestWithExternalCoCitationsDerivesCoCitationWithoutMutatingBase(t *testing.T) {
	idx := BuildIndex([]Doc{{UnitID: "u1"}, {UnitID: "u2"}, {UnitID: "u3"}})
	base := NewEngine(idx)
	if st := base.ensureClusterState(); st == nil {
		t.Fatal("base cluster state did not materialize")
	}
	u1, ok1 := idx.LookupUnit("u1")
	u2, ok2 := idx.LookupUnit("u2")
	u3, ok3 := idx.LookupUnit("u3")
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("lookup units = %v %v %v", ok1, ok2, ok3)
	}
	if base.clusterState.hasCoCitationEdge(u1, u3) {
		t.Fatal("base engine already couples u1/u3")
	}

	derived := base.WithExternalCoCitations([][]string{{"u1", "u3", "u3"}})
	if derived == base {
		t.Fatal("expected a derived engine")
	}
	st := derived.ensureClusterState()
	if st == nil {
		t.Fatal("derived cluster state is nil")
	}
	if !st.hasCoCitationEdge(u1, u3) {
		t.Fatal("external co-citation pair was not materialized")
	}
	if st.hasCoCitationEdge(u1, u2) {
		t.Fatal("unrelated pair was coupled")
	}
	if st.hasCitationEdge(u1, u3) {
		t.Fatal("external co-citation must not fabricate a first-order citation edge")
	}
	if base.clusterState.hasCoCitationEdge(u1, u3) {
		t.Fatal("base engine was mutated")
	}

	again := base.WithExternalCoCitations([][]string{{"u1", "u3"}})
	againState := again.ensureClusterState()
	if !reflect.DeepEqual(st.cocitePeers, againState.cocitePeers) ||
		!reflect.DeepEqual(st.cociteRowPtr, againState.cociteRowPtr) {
		t.Fatal("same declaration produced different co-citation CSR")
	}

	if base.WithExternalCoCitations(nil) != base {
		t.Fatal("an empty declaration must return the base engine")
	}
}
