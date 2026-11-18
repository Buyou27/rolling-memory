// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import "testing"

// TestValidateWidePosRecordBounds：widePosPool 记录幅值防御——越界值必须在解析期拒绝，
// 不再流向下游 make（原缺口：自洽段携带 Sentence=1<<40 触发 makeslice panic/OOM）。
// 合法坐标上界为工程安全上限 widePosMaxCoordinate；生产宽坐标保真测试钉住的
// {70000, 70001}（numPos 无关）必须始终放行。
func TestValidateWidePosRecordBounds(t *testing.T) {
	valid := []struct {
		name           string
		pIdx           uint32
		sent, slot, np int
	}{
		{"sentinel sentence", 0, -1, 1, 8},
		{"wide coordinate regression", 0, 70000, 70001, 2},
		{"coordinate ceiling", 1, widePosMaxCoordinate, widePosMaxCoordinate, 8},
	}
	for _, tc := range valid {
		if err := validateWidePosRecord(tc.pIdx, tc.sent, tc.slot, tc.np); err != nil {
			t.Fatalf("%s must be valid: %v", tc.name, err)
		}
	}
	bad := []struct {
		name           string
		pIdx           uint32
		sent, slot, np int
	}{
		{"huge sentence", 0, 1 << 40, 1, 8},
		{"huge slot", 0, 1, 1 << 40, 8},
		{"sentence over ceiling", 0, widePosMaxCoordinate + 1, 1, 8},
		{"slot underflow", 0, 1, -2, 8},
		{"pos index at numPos", 8, 0, 1, 8},
	}
	for _, tc := range bad {
		if err := validateWidePosRecord(tc.pIdx, tc.sent, tc.slot, tc.np); err == nil {
			t.Fatalf("%s: out-of-range record must be rejected", tc.name)
		}
	}
}
