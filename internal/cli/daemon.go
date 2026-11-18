// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/mcp"
)

// daemonStartupBudget bounds explicit start/restart waits issued by the CLI.
const daemonStartupBudget = 30 * time.Second

// runDaemon implements `daemon status|start|stop|restart`, the explicit control
// surface for the shared local engine daemon. The console drives the same
// subcommands so both entry points share one implementation.
func (a *application) runDaemon() int {
	cfg, err := config.Load(config.LoadOptions{
		Path:  a.args.StringPtr("config"),
		DB:    a.args.StringPtr("db"),
		Mode:  a.args.StringPtr("mode"),
		Scope: a.args.StringPtr("scope"),
	})
	if err != nil {
		return a.fail(err)
	}
	if cfg.Mode != "local" {
		return a.fail(errors.New("daemon requires local mode and local_plain"))
	}
	sockPath := mcp.DefaultUnixSocketPath()
	action := a.args.Subcommand()
	if action == "" {
		action = "status"
	}
	switch action {
	case "status":
		return a.emitDaemonStatus(cfg, sockPath)
	case "start":
		if err := mcp.EnsureUnixDaemonContext(context.Background(), sockPath, cfg); err != nil {
			return a.fail(err)
		}
		return a.emitDaemonStatus(cfg, sockPath)
	case "stop":
		if err := mcp.ShutdownUnixDaemon(sockPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return a.fail(err)
			}
		}
		return a.emitDaemonStatus(cfg, sockPath)
	case "restart":
		if err := mcp.ShutdownUnixDaemon(sockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return a.fail(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), daemonStartupBudget)
		defer cancel()
		if err := mcp.EnsureUnixDaemonContext(ctx, sockPath, cfg); err != nil {
			return a.fail(err)
		}
		return a.emitDaemonStatus(cfg, sockPath)
	default:
		return a.fail(errors.New("daemon action must be one of status, start, stop, restart"))
	}
}

func (a *application) emitDaemonStatus(cfg *config.Config, sockPath string) int {
	result := map[string]any{
		"ok":                      true,
		"action":                  a.args.Subcommand(),
		"online":                  false,
		"socket":                  sockPath,
		"db":                      cfg.DB,
		"config":                  cfg.Path,
		"configured_memory_limit": cfg.Daemon.MemoryLimit,
	}
	if health, err := mcp.PingUnixDaemon(sockPath, daemonStartupBudget); err == nil && health != nil {
		result["online"] = true
		result["pid"] = health.PID
		result["config_id"] = health.ConfigID
		if expected, idErr := mcp.DaemonConfigID(cfg); idErr == nil {
			result["expected_config_id"] = expected
			result["config_matches"] = expected == health.ConfigID
		}
		result["uptime_sec"] = health.UptimeSec
		result["started_at"] = health.StartedAtUnix
		result["heap_alloc_bytes"] = health.HeapAllocBytes
		result["heap_sys_bytes"] = health.HeapSysBytes
		result["total_ram_bytes"] = health.TotalRAMBytes
		result["mem_limit_bytes"] = health.MemLimitBytes
		result["mem_limit_source"] = health.MemLimitSource
		result["mem_limit_config"] = health.MemLimitConfig
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		result["health_error"] = err.Error()
	}
	return a.emitDaemonResult(result)
}

func (a *application) emitDaemonResult(result map[string]any) int {
	if err := a.emit(result, func() { a.printDaemonStatus(result) }); err != nil {
		return a.fail(err)
	}
	return 0
}

func (a *application) printDaemonStatus(result map[string]any) {
	if online, _ := result["online"].(bool); online {
		a.printf("daemon: running (pid %v, uptime %vs)\n", result["pid"], result["uptime_sec"])
		a.printf("socket: %v\n", result["socket"])
		a.printf("memory: heap %v / sys %v, limit %v (%v)\n",
			result["heap_alloc_bytes"], result["heap_sys_bytes"], result["mem_limit_bytes"], result["mem_limit_source"])
		return
	}
	a.printf("daemon: not running\n")
	a.printf("socket: %v\n", result["socket"])
	a.printf("configured memory limit: %v\n", result["configured_memory_limit"])
}
