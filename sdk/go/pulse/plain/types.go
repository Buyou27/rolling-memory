// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package plain 维护明文词项编码器、8-bit 结构标志位、词元装饰、词项抽取器与空格粘合器。
package plain

import "github.com/rocareer/rolling-memory/internal/core"

type (
	TermRole     = core.TermRole
	Term         = core.Term
	TermPosition = core.TermPosition
	DocMeta      = core.DocMeta
	Doc          = core.Doc
	Index        = core.Index
)

const (
	RolePrimary            = core.RolePrimary
	RoleMorphStem          = core.RoleMorphStem
	RoleSubwordPiece       = core.RoleSubwordPiece
	RoleClosedBeacon       = core.RoleClosedBeacon
	RoleBiwordHomomorph    = core.RoleBiwordHomomorph
	ExtFlagSeenPrimary     = core.ExtFlagSeenPrimary
	ExtFlagSeenMorphOrClos = core.ExtFlagSeenMorphOrClos
	ExtFlagSeenSubword     = core.ExtFlagSeenSubword
)
