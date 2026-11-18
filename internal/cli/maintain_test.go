// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// maintainTestWrite 在目录里落一个指定体积与年龄的文件。
func maintainTestWrite(t *testing.T, dir, name string, age time.Duration, size int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

func TestMaintainBackupFamilyRecognizesManagedNames(t *testing.T) {
	cases := map[string]string{
		"rolling-memory.bak-2039":     "rolling-memory",
		"rolling-admin.bak-1834":      "rolling-admin",
		"rmem.bak":                    "rmem",
		"rolling-admin.old":           "rolling-admin",
		"rolling-memory":              "",
		"rolling-memory.bak-tmp-name": "rolling-memory",
	}
	for name, want := range cases {
		family, ok := maintainBackupFamily(name)
		if want == "" {
			if ok {
				t.Fatalf("%s: expected no family, got %q", name, family)
			}
			continue
		}
		if !ok || family != want {
			t.Fatalf("%s: family=%q ok=%v, want %q", name, family, ok, want)
		}
	}
}

func TestMaintainScanBackupsKeepsNewestPerFamily(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	oldest := maintainTestWrite(t, dir, "rolling-memory.bak-0101", 72*time.Hour, 10)
	mid := maintainTestWrite(t, dir, "rolling-memory.bak-0202", 48*time.Hour, 10)
	newest := maintainTestWrite(t, dir, "rolling-memory.bak-0303", 2*time.Hour, 10)
	active := maintainTestWrite(t, dir, "rolling-memory", 3*time.Hour, 10)
	otherFamily := maintainTestWrite(t, dir, "rmem.bak-0102", 30*time.Hour, 5)

	byPath := map[string]maintainItem{}
	for _, item := range maintainScanBackups([]string{dir}, 2, now) {
		byPath[item.Path] = item
	}
	if byPath[oldest].Action != "delete" {
		t.Fatalf("oldest backup must be planned for deletion, got %q", byPath[oldest].Action)
	}
	if byPath[mid].Action != "keep" || byPath[newest].Action != "keep" {
		t.Fatalf("newest two backups must be kept: mid=%q newest=%q", byPath[mid].Action, byPath[newest].Action)
	}
	if _, listed := byPath[active]; listed {
		t.Fatalf("active binary must never be listed as a backup")
	}
	if byPath[otherFamily].Action != "keep" {
		t.Fatalf("single-member family must be kept, got %q", byPath[otherFamily].Action)
	}
}

func TestMaintainScanBackupsTransientOldNeedsMinAge(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fresh := maintainTestWrite(t, dir, "panel.old", 1*time.Minute, 5)
	stale := maintainTestWrite(t, dir, "rmem.old", 2*time.Hour, 5)
	if now.Sub(now.Add(-1*time.Minute)) >= maintainTransientMinAge {
		t.Fatalf("fixture assumption broken")
	}
	byPath := map[string]maintainItem{}
	for _, item := range maintainScanBackups([]string{dir}, 0, now) {
		byPath[item.Path] = item
	}
	if byPath[fresh].Action != "keep" {
		t.Fatalf("fresh .old must be kept, got %q", byPath[fresh].Action)
	}
	if byPath[stale].Action != "delete" {
		t.Fatalf("stale .old must be deleted, got %q", byPath[stale].Action)
	}
}

func TestMaintainScanCachesOnlyManagedPatterns(t *testing.T) {
	stateDir := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "memory.db")
	now := time.Now()
	part := maintainTestWrite(t, stateDir, "native_part_cache_v6.json", time.Hour, 64)
	turn := maintainTestWrite(t, stateDir, "efficacy_turn_cache_v46.json", time.Hour, 32)
	ledger := maintainTestWrite(t, stateDir, "roi_ledger_v1.json", time.Hour, 16)
	backup := maintainTestWrite(t, stateDir, "native_part_cache_v6.json.keep", time.Hour, 8)
	csr := maintainTestWrite(t, dbDir, "memory.db.zcsr", time.Hour, 128)
	csrGen := maintainTestWrite(t, dbDir, "memory.db.zcsr.gen", time.Hour, 4)

	byPath := map[string]maintainItem{}
	for _, item := range maintainScanCaches(stateDir, now) {
		byPath[item.Path] = item
	}
	if byPath[part].Action != "clear" || byPath[turn].Action != "clear" {
		t.Fatalf("managed caches must be clearable: part=%q turn=%q", byPath[part].Action, byPath[turn].Action)
	}
	if _, listed := byPath[csr]; listed {
		t.Fatalf("engine disk-cluster cache is not a state cache; must not be listed")
	}
	if _, listed := byPath[csrGen]; listed {
		t.Fatalf("engine disk-cluster fingerprint is not a state cache; must not be listed")
	}
	if _, listed := byPath[ledger]; listed {
		t.Fatalf("roi ledger is live state, not a cache; must not be listed")
	}
	if _, listed := byPath[backup]; listed {
		t.Fatalf("non-matching suffix must not be listed")
	}

	kept := map[string]maintainItem{}
	for _, item := range maintainScanEngineCache(dbPath, false, now) {
		kept[item.Path] = item
	}
	if kept[csr].Action != "keep" || kept[csrGen].Action != "keep" {
		t.Fatalf("engine cache must be kept by default: csr=%q gen=%q", kept[csr].Action, kept[csrGen].Action)
	}
	purged := map[string]maintainItem{}
	for _, item := range maintainScanEngineCache(dbPath, true, now) {
		purged[item.Path] = item
	}
	if purged[csr].Action != "clear" || purged[csrGen].Action != "clear" {
		t.Fatalf("explicit purge must mark the engine cache clearable: csr=%q gen=%q", purged[csr].Action, purged[csrGen].Action)
	}
}

func TestMaintainApplyCleanRemovesPlannedAndWritesReceipt(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()
	deletable := maintainTestWrite(t, dir, "rmem.bak-0001", time.Hour, 10)
	kept := maintainTestWrite(t, dir, "rmem.bak-0002", time.Hour, 10)

	result, err := maintainApplyClean([]maintainItem{
		{Path: deletable, Bytes: 10, Action: "delete"},
		{Path: kept, Bytes: 10, Action: "keep"},
	}, stateDir, time.Now())
	if err != nil {
		t.Fatalf("apply clean: %v", err)
	}
	if _, err := os.Stat(deletable); !os.IsNotExist(err) {
		t.Fatalf("planned file must be removed")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("kept file must remain: %v", err)
	}
	if got := result["freed_bytes"].(int64); got != 10 {
		t.Fatalf("freed_bytes=%d, want 10", got)
	}
	receipt, _ := result["receipt"].(string)
	body, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatalf("receipt must exist: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("receipt must be valid JSON: %v", err)
	}
	if removed, _ := parsed["removed"].([]any); len(removed) != 1 {
		t.Fatalf("receipt must record exactly one removed file, got %v", parsed["removed"])
	}
}

// maintainTestEnv 把 HOME / XDG 目录整体挪进临时目录，避免测试读写真机状态目录。
func maintainTestEnv(t *testing.T) (home string, stateDir string) {
	t.Helper()
	home = t.TempDir()
	stateDir = filepath.Join(home, ".local", "state", "rolling-memory")
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	return home, stateDir
}

func maintainRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestMaintainStatusAndCleanEndToEnd(t *testing.T) {
	home, stateDir := maintainTestEnv(t)
	dbDir := filepath.Join(home, ".local", "share", "rolling-memory")
	dbPath := filepath.Join(dbDir, "memory.db")
	binDir := filepath.Join(dbDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	oldest := maintainTestWrite(t, binDir, "rmem.bak-0101", 72*time.Hour, 100)
	maintainTestWrite(t, binDir, "rmem.bak-0202", 40*time.Hour, 100)
	maintainTestWrite(t, binDir, "rmem.bak-0303", 2*time.Hour, 100)
	cachePath := maintainTestWrite(t, stateDir, "native_part_cache_v6.json", time.Hour, 50)
	engineCSR := maintainTestWrite(t, dbDir, "memory.db.zcsr", time.Hour, 70)
	engineGen := maintainTestWrite(t, dbDir, "memory.db.zcsr.gen", time.Hour, 7)

	code, stdout, stderr := maintainRun(t, "maintain", "status", "--json", "--db", dbPath)
	if code != 0 {
		t.Fatalf("maintain status exit=%d stderr=%s", code, stderr)
	}
	report := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, stdout)
	}
	backups, _ := report["backups"].(map[string]any)
	if backups == nil {
		t.Fatalf("status must carry a backups section: %s", stdout)
	}
	if got := int(backups["reclaimable_bytes"].(float64)); got != 100 {
		t.Fatalf("backups reclaimable=%d, want 100 (only the oldest)", got)
	}
	caches, _ := report["caches"].(map[string]any)
	if got := int(caches["reclaimable_bytes"].(float64)); got != 50 {
		t.Fatalf("caches reclaimable=%d, want 50", got)
	}
	engineCache, _ := report["engine_cache"].(map[string]any)
	if engineCache == nil {
		t.Fatalf("status must carry an engine_cache section: %s", stdout)
	}
	if got := int(engineCache["reclaimable_bytes"].(float64)); got != 0 {
		t.Fatalf("engine cache reclaimable=%d, want 0 (kept by default)", got)
	}
	if got := int(engineCache["total_bytes"].(float64)); got != 77 {
		t.Fatalf("engine cache total=%d, want 77", got)
	}
	reclaim, _ := report["reclaimable"].(map[string]any)
	if reclaim == nil || reclaim["total_bytes"] == nil {
		t.Fatalf("status must carry reclaimable totals: %s", stdout)
	}

	// 预演：不落盘。
	code, stdout, stderr = maintainRun(t, "maintain", "clean", "--json", "--db", dbPath)
	if code != 0 {
		t.Fatalf("clean preview exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(oldest); err != nil {
		t.Fatalf("preview must not delete anything: %v", err)
	}
	if !strings.Contains(stdout, `"apply": false`) {
		t.Fatalf("preview must report apply=false: %s", stdout)
	}

	// --apply 缺 --yes：拒绝执行。
	code, _, stderr = maintainRun(t, "maintain", "clean", "--db", dbPath, "--apply")
	if code != 1 || !strings.Contains(stderr, "--yes") {
		t.Fatalf("apply without --yes must fail with guidance, code=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(oldest); err != nil {
		t.Fatalf("refused apply must not delete anything: %v", err)
	}

	// 执行：备份清理 + --caches 清缓存。
	code, stdout, stderr = maintainRun(t, "maintain", "clean", "--json", "--db", dbPath, "--apply", "--yes", "--caches")
	if code != 0 {
		t.Fatalf("clean apply exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatalf("oldest backup must be removed after apply")
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("cache must be cleared with --caches")
	}
	if _, err := os.Stat(engineCSR); err != nil {
		t.Fatalf("engine cache must survive a --caches cleanup: %v", err)
	}
	if _, err := os.Stat(engineGen); err != nil {
		t.Fatalf("engine cache fingerprint must survive a --caches cleanup: %v", err)
	}
	result := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("clean JSON: %v", err)
	}
	if got := int(result["removed"].(float64)); got != 2 {
		t.Fatalf("removed=%d, want 2 (one backup + one cache)", got)
	}
	if got := int(result["freed_bytes"].(float64)); got != 150 {
		t.Fatalf("freed_bytes=%d, want 150", got)
	}

	// 显式 --purge-engine-cache：此时才删除引擎磁盘簇缓存。
	code, stdout, stderr = maintainRun(t, "maintain", "clean", "--json", "--db", dbPath, "--apply", "--yes", "--purge-engine-cache")
	if code != 0 {
		t.Fatalf("clean purge apply exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(engineCSR); !os.IsNotExist(err) {
		t.Fatalf("engine cache must be removed with --purge-engine-cache")
	}
	if _, err := os.Stat(engineGen); !os.IsNotExist(err) {
		t.Fatalf("engine cache fingerprint must be removed with --purge-engine-cache")
	}
	purged := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &purged); err != nil {
		t.Fatalf("purge JSON: %v", err)
	}
	if got := int(purged["removed"].(float64)); got != 2 {
		t.Fatalf("removed=%d, want 2 (engine cache + fingerprint)", got)
	}
	if got := int(purged["freed_bytes"].(float64)); got != 77 {
		t.Fatalf("freed_bytes=%d, want 77", got)
	}
}

func TestMaintainCompactPreviewThenApply(t *testing.T) {
	home, _ := maintainTestEnv(t)
	dbPath := filepath.Join(home, "vault", "memory.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	memory, err := facade.New(&config.Config{DB: dbPath, Scope: "mem:maintain-test", Mode: "local"})
	if err != nil {
		t.Fatalf("seed vault: %v", err)
	}
	connection := memory.Store.DB()
	if _, err := connection.Exec("CREATE TABLE maintain_filler(x BLOB)"); err != nil {
		t.Fatalf("create filler: %v", err)
	}
	for index := 0; index < 256; index++ {
		if _, err := connection.Exec("INSERT INTO maintain_filler VALUES (zeroblob(4096))"); err != nil {
			t.Fatalf("insert filler: %v", err)
		}
	}
	if _, err := connection.Exec("DELETE FROM maintain_filler"); err != nil {
		t.Fatalf("delete filler: %v", err)
	}
	if err := memory.Close(); err != nil {
		t.Fatalf("close seed vault: %v", err)
	}

	code, stdout, stderr := maintainRun(t, "maintain", "compact", "--json", "--db", dbPath, "--scope", "mem:maintain-test")
	if code != 0 {
		t.Fatalf("compact preview exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	preview := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatalf("compact preview JSON: %v\n%s", err, stdout)
	}
	if get := int(preview["free_bytes"].(float64)); get <= 0 {
		t.Fatalf("preview must report reclaimable free pages, got %d\n%s", get, stdout)
	}
	if apply, _ := preview["apply"].(bool); apply {
		t.Fatalf("preview must report apply=false")
	}

	code, stdout, stderr = maintainRun(t, "maintain", "compact", "--json", "--db", dbPath, "--scope", "mem:maintain-test", "--apply", "--yes")
	if code != 0 {
		t.Fatalf("compact apply exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	result := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("compact JSON: %v\n%s", err, stdout)
	}
	if freed := int(result["freed_bytes"].(float64)); freed <= 0 {
		t.Fatalf("VACUUM must reclaim the freed pages, freed=%d\n%s", freed, stdout)
	}
	if after := int(result["after_bytes"].(float64)); after >= int(result["before_bytes"].(float64)) {
		t.Fatalf("after_bytes must be smaller: after=%d before=%v", after, result["before_bytes"])
	}
}
