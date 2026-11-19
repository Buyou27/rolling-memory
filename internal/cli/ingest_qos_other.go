// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build !darwin && !linux

package cli

// applyIngestWorkerQoS is a no-op on platforms without a portable background
// priority call in this build.
func applyIngestWorkerQoS() {}
