//go:build darwin || linux

// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func unixDaemonPeerVerificationAvailable() bool { return true }

func unixDaemonFileOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func unixDaemonLock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func unixDaemonLockBusy(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)
}

func unixDaemonConnectionRefused(err error) bool { return errors.Is(err, unix.ECONNREFUSED) }
