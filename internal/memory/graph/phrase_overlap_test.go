// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package graph

import (
	"reflect"
	"testing"
)

func TestHanRunWindowPairsOnlyReturnsAdjacentOverlappingWindows(t *testing.T) {
	cases := []struct {
		name string
		text string
		want [][2]string
	}{
		{
			name: "single two-character run has no pair",
			text: "同意",
			want: nil,
		},
		{
			name: "three characters produce one adjacent window pair",
			text: "全部同",
			want: [][2]string{{"全部", "部同"}},
		},
		{
			name: "punctuation splits runs",
			text: "全部，同意",
			want: nil,
		},
		{
			name: "mixed script keeps only Han runs",
			text: "user 全部同意",
			want: [][2]string{{"全部", "部同"}, {"部同", "同意"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hanRunWindowPairs(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("hanRunWindowPairs(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
