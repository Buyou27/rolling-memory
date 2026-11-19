// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package updatecheck

import (
	"strconv"
	"strings"
)

type semverValue struct {
	nums [3]int
	pre  []string
}

// CompareSemver compares two version strings. A leading `v` is accepted; build
// metadata is ignored; a missing minor/patch component is treated as zero. When
// either side is not a semantic version the comparison falls back to a
// case-insensitive string comparison so untagged development builds still get
// a stable answer.
func CompareSemver(a, b string) int {
	left, leftOK := parseSemver(a)
	right, rightOK := parseSemver(b)
	if !leftOK || !rightOK {
		return strings.Compare(strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b)))
	}
	for i := 0; i < 3; i++ {
		if left.nums[i] < right.nums[i] {
			return -1
		}
		if left.nums[i] > right.nums[i] {
			return 1
		}
	}
	return comparePrerelease(left.pre, right.pre)
}

func sameVersion(a, b string) bool {
	return CompareSemver(a, b) == 0
}

// NormalizeVersion removes surrounding whitespace and a leading `v` so the
// persisted state and the console display use one spelling.
func NormalizeVersion(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) > 1 && (value[0] == 'v' || value[0] == 'V') {
		return value[1:]
	}
	return value
}

func parseSemver(raw string) (semverValue, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return semverValue{}, false
	}
	if value[0] == 'v' || value[0] == 'V' {
		value = value[1:]
	}
	if value == "" {
		return semverValue{}, false
	}
	if index := strings.IndexByte(value, '+'); index >= 0 {
		value = value[:index]
	}
	core := value
	pre := ""
	if index := strings.IndexByte(value, '-'); index >= 0 {
		core = value[:index]
		pre = value[index+1:]
	}
	fields := strings.Split(core, ".")
	if len(fields) == 0 || len(fields) > 3 {
		return semverValue{}, false
	}
	var result semverValue
	for index, field := range fields {
		if field == "" {
			return semverValue{}, false
		}
		number, err := strconv.Atoi(field)
		if err != nil || number < 0 {
			return semverValue{}, false
		}
		result.nums[index] = number
	}
	if pre != "" {
		for _, identifier := range strings.Split(pre, ".") {
			if identifier == "" {
				return semverValue{}, false
			}
			for _, char := range identifier {
				if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
					(char >= '0' && char <= '9') || char == '-' {
					continue
				}
				return semverValue{}, false
			}
		}
		result.pre = strings.Split(pre, ".")
	}
	return result, true
}

func comparePrerelease(left, right []string) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		a, aNumeric := numericIdentifier(left[index])
		b, bNumeric := numericIdentifier(right[index])
		switch {
		case aNumeric && bNumeric:
			if a < b {
				return -1
			}
			if a > b {
				return 1
			}
		case aNumeric && !bNumeric:
			return -1
		case !aNumeric && bNumeric:
			return 1
		default:
			if compare := strings.Compare(left[index], right[index]); compare != 0 {
				return compare
			}
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

func numericIdentifier(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return number, true
}
