// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"regexp"
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
