// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import (
	"math"
	"strings"
	"unicode"
)

// 全档倒排认识论探针：把倒排索引自身持有的全局统计量（词项文档频次与倒排交集）
// 转换为关于整库 M_N 的确定性形式事实，供交付层在进入下游模型之前决定
// 「零命中静默」与「显式空洞凭证」。
//
// 三条事实全部来自 QueryIndexView 的精确全局统计（CorpusSize / TermDF /
// ForEachPostingUnit），不依赖候选集抽样、不依赖词表、不依赖语义判断：
//   - C1 oov_proper      ：非句首专名原词在全库零记载，且无真实载体宽度守恒的同源词干
//   - C2 broken_compound ：相邻专名二元组各自在库，但全库无任何单元同时包含两者
//   - C3 disjoint_anchor ：最高权重专名锚与最高信息量焦点实词在全库零同单元共现
//
// 载体宽度守恒判据：伴随词 c 能作为母词 w 的同源屈折词干，当且仅当其真实载体宽度
// 占母词比例不低于 1-1/e（约 0.632）。BPE 对未登录专名的二元切分碎片（如 aspen->as,
// reykjavik->re, whitmore->whit）宽度比落在 0.22~0.60，不得在母词零记载时冒充在库。

// 空洞凭证类型。
const (
	VoidKindOOVProper      = "oov_proper"
	VoidKindBrokenCompound = "broken_compound"
	VoidKindDisjointAnchor = "disjoint_anchor"
)

// VoidCertificate 是一条关于整库的确定性空洞凭证（kind + 涉及表面词 + 各自档频）。
type VoidCertificate struct {
	Kind  string   `json:"kind"`
	Terms []string `json:"terms"`
	DFs   []int    `json:"dfs"`
}

// CoverageReport 是查询的整库信息完备度报告。
// FullMass 为查询实质词槽在全库零假设下的总自信息（Nats，零档词取 ln(N+1) 上界）；
// OOVMass 为零记载/无同源词干词槽的自信息和；CoveredMass 为命中集中实际覆盖的部分；
// Completeness = CoveredMass / FullMass。
type CoverageReport struct {
	FullMass     float64 `json:"full_mass"`
	OOVMass      float64 `json:"oov_mass"`
	CoveredMass  float64 `json:"covered_mass"`
	Completeness float64 `json:"completeness"`
	ActiveTerms  int     `json:"active_terms"`
}

// TrueCarrierWidth 返回字符串按字符信息载体单元累计的真实宽度（连续重复字符按 1/4 计）。
// 与编码器 RawBitsX10 的口径一致，用于比较母词与伴随词的主干信息自由度。
func TrueCarrierWidth(s string) float64 {
	var total uint32
	var prev rune
	runLen := 0
	for _, r := range s {
		if lr := unicode.ToLower(r); lr == prev {
			runLen++
		} else {
			prev, runLen = lr, 1
		}
		rw := uint32(CarrierUnits(r))
		if runLen >= 3 {
			rw /= 4
		}
		total += rw
	}
	return float64(total)
}

// probeTerm 是探针侧的词槽视图。
type probeTerm struct {
	surface  string
	hash     uint64
	df       int
	mass     float64
	proper   bool
	resolved bool
	stemDF   int
	sentence int
	slot     int
}

// archiveProbeTerms 把查询文本切分为词槽并计算各自的整库档频、同源分辨率与自信息。
func (a *TermExtractor) archiveProbeTerms(idx QueryIndexView, text string) []probeTerm {
	if idx == nil {
		return nil
	}
	enc := a.Encoder()
	n := idx.CorpusSize()
	if n <= 0 {
		return nil
	}
	ubiquitous := float64(n) / math.E
	var out []probeTerm
	for sentIdx, sent := range splitSentences(text) {
		for slot, word := range SplitWords(sent) {
			surface := strings.TrimSpace(word)
			low := strings.ToLower(surface)
			if low == "" || !hasLetter(low) {
				continue
			}
			pt := probeTerm{
				surface:  surface,
				hash:     enc.KeyedHash64(low),
				sentence: sentIdx,
				slot:     slot,
			}
			pt.df = idx.TermDF(pt.hash)
			pt.proper = looksProper(surface, slot)
			pt.stemDF = a.resolvedStemDF(idx, low, pt.df, ubiquitous)
			pt.resolved = pt.df > 0 || pt.stemDF > 0
			if !pt.resolved && strings.Contains(low, "-") {
				pt.resolved = a.hyphenPartsResolved(idx, low, ubiquitous)
			}
			// 实质词槽：剔除全库常见功能词（df 超过 N/e）。
			if float64(pt.df) > ubiquitous && pt.stemDF == 0 {
				continue
			}
			eff := pt.df
			if eff == 0 && pt.stemDF > 0 {
				eff = pt.stemDF
			}
			pt.mass = math.Log((float64(n) + 1.0) / (float64(eff) + 1.0))
			out = append(out, pt)
		}
	}
	return out
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// looksProper 判定词槽是否为专名形态：非句首首字母大写或词内驼峰，且字母数不少于 2。
// 纯字母数字粘连的标识符（如 eccv2024）不计入专名，避免把编号当实体。
func looksProper(word string, slot int) bool {
	runes := []rune(word)
	letters := 0
	hasUpper, hasLower, camel, hasDigit := false, false, false, false
	for i, r := range runes {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
			if i > 0 && hasLower {
				camel = true
			}
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < 2 {
		return false
	}
	if hasDigit && !camel {
		return false
	}
	return (hasUpper && slot > 0) || camel
}

// resolvedStemDF 返回可用于认证母词在库的同源词干最小档频；无合格词干返回 0。
func (a *TermExtractor) resolvedStemDF(idx QueryIndexView, low string, primaryDF int, ubiquitous float64) int {
	hostWidth := TrueCarrierWidth(low)
	if hostWidth <= 0 {
		return 0
	}
	minConservation := 1.0 - 1.0/math.E
	best := 0
	for _, tag := range CanonicalCompanionTags(low) {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || tag == low || strings.HasPrefix(tag, "@") || strings.HasPrefix(tag, "cal:") {
			continue
		}
		if TrueCarrierWidth(tag)/hostWidth < minConservation {
			continue
		}
		df := idx.TermDF(a.Encoder().KeyedHash64(tag))
		if df <= 0 || float64(df) > ubiquitous {
			continue
		}
		if best == 0 || df < best {
			best = df
		}
	}
	if primaryDF > 0 && (best == 0 || primaryDF < best) {
		return 0
	}
	return best
}

// hyphenPartsResolved 报告连字符复合词的每个部件是否都能在库解析。
func (a *TermExtractor) hyphenPartsResolved(idx QueryIndexView, low string, ubiquitous float64) bool {
	parts := strings.Split(low, "-")
	seen := 0
	for _, part := range parts {
		if len([]rune(part)) < 2 || !hasLetter(part) {
			continue
		}
		seen++
		h := a.Encoder().KeyedHash64(part)
		df := idx.TermDF(h)
		if df > 0 && float64(df) <= ubiquitous {
			continue
		}
		if a.resolvedStemDF(idx, part, df, ubiquitous) > 0 {
			continue
		}
		return false
	}
	return seen > 0
}

// postingIntersects 报告两个词项是否存在全库同单元共现。
func postingIntersects(idx QueryIndexView, a, b uint64) bool {
	dfA, dfB := idx.TermDF(a), idx.TermDF(b)
	if dfA <= 0 || dfB <= 0 {
		return false
	}
	small, large := a, b
	if dfA > dfB {
		small, large = b, a
	}
	units := make(map[int32]struct{}, idx.TermDF(small))
	idx.ForEachPostingUnit(small, func(unit int32) { units[unit] = struct{}{} })
	if len(units) == 0 {
		return false
	}
	found := false
	idx.ForEachPostingUnit(large, func(unit int32) {
		if found {
			return
		}
		if _, ok := units[unit]; ok {
			found = true
		}
	})
	return found
}

// ArchiveVoidProbe 返回查询相对整库的确定性空洞凭证集（C1/C2/C3）。
func (a *TermExtractor) ArchiveVoidProbe(idx QueryIndexView, text string) []VoidCertificate {
	terms := a.archiveProbeTerms(idx, text)
	if len(terms) == 0 {
		return nil
	}
	var certs []VoidCertificate

	// C1：非句首专名原词零记载且无同源词干。
	for _, t := range terms {
		if t.proper && !t.resolved {
			certs = append(certs, VoidCertificate{VoidKindOOVProper, []string{t.surface}, []int{t.df}})
		}
	}

	// C2：相邻专名二元组各自在库但全库零同单元共现。
	for i := 0; i+1 < len(terms); i++ {
		x, y := terms[i], terms[i+1]
		if x.sentence != y.sentence || y.slot != x.slot+1 {
			continue
		}
		if !x.proper || !y.proper || x.df <= 0 || y.df <= 0 {
			continue
		}
		if !postingIntersects(idx, x.hash, y.hash) {
			certs = append(certs, VoidCertificate{
				VoidKindBrokenCompound,
				[]string{x.surface, y.surface},
				[]int{x.df, y.df},
			})
		}
	}

	// C3：最高权重专名锚与最高信息量焦点实词全库零同单元共现。
	var bestProper, bestFocal *probeTerm
	var sumMass, sumSq float64
	for i := range terms {
		t := &terms[i]
		if t.slot == 0 || t.df <= 0 {
			continue
		}
		sumMass += t.mass
		sumSq += t.mass * t.mass
		if t.proper {
			if bestProper == nil || t.mass > bestProper.mass {
				bestProper = t
			}
			continue
		}
		if bestFocal == nil || t.mass > bestFocal.mass {
			bestFocal = t
		}
	}
	if bestProper != nil && bestFocal != nil && sumMass > 0 {
		mu := sumSq / sumMass
		if bestFocal.mass >= mu && !postingIntersects(idx, bestProper.hash, bestFocal.hash) {
			certs = append(certs, VoidCertificate{
				VoidKindDisjointAnchor,
				[]string{bestProper.surface, bestFocal.surface},
				[]int{bestProper.df, bestFocal.df},
			})
		}
	}
	return certs
}

// QueryCoverage 计算查询的整库信息完备度；isHit 判定某单元是否已交付。
func (a *TermExtractor) QueryCoverage(idx QueryIndexView, text string, isHit func(unit int32) bool) CoverageReport {
	terms := a.archiveProbeTerms(idx, text)
	report := CoverageReport{ActiveTerms: len(terms)}
	if len(terms) == 0 {
		return report
	}
	for _, t := range terms {
		report.FullMass += t.mass
		if !t.resolved {
			report.OOVMass += t.mass
			continue
		}
		if isHit == nil {
			continue
		}
		covered := false
		idx.ForEachPostingUnit(t.hash, func(unit int32) {
			if !covered && isHit(unit) {
				covered = true
			}
		})
		if covered {
			report.CoveredMass += t.mass
		}
	}
	if report.FullMass > 0 {
		report.Completeness = report.CoveredMass / report.FullMass
	}
	return report
}
