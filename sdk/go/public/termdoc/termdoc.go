// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package termdoc 是词项（Term）与文档（Doc）叶类型及查询倒排窄视图的唯一真源：
// 纯数据声明与纯字段推导，零第三方依赖、零检索算子。core（检索内核）与
// pulse/plain（脉冲编码器）统一以类型别名共享本包，禁止各自另立同构类型。
package termdoc

// TermRole 表示词项（Term）在所属物理词槽偏序格 L(s) 中的代数角色（8-bit 扩展域：低位 0..4 为现役闭集，高 5 bits 预留未来扩展）。
type TermRole uint8

const (
	RolePrimary         TermRole = 0 // 原词表面核（精确词面，独占 1 个物理词槽）
	RoleMorphStem       TermRole = 1 // 一阶同根词干（屈折/派生词干、撇号剥离、变音折叠、游程坍缩）
	RoleSubwordPiece    TermRole = 2 // 二阶复合子词（次级切分块、剥离数字的前导字母块等）
	RoleClosedBeacon    TermRole = 3 // 闭集世界知识装饰（@*）与日历信标（cal:*）
	RoleBiwordHomomorph TermRole = 4 // 相邻双词合成闭集同态修饰（如 "New Zealand" -> @geo:cc:nz）
)

// Term.ExtFlags（16-bit 扩展声明标志位域）低 6 位记录词项在单元内跨词槽出现的角色分布掩码，
// 高 10 位预留未来扩展（零结构体内存膨胀）：
const (
	ExtFlagSeenPrimary     uint16 = 1 << 0 // 在单元内曾作为原词表面核 (RolePrimary) 出现
	ExtFlagSeenMorphOrClos uint16 = 1 << 1 // 在单元内曾作为一阶词干 (RoleMorphStem) 或闭集信标 (RoleClosedBeacon) 出现
	ExtFlagSeenSubword     uint16 = 1 << 2 // 在单元内曾作为二阶子词 (RoleSubwordPiece / RoleBiwordHomomorph) 出现
)

// Term 表示键控词项（特征项）：身份哈希 + 声明式载体元数据 {b, f}（Bits 为载体最大
// 宽度、Flags 为 8 位 ClassFlags 并集、Role 为词槽偏序格角色、ExtFlags 为 16 位预留扩展标志位域）
// 以及所属主词族 Group 与首现句槽坐标 (Sent, Slot)。全部为平波声明字段，零先验能量，
// 不参与词项身份计算（身份唯一由 Hash 决定；利用原结构体对齐空洞内嵌 Role 与 ExtFlags，总宽恒定 32B）。
type Term struct {
	Hash     uint64
	Bits     uint16
	Flags    uint8
	Role     TermRole
	ExtFlags uint16
	Group    uint64
	Sent     uint16
	Slot     uint16
}

// PackTermRoleExt packs TermRole and low ExtFlags bits for storage adapters.
func PackTermRoleExt(role TermRole, ext uint16) uint8 {
	return (uint8(role) & 0x1F) | uint8((ext&0x07)<<5)
}

// UnpackTermRoleExt unpacks TermRole and low ExtFlags bits for storage adapters.
func UnpackTermRoleExt(re uint8) (TermRole, uint16) {
	return TermRole(re & 0x1F), uint16((re >> 5) & 0x07)
}

// PackInferredOrdinalDay 与 UnpackInferredOrdinalDay 显式承载查询侧序数日信标的推断日：
// 当 RoleClosedBeacon + ExtFlagSeenMorphOrClos 的裸 ISO 日词面在语料中无倒排命中（df=0）时，
// (Sent, Slot) 复合值承载自 Unix 纪元起的天数（写入侧限 32 位；复合值 0 表示无推断日）。
// 该通道仅被簇内同日激活路径读取，不参与任何位置语义。
func PackInferredOrdinalDay(day int64) (sent, slot uint16) {
	return uint16((uint64(day) >> 16) & 0xFFFF), uint16(uint64(day) & 0xFFFF)
}

// UnpackInferredOrdinalDay 还原 PackInferredOrdinalDay 承载的推断日。
func UnpackInferredOrdinalDay(sent, slot uint16) int64 {
	return int64(uint32(sent)<<16 | uint32(slot))
}

// InferSlotRole 推导词项相对于宿主主词 host 的槽内偏序格角色：
//  1. 双词闭集同态修饰（RoleBiwordHomomorph）与单/双词闭集信标（RoleClosedBeacon）具有代数封闭性，恒保持各自闭集角色；
//  2. 剥离尾部数字的前导字母块伴随（ExtFlagSeenSubword != 0 且 host 含数字而自身不含数字）恒为 RoleSubwordPiece（免疫跨槽 max(Bits) 冲刷）；
//  3. 其余跨槽或合成夹具情况按声明位与宿主宽度差（host.Bits >= a.Bits+3）无损推导。
func (a Term) InferSlotRole(host Term) TermRole {
	const (
		flagDigit     uint8 = 1 << 2
		flagCompanion uint8 = 1 << 4
	)
	if a.Role == RoleBiwordHomomorph && (host.Hash == 0 || host.Hash == a.Group) {
		return RoleBiwordHomomorph
	}
	if a.Role == RoleClosedBeacon {
		return RoleClosedBeacon
	}
	if a.ExtFlags&ExtFlagSeenSubword != 0 && (a.ExtFlags&(ExtFlagSeenPrimary|ExtFlagSeenMorphOrClos) == 0 || (host.Hash != 0 && host.Hash != a.Hash && host.Flags&flagDigit != 0 && a.Flags&flagDigit == 0)) {
		return RoleSubwordPiece
	}
	if host.Hash != 0 && host.Hash != a.Hash && int(host.Bits) >= int(a.Bits)+3 {
		return RoleSubwordPiece
	}
	if a.Flags&flagCompanion == 0 || ((host.Hash == 0 || host.Hash == a.Hash) && a.Role == RolePrimary && a.ExtFlags&ExtFlagSeenPrimary != 0) {
		return RolePrimary
	}
	return RoleMorphStem
}

// IsSlot2Companion 报告伴随词项相对于宿主主词 host 是否处于二阶复合子词角色（RoleSubwordPiece）。
func (a Term) IsSlot2Companion(host Term) bool {
	return a.InferSlotRole(host) == RoleSubwordPiece
}

// TermPosition locates one carrier occurrence in the caller's sentence stream.
type TermPosition struct {
	Sentence int
	Slot     int
}

// DocMeta carries the optional unit metadata mirrored from the cloud wire.
type DocMeta struct {
	OccurredAt int64
	NarratedAt int64
	IngestedAt int64
	AuthorHash uint64
	DocHash    uint64
	BlockHash  uint64
	LineStart  int
	LineEnd    int
	Layer      string
	CitedIDs   []string
}

func (m DocMeta) RefTime() int64 {
	for _, at := range [3]int64{m.OccurredAt, m.NarratedAt, m.IngestedAt} {
		if at > 0 {
			return at
		}
	}
	return 0
}

// Doc 是一条待入库记忆单元（去重词项表 + 逐词项出现次数 + 可选元信息）。
type Doc struct {
	UnitID     string
	OccurredAt int64
	NarratedAt int64
	IngestedAt int64
	AuthorHash uint64
	DocHash    uint64
	BlockHash  uint64
	LineStart  int
	LineEnd    int
	Layer      string
	CitedIDs   []string
	RawText    string
	Terms      map[uint64]Term
	Counts     map[uint64]int
	Positions  map[uint64][]TermPosition
}

func (d Doc) Meta() DocMeta {
	var cited []string
	if len(d.CitedIDs) > 0 {
		cited = append([]string(nil), d.CitedIDs...)
	}
	return DocMeta{
		OccurredAt: d.OccurredAt,
		NarratedAt: d.NarratedAt,
		IngestedAt: d.IngestedAt,
		AuthorHash: d.AuthorHash,
		DocHash:    d.DocHash,
		BlockHash:  d.BlockHash,
		LineStart:  d.LineStart,
		LineEnd:    d.LineEnd,
		Layer:      d.Layer,
		CitedIDs:   cited,
	}
}

// QueryIndexView 是查询侧词项抽取消费的只读倒排窄视图：仅暴露语料规模、词项
// df/mass、倒排遍历与单元时间坐标，不暴露索引结构本体。core.Index 天然满足本接口；
// 无 core 依赖的编码器（pulse/plain 及其分发镜像）经本接口接入任意倒排实现。
type QueryIndexView interface {
	// CorpusSize 返回语料单元总数 N；nil 接收者实现必须返回 0。
	CorpusSize() int
	// TermDF 与 TermDFOf 返回词项文档频率（无匹配返回 0）。
	TermDF(hash uint64) int
	TermDFOf(hash uint64) int
	// TermMassOf 返回词项语料质量（无匹配返回 ok=false）。
	TermMassOf(hash uint64) (float64, bool)
	// ForEachPostingUnit 遍历携带词项 hash 的全部单元序号。
	ForEachPostingUnit(hash uint64, fn func(unit int32))
	// UnitTime 返回单元的 occurredAt/narratedAt/ingestedAt 三时钟坐标；越界或缺失返回 ok=false。
	UnitTime(unit int32) (occurredAt, narratedAt, ingestedAt int64, ok bool)
}

// QueryExtractor abstracts query-side term and position extraction for Engine.Query.
type QueryExtractor interface {
	ExtractQueryWithPositions(idx QueryIndexView, text string) (map[uint64]Term, map[uint64]int, map[uint64][]TermPosition)
}
