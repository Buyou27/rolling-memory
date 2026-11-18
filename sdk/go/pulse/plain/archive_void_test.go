// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import (
	"math"
	"testing"
)

// fakeIndex 是 ArchiveVoidProbe 测试用的极简全档倒排视图。
type fakeIndex struct {
	n        int
	docs     []map[uint64]bool
	postings map[uint64][]int32
}

func newFakeIndex(n int, docs []map[uint64]bool) *fakeIndex {
	f := &fakeIndex{n: n, docs: docs, postings: map[uint64][]int32{}}
	for unit, terms := range docs {
		for h := range terms {
			f.postings[h] = append(f.postings[h], int32(unit))
		}
	}
	return f
}

func (f *fakeIndex) CorpusSize() int                   { return f.n }
func (f *fakeIndex) TermDF(h uint64) int               { return len(f.postings[h]) }
func (f *fakeIndex) TermDFOf(h uint64) int             { return len(f.postings[h]) }
func (f *fakeIndex) TermMassOf(uint64) (float64, bool) { return 0, false }
func (f *fakeIndex) ForEachPostingUnit(h uint64, fn func(int32)) {
	for _, u := range f.postings[h] {
		fn(u)
	}
}
func (f *fakeIndex) UnitTime(int32) (int64, int64, int64, bool) { return 0, 0, 0, false }

func corpusDoc(t *testing.T, te *TermExtractor, words ...string) map[uint64]bool {
	t.Helper()
	enc := te.Encoder()
	doc := make(map[uint64]bool, len(words))
	for _, w := range words {
		doc[enc.KeyedHash64(w)] = true
	}
	return doc
}

func hasKind(certs []VoidCertificate, kind string) bool {
	for _, c := range certs {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

// 未登录专名的 BPE 半宽残片（as/pen, re）不得冒充母词在库。
func TestArchiveVoidProbeOOVProperWithSubwordDecoy(t *testing.T) {
	te := NewTermExtractor(nil)
	idx := newFakeIndex(1000, []map[uint64]bool{
		corpusDoc(t, te, "pen", "boat", "tour", "trip"),
		corpusDoc(t, te, "as", "hotel", "stay"),
	})
	certs := te.ArchiveVoidProbe(idx, "How much did I pay for the boat tour during my trip to Aspen?")
	if !hasKind(certs, VoidKindOOVProper) {
		t.Fatalf("want oov_proper certificate, got %+v", certs)
	}
}

// 真实屈折词干（宽度比 >= 1-1/e）允许认证母词在库，不得误报。
func TestArchiveVoidProbeAcceptsTrueInflection(t *testing.T) {
	te := NewTermExtractor(nil)
	idx := newFakeIndex(1000, []map[uint64]bool{
		corpusDoc(t, te, "malamute", "alaskan", "edinburgh", "ground"),
		corpusDoc(t, te, "malamute", "dog"),
	})
	certs := te.ArchiveVoidProbe(idx, "I saw some cute Alaskan Malamutes lying on the ground in Edinburgh.")
	if hasKind(certs, VoidKindOOVProper) {
		t.Fatalf("inflectional stem must resolve the host word, got %+v", certs)
	}
}

// 复合专名各自在库但全库零同单元共现 -> broken_compound。
func TestArchiveVoidProbeBrokenCompound(t *testing.T) {
	te := NewTermExtractor(nil)
	idx := newFakeIndex(1000, []map[uint64]bool{
		corpusDoc(t, te, "fitzwilliam", "museum", "exhibition"),
		corpusDoc(t, te, "college", "lecture", "seminar"),
	})
	certs := te.ArchiveVoidProbe(idx, "I remember visiting the Fitzwilliam College last spring.")
	if !hasKind(certs, VoidKindBrokenCompound) {
		t.Fatalf("want broken_compound certificate, got %+v", certs)
	}
}

// 单锚皆在库但主专名与焦点实词零同单元共现 -> disjoint_anchor。
func TestArchiveVoidProbeDisjointAnchor(t *testing.T) {
	te := NewTermExtractor(nil)
	idx := newFakeIndex(1000, []map[uint64]bool{
		corpusDoc(t, te, "sydney", "harbour", "photo"),
		corpusDoc(t, te, "tasting", "wine", "vineyard"),
		corpusDoc(t, te, "pay", "wine", "bottle"),
	})
	certs := te.ArchiveVoidProbe(idx, "How much did I pay for the wine tasting in Sydney?")
	if !hasKind(certs, VoidKindDisjointAnchor) {
		t.Fatalf("want disjoint_anchor certificate, got %+v", certs)
	}
}

// 完备在库查询不得产生任何空洞凭证。
func TestArchiveVoidProbeCleanQuery(t *testing.T) {
	te := NewTermExtractor(nil)
	idx := newFakeIndex(1000, []map[uint64]bool{
		corpusDoc(t, te, "stay", "cambridge", "hotel", "booked", "weekend"),
	})
	certs := te.ArchiveVoidProbe(idx, "Where did I stay in Cambridge?")
	if len(certs) != 0 {
		t.Fatalf("clean query must not emit certificates, got %+v", certs)
	}
}

// 完备度：零记载专名计入 OOVMass，命中集覆盖其余词槽时 Completeness 介于 0 与 1。
func TestQueryCoverageOOVAndPartialHit(t *testing.T) {
	te := NewTermExtractor(nil)
	idx := newFakeIndex(1000, []map[uint64]bool{
		corpusDoc(t, te, "boat", "tour", "trip"),
		corpusDoc(t, te, "boat", "tour", "harbour"),
	})
	report := te.QueryCoverage(idx, "How much did I pay for the boat tour during my trip to Aspen?", func(unit int32) bool {
		return unit == 0
	})
	if report.OOVMass <= 0 {
		t.Fatalf("absent proper must contribute OOV mass, got %+v", report)
	}
	if !(report.Completeness > 0 && report.Completeness < 1) {
		t.Fatalf("partial hit must yield completeness in (0,1), got %+v", report)
	}
	if report.FullMass <= report.CoveredMass {
		t.Fatalf("full mass must exceed covered mass, got %+v", report)
	}
}

// 真实载体宽度比：屈折词干约 0.84~0.89，BPE 残片约 0.22~0.60。
func TestTrueCarrierWidthSeparation(t *testing.T) {
	cases := []struct {
		host, piece string
		min, max    float64
	}{
		{"malamutes", "malamute", 0.80, 0.95},
		{"waitrose's", "waitrose", 0.80, 0.95},
		{"aspen", "as", 0.30, 0.50},
		{"reykjavik", "re", 0.15, 0.30},
	}
	for _, c := range cases {
		ratio := TrueCarrierWidth(c.piece) / TrueCarrierWidth(c.host)
		if ratio < c.min || ratio > c.max {
			t.Fatalf("%s/%s ratio = %.2f, want in [%.2f,%.2f]", c.piece, c.host, ratio, c.min, c.max)
		}
	}
}

// TestHyphenPartsResolvedRequiresEveryPart 钉住语种无关裁决：连字符复合词的
// 部件不按字符数跳过，单字符部件缺库内证据即判未解析（宁漏勿错）。
func TestHyphenPartsResolvedRequiresEveryPart(t *testing.T) {
	te := NewTermExtractor(nil)
	idx := newFakeIndex(1000, []map[uint64]bool{
		corpusDoc(t, te, "frame", "photo"),
	})
	const ubiquitous = 1000 / math.E
	if !te.hyphenPartsResolved(idx, "photo-frame", ubiquitous) {
		t.Fatal("both parts are in-corpus and rare; compound must resolve")
	}
	if te.hyphenPartsResolved(idx, "a-frame", ubiquitous) {
		t.Fatal("absent single-letter part must not be skipped by a character-count gate")
	}
	if te.hyphenPartsResolved(idx, "2024-05", ubiquitous) {
		t.Fatal("parts without letters must not resolve")
	}
}
