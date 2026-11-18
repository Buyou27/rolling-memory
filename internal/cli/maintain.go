// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/mcp"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// maintain 是面向用户的空间维护入口（体检 / 清理 / 瘦身）。
//
// 设计约束：
//  1. 只读预演为默认：clean / compact 未带 --apply --yes 时只出计划，不落盘；
//  2. clean 只删除「确定是历史副本」的文件——活跃件名永不匹配（仅 *.bak-* / *.old），
//     且按 (目录, 族) 保留最新 N 份（默认 2）；删除逐文件报告，单文件失败不中断；
//  3. 缓存清理必须显式 --caches：先经守护进程控制通道丢弃内存快照，再删可再生成文件；
//  4. compact 走产品自身的守护进程 /v1/shutdown 有序停机，VACUUM 后按需复位，
//     不引入第二套锁定协议、不裸杀进程；读不到空闲页就如实报 0，不伪造读数。
const (
	// maintainDefaultBackupKeep 是每个 (目录, 族) 默认保留的最新备份份数。
	maintainDefaultBackupKeep = 2
	// maintainTransientMinAge 是 *.old（更新换件的瞬态副本）允许删除的最小年龄，
	// 避免误删另一进程正在进行的换件中间态。
	maintainTransientMinAge = 10 * time.Minute
	// maintainReceiptDirName 是清理/瘦身回执目录（状态目录下）。
	maintainReceiptDirName = "maintain-receipts"
)

// maintainCachePatterns 是可再生成缓存的受管文件名模式（仅状态目录、仅此清单）。
var maintainCachePatterns = []string{
	"native_part_cache_v*.json",
	"native_telemetry_ledger_v*.json",
	"efficacy_turn_cache_v*.json",
}

// maintainLogNames 只展示、不清理：日志轮转由各写入方承担。
var maintainLogNames = []string{"rolling-memory.log", "hook.log", "panel-admin.log"}

// maintainItem 是体检/清理计划里的一个文件。
type maintainItem struct {
	Path    string    `json:"path"`
	Bytes   int64     `json:"bytes"`
	AgeDays int       `json:"age_days"`
	Family  string    `json:"family,omitempty"`
	Action  string    `json:"action"` // delete | keep | clear | report
	mod     time.Time `json:"-"`
}

// maintainScanDirs 返回安装目录下允许清理旧副本的三个位置（不递归）。
func maintainScanDirs(dataDir string) []string {
	return []string{
		filepath.Join(dataDir, "bin"),
		filepath.Join(dataDir, "admin", "bin"),
		filepath.Join(dataDir, "admin"),
	}
}

// maintainBackupFamily 判定一个文件名是否是历史副本并给出族名。
// 只认三种受管形态：`<name>.bak-<stamp>` / `<name>.bak` / `<name>.old`。
func maintainBackupFamily(name string) (string, bool) {
	if idx := strings.Index(name, ".bak-"); idx > 0 {
		return name[:idx], true
	}
	if strings.HasSuffix(name, ".bak") && len(name) > len(".bak") {
		return strings.TrimSuffix(name, ".bak"), true
	}
	if strings.HasSuffix(name, ".old") && len(name) > len(".old") {
		return strings.TrimSuffix(name, ".old"), true
	}
	return "", false
}

// maintainScanBackups 扫描旧版本备份，按 (目录, 族) 保留最新 keep 份；
// 其余标记为待删除。*.old 额外要求超过瞬态最小年龄。
func maintainScanBackups(dirs []string, keep int, now time.Time) []maintainItem {
	if keep < 0 {
		keep = 0
	}
	type groupKey struct{ dir, family string }
	groups := map[groupKey][]maintainItem{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			family, ok := maintainBackupFamily(entry.Name())
			if !ok {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			key := groupKey{dir, family}
			groups[key] = append(groups[key], maintainItem{
				Path:    filepath.Join(dir, entry.Name()),
				Bytes:   info.Size(),
				AgeDays: int(now.Sub(info.ModTime()).Hours() / 24),
				Family:  family,
				mod:     info.ModTime(),
			})
		}
	}
	var items []maintainItem
	for _, members := range groups {
		sort.Slice(members, func(i, j int) bool { return members[i].mod.After(members[j].mod) })
		for index, item := range members {
			transient := strings.HasSuffix(item.Path, ".old")
			switch {
			case index < keep:
				item.Action = "keep"
			case transient && now.Sub(item.mod) < maintainTransientMinAge:
				item.Action = "keep"
			default:
				item.Action = "delete"
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
}

// maintainScanCaches 扫描状态目录下的可再生成缓存（仅清单内的文件模式）。
//
// 金库引擎的 .zcsr 磁盘簇不在此列：它不是一次性产物，而是冷启动索引本身，
// 由 maintainScanEngineCache 单独管理并默认保留。
func maintainScanCaches(stateDir string, now time.Time) []maintainItem {
	var items []maintainItem
	for _, pattern := range maintainCachePatterns {
		matches, err := filepath.Glob(filepath.Join(stateDir, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			info, statErr := os.Lstat(match)
			if statErr != nil || !info.Mode().IsRegular() {
				continue
			}
			items = append(items, maintainItem{
				Path:    match,
				Bytes:   info.Size(),
				AgeDays: int(now.Sub(info.ModTime()).Hours() / 24),
				Action:  "clear",
				mod:     info.ModTime(),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
}

// maintainScanEngineCache 返回金库引擎磁盘簇缓存（.zcsr 与指纹档 .zcsr.gen）的条目。
//
// 该缓存缺失时，守护进程会在启动路径上重建索引与簇状态：数万单元规模下耗时以分钟计，
// 构建期堆峰值可达数 GB，期间召回排队甚至超时。因此默认保留（Action=keep）并只读展示，
// 仅在显式 purge（CLI --purge-engine-cache）时才标记为可清除。
func maintainScanEngineCache(dbPath string, purge bool, now time.Time) []maintainItem {
	if strings.TrimSpace(dbPath) == "" {
		return nil
	}
	action := "keep"
	if purge {
		action = "clear"
	}
	var items []maintainItem
	for _, path := range []string{dbPath + ".zcsr", dbPath + ".zcsr.gen"} {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		items = append(items, maintainItem{
			Path:    path,
			Bytes:   info.Size(),
			AgeDays: int(now.Sub(info.ModTime()).Hours() / 24),
			Action:  action,
			mod:     info.ModTime(),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
}

// maintainScanLogs 列出日志文件用于展示（不清理）。
func maintainScanLogs(stateDir string, now time.Time) []maintainItem {
	var items []maintainItem
	for _, name := range maintainLogNames {
		path := filepath.Join(stateDir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		items = append(items, maintainItem{
			Path:    path,
			Bytes:   info.Size(),
			AgeDays: int(now.Sub(info.ModTime()).Hours() / 24),
			Action:  "report",
			mod:     info.ModTime(),
		})
	}
	return items
}

// maintainFileBytes 返回路径的字节数（不存在或非文件报 0）。
func maintainFileBytes(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0
	}
	return info.Size()
}

// maintainVaultStat 汇总金库与侧车的文件占用、空闲页，以及管理后台数据库体量。
func maintainVaultStat(memory *facade.Memory, dataDir string) map[string]any {
	dbPath := memory.Config.DB
	adminDB := filepath.Join(dataDir, "admin", "rolling-admin.db")
	stat := map[string]any{
		"db_path":        dbPath,
		"db_bytes":       maintainFileBytes(dbPath),
		"wal_bytes":      maintainFileBytes(dbPath + "-wal"),
		"page_size":      int64(0),
		"free_pages":     int64(0),
		"free_bytes":     int64(0),
		"sidecar_bytes":  maintainFileBytes(dbPath + ".efficacy.sqlite"),
		"admin_db_path":  adminDB,
		"admin_db_bytes": maintainFileBytes(adminDB),
	}
	if memory.Store == nil || memory.Store.DB() == nil {
		return stat
	}
	var pageSize, freePages int64
	if err := memory.Store.DB().QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		return stat
	}
	if err := memory.Store.DB().QueryRow("PRAGMA freelist_count").Scan(&freePages); err != nil {
		return stat
	}
	stat["page_size"] = pageSize
	stat["free_pages"] = freePages
	stat["free_bytes"] = pageSize * freePages
	return stat
}

// maintainHumanBytes 渲染人类可读体积。
func maintainHumanBytes(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// maintainSection 汇总一组文件（总量 / 可回收量 / 保留数）。
func maintainSection(key string, items []maintainItem) map[string]any {
	var total, reclaimable int64
	kept := 0
	for _, item := range items {
		total += item.Bytes
		switch item.Action {
		case "delete", "clear":
			reclaimable += item.Bytes
		case "keep":
			kept++
		}
	}
	return map[string]any{
		"key":               key,
		"total_bytes":       total,
		"reclaimable_bytes": reclaimable,
		"kept_count":        kept,
		"items":             items,
	}
}

// maintainDaemonStat 报告共享守护进程的当前状态。
func maintainDaemonStat() map[string]any {
	sock := mcp.DefaultUnixSocketPath()
	running := false
	if info, err := os.Lstat(sock); err == nil && info.Mode()&os.ModeSocket != 0 {
		running = true
	}
	return map[string]any{"socket": sock, "running": running}
}

// maintainWriteReceipt 把一次执行的明细写入状态目录回执（0o600，目录 0o700）。
func maintainWriteReceipt(stateDir, action string, payload map[string]any, now time.Time) (string, error) {
	dir := filepath.Join(stateDir, maintainReceiptDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s.json", action, now.UTC().Format("20060102T150405Z"))
	path := filepath.Join(dir, name)
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// maintainApplyClean 删除计划中的 delete/clear 文件，逐文件容错并返回回执路径。
func maintainApplyClean(plan []maintainItem, stateDir string, now time.Time) (map[string]any, error) {
	var removed []map[string]any
	var failures []string
	var freed int64
	for _, item := range plan {
		if item.Action != "delete" && item.Action != "clear" {
			continue
		}
		if err := os.Remove(item.Path); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", item.Path, err))
			continue
		}
		freed += item.Bytes
		removed = append(removed, map[string]any{"path": item.Path, "bytes": item.Bytes, "action": item.Action})
	}
	receipt, err := maintainWriteReceipt(stateDir, "clean", map[string]any{
		"action":      "clean",
		"at":          now.UTC().Format(time.RFC3339),
		"freed_bytes": freed,
		"removed":     removed,
		"failed":      failures,
	}, now)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"freed_bytes": freed,
		"removed":     len(removed),
		"failed":      failures,
		"receipt":     receipt,
	}, nil
}

// maintainCompactReport 汇总一次金库瘦身的读数。
type maintainCompactReport struct {
	beforeBytes    int64
	afterBytes     int64
	freedBytes     int64
	freeBytes      int64
	walBefore      int64
	walAfter       int64
	sidecarStatus  string
	sidecarFreed   int64
	daemonWasUp    bool
	daemonBackUp   bool
	daemonEligible bool
	messages       []string
	receipt        string
}

// maintainCompactRun 对目标金库与其侧车执行 VACUUM（apply=false 时只取读数）。
func maintainCompactRun(memory *facade.Memory, apply, restartDaemon bool, now time.Time) (*maintainCompactReport, error) {
	report := &maintainCompactReport{}
	dbPath := memory.Config.DB
	report.beforeBytes = maintainFileBytes(dbPath)
	report.walBefore = maintainFileBytes(dbPath + "-wal")

	// 守护进程只服务默认金库；自定义 --db 一律进程内直连，无守护进程可停。
	sock := mcp.DefaultUnixSocketPath()
	report.daemonEligible = mcp.ShouldUseUnixDaemon(dbPath)
	if report.daemonEligible {
		if info, err := os.Lstat(sock); err == nil && info.Mode()&os.ModeSocket != 0 {
			report.daemonWasUp = true
		}
	}
	if !apply {
		return report, nil
	}

	if report.daemonWasUp {
		if err := mcp.ShutdownUnixDaemon(sock); err != nil {
			report.messages = append(report.messages, "daemon shutdown was not confirmed: "+err.Error())
		}
		// 兜底轮询：ShutdownUnixDaemon 已等待确认，此处防御 socket 残留。
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Lstat(sock); err != nil {
				break
			}
			time.Sleep(30 * time.Millisecond)
		}
	}

	connection := memory.Store.DB()
	if _, err := connection.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		report.messages = append(report.messages, "wal checkpoint failed: "+err.Error())
	}
	if _, err := connection.Exec("VACUUM"); err != nil {
		return report, fmt.Errorf("vacuum failed: %w", err)
	}
	if _, err := connection.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		report.messages = append(report.messages, "post-checkpoint failed: "+err.Error())
	}
	var check string
	if err := connection.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		report.messages = append(report.messages, fmt.Sprintf("quick_check: %s", firstNonEmptyString(check, "unavailable")))
	}

	// 侧车账本同样瘦身；被其他进程占用时如实降级为 skipped。
	sidecarPath := dbPath + ".efficacy.sqlite"
	if _, err := os.Stat(sidecarPath); err == nil {
		sidecarBefore := maintainFileBytes(sidecarPath)
		if side, err := db.Connect(sidecarPath, db.LocalProfile()); err == nil {
			if _, vErr := side.Exec("VACUUM"); vErr != nil {
				report.sidecarStatus = "skipped: " + vErr.Error()
			} else {
				report.sidecarStatus = "compacted"
				report.sidecarFreed = sidecarBefore - maintainFileBytes(sidecarPath)
			}
			_ = side.Close()
		} else {
			report.sidecarStatus = "skipped: " + err.Error()
		}
	} else {
		report.sidecarStatus = "absent"
	}

	report.afterBytes = maintainFileBytes(dbPath)
	report.walAfter = maintainFileBytes(dbPath + "-wal")
	report.freedBytes = report.beforeBytes - report.afterBytes

	if report.daemonWasUp && restartDaemon {
		if err := mcp.EnsureUnixDaemon(sock); err != nil {
			report.messages = append(report.messages, "daemon restart failed: "+err.Error())
		} else {
			report.daemonBackUp = true
		}
	}

	receipt, err := maintainWriteReceipt(config.DefaultStateDir(), "compact", map[string]any{
		"action":           "compact",
		"at":               now.UTC().Format(time.RFC3339),
		"db":               dbPath,
		"before_bytes":     report.beforeBytes,
		"after_bytes":      report.afterBytes,
		"freed_bytes":      report.freedBytes,
		"sidecar":          report.sidecarStatus,
		"sidecar_freed":    report.sidecarFreed,
		"daemon_was_up":    report.daemonWasUp,
		"daemon_restarted": report.daemonBackUp,
		"messages":         report.messages,
	}, now)
	if err != nil {
		return report, err
	}
	report.receipt = receipt
	return report, nil
}

// --------------------------------------------------------------- command impl

// runMaintainCommand 分派 maintain 的三种动作。
func (a *application) runMaintainCommand(memory *facade.Memory) (int, error) {
	sub := strings.TrimSpace(a.args.Subcommand())
	if sub == "" {
		sub = "status"
	}
	switch sub {
	case "status":
		return a.runMaintainStatus(memory)
	case "clean":
		return a.runMaintainClean(memory)
	case "compact":
		return a.runMaintainCompact(memory)
	}
	return 1, fmt.Errorf("unknown maintain action %q (status|clean|compact)", sub)
}

// runMaintainStatus 输出只读体检报告（含各动作的可回收量与执行命令）。
func (a *application) runMaintainStatus(memory *facade.Memory) (int, error) {
	now := time.Now()
	dataDir := filepath.Dir(memory.Config.DB)
	stateDir := config.DefaultStateDir()
	backupItems := maintainScanBackups(maintainScanDirs(dataDir), maintainDefaultBackupKeep, now)
	cacheItems := maintainScanCaches(stateDir, now)
	engineCacheItems := maintainScanEngineCache(memory.Config.DB, false, now)
	logItems := maintainScanLogs(stateDir, now)
	vault := maintainVaultStat(memory, dataDir)
	daemon := maintainDaemonStat()

	backups := maintainSection("backups", backupItems)
	caches := maintainSection("caches", cacheItems)
	engineCache := maintainSection("engine_cache", engineCacheItems)
	logs := maintainSection("logs", logItems)
	reclaimBackups := backups["reclaimable_bytes"].(int64)
	reclaimCaches := caches["reclaimable_bytes"].(int64)
	reclaimCompact, _ := vault["free_bytes"].(int64)

	report := map[string]any{
		"ok":           true,
		"action":       "status",
		"apply":        false,
		"data_dir":     dataDir,
		"state_dir":    stateDir,
		"backups":      backups,
		"caches":       caches,
		"logs":         logs,
		"vault":        vault,
		"daemon":       daemon,
		"engine_cache": engineCache,
		"keep_policy": map[string]any{
			"backup_keep_per_family": maintainDefaultBackupKeep,
		},
		"reclaimable": map[string]any{
			"backups_bytes": reclaimBackups,
			"caches_bytes":  reclaimCaches,
			"compact_bytes": reclaimCompact,
			"total_bytes":   reclaimBackups + reclaimCaches + reclaimCompact,
		},
	}
	return 0, a.emit(report, func() { a.printMaintainStatus(report) })
}

// runMaintainClean 预演/执行旧备份与缓存清理。
func (a *application) runMaintainClean(memory *facade.Memory) (int, error) {
	apply := a.args.Flag("apply")
	confirm := a.args.Flag("yes")
	if apply && !confirm {
		return 1, fmt.Errorf("clean needs --yes to execute (dry-run without --apply)")
	}
	keep := a.args.Int("keep")
	includeCaches := a.args.Flag("caches")
	purgeEngineCache := a.args.Flag("purge_engine_cache")
	now := time.Now()
	dataDir := filepath.Dir(memory.Config.DB)
	stateDir := config.DefaultStateDir()

	plan := maintainScanBackups(maintainScanDirs(dataDir), keep, now)
	if includeCaches {
		plan = append(plan, maintainScanCaches(stateDir, now)...)
	}
	// 引擎磁盘簇默认以 keep 进计划（只展示、不删除）；显式 purge 才标记 clear。
	plan = append(plan, maintainScanEngineCache(memory.Config.DB, purgeEngineCache, now)...)
	var plannedBytes int64
	for _, item := range plan {
		if item.Action == "delete" || item.Action == "clear" {
			plannedBytes += item.Bytes
		}
	}
	report := map[string]any{
		"ok":                 true,
		"action":             "clean",
		"apply":              apply,
		"keep":               keep,
		"include_caches":     includeCaches,
		"purge_engine_cache": purgeEngineCache,
		"plan":               plan,
		"planned_bytes":      plannedBytes,
	}
	if apply {
		if includeCaches || purgeEngineCache {
			notified, err := mcp.InvalidateUnixDaemonCachesVerified(context.Background(), mcp.DefaultUnixSocketPath(), memory.Config)
			report["daemon_caches_invalidated"] = notified
			if err != nil {
				report["daemon_cache_note"] = err.Error()
			}
		}
		if purgeEngineCache {
			if daemonStat := maintainDaemonStat(); daemonStat["running"] == true {
				// 运行中的守护进程持有 .zcsr 的 mmap：删除后空间要到进程退出才真正释放。
				report["engine_cache_note"] = "engine cache space is released when the running daemon exits; the next recall rebuilds it"
			}
		}
		result, err := maintainApplyClean(plan, stateDir, now)
		if err != nil {
			return 1, err
		}
		for key, value := range result {
			report[key] = value
		}
	}
	return 0, a.emit(report, func() { a.printMaintainClean(report) })
}

// runMaintainCompact 预演/执行金库瘦身。
func (a *application) runMaintainCompact(memory *facade.Memory) (int, error) {
	apply := a.args.Flag("apply")
	confirm := a.args.Flag("yes")
	if apply && !confirm {
		return 1, fmt.Errorf("compact needs --yes to execute (dry-run without --apply)")
	}
	restartDaemon := !a.args.Flag("no_daemon_restart")
	now := time.Now()
	vault := maintainVaultStat(memory, filepath.Dir(memory.Config.DB))
	result, err := maintainCompactRun(memory, apply, restartDaemon, now)
	if err != nil {
		return 1, err
	}
	report := map[string]any{
		"ok":               true,
		"action":           "compact",
		"apply":            apply,
		"db":               memory.Config.DB,
		"db_bytes":         result.beforeBytes,
		"wal_bytes":        result.walBefore,
		"free_pages":       vault["free_pages"],
		"free_bytes":       vault["free_bytes"],
		"sidecar_bytes":    vault["sidecar_bytes"],
		"daemon_was_up":    result.daemonWasUp,
		"daemon_restarted": result.daemonBackUp,
		"messages":         result.messages,
	}
	if apply {
		report["before_bytes"] = result.beforeBytes
		report["after_bytes"] = result.afterBytes
		report["freed_bytes"] = result.freedBytes
		report["wal_bytes_after"] = result.walAfter
		report["sidecar"] = result.sidecarStatus
		report["sidecar_freed_bytes"] = result.sidecarFreed
		report["receipt"] = result.receipt
	}
	return 0, a.emit(report, func() { a.printMaintainCompact(report) })
}

// --------------------------------------------------------------- human render

func (a *application) printMaintainStatus(report map[string]any) {
	a.printLine("maintenance report (read-only)")
	a.printf("  data dir:  %s\n", report["data_dir"])
	a.printf("  state dir: %s\n", report["state_dir"])
	for _, key := range []string{"backups", "caches", "logs"} {
		section, _ := report[key].(map[string]any)
		if section == nil {
			continue
		}
		items, _ := section["items"].([]maintainItem)
		if len(items) == 0 {
			continue
		}
		a.printf("  %s: %d files · %s · reclaimable %s", key, len(items),
			maintainHumanBytes(section["total_bytes"].(int64)), maintainHumanBytes(section["reclaimable_bytes"].(int64)))
		if key == "backups" {
			a.printf(" (keep newest %d per family)", maintainDefaultBackupKeep)
		}
		a.printLine("")
		printed := 0
		for _, item := range items {
			if item.Action != "delete" && item.Action != "clear" {
				continue
			}
			if printed >= 20 {
				a.printf("    ... and %d more\n", countMaintainAction(items)-printed)
				break
			}
			a.printf("    %s %s (%s, %dd)\n", item.Action, item.Path, maintainHumanBytes(item.Bytes), item.AgeDays)
			printed++
		}
	}
	if section, _ := report["engine_cache"].(map[string]any); section != nil {
		if items, _ := section["items"].([]maintainItem); len(items) > 0 {
			a.printf("  engine cache: %s · kept by default (a missing cache rebuilds the index: minutes, multi-GB peak)\n",
				maintainHumanBytes(section["total_bytes"].(int64)))
			a.printLine("    purge: rmem maintain clean --purge-engine-cache --apply --yes")
		}
	}
	vault, _ := report["vault"].(map[string]any)
	if vault != nil {
		a.printf("  vault: %s (free pages %s, reclaimable by compact) · WAL %s · efficacy %s\n",
			maintainHumanBytes(vault["db_bytes"].(int64)),
			maintainHumanBytes(vault["free_bytes"].(int64)),
			maintainHumanBytes(vault["wal_bytes"].(int64)),
			maintainHumanBytes(vault["sidecar_bytes"].(int64)))
	}
	if daemon, ok := report["daemon"].(map[string]any); ok {
		state := "stopped"
		if running, _ := daemon["running"].(bool); running {
			state = "running"
		}
		a.printf("  daemon: %s (%s)\n", state, daemon["socket"])
	}
	reclaim, _ := report["reclaimable"].(map[string]any)
	if reclaim != nil {
		a.printf("  reclaimable total: %s (clean %s + caches %s + compact %s)\n",
			maintainHumanBytes(reclaim["total_bytes"].(int64)),
			maintainHumanBytes(reclaim["backups_bytes"].(int64)),
			maintainHumanBytes(reclaim["caches_bytes"].(int64)),
			maintainHumanBytes(reclaim["compact_bytes"].(int64)))
	}
	a.printLine("  run: rmem maintain clean --apply --yes [--caches] · rmem maintain compact --apply --yes")
}

func countMaintainAction(items []maintainItem) int {
	count := 0
	for _, item := range items {
		if item.Action == "delete" || item.Action == "clear" {
			count++
		}
	}
	return count
}

func (a *application) printMaintainClean(report map[string]any) {
	plan, _ := report["plan"].([]maintainItem)
	planned := countMaintainAction(plan)
	if apply, _ := report["apply"].(bool); !apply {
		a.printf("clean preview: %d files · %s reclaimable (keep newest %d per backup family, caches included: %v, engine cache purged: %v)\n",
			planned, maintainHumanBytes(report["planned_bytes"].(int64)),
			report["keep"], report["include_caches"], report["purge_engine_cache"])
		a.printLine("  run: rmem maintain clean --apply --yes [--caches] [--purge-engine-cache]")
		return
	}
	a.printf("cleaned: %d files removed · %s freed\n",
		report["removed"], maintainHumanBytes(report["freed_bytes"].(int64)))
	if note, _ := report["engine_cache_note"].(string); note != "" {
		a.printf("  engine cache: %s\n", note)
	}
	if failed, ok := report["failed"].([]string); ok {
		for _, line := range failed {
			a.printf("  failed: %s\n", line)
		}
	}
	if receipt, ok := report["receipt"].(string); ok && receipt != "" {
		a.printf("  receipt: %s\n", receipt)
	}
}

func (a *application) printMaintainCompact(report map[string]any) {
	if apply, _ := report["apply"].(bool); !apply {
		a.printf("compact preview: vault %s · free pages %s reclaimable\n",
			maintainHumanBytes(report["db_bytes"].(int64)),
			maintainHumanBytes(report["free_bytes"].(int64)))
		if up, _ := report["daemon_was_up"].(bool); up {
			a.printLine("  the shared engine daemon will be paused and restarted around VACUUM")
		}
		a.printLine("  run: rmem maintain compact --apply --yes")
		return
	}
	a.printf("compacted: %s -> %s (freed %s) · WAL %s -> %s · sidecar %s\n",
		maintainHumanBytes(report["before_bytes"].(int64)),
		maintainHumanBytes(report["after_bytes"].(int64)),
		maintainHumanBytes(report["freed_bytes"].(int64)),
		maintainHumanBytes(report["wal_bytes"].(int64)),
		maintainHumanBytes(report["wal_bytes_after"].(int64)),
		report["sidecar"])
	if restarted, _ := report["daemon_restarted"].(bool); restarted {
		a.printLine("  daemon: restarted")
	}
	for _, line := range anyStringList(report["messages"]) {
		a.printf("  note: %s\n", line)
	}
	if receipt, ok := report["receipt"].(string); ok && receipt != "" {
		a.printf("  receipt: %s\n", receipt)
	}
}

// anyStringList 读取 []string 或 []any 形式的字符串列表。
func anyStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}
