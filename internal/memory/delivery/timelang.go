// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"regexp"
)

// ParseTimeWindows 机械解析查询中的时间锚（EN 月名/日期 + 中文 年/月/日；零 LLM；UTC）。
// 输出三形态（TimeWindow）：绝对窗（带年份）/ 无年日月锚 / 无年月锚。
// 未命中任何日期表达 ⇒ 返回空（调用侧 = 时间腿不出）；**相对表达（"last week"）不在本函数解析**
// ——相对锚需参考时刻，走 ParseTimeWindowsAt（见 timelang_rel.go 头注）。
//
// 口径（与时间腿排序联动）：
//   - 日锚窗 = [锚日 −7d, 锚日 +3d]（前扩承接 "the week before X" 式提问，后扩承接提及漂移）；
//   - 月锚窗 = [月首 −3d, 次月首 +3d]；年锚窗 = 整年；
//   - "between A and B"（≥2 日锚、≥1 个带年）⇒ 合并为一个 hull 窗（缺年锚用带年锚补全）；
//   - 锚 = 解析出的日期（腿内排序用）；裸 may（英文情态动词）不入裸月名规则。

// monthNumbers 月名 → 月号（全名与三字母缩写）
var monthNumbers = map[string]int{
	"january": 1, "jan": 1,
	"february": 2, "feb": 2,
	"march": 3, "mar": 3,
	"april": 4, "apr": 4,
	"may":  5,
	"june": 6, "jun": 6,
	"july": 7, "jul": 7,
	"august": 8, "aug": 8,
	"september": 9, "sept": 9, "sep": 9,
	"october": 10, "oct": 10,
	"november": 11, "nov": 11,
	"december": 12, "dec": 12,
}

const (
	// 数字语境月名（含 may：须后随数字才成窗）
	monthAltAll = "(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sept|sep|oct|nov|dec)"
	// 裸月名（不含 may——英文情态动词高频误命中）
	monthAltBare = "(january|february|march|april|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sept|sep|oct|nov|dec)"
)

var (
	reMDY   = regexp.MustCompile(`\b` + monthAltAll + `\s+(\d{1,2})(?:st|nd|rd|th)?(?:\s*,\s*|\s+)(\d{4})\b`)
	reDMY   = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s+` + monthAltAll + `,?\s+(\d{4})\b`)
	reDMYof = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s+of\s+` + monthAltAll + `,?\s+(\d{4})\b`) // "12th of May 2023"
	// 裸月窗前置守卫："last|this|next + 月名" 由相对月名规则出更具体的年+月窗 ⇒ 裸月窗（跨年泛匹配）不再并存
	rePreMonthNamed = regexp.MustCompile(`\b(last|this|next)\s+$`)
	// CN 年前缀守卫："去年3月" 由相对复合规则消费（防 {MonthOfYear:3} 泛窗并存）
	rePreCNYear = regexp.MustCompile(`(去年|今年|明年|前年|后年|大前年|大后年)$`)
	reMY        = regexp.MustCompile(`\b` + monthAltAll + `,?\s+(\d{4})\b`)
	reMD        = regexp.MustCompile(`\b` + monthAltAll + `\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	reM         = regexp.MustCompile(`\b` + monthAltBare + `\b`)
	reY         = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	reCNMDY     = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`)
	reCNMY      = regexp.MustCompile(`(\d{4})年(\d{1,2})月`)
	reCNY       = regexp.MustCompile(`(\d{4})年`)
	reCNMD      = regexp.MustCompile(`(\d{1,2})月(\d{1,2})日`)
	reCNM       = regexp.MustCompile(`(\d{1,2})月`)

	// ⓪ ISO 日期[+时刻]（code 域主形；时刻可缺——`2026-05-25` / `2026-05-25 14:30` / `2026-05-25T14:30:05`）。
	reISO = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})(?:[ t](\d{1,2}):(\d{2})(?::(\d{2}))?)?`)

	// 时刻后缀（紧随日期锚后 ≤12 字符窗内）。
	reClockHM    = regexp.MustCompile(`^[\s,]*?(?:at\s+|@)?(\d{1,2}):(\d{2})(?::(\d{2}))?\s*(am|pm)?`)
	reClockHopm  = regexp.MustCompile(`^[\s,]*?(?:at\s+|@)?(\d{1,2})\s*(am|pm)`)
	reClockCNHM  = regexp.MustCompile(`^[\s,]*?(\d{1,2})[:：](\d{2})(?::(\d{2}))?`)
	reClockCNWrd = regexp.MustCompile(`^[\s,]*?(凌晨|早上|上午|中午|下午|晚上|傍晚)?(\d{1,2})[点时](?:(\d{1,2})分?(?:(\d{1,2})秒)?|(半))?`)

	reBetween = regexp.MustCompile(`\bbetween\b`)
	// "in may"（裸 may 被排除在月名外，但 "in may" 是明确月份语境）
	reInMay = regexp.MustCompile(`\bin may\b`)
)

const (
	twDayPadBefore = int64(7 * 86400) // 日锚前扩
	twDayPadAfter  = int64(3*86400) + 86399
	twMonthPad     = int64(3 * 86400)
)

// clockAfterWindow 时刻后缀的紧邻窗（**字节**；覆盖 CN 最长钟形「下午3点30分5秒」=21B + 分隔；锚定 = 紧邻 + 可选 at/@
// ⇒ 跨句时钟不绑，防误绑不变量由锚定而非窗宽保证）。
const clockAfterWindow = 24
