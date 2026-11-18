// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ai-go-hub/ai-go-admin/internal/kit/httpx"
	"github.com/ai-go-hub/ai-go-admin/internal/middleware"
	"github.com/ai-go-hub/ai-go-admin/internal/router/registry"

	"github.com/gin-gonic/gin"
)

// 维护面（回收与自愈）全部走产品 CLI 的 `sync prune` 正典路径：分类、回收、跨轮次
// 重锚、回执文件与守护进程失效通知都由引擎实现承担，控制台只做参数校验与报告透传，
// 绝不自研第二套清洗逻辑。预演（preview）是只读分类；执行（apply）必须显式确认，
// 且沿用引擎默认的 fail-safe 口径——源不可读的行一律保留，破坏性的
// `--include-unreadable` 只留在 CLI，不进控制台。
const (
	// maintenancePrunePreviewTimeout 限定一次只读预演的整体时长。
	maintenancePrunePreviewTimeout = 3 * time.Minute
	// maintenancePruneApplyTimeout 限定一次执行（回收 + 重锚 + 孤儿 L3 清理）的整体时长。
	maintenancePruneApplyTimeout = 15 * time.Minute
	// maintenancePruneLimitCeiling 一次维护扫描的单元上限（与 CLI 的扫描上限同量级）。
	maintenancePruneLimitCeiling = 20000
)

func init() {
	registry.RegisterAdmin(func(admin *gin.RouterGroup) {
		g := admin.Group("plugin/" + Name)
		g.Use(limitConsoleRequestBody())
		g.Use(consoleVaultScopeGuard())
		g.POST("/maintenance/prune", middleware.AdminAuth(), middleware.AdminPermission(), maintenancePruneHandler)
	})
}

type maintenancePruneRequest struct {
	Action  string `json:"action"`  // preview | apply
	Limit   int    `json:"limit"`   // 扫描上限（0 = 引擎默认）
	Confirm bool   `json:"confirm"` // apply 必须显式确认
}

// maintenancePruneHandler 运行一次「回收与自愈」维护：preview 只读预演分类（含可重锚
// 计数），apply 执行回收 + 跨轮次重锚并返回回执路径；两者都返回引擎的完整报告。
func maintenancePruneHandler(c *gin.Context) {
	var req maintenancePruneRequest
	if !bindJSON(c, &req) {
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "preview"
	}
	if action != "preview" && action != "apply" {
		httpx.Fail(c, httpx.WithMessage("未知维护动作: "+req.Action+"；仅支持 preview / apply"))
		return
	}
	if action == "apply" && !req.Confirm {
		httpx.Fail(c, httpx.WithMessage("执行清理需要显式确认"))
		return
	}
	limit := req.Limit
	if limit < 0 {
		limit = 0
	}
	if limit > maintenancePruneLimitCeiling {
		limit = maintenancePruneLimitCeiling
	}
	home, _ := os.UserHomeDir()
	bin := resolveBinary(home)
	if bin == "" {
		httpx.Fail(c, httpx.WithMessage("未找到 rolling-memory 引擎二进制"))
		return
	}
	timeout := maintenancePrunePreviewTimeout
	if action == "apply" {
		timeout = maintenancePruneApplyTimeout
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	raw, err := runMaintenancePruneCLI(ctx, bin, resolveMemoryDBPath(home), action == "apply", limit)
	if err != nil {
		httpx.Fail(c, httpx.WithMessage("维护命令执行失败: "+err.Error()))
		return
	}
	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		httpx.Fail(c, httpx.WithMessage("解析维护报告失败: "+err.Error()))
		return
	}
	// 执行会改写对齐状态与索引：完成后丢弃环境健康读数缓存，页面随即读到新计数。
	if action == "apply" {
		invalidateEnvHealthCache(home)
	}
	httpx.Success(c, httpx.WithData(gin.H{"action": action, "report": report}))
}

// runMaintenancePruneCLI 执行一次 `sync prune --json` 子进程：预演只读，执行带
// `--apply --yes`；请求与诊断输出都有界，子进程环境剥离云端凭据（本地模式零外呼）。
func runMaintenancePruneCLI(ctx context.Context, bin, dbPath string, apply bool, limit int) ([]byte, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, errors.New("rolling-memory binary not found")
	}
	args := []string{"sync", "prune", "--db", dbPath, "--json"}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
	if apply {
		args = append(args, "--apply", "--yes")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	stdout := &engineCapture{limit: engineResponseLimit}
	stderr := &engineCapture{limit: engineStderrLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Env = stripCloudBrainEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return stdout.buffer.Bytes(), ctx.Err()
		}
		message := strings.TrimSpace(stderr.buffer.String())
		if message == "" {
			message = err.Error()
		}
		return stdout.buffer.Bytes(), errors.New(message)
	}
	if stdout.overflow {
		return stdout.buffer.Bytes(), errors.New("maintenance report exceeds the size limit")
	}
	return stdout.buffer.Bytes(), nil
}
