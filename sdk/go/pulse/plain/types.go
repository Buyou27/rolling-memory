// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package plain 维护明文词项编码器、8-bit 结构标志位、词元装饰、词项抽取器与空格粘合器。
package plain

import (
	"reflect"

	"github.com/rocareer/rolling-memory/sdk/go/public/termdoc"
)

// 词项/文档叶类型唯一真源在 sdk/go/public/termdoc；本包零 core 依赖，
// 查询倒排经窄视图 QueryIndexView 接入（core.Index 天然满足）。
type (
	TermRole       = termdoc.TermRole
	Term           = termdoc.Term
	TermPosition   = termdoc.TermPosition
	DocMeta        = termdoc.DocMeta
	Doc            = termdoc.Doc
	QueryIndexView = termdoc.QueryIndexView
)

const (
	RolePrimary            = termdoc.RolePrimary
	RoleMorphStem          = termdoc.RoleMorphStem
	RoleSubwordPiece       = termdoc.RoleSubwordPiece
	RoleClosedBeacon       = termdoc.RoleClosedBeacon
	RoleBiwordHomomorph    = termdoc.RoleBiwordHomomorph
	ExtFlagSeenPrimary     = termdoc.ExtFlagSeenPrimary
	ExtFlagSeenMorphOrClos = termdoc.ExtFlagSeenMorphOrClos
	ExtFlagSeenSubword     = termdoc.ExtFlagSeenSubword
)

// queryIndexAbsent 统一判空：nil 接口与包裹在接口中的 typed-nil 指针
// （如 (*core.Index)(nil)）都视为无倒排视图，与旧 *Index 形参的 nil 语义一致。
func queryIndexAbsent(idx QueryIndexView) bool {
	if idx == nil {
		return true
	}
	v := reflect.ValueOf(idx)
	return v.Kind() == reflect.Ptr && v.IsNil()
}
