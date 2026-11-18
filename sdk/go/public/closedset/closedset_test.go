// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package closedset

import (
	"strings"
	"testing"
)

func TestClosedSetTablesIntegrityAndBijection(t *testing.T) {
	if len(StandardClosedSetTable) == 0 || len(UniversalSchemaClosedSetTable) == 0 || len(EverydayTimeClosedSetTable) == 0 {
		t.Fatalf("closed-set tables must be non-empty: std=%d univ=%d everyday=%d",
			len(StandardClosedSetTable), len(UniversalSchemaClosedSetTable), len(EverydayTimeClosedSetTable))
	}
	if len(ClosedSetHarmonicEntries) != len(StandardClosedSetTable)+len(UniversalSchemaClosedSetTable) {
		t.Fatalf("combined table length mismatch: got=%d", len(ClosedSetHarmonicEntries))
	}
	seenTokenRow := make(map[string]string, 7200)
	for i, row := range ClosedSetHarmonicEntries {
		colon := strings.LastIndexByte(row, ':')
		if colon <= 0 {
			t.Fatalf("row %d missing colon: %q", i, row)
		}
		head := row[:colon]
		toks := strings.Split(row[colon+1:], ",")
		if len(toks) < 2 {
			t.Fatalf("row %d (%s) has <2 tokens: %v", i, head, toks)
		}
		for _, tok := range toks {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if tok != strings.ToLower(tok) {
				t.Fatalf("token %q in row %s is not lowercase; lowercase-normalizing lookups can never reach it", tok, head)
			}
			if prevHead, exists := seenTokenRow[tok]; exists {
				t.Fatalf("token %q mapped to multiple closed-set rows (%s vs %s): violates single-valued quotient map", tok, prevHead, head)
			}
			seenTokenRow[tok] = head
		}
	}
	if got := LookupFirstTag("newzealand"); got != "@geo:cc:nz" {
		t.Fatalf("LookupFirstTag(newzealand) = %q, want @geo:cc:nz", got)
	}
	if got := LookupFirstTag("kg"); got != "@unit:mass:kg" {
		t.Fatalf("LookupFirstTag(kg) = %q, want @unit:mass:kg", got)
	}
}

// TestEverydayTimeClosedSetLookups pins the Part-IV deictic calendar and
// daypart classes: multilingual surfaces resolve to their canonical tags, the
// declared false-friend exclusions stay excluded, every token lives in exactly
// one row, and the table stays query-side only (no token may leak into the
// decoration tagMap, so document encoding and the inverted index stay
// untouched).
func TestEverydayTimeClosedSetLookups(t *testing.T) {
	seenDeix := make(map[string]string, 512)
	for i, row := range EverydayTimeClosedSetTable {
		colon := strings.LastIndexByte(row, ':')
		if colon <= 0 {
			t.Fatalf("everyday row %d missing colon: %q", i, row)
		}
		head := row[:colon]
		toks := strings.Split(row[colon+1:], ",")
		if len(toks) < 2 {
			t.Fatalf("everyday row %d (%s) has <2 tokens: %v", i, head, toks)
		}
		for _, tok := range toks {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if prevHead, exists := seenDeix[tok]; exists {
				t.Fatalf("everyday token %q mapped to multiple rows (%s vs %s)", tok, prevHead, head)
			}
			seenDeix[tok] = head
			if got := LookupFirstTag(tok); got != "" {
				t.Fatalf("everyday token %q leaked into the decoration tagMap as %q (must stay query-side only)", tok, got)
			}
		}
	}

	cases := []struct{ token, want string }{
		{"昨天", "@cal:deix:yesterday"},
		{"上周", "@cal:deix:lastweek"},
		{"上个月", "@cal:deix:lastmonth"},
		{"上上个月", "@cal:deix:monthbeforelast"},
		{"去年", "@cal:deix:lastyear"},
		{"前年", "@cal:deix:yearbeforelast"},
		{"明天", "@cal:deix:tomorrow"},
		{"今晚", "@cal:deix:tonight"},
		{"昨晚", "@cal:deix:lastnight"},
		{"yesterday", "@cal:deix:yesterday"},
		{"lastweek", "@cal:deix:lastweek"},
		{"last-week", "@cal:deix:lastweek"},
		{"morgen", "@cal:deix:tomorrow"},
		{"mañana", "@cal:deix:tomorrow"},
		{"manana", "@cal:deix:tomorrow"},
		{"昨日", "@cal:deix:yesterday"},
		{"지난주", "@cal:deix:lastweek"},
		{"semainedernière", "@cal:deix:lastweek"},
		{"早上", "@cal:daypart:morning"},
		{"오전", "@cal:daypart:morning"},
		{"night", "@cal:daypart:night"},
		{"minuit", "@cal:daypart:midnight"},
		{"เที่ยงคืน", "@cal:daypart:midnight"},
		{"黄昏", "@cal:daypart:dusk"},
	}
	for _, tc := range cases {
		if got := LookupEverydayTimeClass(tc.token); got != tc.want {
			t.Fatalf("LookupEverydayTimeClass(%q) = %q, want %q", tc.token, got, tc.want)
		}
	}
	// Declared false-friend exclusions must not resolve to deictic classes.
	if got := LookupEverydayTimeClass("hier"); got != "" {
		t.Fatalf("LookupEverydayTimeClass(hier) = %q, want empty (German hier = here)", got)
	}
	// Ordinal polarity stays a separate query-side table, not merged here.
	if got := LookupEverydayTimeClass("first"); got != "" {
		t.Fatalf("LookupEverydayTimeClass(first) = %q, want empty", got)
	}
}

// TestTrailingDotPairLookupParity：词元组查询（LookupGeoPair / LookupUnitPair）与
// 装饰闭集通道（LookupFirstTag / LookupHarmonics）保持同一末尾点号归一化口径。
func TestTrailingDotPairLookupParity(t *testing.T) {
	g1a, g1b := LookupGeoPair("u.s.")
	g2a, g2b := LookupGeoPair("u.s")
	if g1a != g2a || g1b != g2b || g1a == "" {
		t.Fatalf("LookupGeoPair trailing-dot parity: (%q,%q) vs (%q,%q)", g1a, g1b, g2a, g2b)
	}
	u1a, u1b := LookupUnitPair("kg.")
	u2a, u2b := LookupUnitPair("kg")
	if u1a != u2a || u1b != u2b || u1a == "" {
		t.Fatalf("LookupUnitPair trailing-dot parity: (%q,%q) vs (%q,%q)", u1a, u1b, u2a, u2b)
	}
}
