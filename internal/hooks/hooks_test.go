// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/episodicunit"
	"github.com/rocareer/rolling-memory/internal/ids"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/memory/recall"
	"github.com/rocareer/rolling-memory/internal/store"
	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
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

// testEnv isolates every path the hook touches: HOME/XDG for the defaults and
// explicit ROLLING_MEMORY_* overrides so no live memory core is ever opened.
type testEnv struct {
	base       string
	db         string
	log        string
	configPath string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	restore := clock.SetForTest(time.Unix(testNow, 0), 1_000_000_000)
	t.Cleanup(restore)
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("home: %v", err)
	}
	env := &testEnv{
		base:       base,
		db:         filepath.Join(base, "memory.db"),
		log:        filepath.Join(base, "state", "rolling-memory.log"),
		configPath: filepath.Join(base, "config.toml"),
	}
	variables := map[string]string{
		"HOME":                              home,
		"XDG_CONFIG_HOME":                   filepath.Join(home, "config"),
		"XDG_DATA_HOME":                     filepath.Join(home, "data"),
		"XDG_STATE_HOME":                    filepath.Join(home, "state"),
		"ROLLING_MEMORY_CONFIG":             env.configPath,
		"ROLLING_MEMORY_DB":                 env.db,
		"ROLLING_MEMORY_LOG":                env.log,
		"ROLLING_MEMORY_SCOPE":              "mem:hooks",
		"ROLLING_MEMORY_MODE":               "local",
		"ROLLING_MEMORY_READER":             "deliverable",
		"ROLLING_MEMORY_LLM_PROVIDER":       "",
		"ROLLING_MEMORY_LLM_BASE_URL":       "",
		"ROLLING_MEMORY_LLM_API_KEY_ENV":    "",
		"ROLLING_MEMORY_EMBEDDING_PROVIDER": "",
		"ROLLING_MEMORY_SYNC_BASE_URL":      "",
		"OPENAI_API_KEY":                    "",
		"ROLLING_MEMORY_CLOUD_KEY":          "",
	}
	for key, value := range variables {
		t.Setenv(key, value)
	}
	return env
}

func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	return cfg
}

func openMemory(t *testing.T) *facade.Memory {
	t.Helper()
	memory, err := facade.New(loadConfig(t))
	if err != nil {
		t.Fatalf("api: %v", err)
	}
	// Fresh production databases bootstrap thin; this suite exercises the
	// legacy assertion-graph surfaces (structured seeds, historical tables),
	// so apply its schema on the test database.
	if err := db.EnsureLegacyGraphSchema(memory.Store.DB()); err != nil {
		t.Fatalf("legacy graph schema: %v", err)
	}
	t.Cleanup(func() { memory.Close() })
	return memory
}

// seedCat writes one synthetic structured fact so recall has something to cite.
func seedCat(t *testing.T) {
	t.Helper()
	memory := openMemory(t)
	if _, err := memory.Remember(facade.RememberRequest{
		Content:    strPtr("我养了一只猫，叫小白。"),
		AnchorText: strPtr("我养了一只猫，叫小白。"),
		Assertion: map[string]any{
			"s": map[string]any{"label": "我"},
			"x": "养",
			"o": map[string]any{"label": "小白"},
		},
		Author: "user", AutoSync: boolPtr(false),
	}); err != nil {
		t.Fatalf("remember: %v", err)
	}
}

func runHookMain(t *testing.T, args []string, input string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := HookMain(args, strings.NewReader(input), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func logLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log %s: %v", path, err)
	}
	lines := []map[string]any{}
	for _, text := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(text) == "" {
			continue
		}
		parsed := map[string]any{}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("log line is not JSON: %q", text)
		}
		lines = append(lines, parsed)
	}
	return lines
}

func TestLocalHookNeverContactsBrain(t *testing.T) {
	env := newTestEnv(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("ROLLING_MEMORY_BRAIN_URL", server.URL+"/open/memory/v1")
	t.Setenv("ROLLING_MEMORY_BRAIN_KEY", "test-key")
	payload := fmt.Sprintf(`{"session_id":"local-sentinel","cwd":%q,"prompt":"local sentinel"}`, env.base)
	code, stdout, stderr := runHookMain(t, []string{"recall", "--json"}, payload)
	if code != 0 {
		t.Fatalf("hook exit=%d stdout=%q stderr=%s", code, stdout, stderr)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("local hook made %d remote requests", got)
	}
}

// TestRecallHookNeverInjectsAndCadenceReminds verifies the hook contract: the
// UserPromptSubmit hook never runs automatic recall and never injects recalled
// material; it only captures the prompt locally and, on the four-turn cadence in
// a workspace session, emits the one-line reminder (as the JSON envelope).
func TestRecallHookNeverInjectsAndCadenceReminds(t *testing.T) {
	env := newTestEnv(t)
	seedCat(t)
	wsDir := filepath.Join(env.base, "Rolling-Memory")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	prompt := func(turn int) string {
		return fmt.Sprintf("继续推进第 %d 步：我喜欢猫，我的猫叫小白。", turn)
	}
	payload := func(turn int) string {
		raw, _ := json.Marshal(map[string]any{
			"session_id": "s1",
			"cwd":        wsDir,
			"prompt":     prompt(turn),
		})
		return string(raw)
	}

	for turn := 1; turn <= 3; turn++ {
		code, stdout, stderr := runHookMain(t, []string{"recall", "--json"}, payload(turn))
		if code != 0 || strings.TrimSpace(stdout) != "" {
			t.Fatalf("turn %d must stay silent: exit=%d stdout=%q stderr=%s", turn, code, stdout, stderr)
		}
	}
	// Turn 4 hits the four-turn cadence: a static reminder, never the seeded
	// memory itself.
	code, stdout, stderr := runHookMain(t, []string{"recall", "--json"}, payload(4))
	if code != 0 {
		t.Fatalf("cadence turn exit = %d, stderr = %s", code, stderr)
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("output must end with a newline: %q", stdout)
	}
	// Canonical JSON separators (", ", ": ") and key order.
	prefix := `{"hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": "`
	if !strings.HasPrefix(stdout, prefix) {
		t.Fatalf("JSON envelope drifted from expected shape: %q", stdout)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	hook, ok := decoded["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("missing hookSpecificOutput: %#v", decoded)
	}
	if hook["hookEventName"] != "UserPromptSubmit" {
		t.Fatalf("hookEventName = %#v", hook["hookEventName"])
	}
	context, _ := hook["additionalContext"].(string)
	if !strings.Contains(context, "[Rolling-Memory 4轮节律锚点]") {
		t.Fatalf("cadence reminder missing:\n%s", context)
	}
	if strings.Contains(context, "小白") || strings.Contains(context, "<rolling-memory-recall") {
		t.Fatalf("hook must never inject recalled material:\n%s", context)
	}

	lines := logLines(t, env.log)
	if len(lines) != 4 {
		t.Fatalf("log lines = %#v", lines)
	}
	for index, line := range lines {
		if line["event"] != "recall" || line["injected"].(float64) != 0 {
			t.Fatalf("recall log line %d = %#v", index, line)
		}
	}
	if cadence, ok := lines[3]["cadence"].(float64); !ok || cadence != 1 {
		t.Fatalf("cadence turn must be logged with cadence=1: %#v", lines[3])
	}
}

func TestRecallHookCadenceRemindsOnVaultOnlyDB(t *testing.T) {
	env := newTestEnv(t)
	wsDir := filepath.Join(env.base, "Rolling-Memory")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	mem := openMemory(t)
	if _, err := mem.Store.DB().Exec(`PRAGMA foreign_keys = OFF; DROP TABLE IF EXISTS assertion; DROP TABLE IF EXISTS trace; DROP TABLE IF EXISTS node; PRAGMA foreign_keys = ON;`); err != nil {
		t.Fatalf("drop legacy tables: %v", err)
	}
	if mem.Store.HasLegacyGraph() {
		t.Fatal("expected HasLegacyGraph() == false after dropping assertion/trace/node")
	}
	mem.Close()

	payload := func(turn int) string {
		raw, _ := json.Marshal(map[string]any{
			"session_id": "s-vault-only",
			"cwd":        wsDir,
			"prompt":     fmt.Sprintf("继续推进第 %d 步", turn),
		})
		return string(raw)
	}
	for turn := 1; turn <= 3; turn++ {
		code, stdout, stderr := runHookMain(t, []string{"recall", "--json"}, payload(turn))
		if code != 0 || strings.TrimSpace(stdout) != "" {
			t.Fatalf("turn %d must stay silent on vault-only DB: exit=%d stdout=%q stderr=%s", turn, code, stdout, stderr)
		}
	}
	code, stdout, stderr := runHookMain(t, []string{"recall", "--json"}, payload(4))
	if code != 0 || !strings.Contains(stdout, "[Rolling-Memory 4轮节律锚点]") {
		t.Fatalf("turn 4 must emit cadence reminder on vault-only DB: exit=%d stdout=%q stderr=%s", code, stdout, stderr)
	}
}

func TestRecallEmptyAndAbsentAreSilent(t *testing.T) {
	env := newTestEnv(t)
	code, stdout, _ := runHookMain(t, []string{"recall", "--json"}, "{}")
	if code != 0 || stdout != "" {
		t.Fatalf("empty prompt: exit=%d stdout=%q", code, stdout)
	}
	if _, err := os.Stat(env.log); !os.IsNotExist(err) {
		t.Fatalf("silent empty prompt must not log, stat err = %v", err)
	}
	code, stdout, _ = runHookMain(t, []string{"recall", "--json"},
		`{"prompt":"zzzqqqxxx","session_id":"s1"}`)
	if code != 0 || stdout != "" {
		t.Fatalf("absent recall: exit=%d stdout=%q", code, stdout)
	}
	lines := logLines(t, env.log)
	if len(lines) != 1 || lines[0]["absent"].(float64) != 1 || lines[0]["injected"].(float64) != 0 {
		t.Fatalf("absent log line = %#v", lines)
	}
}

func TestBudgetFlag(t *testing.T) {
	newTestEnv(t)
	seedCat(t)
	// The flag stays accepted on the hook surface; retrieval is agent-driven, so
	// the hook itself stays silent either way.
	code, stdout, stderr := runHookMain(t,
		[]string{"recall", "--json", "--budget-ms", " 5000 "}, `{"prompt":"小白"}`)
	if code != 0 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("budget flag: exit=%d stdout=%q stderr=%s", code, stdout, stderr)
	}
	// A trailing flag without a value falls back to the config budget.
	code, stdout, _ = runHookMain(t, []string{"recall", "--json", "--budget-ms"},
		`{"session_id":"s2","prompt":"小白"}`)
	if code != 0 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("valueless budget flag: exit=%d stdout=%q", code, stdout)
	}
	// standard raises ValueError before its try block, so the CLI exits 1.
	code, stdout, stderr = runHookMain(t, []string{"recall", "--budget-ms", "5x"},
		`{"session_id":"s3","prompt":"小白"}`)
	if code != 1 {
		t.Fatalf("bad budget exit = %d", code)
	}
	if stdout != "" || !strings.Contains(stderr, "error: invalid literal for int() with base 10: '5x'") {
		t.Fatalf("bad budget stderr = %q stdout = %q", stderr, stdout)
	}
	// With --json cli.main prints the error object on stdout instead.
	code, stdout, stderr = runHookMain(t, []string{"recall", "--json", "--budget-ms", "5x"},
		`{"prompt":"小白"}`)
	if code != 1 || stderr != "" {
		t.Fatalf("json bad budget: exit=%d stderr=%q", code, stderr)
	}
	want := `{"ok": false, "error": "invalid literal for int() with base 10: '5x'"}` + "\n"
	if stdout != want {
		t.Fatalf("json bad budget stdout = %q, want %q", stdout, want)
	}
}

func TestBudgetFlagAcceptsPythonIntForms(t *testing.T) {
	cases := map[string]int{
		"42": 42, " 42 ": 42, "+42": 42, "-7": -7, "1_000": 1000, "007": 7,
	}
	for raw, want := range cases {
		got, err := pyInt(raw)
		if err != nil || got != want {
			t.Fatalf("pyInt(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", " ", "1.5", "_1", "1_", "1__0", "0x10", "abc"} {
		if _, err := pyInt(raw); err == nil {
			t.Fatalf("pyInt(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestHookMainFailOpenOnBadInput(t *testing.T) {
	newTestEnv(t)
	code, stdout, _ := runHookMain(t, []string{"recall", "--json"}, "{")
	if code != 0 || stdout != "" {
		t.Fatalf("invalid JSON: exit=%d stdout=%q", code, stdout)
	}
	fallback := filepath.Join(config.DefaultStateDir(), "hook.log")
	raw, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatalf("fallback log: %v", err)
	}
	if !strings.Contains(string(raw), "hook input error: invalid JSON") {
		t.Fatalf("fallback log = %q", raw)
	}
	// The fail-open log carries json.JSONDecodeError.msg, not the Go decoder
	// error ("unexpected EOF"); golden 230/070 pins the wording.
	if !strings.Contains(string(raw), "invalid JSON: Expecting property name enclosed in double quotes") {
		t.Fatalf("fallback log wording = %q", raw)
	}
	if !strings.HasPrefix(string(raw), fmt.Sprintf("%d ", testNow)) {
		t.Fatalf("fallback line must start with the epoch: %q", raw)
	}
}

func TestHookMainInputTooLarge(t *testing.T) {
	newTestEnv(t)
	oversized := strings.Repeat("a", maxHookInput+1)
	code, stdout, _ := runHookMain(t, []string{"recall", "--json"}, oversized)
	if code != 0 || stdout != "" {
		t.Fatalf("oversize input: exit=%d stdout=%q", code, stdout)
	}
	raw, err := os.ReadFile(filepath.Join(config.DefaultStateDir(), "hook.log"))
	if err != nil {
		t.Fatalf("fallback log: %v", err)
	}
	if !strings.Contains(string(raw), "hook input too large") {
		t.Fatalf("fallback log = %q", raw)
	}
}

func TestHookLogCarriesScopeForLiveScopes(t *testing.T) {
	env := newTestEnv(t)
	cfg := loadConfig(t)
	cfg.Scope = "mem:self"
	logHook(cfg, "recall", map[string]any{"status": "absent"})
	raw, err := os.ReadFile(env.log)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(raw), `"scope": "mem:self"`) {
		t.Fatalf("expected live scope in hook log, got %s", string(raw))
	}
}

func TestHookMainNonObjectPayloadIsEmpty(t *testing.T) {
	newTestEnv(t)
	code, stdout, _ := runHookMain(t, []string{"recall", "--json"}, "[1, 2, 3]")
	if code != 0 || stdout != "" {
		t.Fatalf("list payload: exit=%d stdout=%q", code, stdout)
	}
}

func TestHookMainFailOpenWhenStoreCannotOpen(t *testing.T) {
	env := newTestEnv(t)
	blocker := filepath.Join(env.base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("blocker: %v", err)
	}
	t.Setenv("ROLLING_MEMORY_DB", filepath.Join(blocker, "memory.db"))
	code, stdout, _ := runHookMain(t, []string{"recall", "--json"}, `{"prompt":"小白"}`)
	if code != 0 || stdout != "" {
		t.Fatalf("unopenable store: exit=%d stdout=%q", code, stdout)
	}
	raw, err := os.ReadFile(filepath.Join(config.DefaultStateDir(), "hook.log"))
	if err != nil || len(raw) == 0 {
		t.Fatalf("fallback log missing: %v", err)
	}
}

func TestHookMainUnknownEventFailsOpen(t *testing.T) {
	newTestEnv(t)
	code, stdout, _ := runHookMain(t, []string{"bogus"}, "{}")
	if code != 0 || stdout != "" {
		t.Fatalf("unknown event: exit=%d stdout=%q", code, stdout)
	}
}

func TestEpisodicUploadReportsFailuresAndSkipsDeleted(t *testing.T) {
	env := newTestEnv(t)
	path := writeTranscript(t, filepath.Join(env.base, "s1.jsonl"))
	memory := openMemory(t)
	tc, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	userID := episodicunit.DocID(memory.Config.Scope, "[user] 我喜欢猫。我的猫叫小白。")
	tc.Vault.RecordDeletedUnits("episodic", userID)
	payload := map[string]any{"session_id": "s1", "transcript_path": path}
	receipt := uploadCurrentEpisodicTurn(memory, memory.Config, "s1", payload)
	if receipt["uploaded"] != 1 {
		t.Fatalf("deleted unit receipt = %+v", receipt)
	}
	if _, err := os.Stat(env.log); err == nil {
		for _, line := range logLines(t, env.log) {
			if line["vault_write_error"] != nil {
				t.Fatalf("deleted unit logged as failure: %+v", line)
			}
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

}

func TestCurrentTurnUnitsPreservesUnknownTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turn.jsonl")
	content := `{"type":"user","message":{"role":"user","content":"unknown time"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"answer without timestamp"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	units, _ := currentTurnUnits("mem:test", "s1", map[string]any{"transcript_path": path})
	if len(units) != 2 || units[0].OccurredAt != 0 || units[1].OccurredAt != 0 {
		t.Fatalf("unknown source time = %+v", units)
	}
	rows := payloadTurnRows(map[string]any{"last_assistant_message": "payload without timestamp"})
	units, _ = unitsForSegments("mem:test", "", segmentsFromRows(rows), 0, true, len(rows))
	if len(units) != 1 || units[0].OccurredAt != 0 {
		t.Fatalf("unknown payload time = %+v", units)
	}
}

func TestCurrentTurnUnitsRequiresCompletionEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	user := `{"type":"user","message":{"role":"user","content":"user evidence"}}` + "\n"
	tool := `{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"text","text":"tool narration"},{"type":"tool_use","name":"shell"}]}}` + "\n"
	final := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"final answer"}}` + "\n"
	cases := []struct {
		name    string
		content string
		payload map[string]any
		want    []string
	}{
		{"completed", user + tool + final, nil, []string{"[user] user evidence", "[assistant] final answer"}},
		{"tool step without final", user + tool, nil, nil},
		{"plain assistant without completion", user + `{"type":"assistant","message":{"role":"assistant","content":"not marked"}}` + "\n", nil, nil},
		{"temporary assistant transcript", `{"message":{"role":"assistant","content":[{"type":"text","text":"partial preview"}]}}` + "\n", map[string]any{"hook_event_name": "Stop", "last_assistant_message": "partial preview"}, nil},
		{"latest turn incomplete", user + final + `{"type":"user","message":{"role":"user","content":"next"}}` + "\n" + tool, nil, nil},
		{"truncated completion", user + tool + `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn"`, nil, nil},
		{"codex completed", `{"type":"event_msg","payload":{"type":"task_started","turn_id":"a"}}` + "\n" + `{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"a"},"content":[{"type":"input_text","text":"user evidence"}]}}` + "\n" + `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"final answer"}]}}` + "\n" + `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"a"}}` + "\n", nil, []string{"[user] user evidence", "[assistant] final answer"}},
		{"codex aborted", `{"type":"event_msg","payload":{"type":"task_started","turn_id":"a"}}` + "\n" + `{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"a"},"content":[{"type":"input_text","text":"user evidence"}]}}` + "\n" + `{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"a"}}` + "\n", nil, nil},
		{"codex mismatched completion", `{"type":"event_msg","payload":{"type":"task_started","turn_id":"a"}}` + "\n" + `{"type":"response_item","payload":{"type":"message","role":"user","internal_chat_message_metadata_passthrough":{"turn_id":"a"},"content":[{"type":"input_text","text":"user evidence"}]}}` + "\n" + `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"other"}}` + "\n", nil, nil},
		{"dsh completed", `{"type":"turn/start","data":{"turn":1}}` + "\n" + `{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"user evidence"}]}}` + "\n" + `{"type":"assistant/message","data":{"message":{"role":"assistant","content":[{"type":"text","text":"final answer"}]}}}` + "\n" + `{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}` + "\n", nil, []string{"[user] user evidence", "[assistant] final answer"}},
		{"dsh aborted", `{"type":"turn/start","data":{"turn":1}}` + "\n" + `{"type":"user/message","data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"user evidence"}]}}` + "\n" + `{"type":"turn/end","data":{"turn":1,"reason":{"kind":"aborted"}}}` + "\n", nil, nil},
		{"dsh untrusted source", `{"type":"turn/start","data":{"turn":1}}` + "\n" + `{"type":"user/message","data":{"role":"user","source":{"kind":"agent-instructions"},"content":[{"type":"text","text":"not user"}]}}` + "\n" + `{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}` + "\n", nil, nil},
		{"open turn with payload", user + tool, map[string]any{"hook_event_name": "Stop", "last_assistant_message": "fallback"}, nil},
		{"malformed source with payload", "not a transcript\n", map[string]any{"hook_event_name": "Stop", "last_assistant_message": "fallback"}, nil},
		{"invalid compressed source", "not a zstd frame", map[string]any{"hook_event_name": "Stop", "last_assistant_message": "fallback"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := path
			if tc.name == "invalid compressed source" {
				target += ".zst"
			}
			if err := os.WriteFile(target, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{"transcript_path": target}
			for key, value := range tc.payload {
				payload[key] = value
			}
			units, _ := currentTurnUnits("mem:test", "s1", payload)
			if len(units) != len(tc.want) {
				t.Fatalf("units=%+v want=%v", units, tc.want)
			}
			for i, text := range tc.want {
				if units[i].Text != text || !units[i].Completed {
					t.Fatalf("unit %d=%+v want=%q", i, units[i], text)
				}
			}
		})
	}
	missingPayload := map[string]any{"transcript_path": filepath.Join(dir, "missing.jsonl"), "hook_event_name": "Stop", "last_assistant_message": "fallback"}
	if units, _ := currentTurnUnits("mem:test", "s1", missingPayload); len(units) != 0 {
		t.Fatalf("missing source fell back to payload: %+v", units)
	}
	for name, payload := range map[string]map[string]any{
		"no signal":       {"last_assistant_message": "answer"},
		"stop only":       {"hook_event_name": "Stop", "last_assistant_message": "answer"},
		"active stop":     {"hook_event_name": "Stop", "stop_hook_active": true, "last_assistant_message": "answer"},
		"different event": {"hook_event_name": "UserPromptSubmit", "last_assistant_message": "answer"},
		"empty":           {"hook_event_name": "Stop", "last_assistant_message": ""},
		"tool payload":    {"hook_event_name": "Stop", "tool_name": "shell", "last_assistant_message": "answer"},
	} {
		t.Run("payload/"+name, func(t *testing.T) {
			if units, _ := currentTurnUnits("mem:test", "s1", payload); len(units) != 0 {
				t.Fatalf("unverified payload imported: %+v", units)
			}
		})
	}
}

// TestCurrentTurnUnitsAreSegments pins segment boundaries and chunked pointers.
func TestCurrentTurnUnitsAreSegments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turn.jsonl")
	workLog := strings.Repeat("正在定位失败断言并复跑全部包。", 2400)
	content := strings.Join([]string{
		`{"type":"user","sessionId":"s1","timestamp":"2026-10-20T10:00:00","message":{"role":"user","content":"继续执行你之前没有执行完的任务 中断的任务"}}`,
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-10-20T10:00:10","message":{"role":"assistant","stop_reason":"end_turn","content":[` +
			`{"type":"text","text":"先确认断言。"},` +
			`{"type":"text","text":"` + workLog + `"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write turn: %v", err)
	}
	units, _ := currentTurnUnits("mem:test", "s1", map[string]any{"transcript_path": path})
	if len(units) < 3 {
		t.Fatalf("expected the user segment plus chunked work-log segments, got %d", len(units))
	}
	if !strings.HasPrefix(units[0].Text, "[user] 继续执行你之前没有执行完的任务") {
		t.Fatalf("user instruction must be its own unit, got %q", units[0].Text)
	}
	if units[0].RefPointer != "@ref:jsonl:"+path+":0#0" {
		t.Fatalf("user segment pointer = %q", units[0].RefPointer)
	}
	if units[1].Text != "[assistant] 先确认断言。" {
		t.Fatalf("second segment = %q", units[1].Text)
	}
	// The oversized work log is split at sentence boundaries into zero-copy chunks:
	// every chunk stays under the rung and carries `#<segment>.<chunk>`, so delivery
	// resolves it live from the same transcript instead of storing plaintext.
	chunked := 0
	for index, unit := range units[2:] {
		if tokenzr.Count(unit.Text) > episodicunit.TokenBudget {
			t.Fatalf("chunk wider than the rung: %d tokens", tokenzr.Count(unit.Text))
		}
		want := fmt.Sprintf("@ref:jsonl:%s:0#2.%d", path, index)
		if unit.RefPointer != want {
			t.Fatalf("chunk %d pointer = %q, want %q", index, unit.RefPointer, want)
		}
		if unit.SegmentIndex != 2 || unit.ChunkIndex != index {
			t.Fatalf("chunk %d coords = (%d,%d), want (2,%d)", index, unit.SegmentIndex, unit.ChunkIndex, index)
		}
		chunked++
	}
	if chunked == 0 {
		t.Fatalf("expected the oversized segment to be chunked")
	}
	if units[0].TurnRef == "" || units[1].TurnRef == "" {
		t.Fatalf("every unit must carry its turn provenance: %+v", units)
	}
}

func writeTranscript(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := strings.Join([]string{
		`{"type":"user","sessionId":"s1","timestamp":"2026-10-20T10:00:00",` +
			`"message":{"role":"user","content":[{"type":"text","text":"我喜欢猫。我的猫叫小白。"}]}}`,
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-10-20T10:00:10",` +
			`"message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"记住了。"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("transcript: %v", err)
	}
	return path
}

func TestImportTurnsMovesTheCursor(t *testing.T) {
	env := newTestEnv(t)
	path := writeTranscript(t, filepath.Join(env.base, "s1.jsonl"))
	memory := openMemory(t)

	// A relative path resolves against payload cwd; the session falls back to
	// the transcript file stem.
	receipt, err := importTurns(memory, map[string]any{
		"transcript_path": "s1.jsonl", "cwd": env.base,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if receipt["messages"] != 2 || receipt["inserted"] != 2 || receipt["session"] != "s1" {
		t.Fatalf("receipt = %#v", receipt)
	}
	var traces int
	if err := memory.Store.DB().QueryRow(
		"SELECT count(*) FROM trace WHERE scope = ? AND session = ?", "mem:hooks", "s1").Scan(&traces); err != nil {
		t.Fatalf("count traces: %v", err)
	}
	if traces != 2 {
		t.Fatalf("imported traces = %d", traces)
	}

	// The cursor is honoured, so a second run imports nothing new.
	again, err := importTurns(memory, map[string]any{"transcript_path": path})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if again["messages"] != 0 || again["inserted"] != 0 || again["offset"] != 2 {
		t.Fatalf("second receipt = %#v", again)
	}

	// An explicit session id wins over the file stem and has its own cursor.
	explicit, err := importTurns(memory, map[string]any{
		"transcriptPath": path, "sessionId": "explicit",
	})
	if err != nil {
		t.Fatalf("explicit import: %v", err)
	}
	if explicit["session"] != "explicit" || explicit["messages"] != 2 {
		t.Fatalf("explicit receipt = %#v", explicit)
	}

	// Missing files keep the standard receipt shape.
	missing, err := importTurns(memory, map[string]any{
		"transcript_path": filepath.Join(env.base, "nope.jsonl"),
	})
	if err != nil {
		t.Fatalf("missing import: %v", err)
	}
	if missing["messages"] != 0 || missing["inserted"] != 0 || missing["missing"] == nil {
		t.Fatalf("missing receipt = %#v", missing)
	}

	// No transcript path at all is a no-op with the same minimal shape.
	none, err := importTurns(memory, map[string]any{})
	if err != nil {
		t.Fatalf("pathless import: %v", err)
	}
	if none["messages"] != 0 || none["inserted"] != 0 {
		t.Fatalf("pathless receipt = %#v", none)
	}
}

func TestLogLineIsSortedAndRedacted(t *testing.T) {
	newTestEnv(t)
	cfg := loadConfig(t)
	logHook(cfg, "recall", map[string]any{
		"injected": 1, "bands": 2, "raw": 0, "ms": 7, "chars": 40,
		"note": "sk-secretsecret123",
	})
	raw, err := os.ReadFile(cfg.Log)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	text := strings.TrimSuffix(string(raw), "\n")
	previous := -1
	for _, key := range []string{"bands", "chars", "event", "injected", "ms", "note", "raw", "ts"} {
		index := strings.Index(text, `"`+key+`":`)
		if index < 0 || index < previous {
			t.Fatalf("key %q out of order in %q", key, text)
		}
		previous = index
	}
	if strings.Contains(text, "secretsecret123") || !strings.Contains(text, `"note": "***"`) {
		t.Fatalf("log line was not redacted: %q", text)
	}
	if !strings.Contains(text, fmt.Sprintf(`"ts": %d`, testNow)) {
		t.Fatalf("ts = %q", text)
	}
}

func TestHookLogPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions and umask are not available")
	}
	if os.Getenv("ROLLING_MEMORY_TEST_LOG_PERMISSIONS") != "1" {
		for _, mask := range []string{"000", "022", "077"} {
			t.Run("umask_"+mask, func(t *testing.T) {
				command := exec.Command("sh", "-c", "umask \"$1\"; exec \"$2\" -test.run '^TestHookLogPermissions$'", "sh", mask, os.Args[0])
				command.Env = append(os.Environ(), "ROLLING_MEMORY_TEST_LOG_PERMISSIONS=1")
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("permission subprocess: %v\n%s", err, output)
				}
			})
		}
		return
	}

	for _, fallback := range []bool{false, true} {
		name := "main"
		if fallback {
			name = "fallback"
		}
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			path := env.log
			if fallback {
				path = filepath.Join(config.DefaultStateDir(), "hook.log")
			}
			directory := filepath.Dir(path)
			if err := os.MkdirAll(directory, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.Chmod(directory, 0o755); err != nil {
				t.Fatalf("chmod directory: %v", err)
			}
			cfg := &config.Config{Log: path}
			write := func() {
				if fallback {
					fallbackLog("test message")
				} else {
					logHook(cfg, "recall", map[string]any{"injected": 0})
				}
			}
			checkMode := func(path string, want os.FileMode) {
				t.Helper()
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("stat %q: %v", path, err)
				}
				if info.Mode().Perm() != want {
					t.Errorf("%q mode = %o, want %o", path, info.Mode().Perm(), want)
				}
			}

			write()
			checkMode(path, 0o600)
			checkMode(directory, 0o755)
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatalf("chmod log: %v", err)
			}
			write()
			checkMode(path, 0o600)
			if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxLogBytes+1), 0o600); err != nil {
				t.Fatalf("seed oversized log: %v", err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatalf("chmod oversized log: %v", err)
			}
			write()
			checkMode(path, 0o600)
			checkMode(path+".1", 0o600)
			checkMode(directory, 0o755)
			rotated, err := os.Stat(path + ".1")
			if err != nil || rotated.Size() != int64(maxLogBytes+1) {
				t.Fatalf("rotated log = %v, %v", rotated, err)
			}
		})
	}
}

func TestLogRotation(t *testing.T) {
	newTestEnv(t)
	cfg := loadConfig(t)
	if err := os.MkdirAll(filepath.Dir(cfg.Log), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oversized := bytes.Repeat([]byte("x"), maxLogBytes+1)
	if err := os.WriteFile(cfg.Log, oversized, 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	logHook(cfg, "recall", map[string]any{"injected": 0, "absent": 1})
	rotated, err := os.Stat(cfg.Log + ".1")
	if err != nil {
		t.Fatalf("rotated log: %v", err)
	}
	if rotated.Size() != int64(maxLogBytes+1) {
		t.Fatalf("rotated size = %d", rotated.Size())
	}
	lines := logLines(t, cfg.Log)
	if len(lines) != 1 || lines[0]["event"] != "recall" {
		t.Fatalf("fresh log lines = %#v", lines)
	}
}

// TestRecallHookDoesNotScanMountedBackground locks the prompt-hook boundary:
// the hook captures the prompt and injects the recall nudge, while background
// scanning belongs to the ingest worker. A changed mounted file must stay
// untouched in the vault no matter how long the hook has been idle.
func TestRecallHookDoesNotScanMountedBackground(t *testing.T) {
	env := newTestEnv(t)

	// 1. 挂载一个含初始文件的 L1 Background 目录。
	bgDir := filepath.Join(env.base, "mounted-docs")
	if err := os.MkdirAll(bgDir, 0o755); err != nil {
		t.Fatalf("mkdir bgDir: %v", err)
	}
	notePath := filepath.Join(bgDir, "spec.md")
	if err := os.WriteFile(notePath, []byte("初始规格说明 v1：系统采用盲化脉冲协议。\n"), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	mem := openMemory(t)
	tc, err := mem.LocalVault()
	if err != nil {
		t.Fatalf("LocalVault: %v", err)
	}
	if _, err := tc.AddBackgroundDir(bgDir); err != nil {
		t.Fatalf("AddBackgroundDir: %v", err)
	}
	if _, err := tc.SyncBackgroundDirs(context.Background()); err != nil {
		t.Fatalf("initial SyncBackgroundDirs: %v", err)
	}
	var initialSHA string
	if err := mem.Store.DB().QueryRow(`SELECT content_sha256 FROM rm_bg_files LIMIT 1`).Scan(&initialSHA); err != nil || initialSHA == "" {
		t.Fatalf("expected initial AddBackgroundDir to ingest v1 background file, got %q err=%v", initialSHA, err)
	}
	_ = mem.Close()

	// 2. 修改挂载目录下的文件（大小和 mtime 改变）。
	if err := os.WriteFile(notePath, []byte("更新规格说明 v2：后台扫描改由独立 worker 承担。\n"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	futureTime := time.Now().Add(5 * time.Second)
	_ = os.Chtimes(notePath, futureTime, futureTime)

	// 3. 钩子执行后（无论距上次多久）都不得扫描或更新 L1 Background。
	for round := 0; round < 2; round++ {
		code, _, stderr := runHookMain(t, []string{"recall", "--json"}, `{"session_id":"s-bg","prompt":"检查规格"}`)
		if code != 0 {
			t.Fatalf("recall hook round %d failed: code=%d stderr=%s", round, code, stderr)
		}
	}

	memCheck := openMemory(t)
	defer memCheck.Close()
	var updatedSHA string
	if err := memCheck.Store.DB().QueryRow(`SELECT content_sha256 FROM rm_bg_files LIMIT 1`).Scan(&updatedSHA); err != nil {
		t.Fatalf("query rm_bg_files: %v", err)
	}
	if updatedSHA != initialSHA {
		t.Fatalf("prompt hook scanned background files: sha256 changed from %q to %q", initialSHA, updatedSHA)
	}
	var noeticCount int
	if err := memCheck.Store.DB().QueryRow(
		`SELECT COUNT(*) FROM rm_noetic`,
	).Scan(&noeticCount); err != nil {
		t.Fatalf("query rm_noetic: %v", err)
	}
	if noeticCount != 0 {
		t.Fatalf("recall hook must never crystallize user prompts into rm_noetic, got %d", noeticCount)
	}
}

func TestHookStats(t *testing.T) {
	newTestEnv(t)
	cfg := loadConfig(t)
	stats, err := HookStats(cfg)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats["exists"] != false || stats["lines"] != 0 {
		t.Fatalf("empty stats = %#v", stats)
	}
	logHook(cfg, "recall", map[string]any{"injected": 1})
	stats, err = HookStats(cfg)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats["exists"] != true || stats["lines"] != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	last, ok := stats["last"].(map[string]any)
	if !ok || last["event"] != "recall" {
		t.Fatalf("last = %#v", stats["last"])
	}
	if stats["size"].(int64) <= 0 {
		t.Fatalf("size = %#v", stats["size"])
	}
}

func TestPyJoinAndStem(t *testing.T) {
	joins := []struct{ base, path, want string }{
		{"/a/b", "c.jsonl", "/a/b/c.jsonl"},
		{"/a/b/", "c.jsonl", "/a/b/c.jsonl"},
		{"/a/b", "./c.jsonl", "/a/b/c.jsonl"},
		{"/a/b", "../c.jsonl", "/a/b/../c.jsonl"},
		{"/a", "c//d.jsonl", "/a/c/d.jsonl"},
		{"a", "/abs.jsonl", "/abs.jsonl"},
	}
	for _, item := range joins {
		if got := pyJoin(item.base, item.path); got != item.want {
			t.Fatalf("pyJoin(%q, %q) = %q, want %q", item.base, item.path, got, item.want)
		}
	}
	stems := map[string]string{
		"s1.jsonl": "s1", ".bashrc": ".bashrc", "a.tar.gz": "a.tar",
		"a.": "a.", "noext": "noext",
	}
	for path, want := range stems {
		if got := pathStem(path); got != want {
			t.Fatalf("pathStem(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestPyStrMatchesPythonShape(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{nil, "None"},
		{"", ""},
		{true, "True"},
		{false, "False"},
		{json.Number("42"), "42"},
		{json.Number("1.5"), "1.5"},
		{float64(1000), "1000.0"},
		{3, "3"},
	}
	for _, item := range cases {
		if got := pyStr(item.value); got != item.want {
			t.Fatalf("pyStr(%#v) = %q, want %q", item.value, got, item.want)
		}
	}
}

func TestSituationalBootAndSingleBigramCollisionSilence(t *testing.T) {
	env := newTestEnv(t)
	wsDir := filepath.Join(env.base, "Rolling-Memory")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	mem := openMemory(t)
	now := testNow - 30
	_, _, _ = mem.Store.AddTrace("mem:hooks", "请优化 hooks.go 的三维时空态势唤醒机制", store.TraceOptions{
		Session: "prev-sess", Turn: 1, Kind: "message", Author: "user", Audience: "shared",
		OccurredAt: &now, Meta: map[string]any{"locus": "rocareer/Rolling-Memory"},
	})
	nowReply := testNow - 20
	_, _, _ = mem.Store.AddTrace("mem:hooks", "已完成 internal/hooks/hooks.go 三维态势唤醒改造", store.TraceOptions{
		Session: "prev-sess", Turn: 2, Kind: "message", Author: "assistant", Audience: "shared",
		OccurredAt: &nowReply, Meta: map[string]any{"locus": "rocareer/Rolling-Memory"},
	})
	if _, err := mem.Remember(facade.RememberRequest{
		Content:    strPtr("增量批实施卡首版落于 docs/zh-CN/design.md。"),
		AnchorText: strPtr("增量批实施卡首版落于 docs/zh-CN/design.md。"),
		Assertion: map[string]any{
			"s": map[string]any{"label": "增量批实施卡首版"},
			"x": "落于",
			"o": map[string]any{"literal": "docs/zh-CN/design.md"},
		},
		Author: "user", AutoSync: boolPtr(false),
	}); err != nil {
		t.Fatalf("remember card: %v", err)
	}

	// 2. Accidental single-bigram collision ("全部同意 推进实施" sharing only 2-char "实施" with "增量批实施卡首版")
	// MUST be silenced by pure mathematical sequence coverage (0 word lists). The hook no longer
	// recalls, so these assertions pin the retrieval + specificity gate the agent-driven path uses.
	if kept := hookGateRecall(t, mem, wsDir, "new-sess-1", "全部同意 推进实施"); len(kept) != 0 {
		t.Fatalf("expected single-bigram collision prompt to be silenced, got: %s", bandsText(kept))
	}

	// 3. Specific query with contiguous sequence match ("增量批实施卡首版落于哪里") MUST surface its band.
	keptCard := hookGateRecall(t, mem, wsDir, "new-sess-1", "增量批实施卡首版落于哪里")
	if !strings.Contains(bandsText(keptCard), "增量批实施卡首版") {
		t.Fatalf("expected specific entity prompt to recall its band, got: %s", bandsText(keptCard))
	}

	// 4. Scattered 2-char domain words ("开源", "记忆", "插件") in a long architectural prompt
	// MUST NOT trigger a false-positive recall of an unrelated long mission band that merely
	// shares a single 2-char word in its title and scattered 2-char words in its body.
	if _, err := mem.Remember(facade.RememberRequest{
		Content:    strPtr("Rolling-Memory 开源工作使命、三仓拓扑与零泄漏纪律：工作使命登记于本地 AGENTS.md，世界开源的记忆系统与全宿主插件。"),
		AnchorText: strPtr("工作使命登记于本地 AGENTS.md，世界开源的记忆系统与全宿主插件。"),
		Assertion: map[string]any{
			"s": map[string]any{"label": "Rolling-Memory 开源工作使命、三仓拓扑与零泄漏纪律"},
			"x": "裁定为",
			"o": map[string]any{"literal": "工作使命登记于本地 AGENTS.md，世界开源的记忆系统与全宿主插件。"},
		},
		Author: "user", AutoSync: boolPtr(false),
	}); err != nil {
		t.Fatalf("remember mission: %v", err)
	}
	genericPayload := "对照 同业 各种 开源记忆插件 思考 我们的机制 是否 健壮合理"
	if kept := hookGateRecall(t, mem, wsDir, "new-sess-2", genericPayload); len(kept) != 0 {
		t.Fatalf("expected scattered 2-char domain words in long prompt to be silenced, got: %s", bandsText(kept))
	}

	// 5. Specificity gate test: 2-letter ASCII syllable fragments ("ac", "ti", "ct" from "artifact")
	// MUST NOT trigger false recalls on unrelated camelCase test identifiers.
	if kept := recall.FilterBandsBySpecificity("artifact file db", "artifact file db", "Rolling-Memory", []map[string]any{
		{
			"title": "带·TestProactiveLiveBridgeDB",
			"nodes": []map[string]any{{"id": "a_1", "s": "TestProactiveLiveBridgeDB", "x": "验证", "o": "桥接测试", "status": "active"}},
		},
	}); len(kept) != 0 {
		t.Fatalf("expected 0 bands on 2-letter ASCII syllable fragments, got %d", len(kept))
	}
	if got := stripDegenerateTail("再看 stopword_filter.go！]]]]]]]]]]]]]]]]]]]]]]]]]]"); got != "再看 stopword_filter.go！" {
		t.Fatalf("stripDegenerateTail = %q", got)
	}

	// 6. Pre-retrieval subject probe + compact technical acronym ("w3:roi") must recall
	// "Rolling-Memory 独立 ROI 监控屏", while a >=6-hanzi title ("机械核零 LLM 读链") sharing only
	// a single 3-letter word ("llm") with zero ideographic overlap must stay silent.
	if _, err := mem.Remember(facade.RememberRequest{
		Content:    strPtr("Rolling-Memory 独立 ROI 监控屏落位于 admin/web/src/views/admin/dashboard/roi.vue。"),
		AnchorText: strPtr("Rolling-Memory 独立 ROI 监控屏落位于 admin/web/src/views/admin/dashboard/roi.vue。"),
		Assertion: map[string]any{
			"s": map[string]any{"label": "Rolling-Memory 独立 ROI 监控屏"},
			"x": "落位于",
			"o": map[string]any{"literal": "admin/web/src/views/admin/dashboard/roi.vue"},
		},
		Author: "user", AutoSync: boolPtr(false),
	}); err != nil {
		t.Fatalf("remember roi: %v", err)
	}
	if _, err := mem.Remember(facade.RememberRequest{
		Content:    strPtr("机械核零 LLM 读链定义为种子门到图走与结构排序。"),
		AnchorText: strPtr("机械核零 LLM 读链定义为种子门到图走与结构排序。"),
		Assertion: map[string]any{
			"s": map[string]any{"label": "机械核零 LLM 读链"},
			"x": "定义为",
			"o": map[string]any{"literal": "种子门到图走与结构排序"},
		},
		Author: "user", AutoSync: boolPtr(false),
	}); err != nil {
		t.Fatalf("remember llm: %v", err)
	}
	keptROI := hookGateRecall(t, mem, wsDir, "new-sess-3", "检查 ROI 监控 记录 还是有应该检索的 被静默了")
	if !strings.Contains(bandsText(keptROI), "admin/web/src/views/admin/dashboard/roi.vue") {
		t.Fatalf("expected compact technical w3:roi subject to be recalled, got: %s", bandsText(keptROI))
	}
	keptLLM := hookGateRecall(t, mem, wsDir, "new-sess-3", "就是 可以随仓发布吗? 修改配置文件(各家key 和 自己的 llm key等) 直接复现?")
	if strings.Contains(bandsText(keptLLM), "机械核零 LLM 读链") {
		t.Fatalf("expected >=6-hanzi title with lone w3:llm match and zero hanzi overlap to be silenced, got: %s", bandsText(keptLLM))
	}

	// 7. StripInjected must strip <untrusted_gen_ui_state> blocks so widget paths never
	// pollute search cues or trigger false file-path recalls.
	genUIPrompt := "open-webui的默认字体试一下\n<untrusted_gen_ui_state>\n[{\"path\":\"/Users/test/.zcode/v2/visualizations/abc.html\"}]\n</untrusted_gen_ui_state>"
	if stripped := ids.StripInjected(genUIPrompt); strings.Contains(stripped, "untrusted_gen_ui_state") || strings.Contains(stripped, "visualizations") {
		t.Fatalf("expected StripInjected to remove <untrusted_gen_ui_state>, got: %q", stripped)
	}
	if paths := recall.ExtractStructuralPaths("访问 http://localhost:8008/ 首页", 4); len(paths) != 0 {
		t.Fatalf("expected root localhost URL without SPA route to yield 0 file paths, got: %v", paths)
	}
	if !isEphemeralProbeSession("probe-boot-readonly") || isEphemeralProbeSession("session_493f1424b7") {
		t.Fatalf("unexpected isEphemeralProbeSession classification")
	}

	// 8. 4-turn Cadence Reminder & Attribution Line Stripping:
	// Turns 1..3 on an unindexed topic stay 0-token silent; Turn 4 emits the lightweight
	// cadence anchor (logged as injected=0, cadence=1). And stripMemoryAttributionLine
	// removes the opening "Rolling-Memory 记忆" sentence before persisting to trace.
	for turnIdx := 1; turnIdx <= 4; turnIdx++ {
		cadPayload, _ := json.Marshal(map[string]any{
			"session_id": "session_cadence_live_1",
			"cwd":        wsDir,
			"prompt":     fmt.Sprintf("继续推进第 %d 步独立样式微调", turnIdx),
		})
		code, cadOut, _ := runHookMain(t, []string{"recall"}, string(cadPayload))
		if code != 0 {
			t.Fatalf("cadence turn %d exit=%d", turnIdx, code)
		}
		if turnIdx < 4 && strings.TrimSpace(cadOut) != "" {
			t.Fatalf("expected turn %d (<4) to stay silent, got: %q", turnIdx, cadOut)
		}
		if turnIdx == 4 && !strings.Contains(cadOut, "[Rolling-Memory 4轮节律锚点]") {
			t.Fatalf("expected turn 4 to emit 4-turn cadence reminder, got: %q", cadOut)
		}
	}
	rawReply := "技能判定：无（豁免）\n\n💡 本轮经 Rolling-Memory 记忆直达 internal/hooks/hooks.go，省去盲搜。\n\n已完成修改并通过测试。"
	cleanedReply := stripMemoryAttributionLine(rawReply)
	if strings.Contains(cleanedReply, "Rolling-Memory 记忆") || !strings.Contains(cleanedReply, "已完成修改并通过测试。") {
		t.Fatalf("stripMemoryAttributionLine failed: %q", cleanedReply)
	}
}

// hookGateRecall replays the retrieval the UserPromptSubmit hook used to run
// before the reminder-only ruling — recall as the deliverable reader, then the
// deterministic specificity gate — so retrieval-quality assertions keep guarding
// the agent-driven path without the hook performing any recall itself.
func hookGateRecall(t *testing.T, mem *facade.Memory, wsDir, session, prompt string) []map[string]any {
	t.Helper()
	payload := map[string]any{"session_id": session, "cwd": wsDir}
	reader := "deliverable"
	res, err := mem.Recall(facade.RecallRequest{
		Cue: prompt, Reader: &reader, Presence: workspacePresence(payload), NoCrystallize: true,
	})
	if err != nil {
		t.Fatalf("recall(%q): %v", prompt, err)
	}
	return recall.FilterBandsBySpecificity(prompt, prompt, workspaceProject(payload), res.Bands)
}

// bandsText flattens a band list into one searchable string (titles + rendered nodes).
func bandsText(bands []map[string]any) string {
	var builder strings.Builder
	for _, band := range bands {
		builder.WriteString(fmt.Sprint(band["title"]))
		builder.WriteString(" ")
		nodes, _ := band["nodes"].([]map[string]any)
		for _, node := range nodes {
			builder.WriteString(fmt.Sprint(node["content"]))
			builder.WriteString(" ")
		}
	}
	return builder.String()
}
