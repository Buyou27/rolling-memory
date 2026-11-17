//go:build !darwin && !linux

// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"errors"
	"net"
	"os"
)

func unixDaemonPeerVerificationAvailable() bool { return false }

func unixDaemonFileOwned(os.FileInfo) bool { return false }

func unixDaemonLock(*os.File) error {
	return errors.New("daemon locking is unavailable on this platform")
}

func unixDaemonLockBusy(error) bool { return false }

func unixDaemonConnectionRefused(error) bool { return false }

func verifyUnixDaemonPeer(net.Conn) (int, error) {
	return 0, errors.New("daemon peer verification is unavailable on this platform")
}
