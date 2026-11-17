// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package shaping

import (
	"strings"
	"unicode"
)

// 连跑折叠的触发线与保留数：
//   - runFoldKeep = 3：词频增益 1+min(1, ln tf) 在 tf >= 3（ln 3 > 1）时达到满分饱和，
//     保留 3 份可使折叠对词项打分通道保持严格中性；
//   - runFoldTrigger = 8：正常自然语言强调一般不超过 8 次连续重复，超过 8 次的连续同词/同行
//     视为 OCR 或生成死循环退化段予以折叠。
const (
	runFoldTrigger = 8
	runFoldKeep    = 3
)

// RunFoldRule 定义单条可插拔的退化连跑折叠降噪规则：
// 输入原始或前序规则处理后的文本，返回折叠后的文本及是否发生折叠。
// 规则只允许基于纯字符几何与局部重复周期（零词表、零语种分支、零先验阈值）。
type RunFoldRule func(text string) (folded string, changed bool)

// NoopRunFoldRule 是零操作恒等规则（关闭折叠外挂时注入）。
func NoopRunFoldRule(text string) (string, bool) {
	return text, false
}

// CombineRunFoldRules 将多条 RunFoldRule 按顺序串联为单一复合折叠规则（与
// CombineHarmonicExpanders 同构）：每条非 nil 规则依次作用于前序输出，任一规则
// 触发即标记 changed=true。
func CombineRunFoldRules(rules ...RunFoldRule) RunFoldRule {
	return func(text string) (string, bool) {
		if text == "" {
			return text, false
		}
		cur := text
		anyChanged := false
		for _, r := range rules {
			if r == nil {
				continue
			}
			if next, ok := r(cur); ok {
				cur = next
				anyChanged = true
			}
		}
		return cur, anyChanged
	}
}

// DefaultRunFoldRules 返回默认的正交退化连跑折叠规则表：
//  1. FoldIdenticalTokenRuns     : 行内完全相等 token 连跑（> 8 折至 3）；
//  2. FoldDigitSkeletonTokenRuns : 行内含数字同字符几何骨架计数连跑（周期 1 与周期 2 表格单元）；
//  3. FoldIdenticalLineRuns      : 跨行完全相等非空文本行连跑（> 8 折至 3）。
func DefaultRunFoldRules() []RunFoldRule {
	return []RunFoldRule{
		FoldIdenticalTokenRuns,
		FoldDigitSkeletonTokenRuns,
		FoldIdenticalLineRuns,
	}
}

// CollapseDegenerateTokenRuns 执行默认退化连跑折叠规则全集（单遍融合路径，与
// CombineRunFoldRules(DefaultRunFoldRules()...) 逐字节等价）。
// 仅用于入库索引投影，不改写任何原文存储。
func CollapseDegenerateTokenRuns(text string) string {
	folded, _ := DefaultRunFoldRule(text)
	return folded
}

// DefaultRunFoldRule 是默认规则全集的单遍融合实现（滿足 RunFoldRule 签名）。
func DefaultRunFoldRule(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	lines := strings.Split(text, "\n")
	changed := false
	for li, line := range lines {
		if folded, ok := foldLineTokenRuns(line); ok {
			lines[li] = folded
			changed = true
		}
	}
	kept := lines
	if folded, ok := foldIdenticalLineRuns(lines); ok {
		kept = folded
		changed = true
	}
	if !changed {
		return text, false
	}
	return strings.Join(kept, "\n"), true
}

// FoldIdenticalTokenRuns 独立规则 1：仅折叠单行内按空白切分后完全相等的同 token 连跑。
func FoldIdenticalTokenRuns(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	lines := strings.Split(text, "\n")
	changed := false
	for li, line := range lines {
		toks := strings.Fields(line)
		if len(toks) <= runFoldTrigger {
			continue
		}
		out := make([]string, 0, len(toks))
		lineMod := false
		for i := 0; i < len(toks); {
			j := i + 1
			for j < len(toks) && toks[j] == toks[i] {
				j++
			}
			if j-i > runFoldTrigger {
				out = append(out, toks[i:i+runFoldKeep]...)
				lineMod = true
			} else {
				out = append(out, toks[i:j]...)
			}
			i = j
		}
		if lineMod {
			lines[li] = strings.Join(out, " ")
			changed = true
		}
	}
	if !changed {
		return text, false
	}
	return strings.Join(lines, "\n"), true
}

// FoldDigitSkeletonTokenRuns 独立规则 2：仅折叠单行内含数字的同字符几何骨架计数连跑
// （如递增计数器 "0 1 2 ... 100" 与周期 2 表格单元格 "| H1 | H2 | ... | H208 |"）。
func FoldDigitSkeletonTokenRuns(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	lines := strings.Split(text, "\n")
	changed := false
	for li, line := range lines {
		toks := strings.Fields(line)
		if len(toks) <= runFoldTrigger {
			continue
		}
		skels := make([]string, len(toks))
		hasDigits := make([]bool, len(toks))
		for idx, tok := range toks {
			skels[idx], hasDigits[idx] = digitSkeletonOf(tok)
		}
		out := make([]string, 0, len(toks))
		lineMod := false
		for i := 0; i < len(toks); {
			if i+2*runFoldTrigger < len(toks) {
				s0 := skels[i]
				s1, hasD1 := skels[i+1], hasDigits[i+1]
				if s0 != "" && s1 != "" && hasD1 {
					k := i + 2
					for k+1 < len(toks) && skels[k] == s0 && skels[k+1] == s1 && hasDigits[k+1] {
						k += 2
					}
					if cells := (k - i) / 2; cells > runFoldTrigger {
						out = append(out, toks[i:i+2*runFoldKeep]...)
						lineMod = true
						i += 2 * cells
						continue
					}
				}
			}
			if sk, hasD := skels[i], hasDigits[i]; hasD && !strings.HasSuffix(sk, ",") {
				k := i + 1
				for k < len(toks) && hasDigits[k] && skels[k] == sk {
					k++
				}
				if k-i > runFoldTrigger {
					out = append(out, toks[i:i+runFoldKeep]...)
					lineMod = true
					i = k
					continue
				}
			}
			out = append(out, toks[i])
			i++
		}
		if lineMod {
			lines[li] = strings.Join(out, " ")
			changed = true
		}
	}
	if !changed {
		return text, false
	}
	return strings.Join(lines, "\n"), true
}

// FoldIdenticalLineRuns 独立规则 3：仅折叠跨行去首尾空白后逐字相等的连续非空行。
func FoldIdenticalLineRuns(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	lines := strings.Split(text, "\n")
	folded, ok := foldIdenticalLineRuns(lines)
	if !ok {
		return text, false
	}
	return strings.Join(folded, "\n"), true
}

// digitSkeletonOf 将 token 中的连续数字折叠为占位符 '#'，保留非数字字符几何骨架
// （例如 "H1" / "H208" -> "H#"），用于识别多模态 OCR 表格递增计数器死循环。
func digitSkeletonOf(s string) (string, bool) {
	hasDigit := false
	for _, r := range s {
		if unicode.IsDigit(r) {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return s, false
	}
	var b strings.Builder
	inDigit := false
	for _, r := range s {
		if unicode.IsDigit(r) {
			if !inDigit {
				b.WriteByte('#')
				inDigit = true
			}
		} else {
			inDigit = false
			b.WriteRune(r)
		}
	}
	return b.String(), true
}

// foldLineTokenRuns 折叠单行内按空白切分后的同 token 连跑及含数字同骨架计数连跑
// （如 "0 1 2 ... 100" 或 "| H1 | H2 | ... | H208 |"）；无触发时报告未变更以便
// 调用方保留原始行（含原始空白排版）。
func foldLineTokenRuns(line string) (string, bool) {
	toks := strings.Fields(line)
	if len(toks) <= runFoldTrigger {
		return line, false
	}
	var skels []string
	var hasDigits []bool
	ensureSkels := func() {
		if skels != nil {
			return
		}
		skels = make([]string, len(toks))
		hasDigits = make([]bool, len(toks))
		for idx, tok := range toks {
			skels[idx], hasDigits[idx] = digitSkeletonOf(tok)
		}
	}
	out := make([]string, 0, len(toks))
	changed := false
	i := 0
	for i < len(toks) {
		j := i + 1
		for j < len(toks) && toks[j] == toks[i] {
			j++
		}
		if run := j - i; run > runFoldTrigger {
			out = append(out, toks[i:i+runFoldKeep]...)
			changed = true
			i = j
			continue
		}
		ensureSkels()
		if i+2*runFoldTrigger < len(toks) {
			s0 := skels[i]
			s1, hasD1 := skels[i+1], hasDigits[i+1]
			if s0 != "" && s1 != "" && hasD1 {
				k := i + 2
				for k+1 < len(toks) && skels[k] == s0 && skels[k+1] == s1 && hasDigits[k+1] {
					k += 2
				}
				if cells := (k - i) / 2; cells > runFoldTrigger {
					out = append(out, toks[i:i+2*runFoldKeep]...)
					changed = true
					i += 2 * cells
					continue
				}
			}
		}
		if sk, hasD := skels[i], hasDigits[i]; hasD && !strings.HasSuffix(sk, ",") {
			k := i + 1
			for k < len(toks) && hasDigits[k] && skels[k] == sk {
				k++
			}
			if k-i > runFoldTrigger {
				out = append(out, toks[i:i+runFoldKeep]...)
				changed = true
				i = k
				continue
			}
		}
		out = append(out, toks[i])
		i++
	}
	if !changed {
		return line, false
	}
	return strings.Join(out, " "), true
}

// foldIdenticalLineRuns 折叠去首尾空白后逐字相等的连续行；空白行不参与
// 折叠（保持原有段落结构）。保留段取连跑的前几条原始行。
func foldIdenticalLineRuns(lines []string) ([]string, bool) {
	if len(lines) <= runFoldTrigger {
		return lines, false
	}
	out := make([]string, 0, len(lines))
	changed := false
	i := 0
	for i < len(lines) {
		key := strings.TrimSpace(lines[i])
		j := i + 1
		for key != "" && j < len(lines) && strings.TrimSpace(lines[j]) == key {
			j++
		}
		run := j - i
		if run > runFoldTrigger {
			out = append(out, lines[i:i+runFoldKeep]...)
			changed = true
		} else {
			out = append(out, lines[i:j]...)
		}
		i = j
	}
	if !changed {
		return lines, false
	}
	return out, true
}
