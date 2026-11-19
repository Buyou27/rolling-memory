// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

func verifyUnixDaemonPeer(conn net.Conn) (int, error) {
	pid, err := verifyUnixDaemonPeerOwner(conn)
	if err != nil {
		return 0, err
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	selfInfo, selfErr := os.Stat(exe)
	peerInfo, peerErr := os.Stat("/proc/" + strconv.Itoa(pid) + "/exe")
	if selfErr != nil || peerErr != nil || !os.SameFile(selfInfo, peerInfo) {
		return 0, errUnixDaemonExecutableMismatch
	}
	return pid, nil
}

// verifyUnixDaemonPeerOwner checks the socket's owner and returns its PID
// without requiring the peer to run the same executable. The shutdown path uses
// it to authenticate a stale daemon before asking it to step down.
func verifyUnixDaemonPeerOwner(conn net.Conn) (int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a Unix socket")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var peerErr error
	if err := raw.Control(func(fd uintptr) {
		cred, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if peerErr != nil || cred == nil || cred.Pid <= 0 || cred.Uid != uint32(os.Geteuid()) {
		return 0, fmt.Errorf("daemon peer credentials unavailable or mismatched: %v", peerErr)
	}
	return int(cred.Pid), nil
}
