// plugin: rolling-memory v0.0.58
package rollingmemory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// writeLocalModeConfig 写入仅含运行模式的引擎配置（其余键取缺省）。
func writeLocalModeConfig(t *testing.T, home, mode string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "rolling-memory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	cfg := "[mode]\nname = \"" + mode + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// TestConsoleSwitchesStayLocalRegardlessOfConfigMode 锁定「控制台恒本地」口径：
// 引擎配置里残留的 `[mode] name = "cloud"` 不得把控制台读取的开关面拉回云端，
// 运行模式读数恒为 local（控制台只读写本地明文金库）。
func TestConsoleSwitchesStayLocalRegardlessOfConfigMode(t *testing.T) {
	home := t.TempDir()
	for _, mode := range []string{"local", "cloud", "sideways", ""} {
		writeLocalModeConfig(t, home, mode)
		sw := parseConfigSwitches(filepath.Join(home, ".config", "rolling-memory", "config.toml"), true)
		if sw.Mode != modeLocal {
			t.Fatalf("config mode %q: console mode = %q, want %q", mode, sw.Mode, modeLocal)
		}
	}
}

// TestStripCloudBrainEnvRemovesOverrides 锁定派生 CLI 子进程前的环境清洗：
// 云端端点/密钥覆盖变量必须被剔除，其余环境原样保留（子进程零云端覆盖变量的纵深防线）。
func TestStripCloudBrainEnvRemovesOverrides(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"ROLLING_MEMORY_BRAIN_URL=http://127.0.0.1:18081/open/memory/v1",
		"ROLLING_MEMORY_BRAIN_KEY=mem2.secret",
		"ROLLING_MEMORY_MODE=local",
	}
	got := stripCloudBrainEnv(env)
	want := []string{"PATH=/usr/bin", "ROLLING_MEMORY_MODE=local"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stripCloudBrainEnv = %v, want %v", got, want)
	}
}

// TestCollectArtifactStatsReportsLocalVault 锁定读数来源：恒报本地明文金库坐标，
// 绝不把云端地址/用户 ID 当成本地读数呈现。
func TestCollectArtifactStatsReportsLocalVault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ROLLING_MEMORY_MODE", "")
	writeLocalModeConfig(t, home, "local")

	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	db, err := openWritableVaultDB(dbPath)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	_ = db.Close()

	st := collectArtifactStats(dbPath, UserScope)
	if st.UpstreamEndpoint != "local://vault" {
		t.Fatalf("endpoint = %q, want local://vault", st.UpstreamEndpoint)
	}
	if st.UpstreamUserID != "" {
		t.Fatalf("console must not report a cloud user id, got %q", st.UpstreamUserID)
	}
}

// TestWidgetAndGraphHandlersMakeZeroCloudCalls 锁定两个高频/重型读口的零外呼：
// /widget（1Hz 采样）采样本地 SQLite 金库往返；/graph 只读本地/预置数据。
// 探针哨兵（含注入的子进程云端覆盖变量）必须命中 0 次。
func TestWidgetAndGraphHandlersMakeZeroCloudCalls(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ROLLING_MEMORY_MODE", "")
	writeLocalModeConfig(t, home, "local")

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	// 即便宿主环境里残留云端端点覆盖变量，控制台也不得拨号。
	t.Setenv("ROLLING_MEMORY_BRAIN_URL", srv.URL+"/open/memory/v1")
	t.Setenv("ROLLING_MEMORY_BRAIN_KEY", "mem2.sentinel")

	// 本地金库：真实建表 + 一条情景单元，确保 /graph 走本地合并路径。
	dbPath := filepath.Join(home, ".local", "share", "rolling-memory", "memory.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	t.Setenv("ROLLING_MEMORY_DB", dbPath)
	db, err := openWritableVaultDB(dbPath)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO thin_vault_docs
		(doc_id, layer, occurred_at, raw_text, sentences_json, address_uri, coord64, kept_indices_json)
		VALUES ('local_graph_1', 'episodic', 1762380000, '本地图谱单元', '[]', 'C6:L1:B001#00', 1, '[]')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed local doc: %v", err)
	}
	_ = db.Close()

	widgetRaw := callPluginHandler(t, widgetHandler, "/widget", `{}`)
	var widgetEnvelope struct {
		Code int `json:"code"`
		Data struct {
			Mode             string  `json:"mode"`
			UpstreamOnline   bool    `json:"upstream_online"`
			UpstreamEndpoint string  `json:"upstream_endpoint"`
			UpstreamRTTMS    float64 `json:"upstream_rtt_ms"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(widgetRaw), &widgetEnvelope); err != nil {
		t.Fatalf("decode widget: %v (%s)", err, widgetRaw)
	}
	if widgetEnvelope.Code != 0 || widgetEnvelope.Data.Mode != "local" || widgetEnvelope.Data.UpstreamEndpoint != "local://vault" {
		t.Fatalf("local widget = %s, want mode=local endpoint=local://vault", widgetRaw)
	}
	if !widgetEnvelope.Data.UpstreamOnline {
		t.Fatalf("local vault probe must be online: %s", widgetRaw)
	}

	InvalidateLocomoPublicCorpusCache()
	graphRaw := callPluginHandler(t, graphHandler, "/graph", `{"scope":"default","limit":50}`)
	// 源码树不随包分发 LoCoMo 演示快照（CC BY-NC 语料）：缺夹具时 /graph 合法走本地预览
	// 回退，预设图谱契约仅在快照可用时钉住（与 locomo_public_graph_shape_test.go 同口径）。
	if _, err := loadLocomoDemoCorpus(); err != nil {
		t.Logf("LoCoMo 演示快照不可用（%v）：跳过预设图谱断言，仅校验零外呼契约", err)
	} else {
		if !strings.Contains(graphRaw, `"suite":"locomo"`) {
			t.Fatalf("local graph must return preset LoCoMo preview graph: %s", clipTest(graphRaw, 400))
		}
		if strings.Contains(graphRaw, "local_graph_1") {
			t.Fatalf("preview graph must not expose raw local vault docs: %s", clipTest(graphRaw, 400))
		}
	}

	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("console widget/graph made %d cloud requests, want 0", got)
	}
}

func clipTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestEvalArmGuardOnlyAllowsLocalPlain 锁定评测臂闸：现役只开放本地明文臂与 oracle，
// 脉冲臂（pulse_plain / pulse_cipher）与兼容档位臂一律 fail-closed（不再有 cloud 模式放行分支）。
func TestEvalArmGuardOnlyAllowsLocalPlain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ROLLING_MEMORY_MODE", "")

	writeLocalModeConfig(t, home, "local")
	for _, arm := range []string{"", EvalArmSDKLocalPlain, EvalArmOracle} {
		if _, err := normalizeEvalArm(arm); err != nil {
			t.Fatalf("arm %q must be accepted: %v", arm, err)
		}
	}
	for _, arm := range []string{EvalArmSDKPulsePlain, EvalArmSDKPulseCipher, EvalPulseModePulsePlain, EvalPulseModePulseCipher} {
		_, err := normalizeEvalArm(arm)
		if err == nil || !strings.Contains(err.Error(), "已停用") {
			t.Fatalf("arm %s error = %v, want the explicit retired message", arm, err)
		}
	}
	if _, err := validateEvalRunRequest(evalRunRequest{Suite: SuiteLoCoMo, Tier: "100", ArmMode: EvalArmSDKPulseCipher}); err == nil {
		t.Fatal("pulse arm must fail closed at the run request validator")
	}
}

// TestEvalIngestRejectsPulseArm 端到端锁闸：ingest 请求带脉冲臂必须在任何外部动作之前
// fail-closed（消息可见，且不产生评测准备）。
func TestEvalIngestRejectsPulseArm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ROLLING_MEMORY_MODE", "")
	writeLocalModeConfig(t, home, "local")

	raw := callPluginHandler(t, evalIngestHandler, "/eval/ingest", `{"suite":"locomo","action":"prepare","arm_mode":"sdk-pulse-cipher","pulse_mode":"pulse_cipher"}`)
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode ingest response: %v (%s)", err, raw)
	}
	if envelope.Code == 0 || !strings.Contains(envelope.Message, "已停用") {
		t.Fatalf("pulse ingest = %s, want fail-closed retired-arm error", raw)
	}
}
