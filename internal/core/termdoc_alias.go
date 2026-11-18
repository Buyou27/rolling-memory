// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "github.com/rocareer/rolling-memory/sdk/go/public/termdoc"

// 词项/文档叶类型与查询窄视图唯一真源在 sdk/go/public/termdoc（stdlib-only 叶包）。
// 本文件集中持有 core 侧别名，使 index.go / pipeline.go 等 portable core 文件保持
// math/sort 白名单零外部 import，同时 core 对外 API 与全部调用点不变。
type (
	TermRole = termdoc.TermRole
	Term     = termdoc.Term
	// Atom 是 Term 的向后兼容类型别名（去伪物理化标准术语统一为 Term）。
	Atom           = Term
	TermPosition   = termdoc.TermPosition
	DocMeta        = termdoc.DocMeta
	Doc            = termdoc.Doc
	QueryIndexView = termdoc.QueryIndexView
	QueryExtractor = termdoc.QueryExtractor
)

const (
	RolePrimary         = termdoc.RolePrimary
	RoleMorphStem       = termdoc.RoleMorphStem
	RoleSubwordPiece    = termdoc.RoleSubwordPiece
	RoleClosedBeacon    = termdoc.RoleClosedBeacon
	RoleBiwordHomomorph = termdoc.RoleBiwordHomomorph

	ExtFlagSeenPrimary     = termdoc.ExtFlagSeenPrimary
	ExtFlagSeenMorphOrClos = termdoc.ExtFlagSeenMorphOrClos
	ExtFlagSeenSubword     = termdoc.ExtFlagSeenSubword
)

// PackTermRoleExt packs TermRole and low ExtFlags bits for storage adapters.
func PackTermRoleExt(role TermRole, ext uint16) uint8 {
	return termdoc.PackTermRoleExt(role, ext)
}

// UnpackTermRoleExt unpacks TermRole and low ExtFlags bits for storage adapters.
func UnpackTermRoleExt(re uint8) (TermRole, uint16) {
	return termdoc.UnpackTermRoleExt(re)
}

// UnpackInferredOrdinalDay unpacks the (Sent, Slot) inferred-day carrier.
func UnpackInferredOrdinalDay(sent, slot uint16) int64 {
	return termdoc.UnpackInferredOrdinalDay(sent, slot)
}
