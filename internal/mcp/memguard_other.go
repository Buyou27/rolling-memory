// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build !darwin && !linux && !windows

package mcp

// physicalMemoryBytes reports total physical memory, or 0 when the probe is not
// implemented for this platform (the daemon then keeps the runtime default).
func physicalMemoryBytes() int64 { return 0 }
