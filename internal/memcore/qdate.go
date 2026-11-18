// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import (
	"regexp"
	"strings"
	"time"
)

// TimeWindow represents a parsed query/assertion time window (1:1 shared between repository and service).
type TimeWindow struct {
	Years       []int32 `json:"years,omitempty"`
	Months      []int32 `json:"months,omitempty"`
	Days        []int32 `json:"days,omitempty"`
	Since       int64   `json:"since,omitempty"`
	Until       int64   `json:"until,omitempty"`
	MonthOfYear int     `json:"month_of_year,omitempty"`
	DayOfMonth  int     `json:"day_of_month,omitempty"`
	Anchor      int64   `json:"anchor,omitempty"`
	From        int64   `json:"from,omitempty"`
	To          int64   `json:"to,omitempty"`
	Label       string  `json:"label,omitempty"`
}

// Empty reports whether no dimension or timestamp bound is set on the window.
func (w TimeWindow) Empty() bool {
	return len(w.Years) == 0 && len(w.Months) == 0 && len(w.Days) == 0 && w.Since == 0 && w.Until == 0
}

// DateUTC formats unix seconds as YYYY-MM-DD in UTC (0 -> "").
func DateUTC(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02")
}

// DateTimeUTC formats unix seconds as YYYY-MM-DD (when at UTC midnight) or YYYY-MM-DD HH:MM:SS in UTC.
func DateTimeUTC(ts int64) string {
	if ts <= 0 {
		return ""
	}
	t := time.Unix(ts, 0).UTC()
	if ts%86400 == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04:05")
}

// monthNames 英文月份字面（全称 + 三字母缩写；形式层词表，非语义判定）。
var monthNames = map[string]int{
	"january": 1, "jan": 1, "february": 2, "feb": 2, "march": 3, "mar": 3,
	"april": 4, "apr": 4, "may": 5, "june": 6, "jun": 6, "july": 7, "jul": 7,
	"august": 8, "aug": 8, "september": 9, "sep": 9, "sept": 9, "october": 10, "oct": 10,
	"november": 11, "nov": 11, "december": 12, "dec": 12,
}

var (
	reFourDigitYear = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	reCNYMD         = regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日?`)
	reCNYM          = regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月`)
	reNumYMD        = regexp.MustCompile(`\b(19\d{2}|20\d{2})[-/.](\d{1,2})[-/.](\d{1,2})\b`)
	reENMonthDay    = regexp.MustCompile(`\b(?:january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sept|sep|october|oct|november|nov|december|dec)\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	reENDayMonth    = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?(?:\s+of)?\s+(?:january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sept|sep|october|oct|november|nov|december|dec)\b`)
	reENDayRange    = regexp.MustCompile(`\b(?:between|from)\s+(?:january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sept|sep|october|oct|november|nov|december|dec)\s+(\d{1,2})(?:st|nd|rd|th)?\s+(?:and|to)\s+(?:(?:january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sept|sep|october|oct|november|nov|december|dec)\s+)?(\d{1,2})(?:st|nd|rd|th)?\b`)
	reRelPrevSeason = regexp.MustCompile(`\b(?:last\s+(?:year|summer|spring|winter|autumn|fall))\b|去年`)
)

// ExtractAnchorSentence extracts the full sentence in turnText covering [anchorStart, anchorEnd)
// (supporting both ASCII and CJK full-width sentence punctuation) so leading temporal adverbs
// in the same sentence can be resolved deterministically.
func ExtractAnchorSentence(turnText string, anchorStart, anchorEnd int) string {
	if turnText == "" || anchorStart < 0 || anchorEnd <= anchorStart || anchorEnd > len(turnText) {
		return ""
	}
	isSentBreak := func(b byte) bool {
		switch b {
		case '.', '!', '?', ';', '\n', '\r':
			return true
		}
		return false
	}
	start := anchorStart
	for start > 0 {
		if isSentBreak(turnText[start-1]) {
			break
		}
		if start >= 3 {
			prev3 := turnText[start-3 : start]
			if prev3 == "。" || prev3 == "！" || prev3 == "？" || prev3 == "；" {
				break
			}
		}
		start--
	}
	end := anchorEnd
	for end < len(turnText) {
		if isSentBreak(turnText[end]) {
			end++
			break
		}
		if end+3 <= len(turnText) {
			next3 := turnText[end : end+3]
			if next3 == "。" || next3 == "！" || next3 == "？" || next3 == "；" {
				end += 3
				break
			}
		}
		end++
	}
	return strings.TrimSpace(turnText[start:end])
}

// PickTimeAnchorKind selects the resolved anchor timestamp from a slice of windows and classifies
// whether it came from the explicit prefix [0..absN) or the relative suffix.
func PickTimeAnchorKind(wins []TimeWindow, absN int) (int64, string) {
	var anchor int64
	kind := ""
	allAbs := absN > 0 && len(wins) == absN
	for i, w := range wins {
		if w.Anchor <= 0 {
			continue
		}
		if allAbs {
			return w.Anchor, "explicit"
		}
		anchor = w.Anchor
		if i < absN {
			kind = "explicit"
		} else {
			kind = "relative_resolved"
		}
	}
	if anchor <= 0 {
		return 0, ""
	}
	return anchor, kind
}

func pickTimeAnchorKind(wins []TimeWindow, absN int) (int64, string) {
	return PickTimeAnchorKind(wins, absN)
}
