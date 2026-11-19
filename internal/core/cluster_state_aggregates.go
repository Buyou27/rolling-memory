// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// 本件是簇状态 append-only 快路径的充分统计量与增量维护：把 buildEffectiveWeightsFromDyads
// 内每词族的聚合（df/multiDocCount/簇多重集/伙伴多重集）压缩持久化为「升序键 + 整数计数」，
// 既作为全量构建的中间产物（与内联计算逐位等价），也作为后续纯追加重建的更新基座；
// 同时提供短语倒排的旧表复用合并（旧 (pair,unit) 全序 + 新文档 postings 归并）。
package core

import (
	"math"
	"sort"
)

// clusterFamilyInfo 是全量构建期每词族的累加器（原 buildEffectiveWeightsFromDyads 内部类型外提）。
type clusterFamilyInfo struct {
	lastUnit      int32
	unitCount     int
	multiDocCount int
	clusters      []int32
	partners      []uint64
	weight        float64
}

// clusterFamilyAgg 是单族压缩统计：升序键 + 出现计数。簇键为时间桶时刻，
// 0 表示无时间簇（按出现次数计独立簇）；伙伴键为族根键。
type clusterFamilyAgg struct {
	df            int32
	multiDocCount int32
	weight        float64
	clusterKey    []int64
	clusterCnt    []int32
	partnerKey    []uint64
	partnerCnt    []int32
}

// clusterAggregates 是词族有效权重的增量充分统计量（仅内存态；.zcsr 水合状态不带）。
type clusterAggregates struct {
	unitIDs  []string // 统计所属索引的单元顺序（append 重映射校验）
	fams     map[uint64]*clusterFamilyAgg
	clusterN int32
	// 族根映射快照（升序词项键 + 族根键）：追加时若任一旧词项的族根变化，
	// 统计量失效，必须回退全量重建。
	termParentKeys []uint64
	termParentVals []uint64
	// extended 标记这份统计量由 append-only 快路径扩展而来（测试观测点）。
	extended bool
}

func familyEffectiveWeight(df, multiDocCount, clusters int, total, entropy float64, clusterN int) float64 {
	effDF := 1.0
	if df > 1 {
		if clusters < 1 {
			clusters = 1
		}
		if clusters > df {
			clusters = df
		}
		d := float64(df - 1)
		indep := math.Pow(float64(clusters-1)/d, 1/eulerNumber)
		disp := 1.0
		if total > 0 || multiDocCount > 0 {
			disp = familyDispersion(df, entropy)
		}
		effDF = 1 + d*indep*disp
	}
	return math.Log1p((float64(clusterN) + 1) / (effDF + 1))
}

// familyDispersion is the partner-entropy share H/ln(df) of one family, referenced
// to its document count: a family whose df documents each name a different partner
// has H = ln(df) and reaches 1, the independent-event limit. Partner counts beyond
// df saturate at 1, since no family can carry more effective events than documents.
// A family with fewer than two documents carries no dispersion.
func familyDispersion(df int, entropy float64) float64 {
	if df < 2 {
		return 0
	}
	return math.Min(1, math.Max(0, entropy/math.Log(float64(df))))
}

// familyEffectiveWeightFromAgg 由压缩统计重算族权重（熵按升序键 + 计数求和，
// 与全量构建的升序游程逐位一致）。
func familyEffectiveWeightFromAgg(f *clusterFamilyAgg, clusterN int) float64 {
	clusters := familyClusterCount(f.clusterKey, f.clusterCnt)
	total, entropy := familyEntropy(f.partnerKey, f.partnerCnt)
	return familyEffectiveWeight(int(f.df), int(f.multiDocCount), clusters, total, entropy, clusterN)
}

func familyEntropy(keys []uint64, counts []int32) (total, entropy float64) {
	for _, c := range counts {
		total += float64(c)
	}
	if total <= 0 {
		return 0, 0
	}
	for i := range keys {
		p := float64(counts[i]) / total
		entropy -= p * math.Log(p)
	}
	return total, entropy
}

// familyClusterCount 统计族内唯一簇数：有时间键按唯一时刻计，无时间键（0）
// 按出现次数计（每个无时间文档各自独立成簇）。
func familyClusterCount(keys []int64, counts []int32) int {
	n := 0
	for i, k := range keys {
		if k == 0 {
			n += int(counts[i])
		} else {
			n++
		}
	}
	return n
}

// clusterKeyOf 把簇序号映射为稳定键：时间桶序号 → 桶时刻；无时间簇 → 0。
func clusterKeyOf(cl int32, bucketTimes []int64) int64 {
	if int(cl) >= 0 && int(cl) < len(bucketTimes) {
		return bucketTimes[cl]
	}
	return 0
}

// compressInt64Keys 把升序键压缩为键 + 计数。
func compressInt64Keys(src []int64, keys []int64, counts []int32) ([]int64, []int32) {
	for i := 0; i < len(src); {
		j := i + 1
		for j < len(src) && src[j] == src[i] {
			j++
		}
		keys = append(keys, src[i])
		counts = append(counts, int32(j-i))
		i = j
	}
	return keys, counts
}

func compressUint64Keys(src []uint64, keys []uint64, counts []int32) ([]uint64, []int32) {
	for i := 0; i < len(src); {
		j := i + 1
		for j < len(src) && src[j] == src[i] {
			j++
		}
		keys = append(keys, src[i])
		counts = append(counts, int32(j-i))
		i = j
	}
	return keys, counts
}

// mergeInt64Counts 把增量（已压缩、升序）并入旧键计数表。
// mergeInt64Counts 把增量计数（可含负值，已按升序键压缩）并入旧键计数表；
// 合并后计数 ≤0 的键被丢弃（扣减归零等价于从未出现）。
func mergeInt64Counts(oldKeys []int64, oldCnt []int32, addKeys []int64, addCnt []int32) ([]int64, []int32) {
	if len(addKeys) == 0 {
		return oldKeys, oldCnt
	}
	keys := make([]int64, 0, len(oldKeys)+len(addKeys))
	counts := make([]int32, 0, len(oldKeys)+len(addKeys))
	i, j := 0, 0
	for i < len(oldKeys) && j < len(addKeys) {
		var k int64
		var c int32
		switch {
		case oldKeys[i] < addKeys[j]:
			k, c = oldKeys[i], oldCnt[i]
			i++
		case oldKeys[i] > addKeys[j]:
			k, c = addKeys[j], addCnt[j]
			j++
		default:
			k, c = oldKeys[i], oldCnt[i]+addCnt[j]
			i++
			j++
		}
		if c > 0 {
			keys = append(keys, k)
			counts = append(counts, c)
		}
	}
	for ; i < len(oldKeys); i++ {
		if oldCnt[i] > 0 {
			keys = append(keys, oldKeys[i])
			counts = append(counts, oldCnt[i])
		}
	}
	for ; j < len(addKeys); j++ {
		if addCnt[j] > 0 {
			keys = append(keys, addKeys[j])
			counts = append(counts, addCnt[j])
		}
	}
	return keys, counts
}

// mergeUint64Counts 把增量计数（可含负值，已按升序键压缩）并入旧键计数表；
// 合并后计数 ≤0 的键被丢弃。
func mergeUint64Counts(oldKeys []uint64, oldCnt []int32, addKeys []uint64, addCnt []int32) ([]uint64, []int32) {
	if len(addKeys) == 0 {
		return oldKeys, oldCnt
	}
	keys := make([]uint64, 0, len(oldKeys)+len(addKeys))
	counts := make([]int32, 0, len(oldKeys)+len(addKeys))
	i, j := 0, 0
	for i < len(oldKeys) && j < len(addKeys) {
		var k uint64
		var c int32
		switch {
		case oldKeys[i] < addKeys[j]:
			k, c = oldKeys[i], oldCnt[i]
			i++
		case oldKeys[i] > addKeys[j]:
			k, c = addKeys[j], addCnt[j]
			j++
		default:
			k, c = oldKeys[i], oldCnt[i]+addCnt[j]
			i++
			j++
		}
		if c > 0 {
			keys = append(keys, k)
			counts = append(counts, c)
		}
	}
	for ; i < len(oldKeys); i++ {
		if oldCnt[i] > 0 {
			keys = append(keys, oldKeys[i])
			counts = append(counts, oldCnt[i])
		}
	}
	for ; j < len(addKeys); j++ {
		if addCnt[j] > 0 {
			keys = append(keys, addKeys[j])
			counts = append(counts, addCnt[j])
		}
	}
	return keys, counts
}

// buildClusterAggregates 从全量构建期的族累加器生成压缩统计量（调用前 clusters/partners 已排序）。
func buildClusterAggregates(idx *Index, families map[uint64]*clusterFamilyInfo, st *rmClusterState) *clusterAggregates {
	agg := &clusterAggregates{
		unitIDs:  append([]string(nil), idx.UnitIDs...),
		fams:     make(map[uint64]*clusterFamilyAgg, len(families)),
		clusterN: int32(st.clusterN),
	}
	for k, f := range families {
		fa := &clusterFamilyAgg{df: int32(f.unitCount), multiDocCount: int32(f.multiDocCount)}
		keys64 := make([]int64, 0, len(f.clusters))
		for _, cl := range f.clusters {
			keys64 = append(keys64, clusterKeyOf(cl, st.bucketTimes))
		}
		sort.Slice(keys64, func(i, j int) bool { return keys64[i] < keys64[j] })
		fa.clusterKey, fa.clusterCnt = compressInt64Keys(keys64, nil, nil)
		fa.partnerKey, fa.partnerCnt = compressUint64Keys(f.partners, nil, nil)
		fa.weight = familyEffectiveWeightFromAgg(fa, st.clusterN)
		agg.fams[k] = fa
	}
	agg.termParentKeys = make([]uint64, len(st.termKeys))
	copy(agg.termParentKeys, st.termKeys)
	agg.termParentVals = make([]uint64, len(st.termKeys))
	for i, h := range st.termKeys {
		agg.termParentVals[i] = st.parentGroup[h]
	}
	return agg
}

// fillEffWeightFromAggregates 用族统计量填充 st.effWeight：每词项取族根统计的权重
// （未在统计中的根沿用全量路径的零权重），再按同族覆盖取最小值。
func (st *rmClusterState) fillEffWeightFromAggregates(idx *Index, agg *clusterAggregates) {
	for h := range idx.DF {
		r := h
		if g, ok := st.parentGroup[h]; ok && g != 0 {
			r = g
		}
		if fa, ok := agg.fams[r]; ok {
			st.effWeight[h] = fa.weight
		} else {
			st.effWeight[h] = 0
		}
	}
	for u := 0; u < idx.N; u++ {
		for _, entry := range st.docEntriesOf(int32(u)) {
			if entry.group == 0 {
				continue
			}
			if fa, ok := agg.fams[entry.group]; ok && fa.weight < st.effWeight[entry.id] {
				st.effWeight[entry.id] = fa.weight
			}
		}
	}
}

// incrementalDelta 是通过校验的增量重建描述：旧单元 → 新单元重映射（-1 = 已移除）、
// 新单元标记（delta：新增与覆盖）、待扣减旧单元（dropUnits）与旧状态引用。
type incrementalDelta struct {
	prev       *rmClusterState
	remap      []int32
	delta      []bool
	deltaUnits []int32
	dropUnits  []int32
}

// scanParentGroups 按构建期同式（每文档取 UnitGroups 覆盖值、跨文档取最小）预扫描族根映射。
func scanParentGroups(idx *Index) map[uint64]uint64 {
	pg := make(map[uint64]uint64, len(idx.DF))
	for u := 0; u < idx.N; u++ {
		var ug map[uint64]uint64
		if u < len(idx.UnitGroups) {
			ug = idx.UnitGroups[u]
		}
		for _, h := range idx.DocTerms[u] {
			g := h
			if ug != nil {
				if v := ug[h]; v != 0 {
					g = v
				}
			}
			if prev, ok := pg[h]; !ok || g < prev {
				pg[h] = g
			}
		}
	}
	return pg
}

// takeIncrementalDelta 校验增量门槛：旧状态带统计量与短语倒排、幸存旧单元顺序保持、
// 旧词项族根不变。被删除/覆盖的旧单元记入 dropUnits；校验失败返回 nil（回退全量重建）。
func (e *Engine) takeIncrementalDelta(idx *Index) *incrementalDelta {
	prev := e.prevClusterState
	drops := e.prevDrops
	e.prevClusterState = nil
	e.prevDrops = nil
	if prev == nil || prev.agg == nil || idx == nil || prev.builtIndex == nil {
		return nil
	}
	if len(prev.phraseKeys) != len(prev.phraseRowPtr)-1 || len(prev.phraseUnits) != int(prev.phraseRowPtr[len(prev.phraseRowPtr)-1]) {
		return nil
	}
	oldIDs := prev.agg.unitIDs
	remap := make([]int32, len(oldIDs))
	oldSet := make(map[string]struct{}, len(oldIDs))
	dropUnits := make([]int32, 0, len(drops))
	for i, id := range oldIDs {
		oldSet[id] = struct{}{}
		if _, gone := drops[id]; gone {
			remap[i] = -1
			dropUnits = append(dropUnits, int32(i))
			continue
		}
		u, ok := idx.LookupUnit(id)
		if !ok {
			remap[i] = -1
			dropUnits = append(dropUnits, int32(i))
			continue
		}
		remap[i] = u
	}
	last := int32(-1)
	for _, u := range remap {
		if u < 0 {
			continue
		}
		if u <= last {
			return nil
		}
		last = u
	}
	delta := make([]bool, idx.N)
	deltaUnits := make([]int32, 0, idx.N)
	for u := 0; u < idx.N; u++ {
		id := idx.UnitIDs[u]
		_, wasOld := oldSet[id]
		_, dropped := drops[id]
		if !wasOld || dropped {
			delta[u] = true
			deltaUnits = append(deltaUnits, int32(u))
		}
	}
	if len(deltaUnits) == 0 && len(dropUnits) == 0 {
		return nil
	}
	now := scanParentGroups(idx)
	for i, h := range prev.agg.termParentKeys {
		nowVal, still := now[h]
		if !still {
			// 词项随被删单元整体消失：族贡献由扣减逻辑移除，无需族根一致。
			continue
		}
		if nowVal != prev.agg.termParentVals[i] {
			return nil
		}
	}
	return &incrementalDelta{prev: prev, remap: remap, delta: delta, deltaUnits: deltaUnits, dropUnits: dropUnits}
}

// mergePhraseCSR 复用旧短语倒排（(pair, unit) 全序）与 delta postings 归并：
// 旧单元按单调 remap 重映射后仍升序，与已排序的 delta postings 做双路归并。
func mergePhraseCSR(prev *rmClusterState, remap []int32, delta []dyadPosting) ([]rmDyad, []uint32, []int32) {
	keys := make([]rmDyad, 0, len(prev.phraseKeys)+len(delta))
	units := make([]int32, 0, len(prev.phraseUnits)+len(delta))
	rowPtr := make([]uint32, 0, len(prev.phraseKeys)+1)
	emit := func(p rmDyad, u int32) {
		if len(keys) == 0 || keys[len(keys)-1] != p {
			keys = append(keys, p)
			rowPtr = append(rowPtr, uint32(len(units)))
		}
		units = append(units, u)
	}
	di := 0
	for oi := 0; oi < len(prev.phraseKeys); oi++ {
		op := prev.phraseKeys[oi]
		oUnits := prev.phraseUnits[prev.phraseRowPtr[oi]:prev.phraseRowPtr[oi+1]]
		for di < len(delta) && dyadLess(delta[di].pair, op) {
			emit(delta[di].pair, delta[di].unit)
			di++
		}
		ui := 0
		for ui < len(oUnits) && di < len(delta) && delta[di].pair == op {
			nu := remap[oUnits[ui]]
			if nu < 0 {
				ui++
				continue
			}
			du := delta[di].unit
			switch {
			case nu < du:
				emit(op, nu)
				ui++
			case du < nu:
				emit(op, du)
				di++
			default:
				emit(op, nu)
				ui++
				di++
			}
		}
		for ; ui < len(oUnits); ui++ {
			if nu := remap[oUnits[ui]]; nu >= 0 {
				emit(op, nu)
			}
		}
		for di < len(delta) && delta[di].pair == op {
			emit(op, delta[di].unit)
			di++
		}
	}
	for ; di < len(delta); di++ {
		emit(delta[di].pair, delta[di].unit)
	}
	rowPtr = append(rowPtr, uint32(len(units)))
	return keys, rowPtr, units
}

// dyadLess 比较 (a,b) 字典序（与 dyadPostingSort 的 pair 顺序一致）。
func dyadLess(x, y rmDyad) bool {
	if x.a != y.a {
		return x.a < y.a
	}
	return x.b < y.b
}

// docFamilySet 按构建期同式取单篇文档的首见族集合（文档内去重，保序）。
func docFamilySet(entries []rmDocEntry, root func(uint64) uint64, seen map[uint64]struct{}, dst []uint64) ([]uint64, map[uint64]struct{}) {
	dst = dst[:0]
	if seen == nil {
		seen = make(map[uint64]struct{})
	} else {
		for k := range seen {
			delete(seen, k)
		}
	}
	for _, entry := range entries {
		g := entry.group
		if g == 0 {
			g = root(entry.id)
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		dst = append(dst, g)
	}
	return dst, seen
}

// collectRootPairs 把文档 dyad 归约为族根对并按文档去重后登记到伙伴表（双向）。
func collectRootPairs(dyads []rmDyad, root func(uint64) uint64, into map[uint64][]uint64, scratch []rmDyad) []rmDyad {
	scratch = scratch[:0]
	for _, pair := range dyads {
		a, b := root(pair.a), root(pair.b)
		if a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		scratch = append(scratch, rmDyad{a: a, b: b})
	}
	if len(scratch) > 1 {
		sort.Sort(rmDyadSort(scratch))
	}
	for i, pair := range scratch {
		if i > 0 && pair == scratch[i-1] {
			continue
		}
		into[pair.a] = append(into[pair.a], pair.b)
		into[pair.b] = append(into[pair.b], pair.a)
	}
	return scratch
}

// extendForDelta 从旧统计量中扣减被删除/覆盖单元的贡献，再并入 delta 单元的贡献，
// 并只重算受影响族的权重（clusterN 变化时整体重算）。
func (agg *clusterAggregates) extendForDelta(st *rmClusterState, idx *Index, inc *incrementalDelta, docDyadRowPtr []uint32, docDyadsFlat []rmDyad) {
	addClusters := make(map[uint64][]int64)
	addPartners := make(map[uint64][]uint64)
	subClusters := make(map[uint64][]int64)
	subPartners := make(map[uint64][]uint64)
	getFam := func(id uint64) *clusterFamilyAgg {
		fa := agg.fams[id]
		if fa == nil {
			fa = &clusterFamilyAgg{}
			agg.fams[id] = fa
		}
		return fa
	}
	root := func(id uint64) uint64 {
		if g, ok := st.parentGroup[id]; ok && g != 0 {
			return g
		}
		return id
	}
	var docFams []uint64
	var seen map[uint64]struct{}
	var pairScratch []rmDyad
	var occScratch []rmPackedOccurrence
	var dyadScratch []rmDyad

	// 1) 扣减：被删除/覆盖的旧单元按旧状态重算贡献，再做负计数合并。
	for _, ou := range inc.dropUnits {
		if int(ou) >= len(inc.prev.clusters) {
			continue
		}
		entries := inc.prev.docEntriesOf(ou)
		if inc.prev.hasWidePos() {
			dyadScratch = inc.prev.extractDocDyadsExactInto(entries, dyadScratch[:0])
		} else {
			dyadScratch, occScratch = inc.prev.extractDocDyadsInto(entries, occScratch, dyadScratch[:0])
		}
		ck := clusterKeyOf(inc.prev.clusters[ou], inc.prev.bucketTimes)
		docFams, seen = docFamilySet(entries, root, seen, docFams)
		for _, g := range docFams {
			if fa := agg.fams[g]; fa != nil {
				fa.df--
			}
			subClusters[g] = append(subClusters[g], ck)
		}
		if len(docFams) > 1 {
			for _, g := range docFams {
				if fa := agg.fams[g]; fa != nil {
					fa.multiDocCount--
				}
			}
		}
		pairScratch = collectRootPairs(dyadScratch, root, subPartners, pairScratch)
	}
	for g, list := range subClusters {
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		keys, cnt := compressInt64Keys(list, nil, nil)
		neg := make([]int32, len(cnt))
		for i := range cnt {
			neg[i] = -cnt[i]
		}
		fa := agg.fams[g]
		if fa == nil {
			continue
		}
		fa.clusterKey, fa.clusterCnt = mergeInt64Counts(fa.clusterKey, fa.clusterCnt, keys, neg)
	}
	for g, list := range subPartners {
		sortUint64Asc(list)
		keys, cnt := compressUint64Keys(list, nil, nil)
		neg := make([]int32, len(cnt))
		for i := range cnt {
			neg[i] = -cnt[i]
		}
		fa := agg.fams[g]
		if fa == nil {
			continue
		}
		fa.partnerKey, fa.partnerCnt = mergeUint64Counts(fa.partnerKey, fa.partnerCnt, keys, neg)
	}

	// 2) 并入 delta 单元（新增与覆盖后的新贡献）。
	for _, u := range inc.deltaUnits {
		ck := clusterKeyOf(st.clusters[u], st.bucketTimes)
		docFams, seen = docFamilySet(st.docEntriesOf(u), root, seen, docFams)
		for _, g := range docFams {
			getFam(g).df++
			addClusters[g] = append(addClusters[g], ck)
		}
		if len(docFams) > 1 {
			for _, g := range docFams {
				getFam(g).multiDocCount++
			}
		}
		pairScratch = collectRootPairs(docDyadsFlat[docDyadRowPtr[u]:docDyadRowPtr[u+1]], root, addPartners, pairScratch)
	}
	for g, list := range addClusters {
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		keys, cnt := compressInt64Keys(list, nil, nil)
		fa := getFam(g)
		fa.clusterKey, fa.clusterCnt = mergeInt64Counts(fa.clusterKey, fa.clusterCnt, keys, cnt)
	}
	for g, list := range addPartners {
		sortUint64Asc(list)
		keys, cnt := compressUint64Keys(list, nil, nil)
		fa := getFam(g)
		fa.partnerKey, fa.partnerCnt = mergeUint64Counts(fa.partnerKey, fa.partnerCnt, keys, cnt)
	}

	// 3) 权重：受影响族重算；clusterN 变化时整体重算。df 归零的族等价于从未出现，
	// 与全量路径（未观测族在填充时取零权重）保持一致，直接删除。
	touched := make(map[uint64]struct{}, len(addClusters)+len(subClusters)+len(addPartners)+len(subPartners))
	for g := range addClusters {
		touched[g] = struct{}{}
	}
	for g := range subClusters {
		touched[g] = struct{}{}
	}
	for g := range addPartners {
		touched[g] = struct{}{}
	}
	for g := range subPartners {
		touched[g] = struct{}{}
	}
	for g := range touched {
		if fa := agg.fams[g]; fa != nil && fa.df <= 0 {
			delete(agg.fams, g)
		}
	}
	if int32(st.clusterN) != agg.clusterN {
		agg.clusterN = int32(st.clusterN)
		for _, fa := range agg.fams {
			fa.weight = familyEffectiveWeightFromAgg(fa, st.clusterN)
		}
	} else {
		for g := range touched {
			if fa := agg.fams[g]; fa != nil {
				fa.weight = familyEffectiveWeightFromAgg(fa, st.clusterN)
			}
		}
	}
	agg.unitIDs = append(agg.unitIDs[:0], idx.UnitIDs...)
	agg.extended = true
	// 族根快照随本次构建刷新。
	agg.termParentKeys = make([]uint64, len(st.termKeys))
	copy(agg.termParentKeys, st.termKeys)
	agg.termParentVals = make([]uint64, len(st.termKeys))
	for i, h := range st.termKeys {
		agg.termParentVals[i] = st.parentGroup[h]
	}
}
