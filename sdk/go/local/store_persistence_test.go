// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package local

import (
	"path/filepath"
	"testing"
)

// TestJSONFileStoreSubUnitMappingPersists：子→父映射随 Flush 落盘、随载入重建；
// 删除后双向清理（修复前映射只存内存，进程重启后子单元解析静默失效）。
func TestJSONFileStoreSubUnitMappingPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	s, err := NewJSONFileStore(path, []byte("json-persist-key-v1"))
	if err != nil {
		t.Fatalf("NewJSONFileStore: %v", err)
	}
	if err := s.SaveLocalPlaintext("docA", 1, "parent text", []string{"parent text"}, IngestResponse{KeptSentenceIndices: []int{0}}); err != nil {
		t.Fatalf("save parent: %v", err)
	}
	s.SaveSubUnitMapping("sub0", "docA")
	if err := s.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	s2, err := NewJSONFileStore(path, []byte("json-persist-key-v1"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := s2.ResolveCanonicalUnitID("sub0"); got != "docA" {
		t.Fatalf("persisted sub mapping lost on reload: got %q", got)
	}
	raw, _, _, err := s2.LoadLocalDocument("sub0")
	if err != nil || raw != "parent text" {
		t.Fatalf("sub lookup after reload: raw=%q err=%v", raw, err)
	}

	if n := s2.DeleteLocalUnits([]string{"sub0"}); n != 0 {
		t.Fatalf("sub0 carries no doc record, deleted=%d", n)
	}
	if got := s2.ResolveCanonicalUnitID("sub0"); got != "sub0" {
		t.Fatalf("mapping must be cleaned after unit deletion: got %q", got)
	}
}
