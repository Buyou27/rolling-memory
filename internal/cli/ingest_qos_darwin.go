// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build darwin

package cli

import "golang.org/x/sys/unix"

// applyIngestWorkerQoS puts the scan worker and its builder into macOS
// background scheduling, so foreground recall gets the CPU and I/O first.
func applyIngestWorkerQoS() {
	_ = unix.Setpriority(unix.PRIO_DARWIN_BG, 0, 0)
}
