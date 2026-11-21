// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build darwin || linux

package core

import (
	"os"
	"syscall"
)

func lockBlockStoreHandle(handle *os.File) error {
	return syscall.Flock(int(handle.Fd()), syscall.LOCK_EX)
}

func unlockBlockStoreHandle(handle *os.File) error {
	return syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
}
