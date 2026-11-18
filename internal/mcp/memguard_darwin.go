// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build darwin

package mcp

import "golang.org/x/sys/unix"

// physicalMemoryBytes reports total physical memory, or 0 when the probe fails.
func physicalMemoryBytes() int64 {
	value, err := unix.SysctlUint64("hw.memsize")
	if err != nil || value == 0 {
		return 0
	}
	return int64(value)
}
