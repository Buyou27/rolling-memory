// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/rocareer/rolling-memory/internal/core"
)

func TestCSRFreshnessLineStableAndSensitive(t *testing.T) {
	line := corpusFreshnessLine(10, 55, 10, 100, 7, 5, 5, 7, 42, 3, 4, 6, "636f646563")
	parsed, ok := parseCSRFreshnessLine(line)
	if !ok {
		t.Fatalf("freshness line must parse: %q", line)
	}
	if parsed.docCnt != 10 || parsed.docRowSum != 55 || parsed.docMaxRowID != 10 || parsed.docTextLen != 100 ||
		parsed.docsRev != 7 || parsed.bgCnt != 5 || parsed.bgMaxRowID != 5 || parsed.bgOccSum != 7 ||
		parsed.bgSlotsLen != 42 || parsed.bgRev != 3 || parsed.episodicRevision != 4 || parsed.epiCnt != 6 ||
		parsed.codecKeyHex != "636f646563" {
		t.Fatalf("parsed fields diverged: %+v", parsed)
	}
	mutations := map[string]string{
		"docCnt":           corpusFreshnessLine(11, 55, 10, 100, 7, 5, 5, 7, 42, 3, 4, 6, "636f646563"),
		"docRowSum":        corpusFreshnessLine(10, 56, 10, 100, 7, 5, 5, 7, 42, 3, 4, 6, "636f646563"),
		"docMaxRowID":      corpusFreshnessLine(10, 55, 11, 100, 7, 5, 5, 7, 42, 3, 4, 6, "636f646563"),
		"docTextLen":       corpusFreshnessLine(10, 55, 10, 101, 7, 5, 5, 7, 42, 3, 4, 6, "636f646563"),
		"docsRev":          corpusFreshnessLine(10, 55, 10, 100, 8, 5, 5, 7, 42, 3, 4, 6, "636f646563"),
		"bgCnt":            corpusFreshnessLine(10, 55, 10, 100, 7, 6, 5, 7, 42, 3, 4, 6, "636f646563"),
		"bgRev":            corpusFreshnessLine(10, 55, 10, 100, 7, 5, 5, 7, 42, 4, 4, 6, "636f646563"),
		"episodicRevision": corpusFreshnessLine(10, 55, 10, 100, 7, 5, 5, 7, 42, 3, 5, 6, "636f646563"),
		"epiCnt":           corpusFreshnessLine(10, 55, 10, 100, 7, 5, 5, 7, 42, 3, 4, 7, "636f646563"),
		"codecKey":         corpusFreshnessLine(10, 55, 10, 100, 7, 5, 5, 7, 42, 3, 4, 6, "636f646564"),
	}
	for name, changed := range mutations {
		if changed == line {
			t.Fatalf("%s change must invalidate the freshness line", name)
		}
	}
	// 历史 v3 签名（32 位十六进制）与损坏内容一律判否。
	if _, ok := parseCSRFreshnessLine(strings.Repeat("ab", 16)); ok {
		t.Fatalf("legacy 32-hex signature must not parse as a v4 freshness line")
	}
	if _, ok := parseCSRFreshnessLine("v4|1|2|3"); ok {
		t.Fatalf("truncated freshness line must not parse")
	}
}

func TestDiskClusterCachePersistReuseAndInvalidate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	docs := []core.Doc{
		{
			UnitID: "unit-1", OccurredAt: 1700000000, Layer: LayerEpisodic,
			Terms:     map[uint64]core.Term{11: {Hash: 11, Bits: 40, Role: core.RolePrimary}},
			Counts:    map[uint64]int{11: 1},
			Positions: map[uint64][]core.TermPosition{11: {{Sentence: 1, Slot: 1}}},
		},
		{
			UnitID: "unit-2", OccurredAt: 1700000100, Layer: LayerNoetic,
			Terms:     map[uint64]core.Term{12: {Hash: 12, Bits: 44, Role: core.RolePrimary}},
			Counts:    map[uint64]int{12: 1},
			Positions: map[uint64][]core.TermPosition{12: {{Sentence: 1, Slot: 1}}},
		},
	}
	engine := core.NewEngine(core.BuildIndex(docs))
	engine.MaterializeClusterState()

	line := corpusFreshnessLine(2, 3, 2, 30, 1, 0, 0, 0, 0, 0, 0, 0, "6b6579")
	csrPath, persisted := persistSnapshotCoreCSR(dbPath, line, engine, nil, nil)
	if !persisted || csrPath == "" {
		t.Fatalf("disk cluster must be persisted (ok=%v path=%q)", persisted, csrPath)
	}

	_, genPath := vaultCSRPaths(dbPath)
	if _, err := os.Stat(csrPath); err != nil {
		t.Fatalf("disk cluster must be written: %v", err)
	}
	raw, err := os.ReadFile(genPath)
	if err != nil || strings.TrimSpace(string(raw)) != line {
		t.Fatalf("freshness sidecar must carry the line: %q err=%v", string(raw), err)
	}

	reopened, ok := openSnapshotCoreEngineFromCSR(dbPath, line)
	if !ok || reopened == nil || reopened.Index == nil || reopened.Index.N != len(docs) {
		t.Fatalf("matching freshness line must reuse the disk cluster (ok=%v)", ok)
	}

	// 挂载磁盘簇并释放堆过渡 Map 后，查询必须仍能命中（与 RB 服务端同一读取路径）。
	if err := core.AttachDiskClusterCSR(csrPath, engine, true); err != nil {
		t.Fatalf("attach disk cluster with heap release: %v", err)
	}
	hits := engine.SearchWithPositions(
		map[uint64]core.Term{11: {Hash: 11, Bits: 40, Role: core.RolePrimary}},
		map[uint64][]core.TermPosition{11: {{Sentence: 1, Slot: 1}}},
		5,
	)
	if len(hits) == 0 {
		t.Fatalf("query must still hit after heap release (CSR-backed reads)")
	}

	staleLine := corpusFreshnessLine(2, 3, 2, 30, 2, 0, 0, 0, 0, 0, 0, 0, "6b6579")
	if _, ok := openSnapshotCoreEngineFromCSR(dbPath, staleLine); ok {
		t.Fatalf("stale freshness line must not reuse the disk cluster")
	}

	if err := os.Remove(genPath); err != nil {
		t.Fatal(err)
	}
	if _, ok := openSnapshotCoreEngineFromCSR(dbPath, line); ok {
		t.Fatalf("missing freshness sidecar must not reuse the disk cluster")
	}
}

// TestCorpusSnapshotCSRDeltaLayerAndUpdateFallback pins the production hot path:
// a pure append after the .zcsr baseline must be served by the mmap baseline plus a
// tiny in-memory delta engine (read-your-writes, no full rebuild), while any
// UPDATE must fail the pure-INSERT algebra and fall back to a full rebuild whose
// results stay identical to a cold build.
func TestCorpusSnapshotCSRDeltaLayerAndUpdateFallback(t *testing.T) {
	v, _, dbPath := openIncrTestVault(t, "csr_delta.db")
	base := []struct{ id, text string }{
		{"d1", "Albert photographed the Porto lighthouse at the Douro river sunset."},
		{"d2", "The Dongguan train arrived at noon with braised goose for dinner."},
		{"d3", "Cambridge saw its first snow of late 2022 on December 11th evening."},
	}
	for _, d := range base {
		if err := v.SaveLocalPlaintext(d.id, 1700000100, d.text, SplitSentences(d.text), IngestResponse{AddressURI: "mem://" + d.id}); err != nil {
			t.Fatalf("save %s: %v", d.id, err)
		}
	}
	snap1, err := v.loadCorpusSnapshot(context.Background())
	if err != nil || snap1 == nil {
		t.Fatalf("cold snapshot: %v", err)
	}
	if snap1.coreEngine != nil && snap1.coreEngine.HasDelta() {
		t.Fatalf("cold snapshot must not carry a delta overlay")
	}
	if eng := snap1.ensureSnapshotCoreEngine(v); eng == nil {
		t.Fatalf("cold snapshot must materialize a core engine")
	}
	_, genPath := vaultCSRPaths(dbPath)
	raw, err := os.ReadFile(genPath)
	if err != nil || signatureCodecKey(strings.TrimSpace(string(raw))) == "" {
		t.Fatalf("cold build must persist a canonical v5 freshness sidecar: %q err=%v", strings.TrimSpace(string(raw)), err)
	}

	// Pure append: the next load must reuse the baseline and layer a small delta.
	extra := "A stray cat appeared near the Oxford covered market in July."
	if err := v.SaveLocalPlaintext("d4", 1700000200, extra, SplitSentences(extra), IngestResponse{AddressURI: "mem://d4"}); err != nil {
		t.Fatalf("save d4: %v", err)
	}
	snap2, err := v.loadCorpusSnapshot(context.Background())
	if err != nil || snap2 == nil {
		t.Fatalf("delta snapshot: %v", err)
	}
	if snap2.coreEngine == nil || !snap2.coreEngine.HasDelta() || snap2.coreEngine.DeltaDocCount() != 1 {
		t.Fatalf("pure append must be served by coreEngine native delta overlay (eng=%v hasDelta=%v)", snap2.coreEngine != nil, snap2.coreEngine != nil && snap2.coreEngine.HasDelta())
	}
	if snap2.coreEngine.Index == nil || snap2.coreEngine.Index.N != len(base) {
		t.Fatalf("baseline engine must still cover %d docs", len(base))
	}
	hits, err := v.SearchClientSDKDefault("Oxford covered market stray cat", 5)
	if err != nil {
		t.Fatalf("recall after append: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.UnitID == "d4" {
			found = true
		}
	}
	if !found {
		t.Fatalf("read-your-writes: appended document must be retrievable via the delta layer (hits=%v)", hits)
	}

	// Any UPDATE (here: length-changing) must be carried by the delta layer with
	// its fresh content, still without a full rebuild.
	updated := "Albert photographed the Porto lighthouse under heavy rain that night."
	if err := v.SaveLocalPlaintext("d1", 1700000100, updated, SplitSentences(updated), IngestResponse{AddressURI: "mem://d1"}); err != nil {
		t.Fatalf("update d1: %v", err)
	}
	snap3, err := v.loadCorpusSnapshot(context.Background())
	if err != nil || snap3 == nil {
		t.Fatalf("post-update snapshot: %v", err)
	}
	if snap3.coreEngine == nil || !snap3.coreEngine.HasDelta() {
		t.Fatalf("UPDATE must be carried by coreEngine native delta overlay (eng=%v)", snap3.coreEngine != nil)
	}
	hits3, err := v.SearchClientSDKDefault("Porto lighthouse heavy rain", 5)
	if err != nil {
		t.Fatalf("recall after update: %v", err)
	}
	found = false
	for _, h := range hits3 {
		if h.UnitID == "d1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("updated document must be retrievable with its new content (hits=%v)", hits3)
	}
	snapshotEquivalence(t, v, dbPath, []string{
		"Porto lighthouse", "Oxford covered market", "Dongguan braised goose",
	})
}
