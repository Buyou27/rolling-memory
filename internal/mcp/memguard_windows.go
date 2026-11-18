// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build windows

package mcp

import "golang.org/x/sys/windows"

// physicalMemoryBytes reports total physical memory, or 0 when the probe fails.
func physicalMemoryBytes() int64 {
	var status windows.MemoryStatusEx
	if err := windows.GlobalMemoryStatusEx(&status); err != nil {
		return 0
	}
	if status.TotalPhys == 0 {
		return 0
	}
	return int64(status.TotalPhys)
}
