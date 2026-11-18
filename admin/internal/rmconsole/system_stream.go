// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"context"
	"time"
)

// 记忆服务实时帧：守护进程运行时读数（PID/运行时长/堆内存/生效上限/配置指纹）。
// 前端不做任何定时 HTTP 请求，房间在有人订阅时按固定节拍采样并通过同源 WebSocket 推送；
// 控制动作（启动/停止/重启）由 HTTP 端点立即返回，下一次帧随即收敛。
const (
	// systemStreamInterval 是实时帧的采样与推送节拍；只影响控制台展示的新鲜度，
	// 不影响守护进程本身。
	systemStreamInterval = 5 * time.Second
	// systemSampleTimeout 限定单次 `daemon status` 采样的整体时长。
	systemSampleTimeout = 5 * time.Second
)

// newSystemStreamHub 装配记忆服务流：`system` 帧按固定节拍推全量运行时读数。
func newSystemStreamHub(key streamKey, home string) *streamHub {
	bin := resolveBinary(home)
	dbPath := resolveMemoryDBPath(home)
	return newStreamHub(key, systemStreamInterval,
		streamFrameProducer{event: "system", alwaysPush: true, minInterval: systemStreamInterval, produce: func(ctx context.Context) (string, any, bool) {
			return "", systemFramePayload(ctx, bin, dbPath), true
		}},
	)
}

// systemFramePayload 采样一次守护进程状态并组装实时帧载荷；采样失败时返回
// 可解释的失败帧（ok/online=false + health_error），由页面保留最后一次成功读数。
func systemFramePayload(ctx context.Context, bin, dbPath string) map[string]any {
	sampleCtx, cancel := context.WithTimeout(ctx, systemSampleTimeout)
	defer cancel()
	payload := map[string]any{
		"sampled_at":    time.Now().Unix(),
		"sampled_at_ms": time.Now().UnixMilli(),
	}
	report, err := daemonReport(sampleCtx, bin, dbPath, "status")
	if err != nil {
		payload["ok"] = false
		payload["online"] = false
		payload["health_error"] = err.Error()
		return payload
	}
	for k, v := range report {
		payload[k] = v
	}
	return payload
}
