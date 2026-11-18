// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// 空间维护面（磁盘体检 / 旧副本与缓存清理 / 金库瘦身）全部走产品 CLI 的 `maintain` 正典路径：
// 扫描口径、保留策略、守护进程有序停启与回执文件都由引擎实现承担，控制台只做参数校验与
// 报告透传，绝不自研第二套清理逻辑。status 与不带 --apply 的动作都是只读预演；执行必须
// 显式确认（confirm），保留份数在服务端钳位，破坏性的宽松口径只留在 CLI。
const (
	// maintenanceSpaceStatusTimeout 限定一次只读体检的整体时长。
	maintenanceSpaceStatusTimeout = 60 * time.Second
	// maintenanceSpaceCleanTimeout 限定一次清理（扫描 + 删除）的整体时长。
	maintenanceSpaceCleanTimeout = 5 * time.Minute
	// maintenanceSpaceCompactTimeout 限定一次金库瘦身（有序停机 → VACUUM → 复位）的整体时长。
	maintenanceSpaceCompactTimeout = 20 * time.Minute
	// maintenanceSpaceKeepCeiling 是备份保留份数的服务端上限。
	maintenanceSpaceKeepCeiling = 20
)

func init() {
	registry.RegisterAdmin(func(admin *gin.RouterGroup) {
		g := admin.Group("plugin/" + Name)
		g.Use(limitConsoleRequestBody())
		g.Use(consoleVaultScopeGuard())
		g.POST("/maintenance/space", middleware.AdminAuth(), middleware.AdminPermission(), maintenanceSpaceHandler)
	})
}

type maintenanceSpaceRequest struct {
	Action  string `json:"action"`  // status | clean | compact
	Apply   bool   `json:"apply"`   // false = 只读预演
	Confirm bool   `json:"confirm"` // apply 必须显式确认
	Keep    int    `json:"keep"`    // 备份保留份数（0 = 引擎默认，服务端钳位 ≤ ceiling）
	Caches  bool   `json:"caches"`  // clean 是否连可再生成缓存一起清
}

// normalizeMaintenanceSpaceAction 归一维护动作；空值与未知值都拒绝（status 也要求显式）。
func normalizeMaintenanceSpaceAction(raw string) (string, error) {
	action := strings.ToLower(strings.TrimSpace(raw))
	if action == "" {
		return "status", nil
	}
	switch action {
	case "status", "clean", "compact":
		return action, nil
	}
	return "", fmt.Errorf("未知维护动作: %s；仅支持 status / clean / compact", raw)
}

// clampMaintenanceSpaceKeep 把保留份数钳位到 [0, ceiling]；0 表示沿用引擎默认。
func clampMaintenanceSpaceKeep(keep int) int {
	if keep < 0 {
		return 0
	}
	if keep > maintenanceSpaceKeepCeiling {
		return maintenanceSpaceKeepCeiling
	}
	return keep
}

// maintenanceSpaceArgs 生成一次 `maintain` 调用的完整参数（与 CLI 契约逐位对应）。
func maintenanceSpaceArgs(action string, apply bool, keep int, caches bool, dbPath string) []string {
	args := []string{"maintain", action, "--db", dbPath, "--json"}
	if action == "clean" {
		if keep > 0 {
			args = append(args, "--keep", strconv.Itoa(keep))
		}
		if caches {
			args = append(args, "--caches")
		}
	}
	if apply {
		args = append(args, "--apply", "--yes")
	}
	return args
}

// maintenanceSpaceHandler 运行一次空间维护：status / clean / compact，apply 需显式确认。
func maintenanceSpaceHandler(c *gin.Context) {
	var req maintenanceSpaceRequest
	if !bindJSON(c, &req) {
		return
	}
	action, err := normalizeMaintenanceSpaceAction(req.Action)
	if err != nil {
		httpx.Fail(c, httpx.WithMessage(err.Error()))
		return
	}
	if action == "status" && req.Apply {
		httpx.Fail(c, httpx.WithMessage("status 为只读体检，不接受 apply"))
		return
	}
	if req.Apply && !req.Confirm {
		httpx.Fail(c, httpx.WithMessage("执行维护需要显式确认"))
		return
	}
	keep := clampMaintenanceSpaceKeep(req.Keep)
	timeout := maintenanceSpaceStatusTimeout
	switch action {
	case "clean":
		timeout = maintenanceSpaceCleanTimeout
	case "compact":
		timeout = maintenanceSpaceCompactTimeout
	}
	home, _ := os.UserHomeDir()
	bin := resolveBinary(home)
	if bin == "" {
		httpx.Fail(c, httpx.WithMessage("未找到 rolling-memory 引擎二进制"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	raw, err := runMaintenanceSpaceCLI(ctx, bin, maintenanceSpaceArgs(action, req.Apply, keep, req.Caches, resolveMemoryDBPath(home)))
	if err != nil {
		httpx.Fail(c, httpx.WithMessage("维护命令执行失败: "+err.Error()))
		return
	}
	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		httpx.Fail(c, httpx.WithMessage("解析维护报告失败: "+err.Error()))
		return
	}
	// 执行会改写磁盘占用与缓存：完成后丢弃环境健康读数缓存，页面随即读到新计数。
	if req.Apply {
		invalidateEnvHealthCache(home)
	}
	httpx.Success(c, httpx.WithData(gin.H{"action": action, "apply": req.Apply, "keep": keep, "report": report}))
}

// runMaintenanceSpaceCLI 执行一次 `maintain` 子进程并返回其 JSON 报告：
// 请求与诊断输出都有界，子进程环境剥离云端凭据（本地模式零外呼）。
func runMaintenanceSpaceCLI(ctx context.Context, bin string, args []string) ([]byte, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, errors.New("rolling-memory binary not found")
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
