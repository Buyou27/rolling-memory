// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/mcp"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
)

// engine信道测试：`rolling-memory engine <op> --json` 只允许经已核验本机守护连接
// （同 UID 伪 socket / 旧协议 daemon / 假 socket 不得收到任何业务请求），正控为真实
// 进程内守护进程。全部用例走临时 /tmp socket，绝不触碰用户真实守护进程。

const engineTestTurn = "@ref:zcode:sess_engine_fixture:msg_engine_u1"

// engineSeedZCodeTurnFixture 写入一个最小 ZCode 宿主库（一问一答），
// 与引擎规范会话解析读取的位置一致。
func engineSeedZCodeTurnFixture(t *testing.T, home string) {
	t.Helper()
	dbDir := filepath.Join(home, ".zcode", "cli", "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir zcode fixture: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dbDir, "db.sqlite"))
	if err != nil {
		t.Fatalf("open zcode fixture: %v", err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, sequence INTEGER, time_created INTEGER, data TEXT)`,
		`CREATE TABLE part (message_id TEXT, sequence INTEGER, data TEXT)`,
		`INSERT INTO message (id, session_id, sequence, time_created, data) VALUES
			('msg_engine_u1', 'sess_engine_fixture', 1, 1700000000, '{"role":"user"}')`,
		`INSERT INTO part (message_id, sequence, data) VALUES
			('msg_engine_u1', 0, '{"type":"text","text":"fixture question"}')`,
		`INSERT INTO message (id, session_id, sequence, time_created, data) VALUES
			('msg_engine_a1', 'sess_engine_fixture', 2, 1700000001, '{"role":"assistant"}')`,
		`INSERT INTO part (message_id, sequence, data) VALUES
			('msg_engine_a1', 0, '{"type":"reasoning","text":"hidden"}'),
			('msg_engine_a1', 1, '{"type":"text","text":"fixture reply body"}')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed zcode fixture (%s): %v", stmt, err)
		}
	}
}

// engineStartVerifiedDaemon 启动进程内共享守护进程（同一进程：同 UID、同 PID），
// 播种一个已密封的 @ref: 指针与一份内联文档，并把 socket 路径写进环境变量。
func engineStartVerifiedDaemon(t *testing.T) *facade.Memory {
	t.Helper()
	sockDir, err := os.MkdirTemp("/tmp", "rm-engine-")
	if err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "engine.sock")
	t.Setenv(mcp.DaemonSocketEnv, sockPath)
	t.Setenv(mcp.DaemonEnableEnv, "on")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	engineSeedZCodeTurnFixture(t, home)
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	memory, err := facade.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = memory.Close() })
	tc, err := memory.LocalVault()
	if err != nil {
		t.Fatal(err)
	}
	const occurredAt = int64(1793102400)
	for _, fixture := range []struct{ id, ref, text string }{
		{"ep_engine_question", engineTestTurn + "#0", "[user] fixture question"},
		{"ep_engine_answer", engineTestTurn + "#1", "[assistant] fixture reply body"},
	} {
		packet, _ := tc.Vault.PackBlindPacket(fixture.id, fixture.text, occurredAt)
		if err := tc.Vault.SaveLocalEpisodicPointerIndexed(fixture.id, occurredAt, fixture.ref, engineTestTurn, fixture.text, packet, localvault.IngestResponse{Layer: localvault.LayerEpisodic}); err != nil {
			t.Fatalf("seed indexed pointer: %v", err)
		}
	}
	if err := tc.Vault.SaveLocalPlaintext("doc-engine-inline", occurredAt, "inline engine document body", []string{"inline engine document body"}, localvault.IngestResponse{Layer: localvault.LayerEpisodic}); err != nil {
		t.Fatalf("seed inline document: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- mcp.ServeUnix(sockPath, memory, nil) }()
	t.Cleanup(func() {
		engineShutdownTestDaemon(sockPath)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if health, err := mcp.PingUnixDaemon(sockPath, 100*time.Millisecond); err == nil && health.OK {
			return memory
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("engine test daemon did not become healthy")
	return nil
}

// engineShutdownTestDaemon asks the fixture daemon to step down (test teardown only).
func engineShutdownTestDaemon(sockPath string) {
	client := &http.Client{
		Timeout: time.Second,
		Transport: &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", sockPath)
		}},
	}
	resp, err := client.Post("http://unix/v1/shutdown", "application/json", strings.NewReader(`{}`))
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
}

// engineListenFakeSocket binds a Unix socket at path and serves handler to every
// request, returning the listener plus a counter of every request it saw.
func engineListenFakeSocket(t *testing.T, path string, handler http.HandlerFunc) (*net.UnixListener, *atomic.Int64) {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix %s: %v", path, err)
	}
	unixListener := listener.(*net.UnixListener)
	var requests atomic.Int64
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler(w, r)
	})}
	go func() { _ = server.Serve(unixListener) }()
	t.Cleanup(func() { _ = server.Close() })
	return unixListener, &requests
}

// engineDeadSocket leaves a bound-but-unlistening socket file behind, the signature of
// a daemon that died without cleaning up (and the classic stale-socket case callers
// must fail closed on instead of spawning a replacement as a side effect).
func engineDeadSocket(t *testing.T, path string) {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix %s: %v", path, err)
	}
	unixListener := listener.(*net.UnixListener)
	unixListener.SetUnlinkOnClose(false)
	if err := unixListener.Close(); err != nil {
		t.Fatalf("close dead socket: %v", err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("dead socket file missing: %v", err)
	}
}

// asStringValue reads one string field out of a decoded JSON object.
func asStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func TestEngineRecallRelaysVerifiedDaemonResponse(t *testing.T) {
	env := newEnv(t)
	_ = env
	engineStartVerifiedDaemon(t)

	result := runInput(t, `{"cue":"fixture question"}`, "engine", "recall", "--json")
	if result.code != 0 {
		t.Fatalf("engine recall failed: %+v", result)
	}
	response := jsonObject(t, strings.TrimSpace(result.stdout))
	if response["ok"] != true || response["result"] == nil {
		t.Fatalf("engine recall response = %s", result.stdout)
	}
	if probe := runInput(t, `{"cue":"fixture question","mode":"probe"}`, "engine", "recall", "--json"); probe.code != 0 {
		t.Fatalf("engine probe recall failed: %+v", probe)
	} else if probeResponse := jsonObject(t, strings.TrimSpace(probe.stdout)); probeResponse["ok"] != true || probeResponse["probe"] == nil {
		t.Fatalf("engine probe recall response = %s", probe.stdout)
	}
}

func TestEngineRecallRelaysEngineLevelFailure(t *testing.T) {
	newEnv(t)
	engineStartVerifiedDaemon(t)

	// A reserved filter the engine fails closed on: the daemon answers with its
	// engine-level envelope, which must be relayed instead of being reported as an
	// unreachable daemon (callers branch on engine_error).
	result := runInput(t, `{"cue":"fixture question","asof":"2026-01-01"}`, "engine", "recall", "--json")
	if result.code != 1 {
		t.Fatalf("engine-level failure must exit 1: %+v", result)
	}
	response := jsonObject(t, strings.TrimSpace(result.stdout))
	if response["ok"] != false || response["engine_error"] != true || asStringValue(response["error"]) == "" {
		t.Fatalf("engine-level failure envelope = %s", result.stdout)
	}
}

func TestEngineGraphRelaysEngineProjection(t *testing.T) {
	newEnv(t)
	engineStartVerifiedDaemon(t)

	result := runInput(t, `{"op":"view","limit":32}`, "engine", "graph", "--json")
	if result.code != 0 {
		t.Fatalf("engine graph failed: %+v", result)
	}
	response := jsonObject(t, strings.TrimSpace(result.stdout))
	if response["source"] != "local-vault" || response["graph_schema_version"] != float64(2) {
		t.Fatalf("engine graph envelope = %s", result.stdout)
	}
	nodes, _ := response["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatalf("engine graph returned no nodes: %s", result.stdout)
	}
	var sawCluster, sawWord bool
	for _, raw := range nodes {
		node, _ := raw.(map[string]any)
		switch node["kind"] {
		case "cluster":
			if asStringValue(node["cluster_type"]) != "" && node["member_count"] != nil {
				sawCluster = true
			}
		case "word":
			if asStringValue(node["label"]) != "" {
				sawWord = true
			}
		}
	}
	if !sawCluster || !sawWord {
		t.Fatalf("engine graph lost word/cluster fields: cluster=%v word=%v payload=%s", sawCluster, sawWord, result.stdout)
	}
}

func TestEngineHydrateRelaysCanonicalSegments(t *testing.T) {
	newEnv(t)
	engineStartVerifiedDaemon(t)

	payload := `{"items":[{"doc_id":"ep_engine_answer","ref":"` + engineTestTurn + `#1"}]}`
	result := runInput(t, payload, "engine", "hydrate", "--json")
	if result.code != 0 {
		t.Fatalf("engine hydrate failed: %+v", result)
	}
	response := jsonObject(t, strings.TrimSpace(result.stdout))
	texts, _ := response["texts"].([]any)
	if response["ok"] != true || len(texts) != 1 {
		t.Fatalf("engine hydrate response = %s", result.stdout)
	}
	item, _ := texts[0].(map[string]any)
	if item["verified"] != true || item["text"] != "[assistant] fixture reply body" || item["actual_ref"] != engineTestTurn+"#1" || item["error"] != nil {
		t.Fatalf("hydrated item = %+v", item)
	}

	// The same handler the daemon serves must fail closed per item on identity mismatch.
	mixed := `{"items":[{"ref":"` + engineTestTurn + `#1"},{"doc_id":"ep_engine_answer","ref":"` + engineTestTurn + `#0"}]}`
	result = runInput(t, mixed, "engine", "hydrate", "--json")
	if result.code != 0 {
		t.Fatalf("mixed hydrate batch failed: %+v", result)
	}
	response = jsonObject(t, strings.TrimSpace(result.stdout))
	texts, _ = response["texts"].([]any)
	if response["ok"] != true || len(texts) != 2 {
		t.Fatalf("mixed hydrate response = %s", result.stdout)
	}
	for i, raw := range texts {
		item, _ := raw.(map[string]any)
		if item["text"] != nil || item["verified"] == true || item["error"] == nil || item["error"] == "" {
			t.Fatalf("unverified hydrate item %d returned content: %+v", i, item)
		}
	}
}

func TestEngineDocumentsRelaysPlaintextBody(t *testing.T) {
	newEnv(t)
	engineStartVerifiedDaemon(t)

	result := runInput(t, `{"items":[{"unit_id":"doc-engine-inline"}]}`, "engine", "documents", "--json")
	if result.code != 0 {
		t.Fatalf("engine documents failed: %+v", result)
	}
	response := jsonObject(t, strings.TrimSpace(result.stdout))
	docs, _ := response["docs"].([]any)
	if response["ok"] != true || len(docs) != 1 {
		t.Fatalf("engine documents response = %s", result.stdout)
	}
	doc, _ := docs[0].(map[string]any)
	if doc["unit_id"] != "doc-engine-inline" || doc["raw_text"] != "inline engine document body" || doc["error"] != nil {
		t.Fatalf("inline document = %+v", doc)
	}

	// An unresolvable unit is answered per item with an honest error, not silence.
	result = runInput(t, `{"items":[{"unit_id":"bg:missing#0"}]}`, "engine", "documents", "--json")
	if result.code != 0 {
		t.Fatalf("missing document read failed: %+v", result)
	}
	response = jsonObject(t, strings.TrimSpace(result.stdout))
	docs, _ = response["docs"].([]any)
	doc, _ = docs[0].(map[string]any)
	if response["ok"] != true || doc["raw_text"] != nil || doc["error"] == nil || doc["error"] == "" {
		t.Fatalf("missing document = %s", result.stdout)
	}
}

func TestEngineInvalidateNotifiesRunningDaemon(t *testing.T) {
	newEnv(t)
	engineStartVerifiedDaemon(t)

	result := run(t, "engine", "invalidate", "--json")
	if result.code != 0 {
		t.Fatalf("engine invalidate failed: %+v", result)
	}
	response := jsonObject(t, strings.TrimSpace(result.stdout))
	if response["ok"] != true || response["notified"] != true {
		t.Fatalf("engine invalidate response = %s", result.stdout)
	}
}

func TestEngineFailsClosedOnDeadSocketWithoutSpawning(t *testing.T) {
	newEnv(t)
	sockDir, err := os.MkdirTemp("/tmp", "rm-engine-dead-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "engine.sock")
	t.Setenv(mcp.DaemonSocketEnv, sockPath)
	engineDeadSocket(t, sockPath)

	for _, op := range []string{"recall", "hydrate", "documents"} {
		result := runInput(t, `{"cue":"x"}`, "engine", op, "--json")
		if result.code != 1 {
			t.Fatalf("engine %s on a dead socket must fail closed: %+v", op, result)
		}
		response := jsonObject(t, strings.TrimSpace(result.stdout))
		if response["ok"] != false || response["error"] == nil || response["error"] == "" {
			t.Fatalf("engine %s failure envelope = %s", op, result.stdout)
		}
	}
	result := run(t, "engine", "invalidate", "--json")
	if result.code != 1 {
		t.Fatalf("engine invalidate on a dead socket must fail closed: %+v", result)
	}
	// Fail-closed means the stale socket is reported, never replaced by a spawned daemon.
	if info, err := os.Lstat(sockPath); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("stale socket was disturbed: %v", err)
	}
	if conn, err := net.DialTimeout("unix", sockPath, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("a daemon is listening on the stale socket (unexpected spawn)")
	}
}

func TestEngineRejectsDisabledDaemonRouting(t *testing.T) {
	newEnv(t)
	sockDir, err := os.MkdirTemp("/tmp", "rm-engine-off-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	t.Setenv(mcp.DaemonSocketEnv, filepath.Join(sockDir, "engine.sock"))
	t.Setenv(mcp.DaemonEnableEnv, "0")
	result := runInput(t, `{"cue":"x"}`, "engine", "recall", "--json")
	if result.code != 1 || !strings.Contains(result.stdout, "daemon routing is disabled") {
		t.Fatalf("disabled daemon routing must be refused: %+v", result)
	}
}

func TestEngineRejectsLegacyDaemonWithoutBusinessRequest(t *testing.T) {
	newEnv(t)
	sockDir, err := os.MkdirTemp("/tmp", "rm-engine-legacy-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "engine.sock")
	t.Setenv(mcp.DaemonSocketEnv, sockPath)
	var business atomic.Int64
	_, requests := engineListenFakeSocket(t, sockPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			// A legacy daemon: health answers, but without the versioned identity handshake.
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "pid": os.Getpid()})
			return
		}
		business.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{},"texts":[],"docs":[]}`))
	})
	for _, op := range []string{"recall", "hydrate", "documents"} {
		result := runInput(t, `{"cue":"x"}`, "engine", op, "--json")
		if result.code != 1 {
			t.Fatalf("engine %s against a legacy daemon must fail closed: %+v", op, result)
		}
	}
	if result := run(t, "engine", "invalidate", "--json"); result.code != 1 {
		t.Fatalf("engine invalidate against a legacy daemon must fail closed: %+v", result)
	}
	if got := business.Load(); got != 0 {
		t.Fatalf("legacy daemon received %d business requests, want 0", got)
	}
	if got := requests.Load(); got == 0 {
		t.Fatal("verification probe never reached the legacy daemon")
	}
}

func TestEngineRejectsFakeSocketWithoutBusinessRequest(t *testing.T) {
	newEnv(t)
	sockDir, err := os.MkdirTemp("/tmp", "rm-engine-fake-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "engine.sock")
	t.Setenv(mcp.DaemonSocketEnv, sockPath)
	var business atomic.Int64
	_, _ = engineListenFakeSocket(t, sockPath, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/recall", "/v1/hydrate", "/v1/documents", "/v1/invalidate", "/mcp":
			business.Add(1)
		}
		http.Error(w, "not the memory daemon", http.StatusNotFound)
	})
	for _, op := range []string{"recall", "hydrate", "documents"} {
		result := runInput(t, `{"cue":"x"}`, "engine", op, "--json")
		if result.code != 1 {
			t.Fatalf("engine %s against a fake socket must fail closed: %+v", op, result)
		}
	}
	if result := run(t, "engine", "invalidate", "--json"); result.code != 1 {
		t.Fatalf("engine invalidate against a fake socket must fail closed: %+v", result)
	}
	if got := business.Load(); got != 0 {
		t.Fatalf("fake socket received %d business requests, want 0", got)
	}
}

func TestEngineRequiresRequestJSON(t *testing.T) {
	newEnv(t)
	result := runInput(t, "", "engine", "recall", "--json")
	if result.code != 1 {
		t.Fatalf("missing request must fail: %+v", result)
	}
	response := jsonObject(t, strings.TrimSpace(result.stdout))
	if response["ok"] != false {
		t.Fatalf("missing request envelope = %s", result.stdout)
	}
	if result := run(t, "engine", "bogus"); result.code != 2 {
		t.Fatalf("unknown engine op must be a usage error: %+v", result)
	}
}
