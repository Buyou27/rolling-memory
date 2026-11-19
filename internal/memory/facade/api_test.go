// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
	"github.com/rocareer/rolling-memory/internal/version"
)

const testNow = int64(1793102400)

func TestMain(m *testing.M) {
	// UTC stays for the whole process: restoring time.Local after m.Run would
	// write a global while goroutines outliving their test (vault snapshot
	// refreshes) may still read it, and the process exits immediately after.
	time.Local = time.UTC
	code := m.Run()
	os.Exit(code)
}

func strPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func newMemory(t *testing.T) *facade.Memory {
	t.Helper()
	restore := clock.SetForTest(time.Unix(testNow, 0), 1_000_000_000)
	t.Cleanup(restore)
	directory := t.TempDir()
	dbPath := filepath.Join(directory, "memory.db")
	configPath := filepath.Join(directory, "config.toml")
	mode := "local"
	scope := "mem:test"
	reader := "deliverable"
	cfg, err := config.Load(config.LoadOptions{
		Path: &configPath, DB: &dbPath, Mode: &mode, Scope: &scope, Reader: &reader,
		Env: map[string]string{},
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.Log = filepath.Join(directory, "state", "rolling-memory.log")
	memory, err := facade.New(cfg)
	if err != nil {
		t.Fatalf("api: %v", err)
	}
	// Fresh production databases bootstrap thin; this suite exercises the
	// legacy assertion-graph surfaces, so apply its schema on the test database.
	if err := db.EnsureLegacyGraphSchema(memory.Store.DB()); err != nil {
		t.Fatalf("legacy graph schema: %v", err)
	}
	t.Cleanup(func() { memory.Close() })
	return memory
}

func fact(s, x string, o any) map[string]any {
	return map[string]any{
		"s": map[string]any{"label": s},
		"x": x,
		"o": o,
	}
}

func rememberOne(t *testing.T, memory *facade.Memory, content, s, x string, o any) map[string]any {
	t.Helper()
	receipt, err := memory.Remember(facade.RememberRequest{
		Content: strPtr(content), AnchorText: strPtr(content), Assertion: fact(s, x, o),
		Author: "user", AutoSync: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	return receipt
}

func TestStatusAndDoctor(t *testing.T) {
	memory := newMemory(t)
	status, err := memory.Status("mem:test")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status["version"] != version.Version {
		t.Fatalf("version = %#v", status["version"])
	}
	if _, ok := status["config"].(map[string]any); !ok {
		t.Fatalf("config = %#v", status["config"])
	}
	if _, ok := status["llm_warning"]; ok {
		t.Fatal("status still reports the retired llm_warning field")
	}
	if _, ok := status["vector_warning"]; ok {
		t.Fatal("status still reports the retired vector_warning field")
	}
	doctor, err := memory.Doctor()
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	checks := doctor["checks"].([]any)
	names := []string{}
	for _, check := range checks {
		names = append(names, check.(map[string]any)["name"].(string))
	}
	want := []string{"db_exists", "db_writable", "config", "json1", "corpus"}
	if len(names) != len(want) {
		t.Fatalf("checks = %#v", names)
	}
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("checks = %#v", names)
		}
	}
	if doctor["python"] != "" {
		t.Fatalf("python placeholder = %#v", doctor["python"])
	}
}

func TestRecentOrdersAndFilters(t *testing.T) {
	memory := newMemory(t)
	old, err := memory.Remember(facade.RememberRequest{
		Content: strPtr("老张养了一只猫。"), AnchorText: strPtr("老张养了一只猫"),
		Assertion:  fact("老张", "养", map[string]any{"label": "猫"}),
		OccurredAt: int64(1793000000), Author: "user", AutoSync: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("old: %v", err)
	}
	newer, err := memory.Remember(facade.RememberRequest{
		Content: strPtr("老张住在北京。"), AnchorText: strPtr("老张住在北京"),
		Assertion:  fact("老张", "住在", map[string]any{"label": "北京"}),
		OccurredAt: int64(1793100000), Author: "user", AutoSync: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	oldID := old["written"].([]any)[0].(map[string]any)["id"]
	newID := newer["written"].([]any)[0].(map[string]any)["id"]
	rows, err := memory.Recent(nil, nil, 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(rows) != 2 || rows[0]["id"] != newID || rows[1]["id"] != oldID {
		t.Fatalf("recent = %#v", rows)
	}
	if _, err := memory.Remember(facade.RememberRequest{
		Content: strPtr("老张的暗号是蓝鸟。"), AnchorText: strPtr("老张的暗号是蓝鸟"),
		Assertion: fact("老张", "暗号", map[string]any{"literal": "蓝鸟"}),
		Audience:  "private", Author: "user", AutoSync: boolPtr(false),
	}); err != nil {
		t.Fatalf("private: %v", err)
	}
	deliverable, err := memory.Recent(nil, nil, 10)
	if err != nil {
		t.Fatalf("recent deliverable: %v", err)
	}
	for _, row := range deliverable {
		if row["audience"] == "private" {
			t.Fatalf("deliverable recent leaked private: %#v", row)
		}
	}
	owner, err := memory.Recent(nil, strPtr("owner"), 10)
	if err != nil {
		t.Fatalf("recent owner: %v", err)
	}
	if len(owner) != len(deliverable)+1 {
		t.Fatalf("owner=%d deliverable=%d", len(owner), len(deliverable))
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	memory := newMemory(t)
	if err := memory.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := memory.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestLocalImportIngestsCanonicalUnitsIntoVaultRecall(t *testing.T) {
	memory := newMemory(t)
	scope := "mem:test"
	session := "transcript"
	path := writeTranscript(t, "transcript.jsonl", strings.Join([]string{
		`{"type":"user","sessionId":"cm1","timestamp":"2026-03-02T08:00:00Z","message":{"role":"user","content":"老张搬到了上海。"}}`,
		`{"type":"assistant","sessionId":"cm1","timestamp":"2026-03-02T08:01:00Z","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"记住了。"}]}}`,
	}, "\n")+"\n")
	if _, err := memory.ImportFile(path, &scope, &session, "shared", false); err != nil {
		t.Fatalf("import: %v", err)
	}
	reader := "owner"
	result, err := memory.Recall(facade.RecallRequest{Cue: "老张搬到哪里了？", Reader: &reader})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(result.Bands) == 0 {
		t.Fatalf("expected local vault recall bands, got %+v", result)
	}
	found := false
	for _, hit := range result.RawHits {
		if text, _ := hit["text"].(string); strings.Contains(text, "上海") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a raw hit containing 上海, got %+v", result.RawHits)
	}
	recent, err := memory.Recent(&scope, &reader, 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) == 0 {
		t.Fatalf("expected recent vault items after import")
	}
}

// TestLocalModeNeverUsesCloudTransport locks the no-egress invariant: stored
// credentials (BRAIN_KEY) must not override `mode=local` when no explicit
// BRAIN_URL is set. Explicit BRAIN_URL precedence is covered by
// TestThinClientBrainURLEnvPrecedence.
func TestLocalModeNeverUsesCloudTransport(t *testing.T) {
	t.Setenv("ROLLING_MEMORY_BRAIN_URL", "")
	t.Setenv("ROLLING_MEMORY_BRAIN_KEY", "mem2.test.local-only")
	memory := newMemory(t)
	tc, err := memory.ThinClient()
	if err != nil {
		t.Fatalf("thin client: %v", err)
	}
	if _, ok := tc.Transport.(*thinclient.LocalVaultTransport); !ok {
		t.Fatalf("local mode must use LocalVaultTransport, got %T", tc.Transport)
	}
}
