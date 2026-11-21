// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "sort"

// docBlocks 是一代不可变的块化文档条目表（stage 2d-1b-a/2d-1c-a）。
//
// 块键域与 term 块一致：unit 连续区间 [lo,hi)，两两不交、区间升序、并集 [0,n)。
// 每块持有该区间全部 unit 的 rmDocEntry 行（行内 hash 升序，与簇状态构建同序）、
// 块内 rowPtr，以及块内位置池：entry.posOff 改为块内偏移，位置值与宽坐标一并
// 复制进块。块内一切为整数/定点，不含浮点。
//
// 块按 unit 升序拼接即得全局视图：行、位置池与宽坐标表都与簇状态逐值相等
// （assemblePosPool/assembleWidePos 是分块持久化的写出序）。
//
// DocMass 依赖全局 N/df，只能在查询期按当前 Mass 的 (mass desc, hash asc)
// 顺序对命中 unit 重算（预注册 §4c/§4e）。
type docBlocks struct {
	// bounds 长 len(blocks)+1：bounds[i] 是块 i 的首个 unit，末项等于 n。
	bounds []int32
	blocks []*docBlock
	n      int
	gen    uint64
}

// docBlock 是单块的不可变文档行：rowPtr 为块内行前缀和，entries 为区间内
// 全部 unit 的行拼接（行内 hash 升序），posOff 指向本块的 posPool。
type docBlock struct {
	lo, hi  int32
	rowPtr  []uint32
	entries []rmDocEntry
	posPool []uint32
	// widePos 以块内偏移为键；无宽坐标语料时为 nil。
	widePos map[uint32]TermPosition
}

// buildDocBlocksFromState 把簇状态的文档行按 unit 连续区间切块，每块最多
// maxUnits 个 unit。块数 k=ceil(N/maxUnits)，边界取 i*N/k，与 term 块同形。
// 位置池按条目逐条复制并重编号；宽坐标表同步重编号。
//
// 前置条件：st 由当前 idx 构建（st.docRowPtr 长度为 N+1）。不满足时返回 nil，
// 由调用方保留全量路径，不静默修补。
func buildDocBlocksFromState(st *rmClusterState, idx *Index, maxUnits int) *docBlocks {
	if st == nil || idx == nil || idx.N <= 0 || maxUnits < 1 || len(st.docRowPtr) != idx.N+1 {
		return nil
	}
	k := (idx.N + maxUnits - 1) / maxUnits
	bounds := make([]int32, 1, k+1)
	for i := 1; i <= k; i++ {
		bounds = append(bounds, int32(int64(i)*int64(idx.N)/int64(k)))
	}
	db := &docBlocks{
		bounds: bounds,
		blocks: make([]*docBlock, 0, k),
		n:      idx.N,
		gen:    idx.stateGen,
	}
	for i := 0; i < k; i++ {
		lo, hi := int(bounds[i]), int(bounds[i+1])
		block := &docBlock{lo: int32(lo), hi: int32(hi)}
		start, end := st.docRowPtr[lo], st.docRowPtr[hi]
		block.entries = append([]rmDocEntry(nil), st.docEntriesFlat[start:end]...)
		for j := range block.entries {
			entry := &block.entries[j]
			globalStart := entry.posOff
			entry.posOff = uint32(len(block.posPool))
			if entry.posLen == 0 {
				continue
			}
			if int(globalStart)+int(entry.posLen) > len(st.posPool) {
				return nil
			}
			block.posPool = append(block.posPool, st.posPool[globalStart:globalStart+entry.posLen]...)
			if st.widePos == nil {
				continue
			}
			for d := uint32(0); d < entry.posLen; d++ {
				if wp, ok := st.widePos[globalStart+d]; ok {
					if block.widePos == nil {
						block.widePos = make(map[uint32]TermPosition, 8)
					}
					block.widePos[entry.posOff+d] = wp
				}
			}
		}
		block.rowPtr = make([]uint32, 0, hi-lo+1)
		for u := lo; u < hi; u++ {
			block.rowPtr = append(block.rowPtr, st.docRowPtr[u]-start)
		}
		block.rowPtr = append(block.rowPtr, uint32(len(block.entries)))
		db.blocks = append(db.blocks, block)
	}
	return db
}

// blockOf 返回 unit 所在块与块内下标；越界或块表缺口返回 ok=false。
func (db *docBlocks) blockOf(u int32) (*docBlock, int32, bool) {
	if db == nil || u < 0 || int(u) >= db.n {
		return nil, 0, false
	}
	i := sort.Search(len(db.blocks), func(i int) bool { return db.blocks[i].hi > u })
	if i >= len(db.blocks) {
		return nil, 0, false
	}
	block := db.blocks[i]
	if u < block.lo {
		return nil, 0, false
	}
	return block, u - block.lo, true
}

// docEntriesOf 返回 unit 在块表中的文档行（posOff 为块内偏移）。
func (db *docBlocks) docEntriesOf(u int32) ([]rmDocEntry, bool) {
	block, j, ok := db.blockOf(u)
	if !ok {
		return nil, false
	}
	return block.entries[block.rowPtr[j]:block.rowPtr[j+1]], true
}

// positionsOf 返回 unit 行内某条目的块内位置切片；值与簇状态的
// entryPositionsOf 逐元素相同。
func (db *docBlocks) positionsOf(u int32, entry rmDocEntry) ([]uint32, bool) {
	block, _, ok := db.blockOf(u)
	if !ok {
		return nil, false
	}
	if entry.posLen == 0 {
		return nil, true
	}
	if int(entry.posOff)+int(entry.posLen) > len(block.posPool) {
		return nil, false
	}
	return block.posPool[entry.posOff : entry.posOff+entry.posLen], true
}

// entrySentExactAt 与 rmClusterState.entrySentExactAt 同式：宽坐标优先，否则
// 由打包值解出句号。
func (db *docBlocks) entrySentExactAt(u int32, entry rmDocEntry, i int) (int, bool) {
	block, _, ok := db.blockOf(u)
	if !ok {
		return 0, false
	}
	idx := entry.posOff + uint32(i)
	if wp, ok := block.widePos[idx]; ok {
		return wp.Sentence, true
	}
	if int(idx) >= len(block.posPool) {
		return 0, false
	}
	return int(block.posPool[idx] >> 16), true
}

// assemblePosPool 按块序拼接位置池，等于簇状态的 posPool；也是分块持久化
// 写出全局池的确定序。
func (db *docBlocks) assemblePosPool() []uint32 {
	if db == nil {
		return nil
	}
	total := 0
	for _, block := range db.blocks {
		total += len(block.posPool)
	}
	out := make([]uint32, 0, total)
	for _, block := range db.blocks {
		out = append(out, block.posPool...)
	}
	return out
}

// assembleWidePos 按块序把块内偏移重映射到全局位置池偏移，等于簇状态的
// widePos。每块 posPool 从零重新编号，故后续块必须加上此前块的池长度。
func (db *docBlocks) assembleWidePos() map[uint32]TermPosition {
	if db == nil {
		return nil
	}
	var out map[uint32]TermPosition
	var base uint32
	for _, block := range db.blocks {
		for offset, position := range block.widePos {
			if out == nil {
				out = make(map[uint32]TermPosition, len(block.widePos))
			}
			out[base+offset] = position
		}
		base += uint32(len(block.posPool))
	}
	return out
}

// docMassOf 按当前全局 Mass 的 (mass desc, hash asc) 顺序单遍累加 unit 的
// 词项质量，与 Index.finalizeMasses 的 DocMass 完全同序，因而 Float64bits
// 相等（预注册 §4e）。调用方应按查询内缓存复用结果（docMassCache 语义）。
func (db *docBlocks) docMassOf(idx *Index, u int32) (float64, bool) {
	entries, ok := db.docEntriesOf(u)
	if !ok {
		return 0, false
	}
	if len(entries) == 0 {
		return 0, true
	}
	ids := make([]uint64, len(entries))
	for i := range entries {
		ids[i] = entries[i].id
	}
	sort.Slice(ids, func(a, b int) bool {
		ma, _ := idx.TermMass(ids[a])
		mb, _ := idx.TermMass(ids[b])
		if ma == mb {
			return ids[a] < ids[b]
		}
		return ma > mb
	})
	var sum float64
	for _, h := range ids {
		m, _ := idx.TermMass(h)
		sum += m
	}
	return sum, true
}
