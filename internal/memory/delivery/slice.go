// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"github.com/rocareer/rolling-memory/internal/text"
)

// 本文件 = 记忆交付共用的 Token 阶梯、词元计数转发与逐字子串切片算子（0-LLM · 零外部依赖 · 100% 逐字子串保真）：
//   1. Token 阶梯：全链路统一使用 tokenz-r 计数（4 / 8 / 16 / 32 / 64 / 128 / 256 / 512 / 1024 / 2048 / 4096 / 8192）；
//   2. SliceRunesBounded：按 rune 坐标提取严格子串，用于结构化断言的 (anchor_start, anchor_end) 字节区间绑回。

const (
	// 标准 2 的幂次 Token 阶梯（全链路统一使用 tokenz-r 计数，严格使用 4 / 8 / 16 / 32 / 64 / 128 / 256 / 512 / 1024 / 2048 / 4096 / 8192 阶梯）。
	TokenTier4    = 4
	TokenTier8    = 8
	TokenTier16   = 16
	TokenTier32   = 32
	TokenTier64   = 64
	TokenTier128  = 128
	TokenTier256  = 256
	TokenTier512  = 512
	TokenTier1024 = 1024
	TokenTier2048 = 2048
	TokenTier4096 = 4096
	TokenTier8192 = 8192

	// SliceTargetTokens 单轮触发切片的阈值与单片目标上限（≤1024 不切，>1024 按 1024 切）。
	SliceTargetTokens = TokenTier1024
	// SliceOverlapTokens 相邻切片之间的句界重叠窗口（保持上下文与指代连贯）。
	SliceOverlapTokens = TokenTier128
	// SliceMinTailTokens 尾片最小独立阈值（尾片不足此值时不单独切片，直接并入前片）。
	SliceMinTailTokens = TokenTier256
	// BatchTokenBudget 单批次工作上下文待编码源轮正文最大 Token 预算（与单片 1024 对齐）。
	BatchTokenBudget = TokenTier1024
	// BatchMaxTurns 单批次最大片段条数。
	BatchMaxTurns = 8
	// ContextWindowTurns 紧前历史原文窗口最大轮数。
	ContextWindowTurns = 4
	// ContextSingleTurnTokens 历史上下文单轮最大 Token 数（≤1024 直接给完整原始原文）。
	ContextSingleTurnTokens = TokenTier1024
	// ContextTotalTokenBudget 紧前 1~4 轮历史原文的累计最大 Token 预算。
	ContextTotalTokenBudget = TokenTier1024
	// RecentAssertionWindow 本会话热区近期已落图 structured assertion 保留窗口。
	RecentAssertionWindow = 16
	// PreProbeCap 写前图谱预探针既有活跃断言上限。
	PreProbeCap = 64
)

// TokenCounterFunc 词元计数函数签名（唯一正本 internal/text，本包仅转发）。
type TokenCounterFunc = text.TokenCounterFunc

// CountTokens 使用 tokenz-r 词元计数器计算文本 Token 数（转发 internal/text）。
func CountTokens(s string) int { return text.CountTokens(s) }

// SliceRunesBounded 按 rune 坐标提取 [start, end) 严格子串（等价于 []rune(value)[start:end]，
// 但在扫描到 end 时立即早停且零整篇 []rune 拷贝，适用于数百 KB 源轮原文的锚点提取；
// start < 0、end <= start 或 end 超出总 rune 数时严格返回 ""）。
func SliceRunesBounded(value string, start, end int) string {
	if start < 0 || end <= start {
		return ""
	}
	runeIdx := 0
	byteStart := -1
	for byteIdx := range value {
		if runeIdx == start {
			byteStart = byteIdx
		}
		if runeIdx == end {
			if byteStart < 0 {
				return ""
			}
			return value[byteStart:byteIdx]
		}
		runeIdx++
	}
	if byteStart < 0 || runeIdx != end {
		return ""
	}
	return value[byteStart:]
}
