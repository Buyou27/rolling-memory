// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package eval

import (
	"math"
	"os"
	"strings"
	"sync/atomic"

	"github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// 全档空洞凭证驱动的零交付开关（评测/交付层可选臂）。
//
// 默认关闭：历史读数与 0-ULP 基线保持逐位不变；置 ROLLING_MEMORY_VOID_FLOOR=1
// 时启用「刚性专名零记载 / 复合专名全档零共现」两类凭证触发物理零交付（K=0）。
// 跨锚零共现（C3）不作为零交付判据：多跳与跨文档拼图题普遍满足 C3，
// 硬截断会误杀在库题，只允许作为交付提示。
func evalVoidFloorEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ROLLING_MEMORY_VOID_FLOOR"))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// blockingVoidCertificate 返回是否命中可触发零交付的刚性空洞凭证。
//
// 仅凭证不足：单专名缺失常与其余内容共现（如菜系修饰词未进图注、地点仍在库），
// 纯形式信号无权单独授权物理零交付。因此附加整库信息条件——查询的实质词自信息
// 几乎全部落在零记载侧（OOVMass 不低于 FullMass 的 1-1/e），才判定为整查询空洞。
func blockingVoidCertificate(certs []plain.VoidCertificate, report plain.CoverageReport) bool {
	if report.FullMass <= 0 || report.OOVMass < report.FullMass*(1.0-1.0/math.E) {
		return false
	}
	for _, c := range certs {
		if c.Kind == plain.VoidKindOOVProper || c.Kind == plain.VoidKindBrokenCompound {
			return true
		}
	}
	return false
}

// evalVoidFloorFired 统计本次进程内触发零交付的查询数（供报告核对）。
var evalVoidFloorFired atomic.Int64
