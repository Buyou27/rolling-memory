// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
)

// DaemonMemoryLimitEnv is the standard Go soft memory limit variable. When it
// is set, the daemon leaves the value untouched so an operator override always
// wins over the configuration file.
const DaemonMemoryLimitEnv = "GOMEMLIMIT"

// daemonMemoryState reports the memory limit the running daemon resolved at
// startup, together with its origin, so the console can explain the number it
// shows instead of guessing.
type daemonMemoryState struct {
	StartedAt   time.Time
	TotalRAM    int64
	ConfigValue string
	LimitBytes  int64
	LimitSource string
}

var (
	daemonMemoryMu sync.Mutex
	daemonMemory   daemonMemoryState
)

// ApplyMemoryLimit resolves the configured daemon memory limit and applies it
// through the Go runtime. It returns the state that health responses report.
// A failed physical-memory probe leaves the limit untouched (fail-open): the
// daemon keeps running with the runtime default instead of receiving a guessed
// cap.
func ApplyMemoryLimit(cfg *config.Config) daemonMemoryState {
	configValue := config.MemoryLimitAuto
	if cfg != nil && cfg.Daemon.MemoryLimit != "" {
		configValue = cfg.Daemon.MemoryLimit
	}
	totalRAM := physicalMemoryBytes()
	state := daemonMemoryState{
		StartedAt:   time.Now(),
		TotalRAM:    totalRAM,
		ConfigValue: configValue,
	}
	if _, present := os.LookupEnv(DaemonMemoryLimitEnv); present {
		state.LimitSource = "env"
		if limit := debug.SetMemoryLimit(-1); limit > 0 {
			state.LimitBytes = limit
		}
	} else {
		resolved := config.ResolveMemoryLimit(configValue, totalRAM)
		state.LimitSource = resolved.Source
		if resolved.Bytes > 0 {
			debug.SetMemoryLimit(resolved.Bytes)
			state.LimitBytes = resolved.Bytes
		}
	}
	daemonMemoryMu.Lock()
	daemonMemory = state
	daemonMemoryMu.Unlock()
	return state
}

func currentDaemonMemoryState() daemonMemoryState {
	daemonMemoryMu.Lock()
	defer daemonMemoryMu.Unlock()
	return daemonMemory
}
