// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 本文件 = 写侧统一时间解算引擎：
//   1. 优先解析在场 LLM（写侧编码器）整形的统一时间微语言（DSL）槽 `time_expr`（100% 语言无关、免除模型心算 Unix 秒）；
//   2. 兼容自然语言时间短语：若 `time_expr` 为自然语言短语或未填，走 `parseNaturalTimeText`（底层复用确定性解析器
//      `ParseTimeWindowsAt`，并无缝支持读侧持续时长 `for about N months`、回顾式周末 `had a ... weekend`、
//      中英文旬 `early/mid/late` / `上中下旬`、四季 `spring..winter` / `春夏秋冬` 与模糊数词 `a few / several / 几`）；
//   3. 解不出 ⇒ 返回 (0, "")（诚实留空，不猜）。

var (
	// DSL ① 绝对日期 / 时刻 / 区间 / 年月 / 年（支持秒级精度、可选小数秒及 Z/±HH:MM 时区 YYYY-MM-DD[THH:MM[:SS[.fff]][Z|±HH:MM]]）
	dslRangeRE = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:[t\s](\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d{1,9})?)?(?:z|([+-]\d{2}):?(\d{2}))?)?\s*(?:\.\.|~)\s*(\d{4})-(\d{2})-(\d{2})(?:[t\s](\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d{1,9})?)?(?:z|([+-]\d{2}):?(\d{2}))?)?$`)
	dslDateRE  = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:[t\s](\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d{1,9})?)?(?:z|([+-]\d{2}):?(\d{2}))?)?$`)
	dslMonthRE = regexp.MustCompile(`^(\d{4})-(\d{2})$`)
	dslYearRE  = regexp.MustCompile(`^(19\d{2}|20\d{2})$`)

	// DSL ② 旬 / 年阶段 / 季度 / 季节（带显式年份 · 纯 ASCII 标准 DSL 符号，不含自然语言汉字词表）
	dslTenDayRE    = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(early|mid|late)$`)
	dslYearPhaseRE = regexp.MustCompile(`^(\d{4})-(early|mid|late|end)$`)
	dslQuarterRE   = regexp.MustCompile(`^(\d{4})-q([1-4])$`)
	dslSeasonRE    = regexp.MustCompile(`^(\d{4})-(spring|summer|autumn|fall|winter)$`)
	dslRelYearRE   = regexp.MustCompile(`^(last|this|next):year-(early|mid|late|end)$`)

	// DSL ③ 相对偏移 / 锚定复合偏移：[YYYY-MM-DD[THH:MM[:SS]] ]±N(s|sec|min|h|hr|d|w|m|y|...)
	dslOffsetRE = regexp.MustCompile(`^(?:(\d{4})-(\d{2})-(\d{2})(?:[t\s](\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d{1,9})?)?(?:z|([+-]\d{2}):?(\d{2}))?)?\s+)?([+-]\d{1,6})\s*(s|sec|secs|second|seconds|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|w|m|y|days?|weeks?|months?|years?)$`)

	// DSL ④ 周末 / 星期相对算子：[YYYY-MM-DD ](last|this|next|[+-]\d+):(weekend|mon..sun)
	dslWeekOpRE = regexp.MustCompile(`^(?:(\d{4})-(\d{2})-(\d{2})\s+)?(last|this|next|[+-]?\d{1,2}):(weekends?|mon|monday|tue|tuesday|wed|wednesday|thu|thursday|fri|friday|sat|saturday|sun|sunday)$`)

	// DSL ⑤ 相对季节 / 相对季度：(last|this|next):(spring|summer|autumn|fall|winter|q1..q4)
	dslRelSeasonRE = regexp.MustCompile(`^(last|this|next):(spring|summer|autumn|fall|winter|q[1-4])$`)

	// 自然语言补充 0：显式日期+时分秒（ISO 含可选小数秒与 Z/±HH:MM 时区 / 中文年月日时分秒 / 英文月名日年+时分秒）
	isoDateTimeTextRE = regexp.MustCompile(`\b(19\d{2}|20\d{2})[-/.](\d{1,2})[-/.](\d{1,2})[t\s]+(\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d{1,9})?)?(?:\s*(am|pm))?(?:z|([+-]\d{2}):(\d{2}))?(?:\b|$)`)
	cnDateTimeTextRE  = regexp.MustCompile(`(19\d{2}|20\d{2})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*[日号]?\s*(?:(凌晨|早上|上午|中午|下午|傍晚|晚上)\s*)?(?:(\d{1,2})\s*[:：]\s*(\d{1,2})(?:\s*[:：]\s*(\d{1,2}))?|(\d{1,2})\s*[时点](?:(半)|(?:\s*(\d{1,2})\s*分(?:\s*(\d{1,2})\s*秒)?)?))`)
	enMDYClockTextRE  = regexp.MustCompile(`\b(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sept|sep|oct|nov|dec)\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(19\d{2}|20\d{2})\s+(?:at\s+)?(\d{1,2}):(\d{2})(?::(\d{2}))?(?:\s*(am|pm))?\b`)
	enDMYClockTextRE  = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sept|sep|oct|nov|dec),?\s+(19\d{2}|20\d{2})\s+(?:at\s+)?(\d{1,2}):(\d{2})(?::(\d{2}))?(?:\s*(am|pm))?\b`)

	// 自然语言补充 0b：亚日级相对时间（秒 / 分钟 / 小时）及相对日内时钟后缀
	enSubDayAgoRE   = regexp.MustCompile(`\b(a|an|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|fifteen|twenty|thirty|forty-five|a\s+couple\s+of|a\s+few|several|half\s+an|\d{1,5})\s+(seconds?|secs?|minutes?|mins?|hours?|hrs?)\s+(ago|later|from\s+now)\b`)
	enSubDayInRE    = regexp.MustCompile(`\bin\s+(a|an|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|fifteen|twenty|thirty|forty-five|a\s+couple\s+of|a\s+few|several|half\s+an|\d{1,5})\s+(seconds?|secs?|minutes?|mins?|hours?|hrs?)\b`)
	cnSubDayRelRE   = regexp.MustCompile(`(?:([0-9０-９]{1,5}|[一二两三四五六七八九十百零〇]{1,5})\s*(?:个)?(秒钟?|分钟?|小时|钟头)|半(?:个)?(小时|钟头)|(一刻钟))(?:以)?(前|后|之后|以前)`)
	relDayClockENRE = regexp.MustCompile(`\b(?:at\s+)?(\d{1,2}):(\d{2})(?::(\d{2}))?(?:\s*(am|pm))?\b`)
	relDayClockCNRE = regexp.MustCompile(`(?:(凌晨|早上|上午|中午|下午|傍晚|晚上)\s*)?(?:(\d{1,2})\s*[:：]\s*(\d{1,2})(?:\s*[:：]\s*(\d{1,2}))?|(\d{1,2})\s*[时点](?:(半)|(?:\s*(\d{1,2})\s*分(?:\s*(\d{1,2})\s*秒)?)?))`)

	// 自然语言补充 ①：相对年月回溯（零动词/零季节/零旬词表）
	cnDurationRE = regexp.MustCompile(`([0-9０-９]{1,2}|[一二两三四五六七八九十]{1,3})\s*(?:个)?(月|年)(?:以)?前`)

	// 自然语言补充 ④：中英模糊数词（"a few days ago" / "several weeks ago" / "几天前" / "好几个星期前" / "几个月前"）
	enVagueAgoRE = regexp.MustCompile(`\b(a\s+few|several)\s+(days?|weeks?|months?|years?)\s+ago\b`)
	cnVagueAgoRE = regexp.MustCompile(`(?:好几|几)(?:个)?(天|日|周|星期|礼拜|月|年)(?:以)?前`)
)

func validUTCDate(ys, ms, ds string) (time.Time, bool) {
	y, err1 := strconv.Atoi(ys)
	mo, err2 := strconv.Atoi(ms)
	d, err3 := strconv.Atoi(ds)
	if err1 != nil || err2 != nil || err3 != nil || y < 1900 || y > 2099 || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}
func validUTCDateTimeTZ(ys, ms, ds, hhs, mms, sss, tzHs, tzMs string) (time.Time, bool) {
	base, ok := validUTCDate(ys, ms, ds)
	if !ok {
		return time.Time{}, false
	}
	if hhs == "" {
		return base, true
	}
	hh, err1 := strconv.Atoi(hhs)
	mm, err2 := strconv.Atoi(mms)
	ss := 0
	var err3 error
	if sss != "" {
		ss, err3 = strconv.Atoi(sss)
	}
	if err1 != nil || err2 != nil || err3 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 || ss < 0 || ss > 59 {
		return time.Time{}, false
	}
	t := time.Date(base.Year(), base.Month(), base.Day(), hh, mm, ss, 0, time.UTC)
	if tzHs != "" {
		tzH, errH := strconv.Atoi(tzHs)
		tzM := 0
		var errM error
		if tzMs != "" {
			tzM, errM = strconv.Atoi(tzMs)
		}
		if errH != nil || errM != nil || tzH < -14 || tzH > 14 || tzM < 0 || tzM > 59 {
			return time.Time{}, false
		}
		sign := 1
		if strings.HasPrefix(tzHs, "-") {
			sign = -1
			if tzH < 0 {
				tzH = -tzH
			}
		}
		offsetSec := sign * (tzH*3600 + tzM*60)
		t = t.Add(-time.Duration(offsetSec) * time.Second)
	}
	return t, true
}

func applyMeridiemHour(hh int, mer string) (int, bool) {
	mer = strings.ToLower(strings.TrimSpace(mer))
	switch mer {
	case "":
		return hh, hh >= 0 && hh <= 23
	case "am", "凌晨", "早上", "上午":
		if hh < 1 || hh > 12 {
			return 0, false
		}
		if hh == 12 {
			return 0, true
		}
		return hh, true
	case "pm", "下午", "傍晚", "晚上":
		if hh < 1 || hh > 12 {
			if hh >= 13 && hh <= 23 {
				return hh, true
			}
			return 0, false
		}
		if hh < 12 {
			return hh + 12, true
		}
		return 12, true
	case "中午":
		if hh >= 1 && hh <= 3 {
			return hh + 12, true
		}
		return hh, hh >= 11 && hh <= 14
	}
	return hh, hh >= 0 && hh <= 23
}

// extractExplicitSecondTimestamp 从含显式日期+时分秒的文本中提取秒级 UTC 时间戳（无合法时刻则返回 0）。
func extractExplicitSecondTimestamp(src, low string) int64 {
	if m := isoDateTimeTextRE.FindStringSubmatch(low); m != nil {
		if d, ok := validUTCDate(m[1], m[2], m[3]); ok {
			hh, _ := strconv.Atoi(m[4])
			mm, _ := strconv.Atoi(m[5])
			ss, _ := strconv.Atoi(m[6])
			if hh2, okH := applyMeridiemHour(hh, m[7]); okH && mm >= 0 && mm <= 59 && ss >= 0 && ss <= 59 {
				if t, okTZ := validUTCDateTimeTZ(m[1], m[2], m[3], strconv.Itoa(hh2), m[5], m[6], m[8], m[9]); okTZ {
					return t.Unix()
				}
				return time.Date(d.Year(), d.Month(), d.Day(), hh2, mm, ss, 0, time.UTC).Unix()
			}
		}
	}
	if m := enMDYClockTextRE.FindStringSubmatch(low); m != nil {
		mo := enMonthNumber(m[1])
		if d, ok := validUTCDate(m[3], strconv.Itoa(mo), m[2]); ok {
			hh, _ := strconv.Atoi(m[4])
			mm, _ := strconv.Atoi(m[5])
			ss, _ := strconv.Atoi(m[6])
			if hh2, okH := applyMeridiemHour(hh, m[7]); okH && mm >= 0 && mm <= 59 && ss >= 0 && ss <= 59 {
				return time.Date(d.Year(), d.Month(), d.Day(), hh2, mm, ss, 0, time.UTC).Unix()
			}
		}
	}
	if m := enDMYClockTextRE.FindStringSubmatch(low); m != nil {
		mo := enMonthNumber(m[2])
		if d, ok := validUTCDate(m[3], strconv.Itoa(mo), m[1]); ok {
			hh, _ := strconv.Atoi(m[4])
			mm, _ := strconv.Atoi(m[5])
			ss, _ := strconv.Atoi(m[6])
			if hh2, okH := applyMeridiemHour(hh, m[7]); okH && mm >= 0 && mm <= 59 && ss >= 0 && ss <= 59 {
				return time.Date(d.Year(), d.Month(), d.Day(), hh2, mm, ss, 0, time.UTC).Unix()
			}
		}
	}
	if m := cnDateTimeTextRE.FindStringSubmatch(src); m != nil {
		if d, ok := validUTCDate(m[1], m[2], m[3]); ok {
			mer := m[4]
			var hh, mm, ss int
			if m[5] != "" {
				hh, _ = strconv.Atoi(m[5])
				mm, _ = strconv.Atoi(m[6])
				ss, _ = strconv.Atoi(m[7])
			} else if m[8] != "" {
				hh, _ = strconv.Atoi(m[8])
				if m[9] == "半" {
					mm = 30
				} else if m[10] != "" {
					mm, _ = strconv.Atoi(m[10])
					ss, _ = strconv.Atoi(m[11])
				}
			}
			if hh2, okH := applyMeridiemHour(hh, mer); okH && mm >= 0 && mm <= 59 && ss >= 0 && ss <= 59 {
				return time.Date(d.Year(), d.Month(), d.Day(), hh2, mm, ss, 0, time.UTC).Unix()
			}
		}
	}
	return 0
}

// attachClockToDay 当相对日期解出整日零点 dayUnix 时，若同句含有显式时分秒（如 "yesterday at 14:30:25" / "昨天下午3点20分15秒"），
// 将日内时钟叠加到该相对日期上，产出秒级相对时刻（若同句同时含显式年月日，不叠加防串台）。
func attachClockToDay(dayUnix int64, src, low string) int64 {
	if extractExplicitSecondTimestamp(src, low) > 0 {
		return 0
	}
	d := time.Unix(dayUnix, 0).UTC()
	if m := relDayClockENRE.FindStringSubmatch(low); m != nil {
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		ss, _ := strconv.Atoi(m[3])
		if hh2, okH := applyMeridiemHour(hh, m[4]); okH && mm >= 0 && mm <= 59 && ss >= 0 && ss <= 59 {
			return time.Date(d.Year(), d.Month(), d.Day(), hh2, mm, ss, 0, time.UTC).Unix()
		}
	}
	if m := relDayClockCNRE.FindStringSubmatch(src); m != nil {
		mer := m[1]
		var hh, mm, ss int
		if m[2] != "" {
			hh, _ = strconv.Atoi(m[2])
			mm, _ = strconv.Atoi(m[3])
			ss, _ = strconv.Atoi(m[4])
		} else if m[5] != "" {
			hh, _ = strconv.Atoi(m[5])
			if m[6] == "半" {
				mm = 30
			} else if m[7] != "" {
				mm, _ = strconv.Atoi(m[7])
				ss, _ = strconv.Atoi(m[8])
			}
		}
		if hh2, okH := applyMeridiemHour(hh, mer); okH && mm >= 0 && mm <= 59 && ss >= 0 && ss <= 59 && (hh2 > 0 || mm > 0 || ss > 0) {
			return time.Date(d.Year(), d.Month(), d.Day(), hh2, mm, ss, 0, time.UTC).Unix()
		}
	}
	return 0
}

func parseSubDayCount(raw string) int {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "a", "an", "one":
		return 1
	case "two", "a couple of":
		return 2
	case "three", "a few", "several":
		return 3
	case "four":
		return 4
	case "five":
		return 5
	case "six":
		return 6
	case "seven":
		return 7
	case "eight":
		return 8
	case "nine":
		return 9
	case "ten":
		return 10
	case "eleven":
		return 11
	case "twelve":
		return 12
	case "fifteen":
		return 15
	case "twenty":
		return 20
	case "thirty":
		return 30
	case "forty-five":
		return 45
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return parseCNMonthNum(s)
}

func parseSubDayRelOffset(src, low string, refUTC time.Time) int64 {
	if m := enSubDayAgoRE.FindStringSubmatch(low); m != nil {
		sign := -1
		if m[3] == "later" || strings.HasPrefix(m[3], "from") {
			sign = 1
		}
		if strings.HasPrefix(m[1], "half") && strings.HasPrefix(m[2], "h") {
			return refUTC.Add(time.Duration(sign*30) * time.Minute).Unix()
		}
		if n := parseSubDayCount(m[1]); n > 0 {
			u := m[2]
			switch {
			case strings.HasPrefix(u, "sec"):
				return refUTC.Add(time.Duration(sign*n) * time.Second).Unix()
			case strings.HasPrefix(u, "min"):
				return refUTC.Add(time.Duration(sign*n) * time.Minute).Unix()
			case strings.HasPrefix(u, "h"):
				return refUTC.Add(time.Duration(sign*n) * time.Hour).Unix()
			}
		}
	}
	if m := enSubDayInRE.FindStringSubmatch(low); m != nil {
		if strings.HasPrefix(m[1], "half") && strings.HasPrefix(m[2], "h") {
			return refUTC.Add(30 * time.Minute).Unix()
		}
		if n := parseSubDayCount(m[1]); n > 0 {
			u := m[2]
			switch {
			case strings.HasPrefix(u, "sec"):
				return refUTC.Add(time.Duration(n) * time.Second).Unix()
			case strings.HasPrefix(u, "min"):
				return refUTC.Add(time.Duration(n) * time.Minute).Unix()
			case strings.HasPrefix(u, "h"):
				return refUTC.Add(time.Duration(n) * time.Hour).Unix()
			}
		}
	}
	if m := cnSubDayRelRE.FindStringSubmatch(src); m != nil {
		sign := -1
		if strings.Contains(m[5], "后") {
			sign = 1
		}
		if m[3] != "" { // 半小时前/后
			return refUTC.Add(time.Duration(sign*30) * time.Minute).Unix()
		}
		if m[4] != "" { // 一刻钟前/后
			return refUTC.Add(time.Duration(sign*15) * time.Minute).Unix()
		}
		if n := parseCNMonthNum(m[1]); n > 0 {
			switch m[2] {
			case "秒", "秒钟":
				return refUTC.Add(time.Duration(sign*n) * time.Second).Unix()
			case "分", "分钟":
				return refUTC.Add(time.Duration(sign*n) * time.Minute).Unix()
			case "小时", "钟头":
				return refUTC.Add(time.Duration(sign*n) * time.Hour).Unix()
			}
		}
	}
	return 0
}

func enMonthNumber(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "january", "jan":
		return 1
	case "february", "feb":
		return 2
	case "march", "mar":
		return 3
	case "april", "apr":
		return 4
	case "may":
		return 5
	case "june", "jun":
		return 6
	case "july", "jul":
		return 7
	case "august", "aug":
		return 8
	case "september", "sept", "sep":
		return 9
	case "october", "oct":
		return 10
	case "november", "nov":
		return 11
	case "december", "dec":
		return 12
	}
	return 0
}

func parseCNMonthNum(s string) int {
	s = strings.TrimSpace(s)
	var half strings.Builder
	for _, r := range s {
		if r >= '０' && r <= '９' {
			half.WriteRune('0' + (r - '０'))
		} else {
			half.WriteRune(r)
		}
	}
	if n, err := strconv.Atoi(half.String()); err == nil && n > 0 {
		return n
	}
	digits := map[rune]int{'一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total, cur := 0, 0
	for _, r := range s {
		if r == '百' {
			if cur == 0 {
				cur = 1
			}
			total += cur * 100
			cur = 0
			continue
		}
		if r == '十' {
			if cur == 0 {
				cur = 1
			}
			total += cur * 10
			cur = 0
			continue
		}
		if r == '零' || r == '〇' {
			continue
		}
		if d, ok := digits[r]; ok {
			cur = cur*10 + d
		}
	}
	return total + cur
}

func parseSmallCount(s string) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	switch s {
	case "one":
		return 1
	case "two":
		return 2
	case "three":
		return 3
	case "four":
		return 4
	case "five":
		return 5
	case "six":
		return 6
	case "seven":
		return 7
	case "eight":
		return 8
	case "nine":
		return 9
	case "ten":
		return 10
	case "eleven":
		return 11
	case "twelve":
		return 12
	}
	return 0
}
