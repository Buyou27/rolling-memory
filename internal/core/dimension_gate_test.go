// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// countToken matches a document/unit/group count expression: a df-style
// identifier, len(...) of anything, or a *Count field.
const countToken = `(?:float64\((?:\w*[dD][fF]\w*|len\([^)]*\)|\w*Count)\)|\b\w*[dD][fF]\w*\b|\b\w*Count\b|len\([^)]*\))`

// ePowerTerm matches a bare dimensionless e-power. A term that is multiplied
// or divided by another count (e.g. eulerNumber*float64(dfG)) is a legitimate
// count-to-count ratio and is deliberately not matched by the terminators below.
const ePowerTerm = `(?:eulerNumber\s*\*\s*eulerNumber|eulerSquared|eulerNumber)`

var (
	countTokenRE       = regexp.MustCompile(countToken)
	countAgainstEPower = regexp.MustCompile(countToken + `\s*[<>]=?\s*` + ePowerTerm)
	ePowerAgainstCount = regexp.MustCompile(ePowerTerm + `\s*[<>]=?\s*` + countToken)
	countPlusEPower    = regexp.MustCompile(countToken + `\s*[+\-]\s*` + ePowerTerm)
	ePowerPlusCount    = regexp.MustCompile(ePowerTerm + `\s*[+\-]\s*` + countToken)
)

// ratioContinues reports whether the text right after an e-power term continues
// the same factor with × or ÷ of another count, which makes the expression a
// count-to-count ratio (dimensionally legal) rather than a count compared with a
// pure number.
func ratioContinues(rest string) bool {
	r := strings.TrimSpace(rest)
	if r == "" || (r[0] != '*' && r[0] != '/') {
		return false
	}
	return countTokenRE.MatchString(strings.TrimSpace(r[1:]))
}

// dimensionlessInCountDomain reports whether a source line adds/subtracts a
// dimensionless e-power to/from a count, or compares one against the other.
func dimensionlessInCountDomain(line string) bool {
	if !strings.Contains(line, "eulerNumber") && !strings.Contains(line, "eulerSquared") {
		return false
	}
	if m := countAgainstEPower.FindStringIndex(line); m != nil && !ratioContinues(line[m[1]:]) {
		return true
	}
	if m := countPlusEPower.FindStringIndex(line); m != nil && !ratioContinues(line[m[1]:]) {
		return true
	}
	return ePowerAgainstCount.MatchString(line) || ePowerPlusCount.MatchString(line)
}

// TestDimensionlessPowersStayOutOfCountDomain is the A24 mechanical guard: a
// dimensionless e-power may scale a count, but must never be added to it or
// compared against it (that mixes a pure number with 篇/个/组). Lines that
// legitimately do so must carry a `[count-floor]` marker explaining the unit.
//
// The guard is self-tested: it must flag the legacy shapes it was written for
// (including the two forms rewritten in A24) and must not flag count-to-count
// ratios, otherwise the scan could pass vacuously.
func TestDimensionlessPowersStayOutOfCountDomain(t *testing.T) {
	must := []string{
		`if isTrueProper && float64(dfH) > eulerNumber*eulerNumber && ok {`,
		`if len(groupAnchorUnits[g]) > eulerNumber*eulerNumber {`,
		`if dfCount + eulerNumber > 0 {`,
		`if eulerNumber < float64(df) {`,
		`if unitCount >= eulerSquared {`,
	}
	mustNot := []string{
		`if float64(dfH) <= eulerNumber*float64(dfG) {`,
		`w := math.Exp(-float64(dt) / (tau * eulerNumber))`,
		`x := float64(bits) * eulerNumber / meanBits`,
		`dfCap := math.Ceil(math.Sqrt(float64(len(c.docIDs))) * eulerNumber)`,
	}
	for _, line := range must {
		if !dimensionlessInCountDomain(line) {
			t.Fatalf("守卫必须命中该量纲不齐写法：%s", line)
		}
	}
	for _, line := range mustNot {
		if dimensionlessInCountDomain(line) {
			t.Fatalf("守卫不得误报合法写法：%s", line)
		}
	}

	// The kernel ships to mirrors that exclude the RB-only relation cluster, so a
	// missing sibling directory is skipped rather than treated as a failure.
	roots := []string{"."}
	if _, err := os.Stat(filepath.Join("..", "knn")); err == nil {
		roots = append(roots, filepath.Join("..", "knn"))
	}
	scanned := 0
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", root, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(root, name)
			blob, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for i, line := range strings.Split(string(blob), "\n") {
				scanned++
				if !dimensionlessInCountDomain(line) {
					continue
				}
				if strings.Contains(line, "[count-floor]") {
					continue
				}
				t.Fatalf("%s:%d 把无量纲 e 幂接到计数上（量纲不齐）；改用具名整数地板或用 [count-floor] 标注单位：%s",
					path, i+1, strings.TrimSpace(line))
			}
		}
	}
	if scanned < 5000 {
		t.Fatalf("扫描行数异常（%d），守卫可能扫错目录", scanned)
	}
	t.Logf("A24 量纲守卫：扫描 %d 行，无未标注的量纲不齐写法", scanned)
}
