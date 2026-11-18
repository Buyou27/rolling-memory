// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/episodic"
	"github.com/rocareer/rolling-memory/internal/episodicunit"
	"github.com/rocareer/rolling-memory/internal/harness"
	"github.com/rocareer/rolling-memory/internal/ids"
	"github.com/rocareer/rolling-memory/internal/mcp"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
)

const reconcileDefaultLimit = 50

// HarnessEnvItem describes one coding harness environment on this machine.
type HarnessEnvItem struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Detected       bool   `json:"detected"`
	Active         bool   `json:"active"`
	SkillWired     bool   `json:"skill_wired"`
	MCPWired       bool   `json:"mcp_wired"`
	HooksWired     bool   `json:"hooks_wired"`
	HooksSupported bool   `json:"hooks_supported"`
	// SessionsScanSupported reports whether this build can discover the
	// harness's local session store, so the panel can show its session count.
	SessionsScanSupported bool `json:"sessions_scan_supported"`
	// TurnsScanSupported is false for harnesses this build can discover sessions
	// for but cannot read turn content from (compressed or unsupported transcript
	// schema): their 0/0 turn counts mean "not counted", not "no history".
	TurnsScanSupported bool   `json:"turns_scan_supported"`
	SessionsCount      int    `json:"sessions_count"`
	LocalTurnsCount    int    `json:"local_turns_count"`
	SyncedTurnsCount   int    `json:"synced_turns_count"`
	UnsyncedTurnsCount int    `json:"unsynced_turns_count"`
	LastActiveAt       int64  `json:"last_active_at"`
	LastActiveText     string `json:"last_active_text"`
}

// HealthCheckSubItem is one dimension of the Memory Health score.
type HealthCheckSubItem struct {
	Key      string `json:"key"`
	LabelZH  string `json:"label_zh"`
	LabelEN  string `json:"label_en"`
	Status   string `json:"status"` // "healthy" | "warning" | "error"
	Score    int    `json:"score"`
	MaxScore int    `json:"max_score"`
	DetailZH string `json:"detail_zh"`
	DetailEN string `json:"detail_en"`
}

// LayerAlignmentStat holds the local vs remote cloud alignment state for one cognitive layer.
type LayerAlignmentStat struct {
	Layer                string `json:"layer"`
	LocalVaultCount      int    `json:"local_vault_count"`
	RemoteCloudCount     int    `json:"remote_cloud_count"`
	SyncedCount          int    `json:"synced_count"`
	LocalMissingOnRemote int    `json:"local_missing_on_remote"`
	RemoteOnlyCount      int    `json:"remote_only_count"`
	HarnessUnsyncedTurns int    `json:"harness_unsynced_turns,omitempty"`
	MountedDirsCount     int    `json:"mounted_dirs_count,omitempty"`
	TrackedFilesCount    int    `json:"tracked_files_count,omitempty"`
	DirtyFilesCount      int    `json:"dirty_files_count,omitempty"`
	StaleCount           int    `json:"stale_count,omitempty"`
	TotalPendingSync     int    `json:"total_pending_sync"`
	AlignmentPct         int    `json:"alignment_pct"`
	AlignmentKnown       bool   `json:"alignment_known"`
	Aligned              bool   `json:"aligned"`
}

// EpisodicIndexStatus reports local index coverage and live scan/backfill progress without reading source transcripts.
type EpisodicIndexStatus struct {
	Compatible       int     `json:"compatible"`
	Incompatible     int     `json:"incompatible"`
	Missing          int     `json:"missing"`
	Corrupt          int     `json:"corrupt"`
	NeedsRebuild     int     `json:"needs_rebuild"`
	Orphaned         int     `json:"orphaned"`
	TotalDocs        int     `json:"total_docs"`
	CoveragePct      float64 `json:"coverage_pct"`
	Paused           bool    `json:"paused"`
	Running          bool    `json:"running"`
	BackfillCursor   string  `json:"backfill_cursor,omitempty"`
	ScanSource       string  `json:"scan_source,omitempty"`
	ScanIndex        int     `json:"scan_index,omitempty"`
	SourceItemErrors int     `json:"source_item_errors,omitempty"`
	Error            string  `json:"error,omitempty"`
}

// EnvHealthAndSyncStatus is the unified payload for `rolling-memory sync status --json`
// and `/admin/plugin/rolling-memory/env/health`.
type EnvHealthAndSyncStatus struct {
	ActiveHarnessID    string                        `json:"active_harness_id"`
	ActiveHarnessLabel string                        `json:"active_harness_label"`
	ActiveWorkspace    string                        `json:"active_workspace"`
	ActiveSessionID    string                        `json:"active_session_id"`
	DetectionSource    string                        `json:"detection_source"`
	Mode               string                        `json:"mode"` // local | cloud：本地模式远端读数恒为 0（零外呼）
	Harnesses          []HarnessEnvItem              `json:"harnesses"`
	CloudOnline        bool                          `json:"cloud_online"`
	CloudLatencyMS     int                           `json:"cloud_latency_ms"`
	CloudEndpoint      string                        `json:"cloud_endpoint"`
	CloudUserID        string                        `json:"cloud_user_id"`
	HealthScore        int                           `json:"health_score"`
	HealthGrade        string                        `json:"health_grade"` // "healthy" | "subhealthy" | "degraded"
	HealthChecks       []HealthCheckSubItem          `json:"health_checks"`
	Layers             map[string]LayerAlignmentStat `json:"layers"`
	EpisodicIndex      *EpisodicIndexStatus          `json:"episodic_index,omitempty"`
	TotalPendingSync   int                           `json:"total_pending_sync"`
	UpdatedAt          string                        `json:"updated_at"`
}

// ReconcileReport is the execution receipt for `rolling-memory sync reconcile --json`
// and `/admin/plugin/rolling-memory/sync/reconcile`.
type ReconcileReport struct {
	Layer                        string                 `json:"layer"`
	TargetHarness                string                 `json:"target_harness"`
	EpisodicImportedFromHar      int                    `json:"episodic_imported_from_harness"`
	EpisodicIndexesBackfilled    int                    `json:"episodic_indexes_backfilled"`
	EpisodicIndexScanLimit       int                    `json:"episodic_index_scan_limit_per_harness"`
	EpisodicIndexScanned         int                    `json:"episodic_index_scanned"`
	EpisodicIndexSourceChanged   int                    `json:"episodic_index_source_changed"`
	EpisodicIndexPointerMismatch int                    `json:"episodic_index_pointer_mismatch"`
	EpisodicIndexUnverified      int                    `json:"episodic_index_unverified"`
	EpisodicIndexCompatible      int                    `json:"episodic_index_compatible"`
	EpisodicIndexIncompatible    int                    `json:"episodic_index_incompatible"`
	EpisodicIndexMissing         int                    `json:"episodic_index_missing"`
	EpisodicIndexCorrupt         int                    `json:"episodic_index_corrupt"`
	EpisodicIndexNeedsRebuild    int                    `json:"episodic_index_needs_rebuild"`
	EpisodicPushedToRemote       int                    `json:"episodic_pushed_to_remote"`
	BackgroundSyncedFiles        int                    `json:"background_synced_files"`
	BackgroundPushedToRemote     int                    `json:"background_pushed_to_remote"`
	NoeticPushedToRemote         int                    `json:"noetic_pushed_to_remote"`
	TotalReconciled              int                    `json:"total_reconciled"`
	ElapsedMS                    int                    `json:"elapsed_ms"`
	Warnings                     []string               `json:"warnings,omitempty"`
	Status                       EnvHealthAndSyncStatus `json:"status"`
}

// candidateEpisodicTurn represents one extracted User+Assistant conversational turn from a harness.
type candidateEpisodicTurn struct {
	HarnessID  string
	SessionID  string
	Locus      string
	LocusKnown bool
	DocID      string
	RefPointer string
	// TurnRef is the provenance of the unit (`@ref:zcode:...` / `@ref:jsonl:...`,
	// without the `#<k>` segment suffix). Content-addressed units can be said in
	// several turns; the lineage table keeps every occurrence.
	TurnRef    string
	Text       string
	OccurredAt int64
	// Completed requires an explicit successful completion event.
	Completed bool
}

func (a *application) runSyncCommand(memory *facade.Memory) (int, error) {
	sub := a.args.Subcommand()
	switch sub {
	case "status":
		// 控制台采集路径可经环境变量打开进度事件通道（stderr JSONL），CLI 直用保持静默。
		if beginStatusProgress() {
			defer endStatusProgress()
		}
		harnessFilter := strings.TrimSpace(a.args.String("harness"))
		st, err := inspectEnvHealthAndSync(context.Background(), memory, harnessFilter)
		if err != nil {
			return 1, err
		}
		return 0, a.emit(st, func() {
			active := "none detected"
			if st.ActiveHarnessID != "" {
				active = fmt.Sprintf("%s (%s)", st.ActiveHarnessLabel, st.ActiveHarnessID)
			}
			a.printf("active harness: %s · health: %d%% (%s) · pending sync: %d\n",
				active, st.HealthScore, st.HealthGrade, st.TotalPendingSync)
			if idx := st.EpisodicIndex; idx != nil {
				if idx.Error != "" {
					a.printf("episodic index: unavailable (%s)\n", idx.Error)
				} else {
					stateLabel := "idle"
					if idx.Paused {
						stateLabel = "paused"
					} else if idx.Running {
						stateLabel = "running"
					}
					a.printf("episodic index (%s, %.1f%%): %d/%d compatible · %d missing · %d incompatible · %d corrupt · %d need rebuild\n",
						stateLabel, idx.CoveragePct, idx.Compatible, idx.TotalDocs, idx.Missing, idx.Incompatible, idx.Corrupt, idx.NeedsRebuild)
					if idx.BackfillCursor != "" || idx.ScanSource != "" {
						a.printf("cursors: backfill=%s · scan=%s#%d\n", idx.BackfillCursor, idx.ScanSource, idx.ScanIndex)
					}
				}
			}
		})
	case "reconcile":
		if a.args.Flag("rebuild_episodic") {
			report, err := rebuildEpisodicUnits(context.Background(), memory, a.args.Flag("yes"))
			if err != nil {
				return 1, err
			}
			return 0, a.emit(report, func() {
				a.printf("episodic rebuild estimate: %d units → %d segments · +%d · unmatched %d · %s\n",
					report.OldUnits, report.NewUnits, report.UnitsAdded, report.UnitsRemoved, report.Mode)
				for _, warning := range report.Warnings {
					a.printf("warning: %s\n", warning)
				}
			})
		}
		layer := strings.ToLower(strings.TrimSpace(a.args.String("layer")))
		if layer == "" {
			layer = "all"
		}
		harnessFilter := strings.TrimSpace(a.args.String("harness"))
		limit := a.args.Int("limit")
		if limit <= 0 {
			limit = reconcileDefaultLimit
		}
		rep, err := executeReconcileSync(context.Background(), memory, layer, harnessFilter, limit)
		if err != nil {
			return 1, err
		}
		return 0, a.emit(rep, func() {
			if strings.EqualFold(strings.TrimSpace(rep.Status.Mode), "local") {
				a.printf("reconcile (%s, local_plain): harness +%d · background files +%d · health %d%%\n",
					rep.Layer, rep.EpisodicImportedFromHar, rep.BackgroundSyncedFiles, rep.Status.HealthScore)
				a.printf("episodic index: +%d backfilled · %d compatible · %d missing · %d incompatible · %d corrupt · %d need rebuild\n",
					rep.EpisodicIndexesBackfilled, rep.EpisodicIndexCompatible, rep.EpisodicIndexMissing, rep.EpisodicIndexIncompatible, rep.EpisodicIndexCorrupt, rep.EpisodicIndexNeedsRebuild)
				for _, warning := range rep.Warnings {
					a.printf("warning: %s\n", warning)
				}
				return
			}
			a.printf("reconcile (%s): episodic +%d (harness +%d) · background +%d · noetic +%d · health %d%%\n",
				rep.Layer, rep.EpisodicPushedToRemote, rep.EpisodicImportedFromHar,
				rep.BackgroundPushedToRemote, rep.NoeticPushedToRemote, rep.Status.HealthScore)
		})
	case "prune":
		report, err := pruneEpisodicUnitsWithOptions(context.Background(), memory, EpisodicPruneOptions{
			Limit:               a.args.Int("limit"),
			Apply:               a.args.Flag("apply"),
			Confirmed:           a.args.Flag("yes"),
			IncludeUnreadable:   a.args.Flag("include_unreadable"),
			ReanchorCrossSource: a.args.Flag("reanchor_cross_source"),
		})
		if err != nil {
			return 1, err
		}
		return 0, a.emit(report, func() { a.printEpisodicPrune(report) })
	case "watch":
		if a.args.Flag("pause") {
			if memory == nil || memory.Store == nil || memory.Store.DB() == nil {
				return 1, fmt.Errorf("sync watch --pause requires an open local store")
			}
			if err := SetEpisodicSyncPaused(memory.Store.DB(), true); err != nil {
				return 1, err
			}
			st, err := inspectEnvHealth(context.Background(), memory, "", false)
			if err != nil {
				return 1, err
			}
			return 0, a.emit(map[string]any{"ok": true, "paused": true, "episodic_index": st.EpisodicIndex}, func() {
				a.printf("sync paused (cursors preserved)\n")
			})
		}
		if a.args.Flag("resume") {
			if memory == nil || memory.Store == nil || memory.Store.DB() == nil {
				return 1, fmt.Errorf("sync watch --resume requires an open local store")
			}
			if err := SetEpisodicSyncPaused(memory.Store.DB(), false); err != nil {
				return 1, err
			}
			st, err := inspectEnvHealth(context.Background(), memory, "", false)
			if err != nil {
				return 1, err
			}
			return 0, a.emit(map[string]any{"ok": true, "paused": false, "episodic_index": st.EpisodicIndex}, func() {
				a.printf("sync resumed\n")
			})
		}
		if a.args.Flag("step") {
			if memory == nil || memory.Store == nil || memory.Store.DB() == nil {
				return 1, fmt.Errorf("sync watch --step requires an open local store")
			}
			stepCtx, cancelStep := context.WithTimeout(context.Background(), autoWatchReconcileBudget)
			var backfillRep thinclient.EpisodicIndexBackfillReport
			var backfillErr error
			for stepCtx.Err() == nil {
				pageRep, pErr := runEpisodicIndexBackfillTick(stepCtx, memory)
				backfillRep.Cursor = pageRep.Cursor
				backfillRep.Scanned += pageRep.Scanned
				backfillRep.Indexed += pageRep.Indexed
				backfillRep.Unchanged += pageRep.Unchanged
				backfillRep.Unavailable += pageRep.Unavailable
				backfillRep.Unverified += pageRep.Unverified
				backfillRep.Failed += pageRep.Failed
				if pErr != nil {
					backfillErr = pErr
					break
				}
				if pageRep.Scanned < reconcileDefaultLimit || pageRep.Cursor == "" {
					break
				}
			}
			cancelStep()
			home, _ := os.UserHomeDir()
			var scanErr error
			if home != "" {
				scanScope := config.DefaultScope
				if memory.Config != nil && strings.TrimSpace(memory.Config.Scope) != "" {
					scanScope = strings.TrimSpace(memory.Config.Scope)
				}
				scanKey := "episodic_source_scan_cursor:" + scanScope
				scanCtx, cancelScan := context.WithTimeout(context.Background(), autoWatchReconcileBudget)
				for scanCtx.Err() == nil {
					var beforeCursor []byte
					_ = memory.Store.DB().QueryRow(`SELECT v FROM thin_vault_meta WHERE k = ?`, scanKey).Scan(&beforeCursor)
					sErr := runEpisodicSourceScanTick(scanCtx, memory, home)
					if sErr != nil {
						scanErr = sErr
						break
					}
					var afterCursor []byte
					_ = memory.Store.DB().QueryRow(`SELECT v FROM thin_vault_meta WHERE k = ?`, scanKey).Scan(&afterCursor)
					if string(afterCursor) == string(beforeCursor) || len(afterCursor) == 0 || string(afterCursor) == "{}" || strings.Contains(string(afterCursor), `"source":""`) {
						break
					}
				}
				cancelScan()
			}
			// 只愈合巡检：漂移的情景指针（含跨轮次重锚）随「推进一轮」一并修复；
			// 确证不存在的行留给 `sync prune --apply --yes`，本步永不删除。
			healCtx, cancelHeal := context.WithTimeout(context.Background(), autoWatchReconcileBudget)
			healScanned, healRepaired, healErr := healDriftedEpisodicPointers(healCtx, memory, episodicHealStepLimit)
			cancelHeal()
			if healRepaired > 0 && memory.Config != nil && mcp.ShouldUseUnixDaemon(memory.Config.DB) {
				// 跨进程写：通知常驻守护进程丢弃内存快照，面板与 MCP 立刻看到重锚后的坐标。
				mcp.InvalidateUnixDaemonCaches("")
			}
			st, _ := inspectEnvHealth(context.Background(), memory, "", false)
			var warnings []string
			if backfillErr != nil && !errors.Is(backfillErr, context.DeadlineExceeded) && !strings.Contains(backfillErr.Error(), "checkpoint changed") {
				warnings = append(warnings, backfillErr.Error())
			}
			if scanErr != nil && !errors.Is(scanErr, context.DeadlineExceeded) && !strings.Contains(scanErr.Error(), "checkpoint changed") {
				warnings = append(warnings, scanErr.Error())
			}
			if healErr != nil && !errors.Is(healErr, context.DeadlineExceeded) {
				warnings = append(warnings, healErr.Error())
			}
			out := map[string]any{
				"ok":             true,
				"stepped":        true,
				"backfill":       backfillRep,
				"episodic_index": st.EpisodicIndex,
				"heal": map[string]any{
					"scanned":  healScanned,
					"repaired": healRepaired,
				},
				"warnings": warnings,
			}
			return 0, a.emit(out, func() {
				a.printf("sync step: backfilled +%d (scanned %d) · cursor=%s\n", backfillRep.Indexed, backfillRep.Scanned, backfillRep.Cursor)
				if healScanned > 0 {
					a.printf("sync step: pointer heal scanned %d · repaired %d\n", healScanned, healRepaired)
				}
			})
		}
		intervalSec := a.args.Int("interval_sec")
		if intervalSec <= 0 {
			intervalSec = 10
		}
		if a.args.Flag("once") {
			ran, bgChunks, epiPushed := RunAutoWatchTick(context.Background(), memory, bgSettleWindow, episodicQuiescenceWindow)
			out := map[string]any{
				"ok":                true,
				"once":              true,
				"ran":               ran,
				"background_chunks": bgChunks,
				"episodic_pushed":   epiPushed,
			}
			return 0, a.emit(out, func() {
				a.printf("sync watch (once): ran=%v · background +%d · episodic +%d\n", ran, bgChunks, epiPushed)
			})
		}
		RunAutoWatchLoop(context.Background(), memory, time.Duration(intervalSec)*time.Second)
		return 0, nil
	default:
		return 1, fmt.Errorf("unknown sync subcommand %q (expected status, reconcile or watch)", sub)
	}
}

func inspectEnvHealthAndSync(ctx context.Context, memory *facade.Memory, harnessFilter string) (EnvHealthAndSyncStatus, error) {
	return inspectEnvHealth(ctx, memory, harnessFilter, true)
}

// resolveSyncTransport 初始化并返回薄客户端及本地金库传输面（纯本地模式零外呼，cloudFace 恒为 false）。
func resolveSyncTransport(memory *facade.Memory) (*thinclient.Client, bool, error) {
	tc, err := memory.ThinClient()
	if err != nil {
		return nil, false, fmt.Errorf("init thinclient failed: %w", err)
	}
	if tc == nil {
		return nil, false, fmt.Errorf("init thinclient failed: no client returned")
	}
	return tc, false, nil
}

// cloudFaceWarning 报告「配置声明云端但实际仅有本地面」的降级事实（本地对齐照常执行）。
func cloudFaceWarning(memory *facade.Memory) string {
	if memory == nil || memory.Config == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(memory.Config.Mode), "cloud") {
		return "cloud mode configured but no cloud transport bound (missing credentials or endpoint): reconciled locally only"
	}
	return ""
}

// inspectEnvHealth builds the health/sync snapshot. withTurnCounts selects whether
// harness transcripts are scanned for turn counts: `sync status` needs them, while a
// background reconcile pass only needs wiring and per-layer sets and must not pay a
// full five-harness transcript scan on every tick.
func inspectEnvHealth(ctx context.Context, memory *facade.Memory, harnessFilter string, withTurnCounts bool) (EnvHealthAndSyncStatus, error) {
	home, _ := os.UserHomeDir()
	scope := config.DefaultScope
	if memory != nil && memory.Config != nil && strings.TrimSpace(memory.Config.Scope) != "" {
		scope = strings.TrimSpace(memory.Config.Scope)
	}

	out := EnvHealthAndSyncStatus{
		Harnesses:    []HarnessEnvItem{},
		HealthChecks: []HealthCheckSubItem{},
		Layers:       map[string]LayerAlignmentStat{},
		UpdatedAt:    time.Now().Format("2006-01-02 15:04:05"),
	}

	// 1. Load local SQLite vault unit sets
	localEpisodic := map[string]bool{}
	deletedEpisodic := map[string]bool{}
	localBackground := map[string]bool{}
	localNoeticActive := map[string]bool{}
	localNoeticStaleCount := 0
	mountedDirsCount := 0
	trackedFilesCount := 0
	dirtyFilesCount := 0
	var healthDB *sql.DB

	endVaultStage := stageStart(progressStageVault, nil)
	if memory != nil && memory.Store != nil && memory.Store.DB() != nil {
		db := memory.Store.DB()
		healthDB = db
		if rows, err := db.Query(`SELECT doc_id, COALESCE(layer, 'episodic') FROM thin_vault_docs`); err == nil {
			for rows.Next() {
				var id, lyr string
				if rows.Scan(&id, &lyr) == nil && strings.TrimSpace(id) != "" {
					l := strings.ToLower(strings.TrimSpace(lyr))
					if l == "background" {
						localBackground[id] = true
					} else if l != "noetic" {
						localEpisodic[id] = true
					}
				}
			}
			rows.Close()
		}
		if rows, err := db.Query(`SELECT unit_id FROM thin_vault_deleted_units WHERE layer = 'episodic' OR layer = ''`); err == nil {
			for rows.Next() {
				var uid string
				if rows.Scan(&uid) == nil && strings.TrimSpace(uid) != "" {
					deletedEpisodic[strings.TrimSpace(uid)] = true
				}
			}
			rows.Close()
		}
		if rows, err := db.Query(`SELECT unit_id FROM thin_vault_bg_chunks`); err == nil {
			for rows.Next() {
				var uid string
				if rows.Scan(&uid) == nil && strings.TrimSpace(uid) != "" {
					localBackground[uid] = true
				}
			}
			rows.Close()
		}
		if rows, err := db.Query(`SELECT noetic_id, status FROM thin_vault_noetic`); err == nil {
			for rows.Next() {
				var nid, st string
				if rows.Scan(&nid, &st) == nil && strings.TrimSpace(nid) != "" {
					if strings.EqualFold(strings.TrimSpace(st), "active") {
						localNoeticActive[nid] = true
					} else {
						localNoeticStaleCount++
					}
				}
			}
			rows.Close()
		}
		_ = db.QueryRow(`SELECT COUNT(*) FROM thin_vault_bg_dirs`).Scan(&mountedDirsCount)
		_ = db.QueryRow(`SELECT COUNT(*) FROM thin_vault_bg_files`).Scan(&trackedFilesCount)
		dirtyFilesCount = countDirtyBackgroundFiles(db)
	}
	endVaultStage(map[string]any{
		"episodic": len(localEpisodic), "background": len(localBackground),
		"noetic": len(localNoeticActive), "dirty_files": dirtyFilesCount,
	})

	// 2. Detect Harness environments and scan local conversation turns
	endWiringStage := stageStart(progressStageWiring, nil)
	activeID, activeWS, activeSess, detectSrc, presenceAt := detectActiveHarness(home)
	// An explicit `--harness` filter is a soft scan/display scope (the console passes
	// the last presence-file host to keep its capture single-harness fast); it narrows
	// the inventory but is not recency evidence, so it can never outvote a fresher
	// transcript into reporting a stale host as active.
	displayFilter := ""
	if harnessFilter != "" && harnessFilter != "all" {
		displayFilter = harnessFilter
	}
	listID := activeID
	if displayFilter != "" {
		listID = displayFilter
	}
	defs := harnessEnvDefinitions(home, listID)
	checkReport := harness.Check(defs, harness.InstallOptions{
		Scope: "user",
		Hooks: true,
		Home:  home,
	}, false)
	wiredMap := map[string]map[string]bool{}
	for _, act := range checkReport.Actions {
		if wiredMap[act.Harness] == nil {
			wiredMap[act.Harness] = map[string]bool{}
		}
		if act.Status == harness.StatusOK {
			wiredMap[act.Harness][act.Component] = true
		}
	}

	endWiringStage(map[string]any{"active": activeID})

	totalHarnessUnsynced := 0
	harnessAlignmentKnown := withTurnCounts
	endScanStage := stageStart(progressStageScan, map[string]any{"harnesses": len(defs)})

	for _, d := range defs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		_, statErr := os.Stat(d.Detect)
		detected := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			harnessAlignmentKnown = false
		}
		w := wiredMap[d.ID]
		sessionsCnt, turns, lastTS := 0, []candidateEpisodicTurn(nil), int64(0)
		syncedCnt, unsyncedCnt, localTurnsCnt := 0, 0, 0
		if withTurnCounts {
			var complete bool
			sessionsCnt, turns, lastTS, complete = scanHarnessTurnsWithCoverageContext(ctx, home, d.ID, scope, 2000)
			if !complete {
				harnessAlignmentKnown = false
			}
			for _, turn := range turns {
				if localEpisodic[turn.DocID] || deletedEpisodic[turn.DocID] {
					syncedCnt++
				} else {
					unsyncedCnt++
				}
			}
			localTurnsCnt = len(turns)
			emitStatusProgress(progressStageScan, "step", map[string]any{
				"harness": d.ID, "phase": "done", "cached": false,
				"local_turns": localTurnsCnt, "synced": syncedCnt, "unsynced": unsyncedCnt,
			})
		}
		totalHarnessUnsynced += unsyncedCnt
		if (detected || d.ID == activeID) && !harnessTurnsScannable(d.ID) {
			harnessAlignmentKnown = false
		}
		lastTxt := "-"
		if lastTS > 0 {
			lastTxt = time.Unix(lastTS, 0).Format("2006-01-02 15:04:05")
		}
		// INSTALLED lights on real footprint evidence: a state directory survives
		// an uninstall and can be created by our own wiring, so bare directory
		// existence is not proof the host is still here. Session activity, the
		// host's own app/launcher, or the active pick qualify.
		installed := detected && (sessionsCnt > 0 || lastTS > 0 || d.ID == activeID || harness.AppPresent(d.ID, home))
		out.Harnesses = append(out.Harnesses, HarnessEnvItem{
			ID:                    d.ID,
			Label:                 d.Label,
			Detected:              installed,
			SkillWired:            w["skill"],
			MCPWired:              w["mcp"],
			HooksWired:            w["hooks"],
			HooksSupported:        d.HooksKind != "",
			SessionsScanSupported: harnessSessionsScannable(d.ID),
			TurnsScanSupported:    harnessTurnsScannable(d.ID),
			SessionsCount:         sessionsCnt,
			LocalTurnsCount:       localTurnsCnt,
			SyncedTurnsCount:      syncedCnt,
			UnsyncedTurnsCount:    unsyncedCnt,
			LastActiveAt:          lastTS,
			LastActiveText:        lastTxt,
		})
	}

	// Active harness = strongest recency evidence (live env / presence file write
	// time / transcript recency); zero evidence reports "none" instead of a
	// fabricated default, and a claim whose id has no registry entry is dropped.
	winnerID, winnerSrc := resolveActiveHarness(
		out.Harnesses,
		harnessClaims(out.Harnesses, activeID, detectSrc, presenceAt, time.Now().Unix()),
	)
	var activeItem *HarnessEnvItem
	for i := range out.Harnesses {
		if winnerID == "" || out.Harnesses[i].ID != winnerID {
			continue
		}
		out.Harnesses[i].Active = true
		cp := out.Harnesses[i]
		activeItem = &cp
	}
	endScanStage(map[string]any{"unsynced": totalHarnessUnsynced})

	out.ActiveHarnessID = winnerID
	out.ActiveHarnessLabel = ""
	if activeItem != nil {
		out.ActiveHarnessLabel = activeItem.Label
	} else if winnerID != "" {
		out.ActiveHarnessLabel = winnerID
	}
	out.ActiveWorkspace = activeWS
	out.ActiveSessionID = activeSess
	out.DetectionSource = winnerSrc

	// 3. Probe Cloud Engine & fetch remote unit sets via /open/memory/v1/units。
	// 云面判定以实际传输为准：只有绑定 HTTP 传输（云端配置或显式端点覆盖）才探针与取远端全集；
	// 本地金库传输 = 零外呼硬边界，远端读数恒为 0。
	endIndexStage := stageStart(progressStageIndex, nil)
	tc, cloudFace, err := resolveSyncTransport(memory)
	if err != nil {
		return out, err
	}
	localMode := !cloudFace
	if tc != nil && tc.Vault != nil {
		cfgPath := ""
		if memory != nil && memory.Config != nil {
			cfgPath = memory.Config.Path
		}
		status := &EpisodicIndexStatus{
			Paused: IsEpisodicSyncPaused(healthDB, cfgPath),
		}
		coverage, err := tc.Vault.EpisodicIndexFaultDiagnostics()
		if err != nil {
			status.Error = err.Error()
		} else {
			status.Compatible = coverage.Compatible
			status.Incompatible = coverage.Incompatible
			status.Missing = coverage.Missing
			status.Corrupt = coverage.Corrupt
			status.NeedsRebuild = coverage.NeedsRebuild
			status.Orphaned = coverage.Orphaned
			status.TotalDocs = coverage.Compatible + coverage.Incompatible + coverage.Missing + coverage.Corrupt + coverage.NeedsRebuild
			if status.TotalDocs > 0 {
				status.CoveragePct = float64(status.Compatible*1000/status.TotalDocs) / 10.0
			} else {
				status.CoveragePct = 100.0
			}
		}
		if healthDB != nil {
			if err := healthDB.QueryRow(`SELECT CAST(v AS INTEGER) FROM thin_vault_meta WHERE k = ?`, "episodic_source_scan_item_errors:"+scope).Scan(&status.SourceItemErrors); err != nil && !errors.Is(err, sql.ErrNoRows) {
				status.Error = err.Error()
			}
			_ = healthDB.QueryRow(`SELECT CAST(v AS TEXT) FROM thin_vault_meta WHERE k = ?`, "episodic_index_backfill_cursor:"+scope).Scan(&status.BackfillCursor)
			var scanRaw []byte
			scanErr := healthDB.QueryRow(`SELECT v FROM thin_vault_meta WHERE k = ?`, "episodic_source_scan_cursor:"+scope).Scan(&scanRaw)
			if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
				status.Error = scanErr.Error()
			} else if scanErr == nil && len(scanRaw) > 0 {
				var cur episodicSourceCursor
				if err := json.Unmarshal(scanRaw, &cur); err != nil {
					status.Error = err.Error()
				} else {
					if cur.RetrySources {
						harnessAlignmentKnown = false
					}
					if cur.ItemErrors > status.SourceItemErrors {
						status.SourceItemErrors = cur.ItemErrors
					}
					parts := strings.Split(cur.Source, "\x00")
					if len(parts) >= 3 && parts[2] != "" {
						status.ScanSource = parts[0] + ":" + filepath.Base(parts[2])
						if len(parts) >= 4 && parts[3] != "" {
							status.ScanSource += ":" + parts[3]
						}
					} else {
						status.ScanSource = cur.Source
					}
					status.ScanIndex = cur.Index
				}
			}
			var leaseVal string
			if healthDB.QueryRow(`SELECT CAST(v AS TEXT) FROM thin_vault_meta WHERE k = 'autowatch_lease'`).Scan(&leaseVal) == nil && strings.Contains(leaseVal, ":") {
				parts := strings.Split(leaseVal, ":")
				if len(parts) >= 2 {
					if exp, pErr := strconv.ParseInt(parts[1], 10, 64); pErr == nil && exp >= time.Now().Unix() && !status.Paused {
						status.Running = true
					}
				}
			}
		}
		out.EpisodicIndex = status
	}
	indexFields := map[string]any{}
	if out.EpisodicIndex != nil {
		indexFields["compatible"] = out.EpisodicIndex.Compatible
		indexFields["missing"] = out.EpisodicIndex.Missing
		indexFields["coverage_pct"] = out.EpisodicIndex.CoveragePct
	}
	endIndexStage(indexFields)
	remoteEpisodic := map[string]bool{}
	remoteBackground := map[string]bool{}
	remoteNoetic := map[string]bool{}
	out.Mode = "local"
	out.CloudEndpoint = "local://vault"

	// 4. Compute per-layer alignment stats
	epiStat := computeLayerDiff("episodic", localEpisodic, remoteEpisodic, out.CloudOnline, localMode, totalHarnessUnsynced, 0)
	if !harnessAlignmentKnown || (out.EpisodicIndex != nil && (out.EpisodicIndex.SourceItemErrors > 0 || out.EpisodicIndex.Missing > 0 || out.EpisodicIndex.Corrupt > 0 || out.EpisodicIndex.NeedsRebuild > 0 || out.EpisodicIndex.Error != "")) {
		epiStat.AlignmentKnown = false
		epiStat.Aligned = false
	}
	bgStat := computeLayerDiff("background", localBackground, remoteBackground, out.CloudOnline, localMode, 0, dirtyFilesCount)
	bgStat.MountedDirsCount = mountedDirsCount
	bgStat.TrackedFilesCount = trackedFilesCount
	bgStat.DirtyFilesCount = dirtyFilesCount

	noeStat := computeLayerDiff("noetic", localNoeticActive, remoteNoetic, out.CloudOnline, localMode, 0, 0)
	noeStat.StaleCount = localNoeticStaleCount

	out.Layers["episodic"] = epiStat
	out.Layers["background"] = bgStat
	out.Layers["noetic"] = noeStat
	out.TotalPendingSync = epiStat.TotalPendingSync + bgStat.TotalPendingSync + noeStat.TotalPendingSync

	// 5. Compute 5-dimension Memory Health Score (0-100)
	totalScore := 0

	// (1) Cloud Engine (25 pts) / Local Vault (25 pts · 本地模式)
	cScore := 0
	cStatus := "error"
	cDetailZH := "云端记忆引擎未连接或不可达"
	cDetailEN := "Cloud memory engine unreachable"
	if localMode {
		cScore = 25
		cStatus = "healthy"
		cDetailZH = fmt.Sprintf("本地明文金库 (local_plain) · 零云端请求 · scope=%s", out.CloudEndpoint)
		cDetailEN = fmt.Sprintf("Local plaintext vault (local_plain) · zero cloud requests · scope=%s", out.CloudEndpoint)
	} else if out.CloudOnline {
		cScore = 25
		cStatus = "healthy"
		cDetailZH = fmt.Sprintf("在线 · 延迟 %dms · 命名空间 user_id=%s", out.CloudLatencyMS, out.CloudUserID)
		cDetailEN = fmt.Sprintf("Online · %dms · user_id=%s", out.CloudLatencyMS, out.CloudUserID)
	}
	totalScore += cScore
	engineLabelZH := "云端引擎连通性"
	engineLabelEN := "Cloud Engine Connectivity"
	if localMode {
		engineLabelZH = "本地金库可用性 (local_plain)"
		engineLabelEN = "Local Vault Availability (local_plain)"
	}
	out.HealthChecks = append(out.HealthChecks, HealthCheckSubItem{
		Key: "cloud_engine", LabelZH: engineLabelZH, LabelEN: engineLabelEN,
		Status: cStatus, Score: cScore, MaxScore: 25, DetailZH: cDetailZH, DetailEN: cDetailEN,
	})

	// (2) Active Harness Wiring (15 pts)
	hScore := 0
	hStatus := "warning"
	hDetailZH := fmt.Sprintf("当前环境：%s", out.ActiveHarnessLabel)
	hDetailEN := fmt.Sprintf("Active harness: %s", out.ActiveHarnessLabel)
	if activeItem != nil {
		if activeItem.SkillWired {
			hScore += 5
		}
		if activeItem.MCPWired {
			hScore += 5
		}
		if !activeItem.HooksSupported || activeItem.HooksWired {
			hScore += 5
		}
		if hScore == 15 {
			hStatus = "healthy"
		}
		hDetailZH = fmt.Sprintf("%s · 静态配置 Skill:%v · MCP:%v · Hooks:%v（未验证宿主运行）",
			activeItem.Label, activeItem.SkillWired, activeItem.MCPWired, activeItem.HooksWired)
		hDetailEN = fmt.Sprintf("%s · Configured Skill:%v · MCP:%v · Hooks:%v (host runtime not verified)",
			activeItem.Label, activeItem.SkillWired, activeItem.MCPWired, activeItem.HooksWired)
	}
	totalScore += hScore
	out.HealthChecks = append(out.HealthChecks, HealthCheckSubItem{
		Key: "harness_wiring", LabelZH: "宿主与钩子静态配置", LabelEN: "Harness & Hooks Configuration",
		Status: hStatus, Score: hScore, MaxScore: 15, DetailZH: hDetailZH, DetailEN: hDetailEN,
	})

	// (3) L2 Episodic Alignment (25 pts)
	eScore := (epiStat.AlignmentPct * 25) / 100
	eStatus := "healthy"
	if epiStat.TotalPendingSync > 0 || !epiStat.AlignmentKnown {
		if eScore > 20 {
			eScore = 20
		}
		eStatus = "warning"
	}
	eDetailZH := fmt.Sprintf("本地金库 %d · 远端 %d · 宿主待导入对话 %d 轮 · 远端待补齐 %d",
		epiStat.LocalVaultCount, epiStat.RemoteCloudCount, epiStat.HarnessUnsyncedTurns, epiStat.LocalMissingOnRemote)
	eDetailEN := fmt.Sprintf("Local %d · Remote %d · Unsynced turns %d · Remote missing %d",
		epiStat.LocalVaultCount, epiStat.RemoteCloudCount, epiStat.HarnessUnsyncedTurns, epiStat.LocalMissingOnRemote)
	if localMode {
		eDetailZH = fmt.Sprintf("本地金库 %d · 宿主待导入对话 %d 轮（本地模式无远端）",
			epiStat.LocalVaultCount, epiStat.HarnessUnsyncedTurns)
		eDetailEN = fmt.Sprintf("Local %d · Unsynced turns %d (remote disabled in local mode)",
			epiStat.LocalVaultCount, epiStat.HarnessUnsyncedTurns)
	}
	if !epiStat.AlignmentKnown {
		eDetailZH += " · 部分宿主或历史范围未确认"
		eDetailEN += " · Some hosts or history are not verified"
	}
	totalScore += eScore
	out.HealthChecks = append(out.HealthChecks, HealthCheckSubItem{
		Key: "episodic_sync", LabelZH: "L2 Episodic 历史对话对齐", LabelEN: "L2 Episodic Dialogue Sync",
		Status: eStatus, Score: eScore, MaxScore: 25, DetailZH: eDetailZH, DetailEN: eDetailEN,
	})

	// (4) L1 Background Alignment (20 pts)
	bScore := (bgStat.AlignmentPct * 20) / 100
	bStatus := "healthy"
	if bgStat.TotalPendingSync > 0 {
		if bScore > 16 {
			bScore = 16
		}
		bStatus = "warning"
	}
	bDetailZH := fmt.Sprintf("挂载目录 %d · 本地分块 %d · 远端 %d · 待同步 %d",
		bgStat.MountedDirsCount, bgStat.LocalVaultCount, bgStat.RemoteCloudCount, bgStat.TotalPendingSync)
	bDetailEN := fmt.Sprintf("Dirs %d · Local chunks %d · Remote %d · Pending %d",
		bgStat.MountedDirsCount, bgStat.LocalVaultCount, bgStat.RemoteCloudCount, bgStat.TotalPendingSync)
	if localMode {
		bDetailZH = fmt.Sprintf("挂载目录 %d · 本地分块 %d · 待处理文件 %d（本地模式无远端）",
			bgStat.MountedDirsCount, bgStat.LocalVaultCount, bgStat.TotalPendingSync)
		bDetailEN = fmt.Sprintf("Dirs %d · Local chunks %d · Pending files %d (remote disabled in local mode)",
			bgStat.MountedDirsCount, bgStat.LocalVaultCount, bgStat.TotalPendingSync)
	}
	totalScore += bScore
	out.HealthChecks = append(out.HealthChecks, HealthCheckSubItem{
		Key: "background_sync", LabelZH: "L1 Background 背景知识对齐", LabelEN: "L1 Background Corpus Sync",
		Status: bStatus, Score: bScore, MaxScore: 20, DetailZH: bDetailZH, DetailEN: bDetailEN,
	})

	// (5) L3 Noetic Health (15 pts)
	nScore := (noeStat.AlignmentPct * 15) / 100
	nStatus := "healthy"
	if noeStat.TotalPendingSync > 0 || noeStat.StaleCount > 0 {
		if nScore > 12 {
			nScore = 12
		}
		nStatus = "warning"
	}
	nDetailZH := fmt.Sprintf("活跃印迹 %d · 远端 %d · 待补投 %d · 失效(Stale) %d",
		noeStat.LocalVaultCount, noeStat.RemoteCloudCount, noeStat.LocalMissingOnRemote, noeStat.StaleCount)
	nDetailEN := fmt.Sprintf("Active %d · Remote %d · Pending %d · Stale %d",
		noeStat.LocalVaultCount, noeStat.RemoteCloudCount, noeStat.LocalMissingOnRemote, noeStat.StaleCount)
	if localMode {
		nDetailZH = fmt.Sprintf("活跃印迹 %d · 失效(Stale) %d（本地模式无远端）",
			noeStat.LocalVaultCount, noeStat.StaleCount)
		nDetailEN = fmt.Sprintf("Active %d · Stale %d (remote disabled in local mode)",
			noeStat.LocalVaultCount, noeStat.StaleCount)
	}
	totalScore += nScore
	out.HealthChecks = append(out.HealthChecks, HealthCheckSubItem{
		Key: "noetic_sync", LabelZH: "L3 Noetic 知性印迹健康度", LabelEN: "L3 Noetic Imprint Health",
		Status: nStatus, Score: nScore, MaxScore: 15, DetailZH: nDetailZH, DetailEN: nDetailEN,
	})

	out.HealthScore = totalScore
	switch {
	case totalScore >= 85:
		out.HealthGrade = "healthy"
	case totalScore >= 60:
		out.HealthGrade = "subhealthy"
	default:
		out.HealthGrade = "degraded"
	}

	return out, nil
}

func computeLayerDiff(layer string, localSet, remoteSet map[string]bool, cloudOnline, localMode bool, extraHarnessUnsynced, dirtyFiles int) LayerAlignmentStat {
	synced := 0
	missingOnRemote := 0
	remoteOnly := 0
	if cloudOnline && !localMode {
		for id := range localSet {
			if remoteSet[id] {
				synced++
			} else {
				missingOnRemote++
			}
		}
		for id := range remoteSet {
			if !localSet[id] {
				remoteOnly++
			}
		}
	} else {
		// 本地模式（或云端不可达）：无远端可对齐，等待项只剩宿主未导入与待处理文件。
		synced = len(localSet)
	}
	pending := missingOnRemote + extraHarnessUnsynced + dirtyFiles
	denom := len(localSet) + extraHarnessUnsynced + dirtyFiles
	pct := 100
	if denom > 0 {
		pct = (synced * 100) / denom
		if pct > 100 {
			pct = 100
		}
	}
	aligned := pending == 0
	if !localMode {
		aligned = cloudOnline && pending == 0
	}
	remoteCount := len(remoteSet)
	if localMode {
		remoteCount = 0
	}
	return LayerAlignmentStat{
		Layer:                layer,
		LocalVaultCount:      len(localSet),
		RemoteCloudCount:     remoteCount,
		SyncedCount:          synced,
		LocalMissingOnRemote: missingOnRemote,
		RemoteOnlyCount:      remoteOnly,
		HarnessUnsyncedTurns: extraHarnessUnsynced,
		TotalPendingSync:     pending,
		AlignmentPct:         pct,
		AlignmentKnown:       localMode || cloudOnline,
		Aligned:              aligned,
	}
}

// episodicTurnGroup is one frozen-parse batch of the reconcile save section: every
// turn whose pointer names the same JSONL source, in the order the scanned page
// produced them.
type episodicTurnGroup struct {
	path  string
	turns []candidateEpisodicTurn
}

// groupEpisodicTurnsBySource buckets scanned turns by the JSONL transcript their
// pointer names, in the order the sources first appear, so one frozen parse per
// source can drive every Verified save of that source's turns. Turns whose
// pointers are not JSONL refs (zcode, empty) fall into a pathless group and keep
// the live save path.
func groupEpisodicTurnsBySource(turns []candidateEpisodicTurn) []episodicTurnGroup {
	if len(turns) == 0 {
		return nil
	}
	groups := make([]episodicTurnGroup, 0, 4)
	slotByPath := make(map[string]int, 4)
	for _, turn := range turns {
		path := episodicTurnSourcePath(turn.RefPointer)
		slot, known := slotByPath[path]
		if !known {
			slot = len(groups)
			slotByPath[path] = slot
			groups = append(groups, episodicTurnGroup{path: path})
		}
		groups[slot].turns = append(groups[slot].turns, turn)
	}
	return groups
}

// episodicTurnSourcePath extracts the transcript path of one JSONL pointer
// (`@ref:jsonl:<abs path>:<turn>#<k>`), empty when the pointer is not a JSONL ref.
// The suffix grammar is the one the ingest writes: the path runs up to the colon
// before the turn index.
func episodicTurnSourcePath(refPointer string) string {
	trimmed := strings.TrimSpace(refPointer)
	if !strings.HasPrefix(trimmed, "@ref:jsonl:") {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, "@ref:jsonl:")
	if colon := strings.LastIndex(rest, ":"); colon > 0 {
		return rest[:colon]
	}
	return ""
}

func executeReconcileSync(ctx context.Context, memory *facade.Memory, layer, harnessFilter string, limit int) (ReconcileReport, error) {
	t0 := time.Now()
	if limit <= 0 {
		limit = reconcileDefaultLimit
	}
	rep := ReconcileReport{
		Layer:         layer,
		TargetHarness: harnessFilter,
	}
	if memory == nil || memory.Store == nil || memory.Store.DB() == nil {
		return rep, fmt.Errorf("reconcile requires an open local SQLite memory store")
	}

	tc, _, err := resolveSyncTransport(memory)
	if err != nil {
		return rep, err
	}

	home, _ := os.UserHomeDir()
	scope := config.DefaultScope
	if memory.Config != nil && strings.TrimSpace(memory.Config.Scope) != "" {
		scope = strings.TrimSpace(memory.Config.Scope)
	}
	// 纯本地零外呼硬边界：只补齐本地（宿主对话导入 + 挂载目录分块），不推远端、
	// 不等云端可达；远端计数恒为 0。
	if warning := cloudFaceWarning(memory); warning != "" {
		rep.Warnings = append(rep.Warnings, warning)
	}

	// Pre-fetch current status so we know which harness is active and which units are missing on remote.
	// A background pass (caller supplied a deadline) skips the transcript turn counting:
	// it must stay inside the auto-watch tick and only needs wiring + remote unit sets.
	_, backgroundPass := ctx.Deadline()
	preStatus, err := inspectEnvHealth(ctx, memory, harnessFilter, !backgroundPass)
	if err != nil {
		return rep, err
	}
	targetHar := harnessFilter
	if targetHar == "" || targetHar == "all" {
		targetHar = preStatus.ActiveHarnessID
	}
	rep.TargetHarness = targetHar
	db := memory.Store.DB()

	// 1. Reconcile L2 Episodic (Harness history into local vault)
	if layer == "all" || layer == "episodic" {
		localEpi := map[string]bool{}
		deletedEpi := tc.Vault.DeletedUnitSet("episodic")
		storedRaw := map[string]string{}
		pointerDocs := map[string][]string{}
		unverified := map[string]bool{}
		if rows, qErr := db.Query(`SELECT doc_id, raw_text FROM thin_vault_docs WHERE layer = 'episodic' OR layer = ''`); qErr == nil {
			for rows.Next() {
				var id, raw string
				if rows.Scan(&id, &raw) == nil && id != "" {
					localEpi[id] = true
					storedRaw[id] = raw
					if strings.HasPrefix(raw, "@ref:") {
						pointerDocs[raw] = append(pointerDocs[raw], id)
						unverified[id] = true
					}
				}
			}
			rows.Close()
		}

		// Step 1a: bring harness turns into the local vault. Local import is a zero-copy
		// pointer write (no network, no plaintext duplication, and it resolves live on
		// read), so every turn the harness already finished is visible to local
		// recall/recent on this very pass.
		var harnessIDs []string
		if harnessFilter == "all" {
			for _, h := range preStatus.Harnesses {
				if harnessTurnsScannable(h.ID) {
					harnessIDs = append(harnessIDs, h.ID)
				}
			}
		} else if targetHar != "" {
			harnessIDs = []string{targetHar}
		}

		// Scan breadth: a background pass (caller set a deadline) only needs the newest
		// window because local import converges over successive ticks; a foreground run
		// keeps the full window so one command can drain a backlog. Both walk newest-first.
		scanCap := 2000
		if backgroundPass {
			scanCap = max(limit*4, limit)
		}
		rep.EpisodicIndexScanLimit = scanCap
		rep.Warnings = append(rep.Warnings, "episodic index backfill covers only the scanned harness window; unavailable or unscanned historical sources are unknown")
		changedSources := map[string]bool{}
		for _, hid := range harnessIDs {
			_, turns, _, complete := scanHarnessTurnsWithCoverageContext(ctx, home, hid, scope, scanCap)
			if !complete {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: incomplete or unreadable episodic source coverage", hid))
			}
			if err := ctx.Err(); err != nil {
				return rep, err
			}
			rep.EpisodicIndexScanned += len(turns)
			sort.SliceStable(turns, func(i, j int) bool { return turns[i].OccurredAt > turns[j].OccurredAt })
			for _, group := range groupEpisodicTurnsBySource(turns) {
				var snapshot *episodic.SourceSnapshot
				if group.path != "" {
					if parsed, snapErr := episodic.NewSourceSnapshotContext(ctx, group.path); snapErr == nil {
						snapshot = parsed
					}
				}
				for _, turn := range group.turns {
					if err := ctx.Err(); err != nil {
						return rep, err
					}
					if !turn.Completed {
						continue
					}
					if episodicunit.DocID(scope, turn.Text) != turn.DocID {
						rep.Warnings = append(rep.Warnings, fmt.Sprintf("episodic index %s: content identity mismatch", turn.DocID))
						continue
					}
					if deletedEpi[turn.DocID] {
						continue
					}
					vault := tc.Vault.CloneWithSharedSlotCache()
					vault.SetIdentity(thinclient.VaultIdentity{AssistantID: turn.HarnessID, IncarnationID: turn.SessionID, Locus: turn.Locus})
					for _, id := range pointerDocs[turn.RefPointer] {
						if id != turn.DocID && !changedSources[id] {
							changedSources[id] = true
							rep.EpisodicIndexSourceChanged++
						}
					}
					if localEpi[turn.DocID] {
						stored, ok := storedRaw[turn.DocID]
						if !ok {
							if err := db.QueryRow(`SELECT raw_text FROM thin_vault_docs WHERE doc_id = ?`, turn.DocID).Scan(&stored); err != nil {
								rep.Warnings = append(rep.Warnings, fmt.Sprintf("episodic index %s: %v", turn.DocID, err))
								continue
							}
							storedRaw[turn.DocID] = stored
						}
						if strings.HasPrefix(stored, "@ref:") {
							if stored != turn.RefPointer {
								healed := false
								if strings.TrimSpace(turn.Text) != "" {
									if sErr := vault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, turn.DocID, turn.OccurredAt, turn.RefPointer, turn.TurnRef, turn.Text, thinclient.IngestResponse{}); sErr == nil {
										var current string
										if err := db.QueryRowContext(ctx, `SELECT raw_text FROM thin_vault_docs WHERE doc_id = ?`, turn.DocID).Scan(&current); err != nil {
											return rep, err
										}
										storedRaw[turn.DocID] = current
										if current == turn.RefPointer {
											delete(unverified, turn.DocID)
										}
										healed = true
									} else if errors.Is(sErr, thinclient.ErrEpisodicUnitDeleted) {
										deletedEpi[turn.DocID] = true
										continue
									} else if !errors.Is(sErr, thinclient.ErrEpisodicIndexItem) && !errors.Is(sErr, thinclient.ErrEpisodicPointerSourceConflict) {
										return rep, sErr
									}
								}
								if !healed {
									rep.EpisodicIndexPointerMismatch++
								}
							} else if strings.TrimSpace(turn.Text) != "" {
								created, err := vault.EnsureLocalEpisodicIndexVerified(scope, snapshot, turn.DocID, turn.RefPointer, turn.Text)
								if err != nil {
									if errors.Is(err, thinclient.ErrEpisodicUnitDeleted) {
										deletedEpi[turn.DocID] = true
										continue
									}
									if !errors.Is(err, thinclient.ErrEpisodicIndexItem) {
										return rep, err
									}
									rep.Warnings = append(rep.Warnings, fmt.Sprintf("episodic index %s: %v", turn.DocID, err))
								} else {
									delete(unverified, turn.DocID)
									if created {
										rep.EpisodicIndexesBackfilled++
									}
									if _, err := db.ExecContext(ctx, `UPDATE thin_vault_docs SET assistant_id = ?, incarnation_id = ?, locus = CASE WHEN ? THEN ? ELSE locus END, occurred_at = ? WHERE doc_id = ? AND raw_text = ? AND NOT EXISTS (SELECT 1 FROM thin_vault_deleted_units WHERE unit_id = thin_vault_docs.doc_id AND (layer = 'episodic' OR layer = '')) AND (COALESCE(assistant_id, '') != ? OR COALESCE(incarnation_id, '') != ? OR (? AND COALESCE(locus, '') != ?) OR occurred_at != ?)`, turn.HarnessID, turn.SessionID, turn.LocusKnown, turn.Locus, turn.OccurredAt, turn.DocID, turn.RefPointer, turn.HarnessID, turn.SessionID, turn.LocusKnown, turn.Locus, turn.OccurredAt); err != nil {
										return rep, err
									}
								}
							}
						}
						continue
					}
					if strings.TrimSpace(turn.Text) == "" || turn.RefPointer == "" {
						continue
					}
					if err := vault.SaveLocalEpisodicPointerIndexedVerified(scope, snapshot, turn.DocID, turn.OccurredAt, turn.RefPointer, turn.TurnRef, turn.Text, thinclient.IngestResponse{}); err != nil {
						if errors.Is(err, thinclient.ErrEpisodicUnitDeleted) {
							deletedEpi[turn.DocID] = true
							continue
						}
						if !errors.Is(err, thinclient.ErrEpisodicIndexItem) && !errors.Is(err, thinclient.ErrEpisodicPointerSourceConflict) {
							return rep, err
						}
						rep.Warnings = append(rep.Warnings, fmt.Sprintf("episodic import %s: %v", turn.DocID, err))
						continue
					}
					storedRaw[turn.DocID] = turn.RefPointer
					rep.EpisodicImportedFromHar++
					localEpi[turn.DocID] = true
				}
				if snapshot != nil {
					_ = snapshot.Close()
				}
			}
		}
		rep.EpisodicIndexUnverified = len(unverified)
		if !backgroundPass {
			coverage, indexErr := tc.Vault.EpisodicIndexFaultDiagnostics()
			if indexErr != nil {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("episodic index coverage unavailable: %v", indexErr))
			} else {
				rep.EpisodicIndexCompatible = coverage.Compatible
				rep.EpisodicIndexIncompatible = coverage.Incompatible
				rep.EpisodicIndexMissing = coverage.Missing
				rep.EpisodicIndexCorrupt = coverage.Corrupt
				rep.EpisodicIndexNeedsRebuild = coverage.NeedsRebuild
			}
		}
	}

	// 2. Reconcile L1 Background (mounted dirs sync when dirty)
	if layer == "all" || layer == "background" {
		if layer == "background" || preStatus.Layers["background"].DirtyFilesCount > 0 {
			if bgRep, bgErr := tc.SyncBackgroundDirs(ctx); bgErr == nil {
				rep.BackgroundSyncedFiles = bgRep.IngestedFiles
			} else {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("background dir sync: %v", bgErr))
			}
		}
	}

	rep.TotalReconciled = rep.EpisodicImportedFromHar + rep.BackgroundSyncedFiles
	rep.ElapsedMS = int(time.Since(t0).Milliseconds())
	if !backgroundPass && memory.Config != nil && mcp.ShouldUseUnixDaemon(memory.Config.DB) {
		mcp.InvalidateUnixDaemonCaches("")
	}
	if backgroundPass {
		// A background pass reports the snapshot it started from: re-scanning every
		// transcript here would double the tick cost and the caller never reads it.
		rep.Status = preStatus
	} else {
		postStatus, _ := inspectEnvHealth(ctx, memory, harnessFilter, true)
		rep.Status = postStatus
	}
	return rep, nil
}

// detectActiveHarness inspects `active_workspace.json` (the hook-written presence
// file, which carries its own `updated_at`) and runtime environment variables to
// identify which coding harness (ZCode / Claude Code / Codex / dsh / CodeBuddy /
// WorkBuddy) the machine is currently working in. The presence timestamp is
// returned alongside the id so the caller can weigh this signal against
// per-harness transcript recency instead of trusting a possibly stale file.
// No signal → empty id and source "none": emitting a fabricated default (the
// historical hardcoded "zcode") made every machine without evidence claim ZCode
// as active — including machines where ZCode was never installed.
func detectActiveHarness(home string) (harnessID, workspace, sessionID, source string, presenceAt int64) {
	statePath := filepath.Join(config.DefaultStateDir(), "active_workspace.json")
	if raw, err := os.ReadFile(statePath); err == nil {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			carrier, _ := m["carrier"].(string)
			locus, _ := m["locus"].(string)
			sess, _ := m["session_id"].(string)
			carrier = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(carrier)), "harness:")
			if carrier != "" {
				at, _ := m["updated_at"].(float64)
				return carrier, locus, sess, "active_workspace.json", int64(at)
			}
		}
	}
	// An explicit identity stamp outranks inference, mirroring the presence-file
	// precedence in internal/text.
	if v := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_HARNESS")); v != "" {
		return strings.ToLower(v), "", "", "env:ROLLING_MEMORY_HARNESS", 0
	}
	// DeepSeek Harness exports DSH_SESSION_ID / DSH_SHELL to the processes it
	// spawns; those markers are session-scoped and are only ever stamped by DSH
	// itself, while ZCODE_PROJECT_DIR / CLAUDE_PROJECT_DIR are plain shell
	// exports that every descendant inherits — a DSH session started from a
	// ZCode terminal carries both, so the ZCode checks below must not be asked
	// first about a process a DSH session owns.
	if os.Getenv("DSH_SESSION_ID") != "" || os.Getenv("DSH_SHELL") != "" {
		return "dsh", "", "", "env:DSH_SESSION_ID", 0
	}
	if os.Getenv("ZCODE_PROJECT_DIR") != "" {
		return "zcode", os.Getenv("ZCODE_PROJECT_DIR"), "", "env:ZCODE_PROJECT_DIR", 0
	}
	if os.Getenv("CLAUDE_PROJECT_DIR") != "" {
		return "claude-code", os.Getenv("CLAUDE_PROJECT_DIR"), "", "env:CLAUDE_PROJECT_DIR", 0
	}
	if os.Getenv("CODEX_HOME") != "" {
		return "codex", "", "", "env:CODEX_HOME", 0
	}
	return "", "", "", "none", 0
}

// harnessEnvDefinitions is the environment-inventory list behind the console's
// environment panel: the five core harnesses are always listed; an extended
// harness (Cursor / Windsurf / Cline / WorkBuddy / Doubao) joins the list once it
// is present on this machine or is the detected active host, so the panel never
// omits the environment the user is actually in, nor pads the grid with hosts
// that have no footprint here.
func harnessEnvDefinitions(home, activeID string) []harness.Harness {
	defs := harness.BuildHarnesses(home, "")
	for _, d := range harness.BuildExtendedHarnesses(home) {
		if d.ID == activeID {
			defs = append(defs, d)
			continue
		}
		if _, err := os.Stat(d.Detect); err == nil {
			defs = append(defs, d)
		}
	}
	return defs
}

// activeHarnessClaim is one piece of recency evidence for the active-harness pick.
type activeHarnessClaim struct {
	id     string
	ts     int64
	source string
}

// harnessClaims assembles the recency evidence behind the active-harness pick: a
// live harness environment variable (the calling process runs inside that harness
// right now) is timestamped `now`; the hook-written presence file keeps its own
// write time; every harness with a transcript contributes its last-activity time.
// The explicit CLI filter deliberately contributes nothing here: it is a scan
// scope, not evidence.
func harnessClaims(items []HarnessEnvItem, signalID, detectSrc string, presenceAt, now int64) []activeHarnessClaim {
	var claims []activeHarnessClaim
	switch {
	case strings.HasPrefix(detectSrc, "env:"):
		claims = append(claims, activeHarnessClaim{signalID, now, detectSrc})
	case detectSrc == "active_workspace.json" && presenceAt > 0:
		claims = append(claims, activeHarnessClaim{signalID, presenceAt, detectSrc})
	}
	for _, it := range items {
		if it.LastActiveAt > 0 {
			claims = append(claims, activeHarnessClaim{it.ID, it.LastActiveAt, "recent_activity"})
		}
	}
	return claims
}

// resolveActiveHarness picks the harness with the strongest recency evidence: the
// newest timestamp wins, and equal timestamps keep the earlier (more specific)
// claim. Claims whose id is not part of the inventory are dropped, and no claims
// at all yield an empty id — the caller then reports "none" rather than inventing
// a default.
func resolveActiveHarness(items []HarnessEnvItem, claims []activeHarnessClaim) (harnessID, source string) {
	inInventory := make(map[string]bool, len(items))
	for _, it := range items {
		inInventory[it.ID] = true
	}
	best := -1
	for i, c := range claims {
		if c.ts <= 0 || !inInventory[c.id] {
			continue
		}
		if best < 0 || c.ts > claims[best].ts {
			best = i
		}
	}
	if best < 0 {
		return "", "none"
	}
	return claims[best].id, claims[best].source
}

// harnessSessionsScannable reports whether this build can discover the
// harness's local conversation sessions (the count shown on the panel).
func harnessSessionsScannable(harnessID string) bool {
	switch harnessID {
	case "zcode", "claude-code", "codex", "dsh", "codebuddy", "workbuddy":
		return true
	}
	return false
}

// harnessTurnsScannable reports whether scanHarnessTurns can also read those
// sessions' turn content. A harness whose transcript is compressed or uses an
// unsupported event schema counts sessions but reports zero turns, and the panel
// must label that as "not counted" rather than "no history".
func harnessTurnsScannable(harnessID string) bool {
	switch harnessID {
	case "zcode", "claude-code", "codex", "dsh", "codebuddy", "workbuddy":
		return true
	}
	return false
}

func harnessHasExcludedSubagentSources(ctx context.Context, home, harnessID string) bool {
	if harnessID == "claude-code" {
		for _, pattern := range []string{
			filepath.Join(home, ".claude", "projects", "*", "*", "subagents", "*.jsonl"),
			filepath.Join(home, ".claude", "projects", "*", "sessions", "*", "subagents", "*.jsonl"),
		} {
			matches, err := filepath.Glob(pattern)
			if err != nil || len(matches) > 0 {
				return true
			}
		}
	}
	if harnessID == "zcode" {
		path := filepath.Join(home, ".zcode", "cli", "db", "db.sqlite")
		if _, err := os.Stat(path); err != nil {
			return !os.IsNotExist(err)
		}
		db, err := sql.Open("sqlite", ids.SQLiteFileURI(path)+"?mode=ro&_pragma=busy_timeout(1500)")
		if err != nil {
			return true
		}
		defer db.Close()
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session WHERE COALESCE(parent_id, '') != '' OR COALESCE(task_type, '') = 'subagent_child'`).Scan(&count); err != nil {
			return true
		}
		return count > 0
	}
	return false
}

// scanHarnessTurns discovers local conversation sessions and extracts User+Assistant
// turn pairs for a given harness (`zcode`, `claude-code`, `codex`, `dsh`, `codebuddy`, `workbuddy`).
func scanHarnessTurns(home, harnessID, scope string, maxTurns int) (int, []candidateEpisodicTurn, int64) {
	sessions, turns, lastActiveAt, _ := scanHarnessTurnsWithCoverage(home, harnessID, scope, maxTurns)
	return sessions, turns, lastActiveAt
}

func scanHarnessTurnsWithCoverage(home, harnessID, scope string, maxTurns int) (int, []candidateEpisodicTurn, int64, bool) {
	return scanHarnessTurnsWithCoverageContext(context.Background(), home, harnessID, scope, maxTurns)
}

func scanHarnessTurnsWithCoverageContext(ctx context.Context, home, harnessID, scope string, maxTurns int) (sessionsCount int, turns []candidateEpisodicTurn, lastActiveAt int64, complete bool) {
	complete = true
	if maxTurns <= 0 {
		maxTurns = 100
	}
	if !harnessTurnsScannable(harnessID) {
		seen := map[string]bool{}
		for _, pattern := range harness.TranscriptGlobs(home, harnessID) {
			matches, _ := filepath.Glob(pattern)
			for _, path := range matches {
				info, err := os.Stat(path)
				if err != nil || info.IsDir() || seen[path] {
					continue
				}
				seen[path] = true
				sessionsCount++
				if info.ModTime().Unix() > lastActiveAt {
					lastActiveAt = info.ModTime().Unix()
				}
			}
		}
		return sessionsCount, nil, lastActiveAt, sessionsCount == 0
	}
	sources, err := discoverEpisodicScanSources(ctx, home, harnessID)
	if err != nil {
		complete = false
	}
	if harnessHasExcludedSubagentSources(ctx, home, harnessID) {
		complete = false
	}
	sessionsCount = len(sources)
	sort.SliceStable(sources, func(i, j int) bool {
		left, _ := os.Stat(sources[i].Path)
		right, _ := os.Stat(sources[j].Path)
		return left != nil && (right == nil || left.ModTime().After(right.ModTime()))
	})
	for _, source := range sources {
		if ctx.Err() != nil {
			return sessionsCount, turns, lastActiveAt, false
		}
		if info, err := os.Stat(source.Path); err == nil && info.ModTime().Unix() > lastActiveAt {
			lastActiveAt = info.ModTime().Unix()
		}
		if len(turns) >= maxTurns {
			complete = false
			continue
		}
		var sourceTurns []candidateEpisodicTurn
		next := 0
		wrapped := false
		before, stampErr := episodicScanSourceStamp(source)
		_, scanErr := streamEpisodicScanSource(ctx, source, scope, func(_ int, turn candidateEpisodicTurn) error {
			if !turn.Completed {
				complete = false
			}
			if len(sourceTurns) < maxTurns {
				sourceTurns = append(sourceTurns, turn)
			} else {
				sourceTurns[next] = turn
				next = (next + 1) % maxTurns
				wrapped = true
				complete = false
			}
			return nil
		})
		if wrapped {
			ordered := make([]candidateEpisodicTurn, 0, len(sourceTurns))
			ordered = append(ordered, sourceTurns[next:]...)
			ordered = append(ordered, sourceTurns[:next]...)
			sourceTurns = ordered
		}
		after, afterErr := episodicScanSourceStamp(source)
		if stampErr != nil || scanErr != nil || afterErr != nil || before != after {
			complete = false
		}
		if remaining := maxTurns - len(turns); len(sourceTurns) > remaining {
			complete = false
			sourceTurns = sourceTurns[len(sourceTurns)-remaining:]
		}
		turns = append(turns, sourceTurns...)
	}
	return sessionsCount, turns, lastActiveAt, complete
}

// countDirtyBackgroundFiles checks whether any mounted background file on disk has changed mtime/size
// compared to `thin_vault_bg_files`.
func countDirtyBackgroundFiles(db *sql.DB) int {
	rows, err := db.Query(`SELECT abs_path, mtime_ns, file_size FROM thin_vault_bg_files`)
	if err != nil {
		return 0
	}
	defer rows.Close()
	dirty := 0
	for rows.Next() {
		var p string
		var mtimeNS, fsize int64
		if rows.Scan(&p, &mtimeNS, &fsize) != nil {
			continue
		}
		info, sErr := os.Stat(p)
		if sErr != nil || info.ModTime().UnixNano() != mtimeNS || info.Size() != fsize {
			dirty++
		}
	}
	return dirty
}
