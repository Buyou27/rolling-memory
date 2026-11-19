// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build linux

package cli

import "golang.org/x/sys/unix"

// applyIngestWorkerQoS lowers the worker to nice 19, the portable Linux way to
// yield CPU to foreground recall.
func applyIngestWorkerQoS() {
	_ = unix.Setpriority(unix.PRIO_PROCESS, 0, 19)
}
