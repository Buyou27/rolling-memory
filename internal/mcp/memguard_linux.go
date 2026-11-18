// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build linux

package mcp

import (
	"os"
	"strconv"
	"strings"
)

// physicalMemoryBytes reports total physical memory, or 0 when the probe fails.
func physicalMemoryBytes() int64 {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kilobytes <= 0 {
			return 0
		}
		return kilobytes * 1024
	}
	return 0
}
