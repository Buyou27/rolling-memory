// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"context"
	"fmt"
	"time"
)

// 版本更新实时帧：缓存文件变化时主动推帧。前端不轮询、不重复发 HTTP；
// 缓存缺失或过期时由本帧生产者触发一次后台检查，检查结果写入同一缓存后
// 再由下一次巡检推给所有订阅者。
const updateStreamInterval = 2 * time.Second

func newUpdateStreamHub(key streamKey, home string) *streamHub {
	return newStreamHub(key, updateStreamInterval,
		streamFrameProducer{event: "update", produce: func(context.Context) (string, any, bool) {
			state, stale := updateStateStale(home)
			if stale {
				triggerUpdateCheckAsync(home, "")
			}
			payload := map[string]any{
				"ok":             state != nil,
				"stale":          stale,
				"checking":       stale && updateCheckIsRunning(),
				"install_method": installMethodFromDisk(home),
			}
			for field, value := range state {
				payload[field] = value
			}
			signature := fmt.Sprintf("%t|%t|%t|%t|%s|%s|%s",
				state != nil,
				stateUpdateAvailable(state),
				stale,
				updateCheckIsRunning(),
				stringField(state, "latest_tag"),
				stringField(state, "error"),
				stringField(state, "checked_at"),
			)
			return signature, payload, true
		}},
	)
}

func stringField(state map[string]any, field string) string {
	if state == nil {
		return ""
	}
	value, _ := state[field].(string)
	return value
}
