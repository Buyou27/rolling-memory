// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func verifyUnixDaemonPeer(conn net.Conn) (int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a Unix socket")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var peerErr error
	if err := raw.Control(func(fd uintptr) {
		var cred *unix.Xucred
		cred, peerErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if peerErr != nil {
			return
		}
		if cred.Uid != uint32(os.Geteuid()) {
			peerErr = errors.New("daemon owner mismatch")
			return
		}
		pid, peerErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); err != nil {
		return 0, err
	}
	if peerErr != nil || pid <= 0 {
		return 0, fmt.Errorf("daemon peer credentials unavailable: %v", peerErr)
	}
	peerData, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(peerData) <= 4 {
		return 0, fmt.Errorf("daemon executable unavailable: %v", err)
	}
	selfData, err := unix.SysctlRaw("kern.procargs2", os.Getpid())
	if err != nil || len(selfData) <= 4 {
		return 0, fmt.Errorf("caller executable unavailable: %v", err)
	}
	peerEnd, selfEnd := bytes.IndexByte(peerData[4:], 0), bytes.IndexByte(selfData[4:], 0)
	if peerEnd <= 0 || selfEnd <= 0 {
		return 0, errors.New("daemon executable path unavailable")
	}
	selfPath := string(selfData[4 : 4+selfEnd])
	peerPath := string(peerData[4 : 4+peerEnd])
	selfInfo, selfErr := os.Stat(selfPath)
	peerInfo, peerErr := os.Stat(peerPath)
	if selfErr != nil || peerErr != nil || !os.SameFile(selfInfo, peerInfo) {
		return 0, errors.New("daemon executable mismatch")
	}
	return pid, nil
}
