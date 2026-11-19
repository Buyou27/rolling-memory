//go:build !darwin && !linux

// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package hostid

// readAncestry is unavailable on platforms without a supported process
// ancestry reader. Environment markers remain the only identity source.
func readAncestry() []Process {
	return nil
}
