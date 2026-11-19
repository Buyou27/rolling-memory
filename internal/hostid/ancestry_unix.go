//go:build darwin || linux

// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package hostid

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// readAncestry reads the parent chain through one ps snapshot. The call is
// made at most once per process and only when environment markers cannot name
// the host, so a short-lived sidecar pays one process spawn at startup.
func readAncestry() []Process {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return nil
	}
	parents := map[int]int{}
	commands := map[int]string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr != nil || ppidErr != nil {
			continue
		}
		parents[pid] = ppid
		if len(fields) > 2 {
			commands[pid] = strings.Join(fields[2:], " ")
		}
	}
	chain := make([]Process, 0, 12)
	current := os.Getppid()
	for i := 0; i < 12 && current > 1; i++ {
		command := commands[current]
		chain = append(chain, Process{PID: current, PPID: parents[current], Command: command})
		next := parents[current]
		if next <= 0 || next == current {
			break
		}
		current = next
	}
	return chain
}
