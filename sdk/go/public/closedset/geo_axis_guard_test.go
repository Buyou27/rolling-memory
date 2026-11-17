// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package closedset

import (
	"strings"
	"testing"
)

// LookupGeoPair 必须限定在 @geo: 轴内：非地理闭集词（venue/calendar/bio/unit）
// 曾因「仅跳过 @unit:」的宽过滤返回外来轴标签（hotel -> @venue:lodging/@dim:travel）。
// 本测试对全表做总体不变量扫描：任何 token 的解析结果要么是它所在行的 @geo: 对，
// 要么为空；且每个 @geo: 行的全部别名都必须可解析。
func TestLookupGeoPairRestrictsToGeoAxisAdversarial(t *testing.T) {
	geoAliases := 0
	nonGeoAliases := 0
	for _, row := range ClosedSetHarmonicEntries {
		colon := strings.LastIndexByte(row, ':')
		if colon <= 0 {
			continue
		}
		head := row[:colon]
		pipe := strings.IndexByte(head, '|')
		if pipe <= 0 {
			continue
		}
		pair0, pair1 := head[:pipe], head[pipe+1:]
		isGeo := strings.HasPrefix(pair0, "@geo:")
		for _, tok := range strings.Split(row[colon+1:], ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			got1, got2 := LookupGeoPair(tok)
			if isGeo {
				if got1 != pair0 || got2 != pair1 {
					t.Fatalf("geo alias %q resolved to (%q,%q), want (%q,%q)", tok, got1, got2, pair0, pair1)
				}
				geoAliases++
				continue
			}
			if got1 != "" || got2 != "" {
				t.Fatalf("non-geo token %q leaked foreign axis (%q,%q)", tok, got1, got2)
			}
			if h := LookupGeoHarmonics(tok); len(h) != 0 {
				t.Fatalf("non-geo token %q leaked harmonics %v", tok, h)
			}
			nonGeoAliases++
		}
	}
	if geoAliases == 0 || nonGeoAliases == 0 {
		t.Fatalf("vacuous scan: geo=%d nonGeo=%d", geoAliases, nonGeoAliases)
	}
	if t1, t2 := LookupGeoPair("paris"); t1 != "@geo:cc:fr" || t2 != "@geo:cont:eu" {
		t.Fatalf("LookupGeoPair(paris) = (%q,%q)", t1, t2)
	}
}
