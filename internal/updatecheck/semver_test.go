// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package updatecheck

import (
	"fmt"
	"runtime"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"0.0.5", "v0.0.6", -1},
		{"v1.2.3", "1.2.3", 0},
		{"1.2.10", "1.2.9", 1},
		{"1.2.3", "1.2.3-rc.1", 1},
		{"1.2.3-rc.2", "1.2.3-rc.10", -1},
		{"1.2.3-alpha", "1.2.3-beta", -1},
		{"1.2", "1.2.0", 0},
	}
	for _, tc := range cases {
		if got := CompareSemver(tc.left, tc.right); got != tc.want {
			t.Fatalf("CompareSemver(%q, %q) = %d, want %d", tc.left, tc.right, got, tc.want)
		}
	}
}

func TestParseSHA256SumsExtractsVersion(t *testing.T) {
	name := fmt.Sprintf("rolling-memory-0.2.0-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	manifest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  " + name + "\n"
	sums, version := ParseSHA256Sums(manifest)
	if version != "0.2.0" {
		t.Fatalf("version = %q, want 0.2.0", version)
	}
	if sums[name] == "" {
		t.Fatalf("manifest entry missing: %#v", sums)
	}
}
