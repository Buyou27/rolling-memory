// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"os"
	"strings"
	"sync/atomic"
	"unicode"
)

// FollowupTightMode 收紧「追问续答」附加条帽（**缺省关** · 单变量臂/实测器材）。
//
// 动机：交付体量的头号来源不是原文切片（仅占证据带 19%，且多数只覆盖源轮 5–11%），
// 而是挂在证据行上的**跨轮追问续答**（占证据带 53% / 单题交付 ~31%）。本档只收紧条帽、不动任何判据结构——
// 关 = 逐字现役（12/10/28/34），开 = 4/3/6/8。
// 卷面依据：
// 100 题同二进制单变量 A/B（#8626 关 93 vs #8627 开 92 · p=1.0 不可判；`gold_lex_hit` 90=90 NULL；题均字符 46,465→41,900 = −9.8%）；
// **200 题 A/B（实证对比）。即「效率轴正向但要价 4 题」⇒ 缺省不启用。
// 开法：进程环境 `RM_RECALL_FOLLOWUP_TIGHT=1`（观测量撤/回退档用）。
var FollowupTightMode atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RM_RECALL_FOLLOWUP_TIGHT"))) {
	case "1", "true", "on", "yes":
		FollowupTightMode.Store(true)
	}
}

// FollowupNoveltyMode 「追问续答只挂新事实」档（**缺省关** · 单变量臂/实测器材）。
//
// 动机：纯条帽紧档 = 交付体量 −9.7% 换 −4 题（实证对比）。本档在既有逐字覆盖去重之上，要求每段至少含一个「未被本带已交付材料覆盖且 tokenz-r 先验 ≥ 20.0b」的 BPE 子词。
// 卷面与回归复核：200 题 A/B（#8635 关 181 vs #8636 开 179 · Δ −2，且 `gold_lex_hit` 掉 2 题；同时历史回归测试
// `TestDeliveryHygieneMediaGateAndFollowupDedup` 证明由常见词构成的问答桥接句如 "What else helps you?" 会被 <20b 阈值误杀）
// ⇒ 按 §4.3 回归不变量保持**缺省关**（出货 100% 守护问答桥接追问段），仅以 `RM_RECALL_FOLLOWUP_NOVELTY=1` 供实验调用。
var FollowupNoveltyMode atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RM_RECALL_FOLLOWUP_NOVELTY"))) {
	case "1", "true", "on", "yes":
		FollowupNoveltyMode.Store(true)
	}
}

// followupNovelMinBits 段保留阈值（20.0b · 实测标定：寒暄/常见词峰值 ≤16.5b（wow 16.5 / oh 14.3 / nice 13.4），
// 实体/金额/编码 ≥22b（$89 22.0 / Breville 26.3 / bes870xl 42.9）——断层 ≥1.3×，取 20.0b 居中）。
// 判据 = 段内须含至少一个「未被本带已交付材料覆盖且 tokenz-r 先验 ≥ 20.0b」的 BPE 子词（新语素 = 新事实的机械形态；
// tokenz-r 先验 = 词表秩 log2(id+2) 分解和，罕见单件/多件罕见词自然走高）。**峰值判据**：常见词堆叠不构成新事实。
const followupNovelMinBits = 20.0

// CompactSupplementImageElaboration trims verbose parenthesized or trailing OCR/visual elaborations
// (e.g. "【图片】a photo of ... (一张米黄色背景的...)" or "【图片】a photo of ... 画面中央是...")
// inside raw-supplement (src2) or followup clips when none of the query cues appear inside the elaboration,
// keeping the concise image caption header while saving hundreds of irrelevant visual description tokens.
func CompactSupplementImageElaboration(text string, queryCues []string) string {
	if len(queryCues) == 0 {
		return text
	}
	idx := strings.Index(text, "【图片】")
	if idx < 0 {
		return text
	}
	// Process each "【图片】" occurrence from right to left so indices remain valid.
	out := text
	searchFrom := len(out)
	for {
		pos := strings.LastIndex(out[:searchFrom], "【图片】")
		if pos < 0 {
			break
		}
		searchFrom = pos
		tail := out[pos:]
		// Isolate up to the next dialogue turn separator " → " if inside a multi-turn followup.
		endSeg := len(tail)
		if arrow := strings.Index(tail, " → "); arrow > 0 {
			endSeg = arrow
		}
		seg := tail[:endSeg]
		cutAt := -1
		if openP := strings.Index(seg, " ("); openP > 0 {
			cutAt = openP
		} else {
			// Detect transition from Latin caption header ("【图片】a photo of ...") to a long CJK visual elaboration paragraph.
			afterTag := pos + len("【图片】")
			seenLatin := false
			for off, r := range out[afterTag : pos+endSeg] {
				if unicode.Is(unicode.Latin, r) {
					seenLatin = true
				} else if seenLatin && IsContinuousSyllabicOrIdeographic(r) {
					cutAt = len("【图片】") + off
					break
				}
			}
		}
		if cutAt <= 0 {
			continue
		}
		elab := seg[cutAt:]
		if CountTokens(elab) <= TokenTier32 {
			continue
		}
		capLow := strings.ToLower(seg[:cutAt])
		elabLow := strings.ToLower(elab)
		matchedCue := false
		for _, cue := range queryCues {
			cLow := strings.ToLower(strings.TrimSpace(cue))
			if CountTokens(cLow) >= 1 && StringShannonBits(cLow) >= 4.5 && strings.Contains(elabLow, cLow) && !strings.Contains(capLow, cLow) {
				matchedCue = true
				break
			}
		}
		if matchedCue {
			continue
		}
		suffix := ""
		if spkIdx := strings.Index(elab, "（讲述日"); spkIdx >= 0 {
			suffix = elab[spkIdx:]
		}
		out = strings.TrimSpace(out[:pos+cutAt]) + suffix + tail[endSeg:]
	}
	return out
}

// StmtCoversSnippet reports whether the rendered assertion statement stmt already covers 100%
// of the non-trivial tokens (StringShannonBits >= 0.50*AtomicConceptEntropyBits, by exact or stem match) in the verbatim anchor snippet snip.
// Only local single-row coverage may elide a redundant snippet; cross-row global bag-of-words
// cancellation is forbidden so intra-sentence modifiers and locative complements are never dropped.
func StmtCoversSnippet(stmt, snip string) bool {
	stmtWords := make(map[string]bool, 16)
	var b strings.Builder
	flushStmt := func() {
		if b.Len() > 0 {
			w := b.String()
			b.Reset()
			stmtWords[w] = true
			if st := StemToken(w); st != "" {
				stmtWords[st] = true
			}
		}
	}
	for _, r := range strings.ToLower(stmt) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		} else {
			flushStmt()
		}
	}
	flushStmt()

	covered := true
	flushSnip := func() {
		if b.Len() == 0 {
			return
		}
		w := b.String()
		b.Reset()
		if StringShannonBits(w) < 0.50*AtomicConceptEntropyBits {
			return
		}
		if !stmtWords[w] && !stmtWords[StemToken(w)] {
			covered = false
		}
	}
	for _, r := range strings.ToLower(snip) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		} else {
			flushSnip()
			if !covered {
				return false
			}
		}
	}
	flushSnip()
	return covered
}
