// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// runScan implements `scan pause|resume|status`, the explicit control surface
// for the background worker. Pausing stops scan passes, snapshot builds and
// deferred crystallization without touching foreground recall.
func (a *application) runScan() int {
	a.setEnvFromArgs()
	memory, err := a.openMemory()
	if err != nil {
		return a.fail(err)
	}
	defer func() { _ = memory.Close() }()
	action := a.args.Subcommand()
	if action == "" {
		action = "status"
	}
	db := memory.Store.DB()
	switch action {
	case "pause":
		if err := SetEpisodicSyncPaused(db, true); err != nil {
			return a.fail(err)
		}
	case "resume":
		if err := SetEpisodicSyncPaused(db, false); err != nil {
			return a.fail(err)
		}
	case "status":
	default:
		return a.fail(errUnexpectedScanAction(action))
	}
	result := map[string]any{
		"ok":        true,
		"action":    action,
		"auto_sync": IsAutoSyncEnabled(memory.Config.Path),
		"paused":    IsEpisodicSyncPaused(db, memory.Config.Path),
	}
	if action == "status" {
		if status, statusErr := memory.Status(memory.Config.Scope); statusErr == nil {
			result["snapshot"] = status["snapshot"]
		}
		result["worker"] = readScanWorkerLease(db)
		result["builder"] = readSnapshotBuildStatus(db)
	}
	if err := a.emit(result, func() {
		a.printf("scan: %s (auto_sync=%v paused=%v)\n", action, result["auto_sync"], result["paused"])
		if snapshot, ok := result["snapshot"].(map[string]any); ok {
			a.printf("snapshot: ready=%v change_seq=%v built=%v gap=%v\n",
				snapshot["ready"], snapshot["change_seq"], snapshot["built_change_seq"], snapshot["change_gap"])
		}
	}); err != nil {
		return a.fail(err)
	}
	return 0
}

func errUnexpectedScanAction(action string) error {
	return &scanActionError{action: action}
}

type scanActionError struct{ action string }

func (e *scanActionError) Error() string {
	return "unknown scan action: " + e.action + " (use pause, resume or status)"
}

// readScanWorkerLease reports the durable ingest worker lease without starting
// a worker. It is diagnostic only; liveness is the TTL, not a PID probe.
func readScanWorkerLease(db *sql.DB) map[string]any {
	out := map[string]any{"present": false, "live": false, "pid": int64(0)}
	if db == nil {
		return out
	}
	var raw string
	if err := db.QueryRow(`SELECT CAST(v AS TEXT) FROM rm_meta WHERE k = ?`, ingestWorkerLeaseKey).Scan(&raw); err != nil {
		return out
	}
	out["present"] = true
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 3)
	if len(parts) >= 2 {
		if pid, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			out["pid"] = pid
		}
		out["live"] = ingestLeaseLive(raw, time.Now(), ingestWorkerLeaseTTL)
	}
	var rawStatus []byte
	if err := db.QueryRow(`SELECT v FROM rm_meta WHERE k = ?`, ingestWorkerStatusKey).Scan(&rawStatus); err == nil {
		var status map[string]any
		if json.Unmarshal(rawStatus, &status) == nil {
			out["status"] = status
		}
	}
	return out
}

// readSnapshotBuildStatus reports the last builder outcome (failures, last
// error, blocked) even when no worker is running.
func readSnapshotBuildStatus(db *sql.DB) map[string]any {
	out := map[string]any{"present": false}
	if db == nil {
		return out
	}
	var raw []byte
	if err := db.QueryRow(`SELECT v FROM rm_meta WHERE k = ?`, snapshotBuildStatusKey).Scan(&raw); err != nil {
		return out
	}
	out["present"] = true
	var status map[string]any
	if json.Unmarshal(raw, &status) == nil {
		out["status"] = status
	}
	return out
}
