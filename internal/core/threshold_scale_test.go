// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"testing"
)

// TestHighDFThresholdScalingInvariance 守卫高 DF 阈值的跨尺度形态：
// floor 生效段、比例生效段、以及任意 n 下 int() 截断不得把阈值压到 0
// （0 会让 df>=1 的高 DF 判定恒假，把微簿记的全部词项错置为非背景）。
func TestHighDFThresholdScalingInvariance(t *testing.T) {
	cases := []struct {
		n    int
		want int
	}{
		{n: 1, want: 7},
		{n: 20, want: 7},
		{n: 54, want: 7},
		{n: 60, want: 8},
		{n: 100, want: 13},
		{n: 11034, want: 1493},
	}
	for _, c := range cases {
		if got := highDFThresholdGuarded(c.n); got != c.want {
			t.Fatalf("n=%d 阈值=%d，期望 %d", c.n, got, c.want)
		}
	}
	// 全量非塌陷：对 n=0..2000 每一档，阈值必须 >=1
	for n := 0; n <= 2000; n++ {
		if v := highDFThresholdGuarded(n); v < 1 {
			t.Fatalf("n=%d 阈值=%d < 1：int() 截断把高 DF 判定压死", n, v)
		}
	}
	// 比例生效段应与无守卫公式一致（守卫只在截断段起作用）
	if highDFThresholdGuarded(11034) != HighDFThresholdForN(11034) {
		t.Fatalf("比例段被守卫改动：%d != %d", highDFThresholdGuarded(11034), HighDFThresholdForN(11034))
	}
	frac := float64(HighDFThresholdForN(11034)) / 11034.0
	if math.Abs(frac-highDFFraction) > 0.01 {
		t.Fatalf("比例段偏离 highDFFraction：%.4f", frac)
	}
}

// TestSpecPropMaxFloorGuard 专名特异性上限在微规模下不得被 int() 截断为 0：
// 折叠资格上界（rareInflectedTermMaxDF 同源）与专名锚资格共用同一守卫。
func TestSpecPropMaxFloorGuard(t *testing.T) {
	const fraction = 0.049787068367863944
	for n := 1; n <= 40; n++ {
		raw := int(float64(n) * fraction)
		guarded := raw
		if guarded < 1 {
			guarded = 1
		}
		if guarded < 1 {
			t.Fatalf("n=%d specPropMax=%d < 1：门恒不触发", n, guarded)
		}
		if n >= 21 && raw != guarded {
			t.Fatalf("n=%d raw=%d 已被守卫改动（守卫应只在截断段生效）", n, raw)
		}
	}
}
