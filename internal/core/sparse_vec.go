// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"sort"
)

// SparseEntry 表示动态能量稀疏向量（SparseVector）中的一个非零坐标项（按 ID 严格升序排列）。
// WeightNats 为该特征维在当前语料参照系 M_N（及可选条件集 Q, S）下的动态信息能量（单位：Nats）。
type SparseEntry struct {
	ID           uint64  // 特征维标识（词族 Group ID、词项 Term ID、短语键或单元下标）
	WeightNats   float64 // 当前参照系下的动态信息能量（Nats，> 0）
	SelfInfoNats float64 // 一阶基础碰撞自信息 I_1（Nats，>= 0）
	TF           int32   // 单元内出现频次或簇内独立成员互证计数（>= 1）
	DF           int     // 语料文档频次 df
	IsCore       bool    // 是否属于非高频剥离核心支撑（df <= N/e^2 或携带刚性结构位）
	Flags        uint8   // 8-bit 结构标志包络
	Role         TermRole
}

// SparseVector 是记忆内核表达「词 / 词组 / 簇 / 语料单元」动态能量的统一紧凑稀疏向量：
// Entries 按 ID 严格升序且无重复坐标；所有标量统计矩（L1 总能量 TotalMassNats、核心能量
// CoreMassNats、L2 范数 NormL2、二阶 Rényi 特征能级 RenyiMuNats）均由当前语料参照系 M_N
// 实时导出，不落盘静态数值快照。
type SparseVector struct {
	Entries       []SparseEntry // 按 ID 严格升序排列的非零能量坐标
	TotalMassNats float64       // L1 总动态信息能量 E = ∑ w_i（Nats）
	CoreMassNats  float64       // 核心特征维（IsCore=true）动态能量和 E_core（Nats）
	NormL2        float64       // L2 欧氏范数 ||w||_2 = sqrt(∑ w_i^2)（Nats）
	RenyiMuNats   float64       // 二阶 Rényi 特征能量标尺 μ = ∑ w_i^2 / ∑ w_i（Nats）
}

type sparseEntryByIDSort []SparseEntry

func (s sparseEntryByIDSort) Len() int           { return len(s) }
func (s sparseEntryByIDSort) Less(i, j int) bool { return s[i].ID < s[j].ID }
func (s sparseEntryByIDSort) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type sparseEntryByWeightDescSort []SparseEntry

func (s sparseEntryByWeightDescSort) Len() int { return len(s) }
func (s sparseEntryByWeightDescSort) Less(i, j int) bool {
	if s[i].WeightNats != s[j].WeightNats {
		return s[i].WeightNats > s[j].WeightNats
	}
	return s[i].ID < s[j].ID
}
func (s sparseEntryByWeightDescSort) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func sortSparseEntriesByID(entries []SparseEntry) {
	if len(entries) < 2 {
		return
	}
	if len(entries) <= 32 {
		for i := 1; i < len(entries); i++ {
			v := entries[i]
			j := i - 1
			for j >= 0 && v.ID < entries[j].ID {
				entries[j+1] = entries[j]
				j--
			}
			entries[j+1] = v
		}
		return
	}
	sort.Sort(sparseEntryByIDSort(entries))
}

// BuildSparseVector 将任意坐标切片规整为标准 SparseVector：过滤非正/非有限权重，按 ID
// 严格升序排列，对同 ID 坐标执行幂等上确界包络（max WeightNats / max SelfInfoNats，累加 TF），
// 并单遍计算 TotalMassNats、CoreMassNats、NormL2 与 RenyiMuNats。
func BuildSparseVector(raw []SparseEntry) SparseVector {
	if len(raw) == 0 {
		return SparseVector{}
	}
	buf := make([]SparseEntry, 0, len(raw))
	for _, e := range raw {
		if math.IsNaN(e.WeightNats) || math.IsInf(e.WeightNats, 0) || e.WeightNats <= 0 {
			continue
		}
		if e.TF < 1 {
			e.TF = 1
		}
		buf = append(buf, e)
	}
	if len(buf) == 0 {
		return SparseVector{}
	}
	sortSparseEntriesByID(buf)
	outIdx := 0
	var tot, coreTot, sqSum float64
	for i := 0; i < len(buf); {
		cur := buf[i]
		i++
		for i < len(buf) && buf[i].ID == cur.ID {
			if buf[i].WeightNats > cur.WeightNats {
				cur.WeightNats = buf[i].WeightNats
			}
			if buf[i].SelfInfoNats > cur.SelfInfoNats {
				cur.SelfInfoNats = buf[i].SelfInfoNats
			}
			cur.TF += buf[i].TF
			if buf[i].DF > cur.DF {
				cur.DF = buf[i].DF
			}
			if buf[i].IsCore {
				cur.IsCore = true
			}
			cur.Flags |= buf[i].Flags
			if buf[i].Role < cur.Role {
				cur.Role = buf[i].Role
			}
			i++
		}
		buf[outIdx] = cur
		outIdx++
		tot += cur.WeightNats
		if cur.IsCore {
			coreTot += cur.WeightNats
		}
		sqSum += cur.WeightNats * cur.WeightNats
	}
	buf = buf[:outIdx]
	var mu float64
	if tot > 0 {
		mu = sqSum / tot
	}
	return SparseVector{
		Entries:       buf,
		TotalMassNats: tot,
		CoreMassNats:  coreTot,
		NormL2:        math.Sqrt(sqSum),
		RenyiMuNats:   mu,
	}
}

func finalizePreSortedSparseVector(buf []SparseEntry) SparseVector {
	if len(buf) == 0 {
		return SparseVector{}
	}
	var tot, coreTot, sqSum float64
	for _, cur := range buf {
		tot += cur.WeightNats
		if cur.IsCore {
			coreTot += cur.WeightNats
		}
		sqSum += cur.WeightNats * cur.WeightNats
	}
	var mu float64
	if tot > 0 {
		mu = sqSum / tot
	}
	return SparseVector{
		Entries:       buf,
		TotalMassNats: tot,
		CoreMassNats:  coreTot,
		NormL2:        math.Sqrt(sqSum),
		RenyiMuNats:   mu,
	}
}

// Len 返回非零能量坐标个数 nnz。
func (v SparseVector) Len() int {
	return len(v.Entries)
}

// IsEmpty 报告稀疏向量是否为空或零能量。
func (v SparseVector) IsEmpty() bool {
	return len(v.Entries) == 0 || v.TotalMassNats <= 0
}

// Lookup 以二分查找读取指定坐标 ID 的能量项（O(log nnz)）。
func (v SparseVector) Lookup(id uint64) (SparseEntry, bool) {
	lo, hi := 0, len(v.Entries)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if v.Entries[mid].ID < id {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(v.Entries) && v.Entries[lo].ID == id {
		return v.Entries[lo], true
	}
	return SparseEntry{}, false
}

// TopEntriesByWeight 返回按动态能量 WeightNats 降序（并列按 ID 升序）排列的前 k 个坐标副本。
func (v SparseVector) TopEntriesByWeight(k int) []SparseEntry {
	if len(v.Entries) == 0 || k <= 0 {
		return nil
	}
	cp := append([]SparseEntry(nil), v.Entries...)
	sort.Sort(sparseEntryByWeightDescSort(cp))
	if len(cp) > k {
		cp = cp[:k]
	}
	return cp
}

// SupremumJoin 执行表示-读出律 2（幂等上确界融合 a ∨ b）：
// 对共享坐标取 max(w_a, w_b)（满足同源幂等律 (a ∨ a).WeightNats == a.WeightNats，消除克隆双计），
// 对正交互补坐标完整保留，并在 TF 中记录跨向量互证频次。双指针线性时间 O(nnz(a)+nnz(b))。
func SupremumJoin(a, b SparseVector) SparseVector {
	if len(a.Entries) == 0 {
		return b
	}
	if len(b.Entries) == 0 {
		return a
	}
	ea, eb := a.Entries, b.Entries
	out := make([]SparseEntry, 0, len(ea)+len(eb))
	i, j := 0, 0
	for i < len(ea) && j < len(eb) {
		if ea[i].ID < eb[j].ID {
			out = append(out, ea[i])
			i++
		} else if ea[i].ID > eb[j].ID {
			out = append(out, eb[j])
			j++
		} else {
			merged := ea[i]
			if eb[j].WeightNats > merged.WeightNats {
				merged.WeightNats = eb[j].WeightNats
			}
			if eb[j].SelfInfoNats > merged.SelfInfoNats {
				merged.SelfInfoNats = eb[j].SelfInfoNats
			}
			merged.TF += eb[j].TF
			if eb[j].DF > merged.DF {
				merged.DF = eb[j].DF
			}
			if eb[j].IsCore {
				merged.IsCore = true
			}
			merged.Flags |= eb[j].Flags
			if eb[j].Role < merged.Role {
				merged.Role = eb[j].Role
			}
			out = append(out, merged)
			i++
			j++
		}
	}
	out = append(out, ea[i:]...)
	out = append(out, eb[j:]...)
	return finalizePreSortedSparseVector(out)
}

// SupremumJoinMany 计算多个稀疏向量的幂等上确界包络 ∨_k v_k（用于构造簇级动态能量向量）。
func SupremumJoinMany(vecs ...SparseVector) SparseVector {
	var acc SparseVector
	for _, v := range vecs {
		if len(v.Entries) == 0 {
			continue
		}
		acc = SupremumJoin(acc, v)
	}
	return acc
}

// IntersectionMeet 计算两稀疏向量的交集下确界包络 a ∧ b（仅保留共享支撑 supp(a) ∩ supp(b)，
// 坐标能量取木桶下确界 min(w_a, w_b)）。双指针线性时间 O(nnz(a)+nnz(b))。
func IntersectionMeet(a, b SparseVector) SparseVector {
	if len(a.Entries) == 0 || len(b.Entries) == 0 {
		return SparseVector{}
	}
	ea, eb := a.Entries, b.Entries
	minCap := len(ea)
	if len(eb) < minCap {
		minCap = len(eb)
	}
	out := make([]SparseEntry, 0, minCap)
	i, j := 0, 0
	for i < len(ea) && j < len(eb) {
		if ea[i].ID < eb[j].ID {
			i++
		} else if ea[i].ID > eb[j].ID {
			j++
		} else {
			merged := ea[i]
			if eb[j].WeightNats < merged.WeightNats {
				merged.WeightNats = eb[j].WeightNats
			}
			if eb[j].SelfInfoNats < merged.SelfInfoNats {
				merged.SelfInfoNats = eb[j].SelfInfoNats
			}
			merged.TF += eb[j].TF
			if eb[j].IsCore {
				merged.IsCore = true
			}
			merged.Flags |= eb[j].Flags
			out = append(out, merged)
			i++
			j++
		}
	}
	return finalizePreSortedSparseVector(out)
}

// ResidualDifference 计算有向差集稀疏向量 a \ b（保留 supp(a) \ supp(b) 中的正交残差坐标）。
func ResidualDifference(a, b SparseVector) SparseVector {
	if len(a.Entries) == 0 {
		return SparseVector{}
	}
	if len(b.Entries) == 0 {
		return a
	}
	ea, eb := a.Entries, b.Entries
	out := make([]SparseEntry, 0, len(ea))
	i, j := 0, 0
	for i < len(ea) && j < len(eb) {
		if ea[i].ID < eb[j].ID {
			out = append(out, ea[i])
			i++
		} else if ea[i].ID > eb[j].ID {
			j++
		} else {
			i++
			j++
		}
	}
	out = append(out, ea[i:]...)
	return finalizePreSortedSparseVector(out)
}

// SalientResidual 计算相对于特征能级标尺 muScale 的显著独有残差向量 I_{>μ}(a \ b)：
// 仅保留 supp(a) \ supp(b) 中满足 IsCore 且 WeightNats > muScale 的坐标，能量取超额部分 w - muScale。
// 当 muScale <= 0 时自动使用向量 a 自身的二阶 Rényi 特征能级 a.RenyiMuNats。
func SalientResidual(a, b SparseVector, muScale float64) SparseVector {
	if len(a.Entries) == 0 {
		return SparseVector{}
	}
	if muScale <= 0 {
		muScale = a.RenyiMuNats
	}
	ea, eb := a.Entries, b.Entries
	out := make([]SparseEntry, 0, len(ea))
	i, j := 0, 0
	for i < len(ea) && j < len(eb) {
		if ea[i].ID < eb[j].ID {
			if ea[i].IsCore && ea[i].WeightNats > muScale {
				cur := ea[i]
				cur.WeightNats -= muScale
				out = append(out, cur)
			}
			i++
		} else if ea[i].ID > eb[j].ID {
			j++
		} else {
			i++
			j++
		}
	}
	for ; i < len(ea); i++ {
		if ea[i].IsCore && ea[i].WeightNats > muScale {
			cur := ea[i]
			cur.WeightNats -= muScale
			out = append(out, cur)
		}
	}
	return finalizePreSortedSparseVector(out)
}

// SparseRowView 是底层 CSR 扁平数组（subjRowPtr / subjIDsPool / subjWeightsPool 或 .zcsr mmap 段）
// 上的零拷贝只读稀疏向量行视图，支持零分配双指针内积与带地板余弦读出。
type SparseRowView struct {
	IDs     []uint64
	Weights []float64
	NormSq  float64
}

func (st *rmClusterState) subjRowView(u int32) SparseRowView {
	if st == nil || u < 0 || int(u)+1 >= len(st.subjRowPtr) || int(u) >= len(st.fastDocs) {
		return SparseRowView{}
	}
	s, e := st.subjRowPtr[u], st.subjRowPtr[u+1]
	return SparseRowView{
		IDs:     st.subjIDsPool[s:e],
		Weights: st.subjWeightsPool[s:e],
		NormSq:  st.fastDocs[u].subjNormSq,
	}
}

// CosineWithFloor 在两个升序 CSR 稀疏行视图上执行双指针内积并按 floor 保底返回 [0, 1] 余弦。
// 与 computeDocSubjectCos 的指令序列 100% 逐位同构（0-ULP）。
func (r SparseRowView) CosineWithFloor(other SparseRowView, floor float64) float64 {
	if r.NormSq <= 0 || other.NormSq <= 0 {
		return floor
	}
	ia, ib := r.IDs, other.IDs
	wa := r.Weights
	var dot float64
	for x, y := 0, 0; x < len(ia) && y < len(ib); {
		if ia[x] < ib[y] {
			x++
		} else if ia[x] > ib[y] {
			y++
		} else {
			dot += wa[x]
			x++
			y++
		}
	}
	if dot <= 0 {
		return floor
	}
	return math.Max(floor, math.Min(1.0, dot/math.Sqrt(r.NormSq*other.NormSq)))
}

// VectorPairDecomposition 承载两个稀疏能量向量 (u, v) 的二项式完备对偶分解：
// (C_{u->v} + R_{u\v})(C_{v->u} + R_{v\u}) ≡ 1。
type VectorPairDecomposition struct {
	SharedGroups          int     // 共享特征维数量 |supp(u) ∩ supp(v)|
	SharedUNats           float64 // u 在交集上的能量和 I_u(u ∩ v)（Nats）
	SharedVNats           float64 // v 在交集上的能量和 I_v(u ∩ v)（Nats）
	SharedMinNats         float64 // 交集下确界能量和 ∑ min(w_u, w_v)（Nats）
	SelfExcessNats        float64 // u 独有残差能量 I(u \ v)（Nats）
	PeerExcessNats        float64 // v 独有残差能量 I(v \ u)（Nats）
	SelfSalientExcessNats float64 // u 超过自身 Rényi 标尺 μ(u) 的显著独有残差 I_{>μ}(u \ v)（Nats）
	PeerSalientExcessNats float64 // v 超过自身 Rényi 标尺 μ(v) 的显著独有残差 I_{>μ}(v \ u)（Nats）
	SelfCoverage          float64 // 正向覆盖率 C_{u->v} = I_u(u ∩ v) / E(u) in [0, 1]
	PeerCoverage          float64 // 反向覆盖率 C_{v->u} = I_v(u ∩ v) / E(v) in [0, 1]
	HarmonicCoverage      float64 // 调和覆盖度 F_1(u, v) = 2*C_{u->v}*C_{v->u}/(C_{u->v}+C_{v->u}) in [0, 1]
	Cosine                float64 // 稀疏向量 L2 余弦相似度 <u, v> / (||u||_2 * ||v||_2) in [0, 1]
}

// DecomposeSparsePair 对任意两个 SparseVector 执行双指针 O(nnz(u)+nnz(v)) 对偶分解。
func DecomposeSparsePair(u, v SparseVector) VectorPairDecomposition {
	var d VectorPairDecomposition
	eu, ev := u.Entries, v.Entries
	var dot float64
	i, j := 0, 0
	for i < len(eu) && j < len(ev) {
		gu, gv := eu[i].ID, ev[j].ID
		if gu < gv {
			w := eu[i].WeightNats
			d.SelfExcessNats += w
			if eu[i].IsCore && w > u.RenyiMuNats {
				d.SelfSalientExcessNats += w - u.RenyiMuNats
			}
			i++
		} else if gu > gv {
			w := ev[j].WeightNats
			d.PeerExcessNats += w
			if ev[j].IsCore && w > v.RenyiMuNats {
				d.PeerSalientExcessNats += w - v.RenyiMuNats
			}
			j++
		} else {
			wu, wv := eu[i].WeightNats, ev[j].WeightNats
			d.SharedGroups++
			d.SharedUNats += wu
			d.SharedVNats += wv
			d.SharedMinNats += math.Min(wu, wv)
			dot += wu * wv
			i++
			j++
		}
	}
	for ; i < len(eu); i++ {
		w := eu[i].WeightNats
		d.SelfExcessNats += w
		if eu[i].IsCore && w > u.RenyiMuNats {
			d.SelfSalientExcessNats += w - u.RenyiMuNats
		}
	}
	for ; j < len(ev); j++ {
		w := ev[j].WeightNats
		d.PeerExcessNats += w
		if ev[j].IsCore && w > v.RenyiMuNats {
			d.PeerSalientExcessNats += w - v.RenyiMuNats
		}
	}
	if u.TotalMassNats > 0 {
		d.SelfCoverage = math.Min(1.0, d.SharedUNats/u.TotalMassNats)
	}
	if v.TotalMassNats > 0 {
		d.PeerCoverage = math.Min(1.0, d.SharedVNats/v.TotalMassNats)
	}
	if d.SelfCoverage+d.PeerCoverage > 0 {
		d.HarmonicCoverage = 2.0 * d.SelfCoverage * d.PeerCoverage / (d.SelfCoverage + d.PeerCoverage)
	}
	if dot > 0 && u.NormL2 > 0 && v.NormL2 > 0 {
		d.Cosine = math.Min(1.0, dot/(u.NormL2*v.NormL2))
	}
	return d
}

// ConditionedReadout 表示稀疏能量向量在给定「查询场 Q × 已选集合覆盖状态 S_{k-1}」下的
// 唯一条件标量泛函读出（Representation-Readout Law 4）。
type ConditionedReadout struct {
	MatchedGroups          int     // 命中的查询词族数 |supp(v) ∩ Q|
	QueryOverlapNats       float64 // 查询侧命中质量 E_Q(Q ∩ v)（Nats）
	VectorOverlapNats      float64 // 向量侧命中质量 E_v(Q ∩ v)（Nats）
	SurvivalAddressingNats float64 // 经已选集指数生存率 exp(-cov_g(S)) 折算后的条件寻址功（Nats）
	QueryCoverage          float64 // 正向查询覆盖率 C_Q(v) = E_Q(Q ∩ v) / E(Q) in [0, 1]
	FocusPurity            float64 // 语料均句能平滑后的反向焦点纯度 C_S(v) = E_v(Q ∩ v) / (E(v) + E_sent_mean) in [0, 1]
	StructuredIncrement    float64 // 双向覆盖几何-包络结构增量 ΔE_struct(v | Q)（Nats）
	SalientNoveltyNats     float64 // 未知伴随支撑 supp(v) \ Q 上超过 μ_Q 的条件存活新颖度能量（Nats）
}

// EvaluateConditionedReadout 将定义域上的 SparseVector 在给定查询词族权重 qGroups 与
// 已选覆盖状态 survivalCov 下坍缩为条件标量读出（名次盲构造律：仅依赖向量自身物理量与 (Q, S)）。
func EvaluateConditionedReadout(v SparseVector, qGroups map[uint64]float64, totalQueryMass, muQ, priorSentNats float64, survivalCov map[uint64]float64) ConditionedReadout {
	if len(v.Entries) == 0 {
		return ConditionedReadout{}
	}
	if totalQueryMass <= 0 && len(qGroups) > 0 {
		var sq float64
		for _, qw := range qGroups {
			if qw > 0 {
				totalQueryMass += qw
				sq += qw * qw
			}
		}
		if muQ <= 0 && totalQueryMass > 0 {
			muQ = sq / totalQueryMass
		}
	}
	if muQ <= 0 {
		muQ = v.RenyiMuNats
	}
	var out ConditionedReadout
	for _, e := range v.Entries {
		surv := 1.0
		if len(survivalCov) > 0 {
			if cov := survivalCov[e.ID]; cov > 0 {
				surv = math.Exp(-cov)
			}
		}
		if qw, inQ := qGroups[e.ID]; inQ && qw > 0 {
			out.MatchedGroups++
			out.QueryOverlapNats += qw
			out.VectorOverlapNats += e.WeightNats
			out.SurvivalAddressingNats += e.WeightNats * surv
		} else if e.IsCore && e.WeightNats > muQ {
			out.SalientNoveltyNats += e.WeightNats * surv
		}
	}
	if totalQueryMass > 0 {
		out.QueryCoverage = math.Min(1.0, out.QueryOverlapNats/totalQueryMass)
	}
	denom := v.TotalMassNats + math.Max(0, priorSentNats)
	if denom > 0 {
		out.FocusPurity = math.Min(1.0, out.VectorOverlapNats/denom)
	}
	if out.QueryOverlapNats > 0 {
		maximum := math.Max(out.QueryCoverage, out.FocusPurity)
		out.StructuredIncrement = out.QueryOverlapNats * (math.Sqrt(out.QueryCoverage*out.FocusPurity) + maximum*maximum*invE)
	}
	return out
}

// toSparseVector 将内部 unitGroupProfile 无损投射为标准 SparseVector。
func (p unitGroupProfile) toSparseVector() SparseVector {
	if len(p.items) == 0 {
		return SparseVector{}
	}
	entries := make([]SparseEntry, len(p.items))
	var coreTot, sqSum float64
	for i, it := range p.items {
		tf := int32(len(it.pos))
		if tf < 1 {
			tf = 1
		}
		entries[i] = SparseEntry{
			ID:           it.group,
			WeightNats:   it.weight,
			SelfInfoNats: it.weight,
			TF:           tf,
			IsCore:       it.isCore,
		}
		if it.isCore {
			coreTot += it.weight
		}
		sqSum += it.weight * it.weight
	}
	return SparseVector{
		Entries:       entries,
		TotalMassNats: p.totalMass,
		CoreMassNats:  coreTot,
		NormL2:        math.Sqrt(sqSum),
		RenyiMuNats:   p.muMass,
	}
}

// ----------------------------------------------------------------------------
// 四级动态能量稀疏向量构造器（1. 词 Term · 2. 词组 Phrase · 3. 单元 Unit · 4. 簇 Cluster）
// ----------------------------------------------------------------------------

// TermEnergyProfile 表达单个「词 / 词族（Term）」在当前语料参照系 M_N 下的完整动态能量画像：
//   - 一阶对角元：基础碰撞自信息 SelfInfoNats 与经跨簇弥散 K_st × 搭档上下文熵 H_ctx 重整化的
//     有效动态能量 EffectiveWeightNats，以及泊松日历凝聚度 CalendarCohesion；
//   - 二阶非对角稀疏向量 PartnerVector：该词在全库同句窗口内共现的全部短语搭档及其超加成束缚能量；
//   - 单元分布稀疏向量 UnitVector：该词在各命中语料单元内的饱和动态能量分布。
type TermEnergyProfile struct {
	TermID              uint64       // 词项 64-bit 哈希标识
	GroupID             uint64       // 所属词族抽象簇标识
	DF                  int          // 语料文档频次 df(t)
	SelfInfoNats        float64      // 一阶碰撞自信息 I_1(t)（Nats）
	EffectiveWeightNats float64      // 二阶短语熵与跨簇重整化后的有效动态能量 w_eff(t)（Nats）
	CalendarCohesion    float64      // 泊松日历超额凝聚度 κ_occ(t) in [0, 1)
	ProperRatio         float64      // 全库大写专名出现占比 ρ_prop(t) in [0, 1]
	PartnerVector       SparseVector // 二阶短语搭档动态能量稀疏向量（ID = 搭档词项哈希，WeightNats = m_phrase）
	UnitVector          SparseVector // 单元级分布动态能量稀疏向量（ID = uint64(unit)，WeightNats = 单元内饱和能量）
}

// TermEnergyVector 实时计算词项 termID 在当前语料参照系 M_N 下的动态能量稀疏向量画像。
func (e *Engine) TermEnergyVector(termID uint64) (TermEnergyProfile, bool) {
	if e == nil || e.Index == nil || e.Index.N == 0 || termID == 0 {
		return TermEnergyProfile{}, false
	}
	idx := e.Index
	df := idx.TermDF(termID)
	if df <= 0 {
		return TermEnergyProfile{}, false
	}
	st := e.ensureClusterState()
	selfInfo, _ := idx.TermMass(termID)
	effW, ok := st.effWeightOf(termID)
	if !ok || effW <= 0 {
		effW = selfInfo
	}
	groupID := termID
	if pg, okG := st.parentGroupOf(termID); okG && pg != 0 {
		groupID = pg
	}
	highDFThresh := idx.HighDFThreshold()
	meanBits := st.entryMeanBits

	posts := st.termPostings(termID)
	const noDay int64 = -1 << 62
	days := make(map[int64]struct{}, len(posts))
	unitEntries := make([]SparseEntry, 0, len(posts))
	for _, p := range posts {
		if int(p.unit) < 0 || int(p.unit) >= idx.N {
			continue
		}
		if d := st.fastDocs[p.unit].day; d != noDay {
			days[d] = struct{}{}
		}
		uEntries := st.docEntriesOf(p.unit)
		if int(p.entry) < 0 || int(p.entry) >= len(uEntries) {
			continue
		}
		de := uEntries[p.entry]
		repeats := math.Min(1.0, math.Log(math.Max(1.0, float64(de.tf))))
		uWeight := effW * (1.0 + repeats)
		if meanBits > 0 && de.bits > 0 {
			uWeight *= bitsGainSqrt(float64(de.bits) / meanBits)
		}
		role, _ := UnpackTermRoleExt(de.roleExt)
		unitEntries = append(unitEntries, SparseEntry{
			ID:           uint64(p.unit),
			WeightNats:   uWeight,
			SelfInfoNats: selfInfo,
			TF:           de.tf,
			DF:           df,
			IsCore:       df <= highDFThresh || (de.flags&rmFlagDocGainMask != 0),
			Flags:        de.flags,
			Role:         role,
		})
	}

	var kappa float64
	n := float64(len(posts))
	dObs := float64(len(days))
	if n > 1 && dObs >= 1 && st.totalDaysM > 0 {
		eNull := st.totalDaysM * (1.0 - math.Exp(-n/st.totalDaysM))
		denom := math.Max(1.0, eNull-1.0)
		kappa = ((n - 1.0) / (n + 2.0)) * math.Max(0, 1.0-(dObs-1.0)/denom)
	}

	var partnerEntries []SparseEntry
	for i, pk := range st.phraseKeys {
		var partner uint64
		if pk.a == termID {
			partner = pk.b
		} else if pk.b == termID {
			partner = pk.a
		} else {
			continue
		}
		pPosts := st.phraseUnits[st.phraseRowPtr[i]:st.phraseRowPtr[i+1]]
		if len(pPosts) == 0 {
			continue
		}
		wb, okB := st.effWeightOf(partner)
		if !okB || wb <= 0 {
			wb, _ = idx.TermMass(partner)
		}
		if wb <= 0 {
			continue
		}
		joint := math.Log1p((float64(idx.N) - float64(len(pPosts)) + 0.5) / (float64(len(pPosts)) + 0.5))
		bindMass := math.Min(effW, wb) * joint / (1.0 + joint)
		pDF := idx.TermDF(partner)
		partnerEntries = append(partnerEntries, SparseEntry{
			ID:           partner,
			WeightNats:   bindMass,
			SelfInfoNats: joint,
			TF:           int32(len(pPosts)),
			DF:           pDF,
			IsCore:       pDF <= highDFThresh,
		})
	}

	return TermEnergyProfile{
		TermID:              termID,
		GroupID:             groupID,
		DF:                  df,
		SelfInfoNats:        selfInfo,
		EffectiveWeightNats: effW,
		CalendarCohesion:    kappa,
		ProperRatio:         st.properRatioOf(termID),
		PartnerVector:       BuildSparseVector(partnerEntries),
		UnitVector:          BuildSparseVector(unitEntries),
	}, true
}

// PhraseEnergyProfile 表达单个「共现词组 / 短语（PhraseDyad）」在当前语料参照系 M_N 下的动态能量画像：
// 严格遵循超加成束缚律 m_phrase(a,b) = min(w_eff(a), w_eff(b)) * J(a,b)/(1+J(a,b))（非两词向量线性相加）。
type PhraseEnergyProfile struct {
	TermA              uint64       // 规范序左词项标识（TermA <= TermB）
	TermB              uint64       // 规范序右词项标识
	PhraseDF           int          // 同句窗口（Δslot <= 4, d >= 1）共现文档数 df_phrase(a,b)
	JointSurprisalNats float64      // 二元共现碰撞惊奇度 J(a,b)（Nats）
	BindingEnergyNats  float64      // 超加成短语束缚能量 m_phrase(a,b)（Nats）
	SuperAdditiveRatio float64      // 束缚饱和比 J(a,b) / (1 + J(a,b)) in (0, 1)
	FeatureVector      SparseVector // 词族支撑稀疏向量（含超加成束缚增益后的二元支撑）
	UnitVector         SparseVector // 命中单元稀疏向量（按各单元内最小物理词槽距离核 exp(-(d-1)/e) 调制）
}

// PhraseEnergyVector 实时计算二元短语 (termA, termB) 在当前语料参照系 M_N 下的动态能量稀疏向量画像。
func (e *Engine) PhraseEnergyVector(termA, termB uint64) (PhraseEnergyProfile, bool) {
	if e == nil || e.Index == nil || e.Index.N == 0 || termA == 0 || termB == 0 || termA == termB {
		return PhraseEnergyProfile{}, false
	}
	if termA > termB {
		termA, termB = termB, termA
	}
	idx := e.Index
	st := e.ensureClusterState()
	pair := rmDyad{a: termA, b: termB}
	posts := st.phrasePostings(pair)
	if len(posts) == 0 {
		return PhraseEnergyProfile{}, false
	}
	wa, okA := st.effWeightOf(termA)
	if !okA || wa <= 0 {
		wa, _ = idx.TermMass(termA)
	}
	wb, okB := st.effWeightOf(termB)
	if !okB || wb <= 0 {
		wb, _ = idx.TermMass(termB)
	}
	if wa <= 0 || wb <= 0 {
		return PhraseEnergyProfile{}, false
	}
	dfPhrase := len(posts)
	joint := math.Log1p((float64(idx.N) - float64(dfPhrase) + 0.5) / (float64(dfPhrase) + 0.5))
	ratio := joint / (1.0 + joint)
	bindMass := math.Min(wa, wb) * ratio
	highDFThresh := idx.HighDFThreshold()

	ga, okGA := st.parentGroupOf(termA)
	if !okGA || ga == 0 {
		ga = termA
	}
	gb, okGB := st.parentGroupOf(termB)
	if !okGB || gb == 0 {
		gb = termB
	}
	ia, _ := idx.TermMass(termA)
	ib, _ := idx.TermMass(termB)
	featEntries := []SparseEntry{
		{
			ID:           ga,
			WeightNats:   wa + 0.5*bindMass,
			SelfInfoNats: ia,
			TF:           1,
			DF:           idx.TermDF(termA),
			IsCore:       idx.TermDF(termA) <= highDFThresh,
		},
		{
			ID:           gb,
			WeightNats:   wb + 0.5*bindMass,
			SelfInfoNats: ib,
			TF:           1,
			DF:           idx.TermDF(termB),
			IsCore:       idx.TermDF(termB) <= highDFThresh,
		},
	}

	wide := st.hasWidePos()
	unitEntries := make([]SparseEntry, 0, len(posts))
	for _, u := range posts {
		if int(u) < 0 || int(u) >= idx.N {
			continue
		}
		entries := st.docEntriesOf(u)
		idxA, idxB := int32(-1), int32(-1)
		for ei := range entries {
			if entries[ei].id == termA {
				idxA = int32(ei)
			} else if entries[ei].id == termB {
				idxB = int32(ei)
			}
		}
		kern := 1.0
		if idxA >= 0 && idxB >= 0 {
			var dist int
			if wide {
				dist = closestExactSlot(st.entryPositionsExactOf(entries[idxA]), st.entryPositionsExactOf(entries[idxB]))
			} else {
				dist = closestPackedSlot(st.entryPositionsOf(entries[idxA]), st.entryPositionsOf(entries[idxB]))
			}
			if dist > 1 {
				kern = math.Exp(-float64(dist-1) * invE)
			}
		}
		unitEntries = append(unitEntries, SparseEntry{
			ID:           uint64(u),
			WeightNats:   bindMass * kern,
			SelfInfoNats: joint,
			TF:           1,
			DF:           dfPhrase,
			IsCore:       dfPhrase <= highDFThresh,
		})
	}

	return PhraseEnergyProfile{
		TermA:              termA,
		TermB:              termB,
		PhraseDF:           dfPhrase,
		JointSurprisalNats: joint,
		BindingEnergyNats:  bindMass,
		SuperAdditiveRatio: ratio,
		FeatureVector:      BuildSparseVector(featEntries),
		UnitVector:         BuildSparseVector(unitEntries),
	}, true
}

// UnitEnergyProfile 表达单个「语料单元（Unit / Doc）」在当前语料参照系 M_N 下的双阶动态能量稀疏向量画像：
//   - GroupVector：词族层一阶动态能量稀疏向量 φ(A_u) ⊙ w(M_N)（组内幂等 max 包络，零伴随双计）；
//   - PhraseVector：单元内实际触发的同句二元短语（PhraseDyad）超加成束缚能量稀疏向量。
type UnitEnergyProfile struct {
	UnitID       string       // 单元唯一标识
	Unit         int32        // 引擎内部单元下标
	OccurredAt   int64        // 发生/参考物理时间戳
	EpisodeID    int32        // 所属内生情节组件标识（未定时为 -1）
	GroupVector  SparseVector // 词族级一阶动态能量稀疏向量（ID = 词族 Group）
	PhraseVector SparseVector // 句内二元短语超加成能量稀疏向量（ID = 短语折叠键）
}

func foldDyadKey64(a, b uint64) uint64 {
	if a > b {
		a, b = b, a
	}
	// Bijective mixing of ordered 64-bit pair into a deterministic 64-bit coordinate key.
	x := a ^ (b + 0x9e3779b97f4a7c15 + (a << 6) + (a >> 2))
	if x == 0 {
		return 1
	}
	return x
}

// UnitEnergyVector 实时计算指定单元 unitID 在当前语料参照系 M_N 下的双阶动态能量稀疏向量画像。
func (e *Engine) UnitEnergyVector(unitID string) (UnitEnergyProfile, bool) {
	if e == nil || e.Index == nil || e.Index.N == 0 {
		return UnitEnergyProfile{}, false
	}
	idx := e.Index
	u, ok := idx.LookupUnit(unitID)
	if !ok {
		return UnitEnergyProfile{}, false
	}
	return e.unitEnergyVectorByIndex(u), true
}

func (e *Engine) unitEnergyVectorByIndex(u int32) UnitEnergyProfile {
	idx := e.Index
	st := e.ensureClusterState()
	sc := e.ensureQueryScratch(idx.N, st.clusterN)
	highDFThresh := idx.HighDFThreshold()
	prof := st.buildUnitGroupProfileWithPos(idx, sc, u, highDFThresh, false)
	groupVec := prof.toSparseVector()
	for i := range groupVec.Entries {
		g := groupVec.Entries[i].ID
		groupVec.Entries[i].DF = idx.TermDF(g)
		if m, okM := idx.TermMass(g); okM && m > 0 {
			groupVec.Entries[i].SelfInfoNats = m
		}
	}

	entries := st.docEntriesOf(u)
	var dyads []rmDyad
	if st.hasWidePos() {
		dyads = st.extractDocDyadsExactInto(entries, nil)
	} else {
		dyads, _ = st.extractDocDyadsInto(entries, nil, nil)
	}
	var phraseEntries []SparseEntry
	for _, pair := range dyads {
		posts := st.phrasePostings(pair)
		if len(posts) == 0 {
			continue
		}
		wa, okA := st.effWeightOf(pair.a)
		if !okA || wa <= 0 {
			wa, _ = idx.TermMass(pair.a)
		}
		wb, okB := st.effWeightOf(pair.b)
		if !okB || wb <= 0 {
			wb, _ = idx.TermMass(pair.b)
		}
		if wa <= 0 || wb <= 0 {
			continue
		}
		joint := math.Log1p((float64(idx.N) - float64(len(posts)) + 0.5) / (float64(len(posts)) + 0.5))
		bindMass := math.Min(wa, wb) * joint / (1.0 + joint)
		phraseEntries = append(phraseEntries, SparseEntry{
			ID:           foldDyadKey64(pair.a, pair.b),
			WeightNats:   bindMass,
			SelfInfoNats: joint,
			TF:           1,
			DF:           len(posts),
			IsCore:       len(posts) <= highDFThresh,
		})
	}

	var ts int64
	if int(u) < len(st.refTimes) {
		ts = st.refTimes[u]
	}
	ep := int32(-1)
	if int(u) < len(st.episodeOf) {
		ep = st.episodeOf[u]
	}
	return UnitEnergyProfile{
		UnitID:       idx.UnitIDs[u],
		Unit:         u,
		OccurredAt:   ts,
		EpisodeID:    ep,
		GroupVector:  groupVec,
		PhraseVector: BuildSparseVector(phraseEntries),
	}
}

// ClusterEnergyProfile 表达单个「认知簇（EntityCluster 或 SpacetimeCluster）」的动态能量稀疏向量画像：
//   - EnvelopeVector：跨成员幂等上确界包络向量 ∨_{u∈C} u（满足 a ∨ a = a，100% 免疫同事件连拍克隆膨胀，
//     每个特征坐标的 TF 记录簇内独立互证该特征的成员单元数）；
//   - IntersectionVector：跨成员共同核心交集向量 ∧_{u∈C} u；
//   - ContractionNats：子簇外延收缩惊奇度 ln((min df + 1) / (df_cluster + 1))（Nats）；
//   - EchoRedundancyRatio：簇内连拍回声冗余率 1 - ||∨ u||_1 / ∑ ||u||_1 in [0, 1)。
type ClusterEnergyProfile struct {
	EpisodeID           int32        // 内生情节组件标识（非情节簇为 -1）
	MemberUnits         []int32      // 簇成员内部单元下标（升序）
	MemberUnitIDs       []string     // 簇成员单元 ID
	ClusterDF           int          // 子簇外延文档数 df_cluster
	ContractionNats     float64      // 包含格交集收缩惊奇度 ln((min df + 1)/(df_cluster + 1))（Nats）
	LinearSumMassNats   float64      // 成员 L1 能量简单算术和 ∑_{u∈C} E(u)（仅作冗余度分母对照，禁作排序判官）
	EchoRedundancyRatio float64      // 簇内同源回声冗余率 1 - Envelope.TotalMassNats / LinearSumMassNats in [0, 1)
	EnvelopeVector      SparseVector // 幂等上确界包络稀疏向量 ∨_{u∈C} u（簇的权威动态能量向量）
	IntersectionVector  SparseVector // 成员交集下确界稀疏向量 ∧_{u∈C} u
}

// ClusterEnergyVector 计算给定单元集合 unitIDs 构成的认知簇动态能量稀疏向量画像。
func (e *Engine) ClusterEnergyVector(unitIDs []string) (ClusterEnergyProfile, bool) {
	if e == nil || e.Index == nil || e.Index.N == 0 || len(unitIDs) == 0 {
		return ClusterEnergyProfile{}, false
	}
	idx := e.Index
	seen := make(map[int32]struct{}, len(unitIDs))
	units := make([]int32, 0, len(unitIDs))
	for _, id := range unitIDs {
		if u, ok := idx.LookupUnit(id); ok {
			if _, dup := seen[u]; !dup {
				seen[u] = struct{}{}
				units = append(units, u)
			}
		}
	}
	if len(units) == 0 {
		return ClusterEnergyProfile{}, false
	}
	sortInt32Asc(units)
	return e.clusterEnergyFromUnits(units, -1), true
}

// EpisodeEnergyVector 计算指定内生情节组件 episodeID（SpacetimeCluster）的动态能量稀疏向量画像。
func (e *Engine) EpisodeEnergyVector(episodeID int32) (ClusterEnergyProfile, bool) {
	if e == nil || e.Index == nil || e.Index.N == 0 || episodeID < 0 {
		return ClusterEnergyProfile{}, false
	}
	st := e.ensureClusterState()
	if st == nil || int(episodeID) >= st.episodeCount {
		return ClusterEnergyProfile{}, false
	}
	var units []int32
	for u, ep := range st.episodeOf {
		if ep == episodeID {
			units = append(units, int32(u))
		}
	}
	if len(units) == 0 {
		return ClusterEnergyProfile{}, false
	}
	return e.clusterEnergyFromUnits(units, episodeID), true
}

// EntityClusterEnergyVector 计算由任意特征组合 groups 构成的实体簇（EntityCluster）动态能量稀疏向量画像：
// 通过倒排链交集求取精确子簇外延 df_cluster(M)、包含格收缩惊奇度 ln((min df + 1)/(df_cluster + 1))
// 以及交集命中单元的幂等上确界包络向量。
func (e *Engine) EntityClusterEnergyVector(groups []uint64) (ClusterEnergyProfile, bool) {
	if e == nil || e.Index == nil || e.Index.N == 0 || len(groups) == 0 {
		return ClusterEnergyProfile{}, false
	}
	idx := e.Index
	st := e.ensureClusterState()
	dedup := make([]uint64, 0, len(groups))
	seenG := make(map[uint64]struct{}, len(groups))
	for _, g := range groups {
		if g == 0 {
			continue
		}
		if _, ok := seenG[g]; !ok {
			seenG[g] = struct{}{}
			dedup = append(dedup, g)
		}
	}
	if len(dedup) == 0 {
		return ClusterEnergyProfile{}, false
	}
	sortUint64Asc(dedup)

	minDF := idx.N + 1
	var intersect []int32
	for gi, g := range dedup {
		uSet := make(map[int32]struct{})
		for _, p := range st.termPostings(g) {
			uSet[p.unit] = struct{}{}
		}
		for u := 0; u < idx.N; u++ {
			for _, de := range st.docEntriesOf(int32(u)) {
				if de.group == g {
					uSet[int32(u)] = struct{}{}
				}
			}
		}
		dfG := len(uSet)
		if dfG == 0 {
			return ClusterEnergyProfile{}, false
		}
		if dfG < minDF {
			minDF = dfG
		}
		curUnits := make([]int32, 0, dfG)
		for u := range uSet {
			curUnits = append(curUnits, u)
		}
		sortInt32Asc(curUnits)
		if gi == 0 {
			intersect = curUnits
		} else {
			w := 0
			i, j := 0, 0
			for i < len(intersect) && j < len(curUnits) {
				if intersect[i] < curUnits[j] {
					i++
				} else if intersect[i] > curUnits[j] {
					j++
				} else {
					intersect[w] = intersect[i]
					w++
					i++
					j++
				}
			}
			intersect = intersect[:w]
			if len(intersect) == 0 {
				return ClusterEnergyProfile{}, false
			}
		}
	}

	prof := e.clusterEnergyFromUnits(intersect, -1)
	prof.ClusterDF = len(intersect)
	if len(dedup) >= 2 && prof.ClusterDF > 0 && prof.ClusterDF < minDF {
		prof.ContractionNats = math.Log(float64(minDF+1) / float64(prof.ClusterDF+1))
	}
	return prof, true
}

func (e *Engine) clusterEnergyFromUnits(units []int32, epID int32) ClusterEnergyProfile {
	idx := e.Index
	st := e.ensureClusterState()
	sc := e.ensureQueryScratch(idx.N, st.clusterN)
	highDFThresh := idx.HighDFThreshold()

	unitIDs := make([]string, len(units))
	var envVec, meetVec SparseVector
	var linearSum float64
	for i, u := range units {
		unitIDs[i] = idx.UnitIDs[u]
		up := st.buildUnitGroupProfileWithPos(idx, sc, u, highDFThresh, false)
		uv := up.toSparseVector()
		linearSum += uv.TotalMassNats
		if i == 0 {
			envVec = uv
			meetVec = uv
		} else {
			envVec = SupremumJoin(envVec, uv)
			meetVec = IntersectionMeet(meetVec, uv)
		}
	}
	for i := range envVec.Entries {
		g := envVec.Entries[i].ID
		envVec.Entries[i].DF = idx.TermDF(g)
		if m, okM := idx.TermMass(g); okM && m > 0 {
			envVec.Entries[i].SelfInfoNats = m
		}
	}
	var echoRed float64
	if linearSum > envVec.TotalMassNats && linearSum > 0 {
		echoRed = math.Max(0, math.Min(1.0, 1.0-envVec.TotalMassNats/linearSum))
	}
	return ClusterEnergyProfile{
		EpisodeID:           epID,
		MemberUnits:         units,
		MemberUnitIDs:       unitIDs,
		ClusterDF:           len(units),
		LinearSumMassNats:   linearSum,
		EchoRedundancyRatio: echoRed,
		EnvelopeVector:      envVec,
		IntersectionVector:  meetVec,
	}
}
