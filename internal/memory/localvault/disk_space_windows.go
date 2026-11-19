// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build windows

package localvault

// diskFreeBytes is unavailable in this portable build; the builder skips the
// free-space guard instead of guessing.
func diskFreeBytes(string) (int64, error) {
	return 0, nil
}
