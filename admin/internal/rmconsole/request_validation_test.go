// plugin: rolling-memory v0.0.58
package rollingmemory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestValidateEvalRunRequestFailClosed 钉住评测请求的套件/档位 fail-closed 契约：
// 未知取值一律显式拒收，绝不静默回退默认套件档位触发一场错误评测。
func TestValidateEvalRunRequestFailClosed(t *testing.T) {
	cases := []struct {
		name    string
		req     evalRunRequest
		wantErr bool
	}{
		{"default empty", evalRunRequest{}, false},
		{"locomo default tier", evalRunRequest{Suite: "locomo", Tier: "100"}, false},
		{"locomo tier 200", evalRunRequest{Suite: "locomo", Tier: "200"}, false},
		{"locomo tier 1540", evalRunRequest{Suite: "locomo", Tier: "1540"}, false},
		{"atm hard", evalRunRequest{Suite: "atm", Tier: "hard"}, false},
		{"atm main alias", evalRunRequest{Suite: "atm", Tier: "main"}, false},
		{"atm 1013", evalRunRequest{Suite: "atm", Tier: "1013"}, false},
		{"unknown suite rejected", evalRunRequest{Suite: "nope"}, true},
		{"unknown locomo tier rejected", evalRunRequest{Suite: "locomo", Tier: "999"}, true},
		{"unknown atm tier rejected", evalRunRequest{Suite: "atm", Tier: "999"}, true},
		{"retired atm tier rejected", evalRunRequest{Suite: "atm", Tier: "100"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateEvalRunRequest(tc.req); (err != nil) != tc.wantErr {
				t.Fatalf("validateEvalRunRequest(%+v) err = %v, wantErr = %v", tc.req, err, tc.wantErr)
			}
		})
	}
}

func runPluginHandler(t *testing.T, handler gin.HandlerFunc, body string) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/admin/plugin/rolling-memory/test", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bad response body %q: %v", recorder.Body.String(), err)
	}
	return payload
}

// responseMessage 读取统一响应体的消息字段（httpx 使用 `message` 键）。
func responseMessage(payload map[string]any) string {
	if m, ok := payload["message"].(string); ok && m != "" {
		return m
	}
	m, _ := payload["msg"].(string)
	return m
}

func TestEvalExportRequiresExactID(t *testing.T) {
	for _, body := range []string{
		`{"suite":"atm","run_id":""}`,
		`{"suite":"unknown","run_id":"atm-baseline-rm-hard-local-plain"}`,
	} {
		payload := runPluginHandler(t, evalExportHandler, body)
		if code, _ := payload["code"].(float64); code == 0 {
			t.Fatalf("request %s must fail closed: %+v", body, payload)
		}
	}
	payload := runPluginHandler(t, evalExportHandler, `{"suite":"atm","run_id":"atm-baseline-hipporag2","format":"json"}`)
	if code, _ := payload["code"].(float64); code != 0 {
		t.Fatalf("baseline export failed: %+v", payload)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing export data: %+v", payload)
	}
	run, ok := data["run"].(map[string]any)
	if !ok || run["id"] != "atm-baseline-hipporag2" {
		t.Fatalf("export did not preserve selected baseline ID: %+v", data)
	}
	if data["items_available"] != false {
		t.Fatalf("baseline must not invent details: %+v", data)
	}
}

func TestEvalExportDeltaMissingAndMeasuredZero(t *testing.T) {
	fingerprint := &EvalComparisonFingerprint{QuestionsSHA256: strings.Repeat("a", 64), CorpusSHA256: strings.Repeat("b", 64), ScoringSHA256: strings.Repeat("c", 64)}
	run := EvalRunRecord{Suite: SuiteATM, Tier: "hard", Protocol: ATMProtocolID, Model: "judge", ScoredQuestions: 2, JudgeScored: 2, Status: evalTaskStatusCompleted,
		MetricsAvailable: map[string]bool{"r10": true, "judge_rate": true}, Comparison: fingerprint}
	baseline := run
	baseline.Comparison = nil
	if got := exportDeltaRows(&run, &baseline)[0].DeltaPP; got != nil {
		t.Fatalf("missing comparison evidence must suppress delta: %v", *got)
	}
	baseline.Comparison = fingerprint
	baseline.R10 = 0.5
	baseline.MetricsAvailable = map[string]bool{"r10": true}
	rows := exportDeltaRows(&run, &baseline)
	if rows[0].Run == nil || *rows[0].Run != 0 || rows[0].DeltaPP == nil || *rows[0].DeltaPP != -50 {
		t.Fatalf("measured zero comparison lost: %+v", rows[0])
	}
	if rows[1].Run != nil || rows[1].Baseline != nil || rows[1].DeltaPP != nil || rows[5].Run == nil || rows[5].Baseline != nil || rows[5].DeltaPP != nil {
		t.Fatalf("missing metrics must stay missing: %+v", rows)
	}
	baseline.ScoredQuestions = 3
	if got := exportDeltaRows(&run, &baseline)[0].DeltaPP; got != nil {
		t.Fatalf("different denominators must not produce a delta: %v", *got)
	}
	run.Status = evalTaskStatusFailed
	baseline.ScoredQuestions = 2
	if got := exportDeltaRows(&run, &baseline)[0].DeltaPP; got != nil {
		t.Fatalf("partial failed run must not produce a delta: %v", *got)
	}
	markdown := renderEvalReproMarkdown(SuiteATM, &run, nil, EvalCloudManifest{}, "", 0)
	if !strings.Contains(markdown, "| R@20 | — | — | — |") || strings.Contains(markdown, "delta column reads as absolute") {
		t.Fatalf("markdown must show missing measurements: %s", markdown)
	}
}

// TestEvalIngestRequiresExplicitAction 钉住灌库接口的显式动作契约：
// 空白或未知动作一律拒收（写路径绝不隐式执行 prepare）。
func TestEvalIngestRequiresExplicitAction(t *testing.T) {
	for _, body := range []string{`{"suite":"locomo"}`, `{"action":"nope"}`} {
		payload := runPluginHandler(t, evalIngestHandler, body)
		code, _ := payload["code"].(float64)
		msg := responseMessage(payload)
		if code == 0 || !strings.Contains(msg, "必须显式指定") {
			t.Fatalf("body %s: want fail-closed rejection, got code=%v msg=%q", body, payload["code"], msg)
		}
	}
}

// TestSyncReconcileRejectsUnknownAction 钉住同步接口的未知动作拒收：
// 未知取值不得静默降级为 reconcile 触发真实同步，且不得调用同步 CLI。
func TestSyncReconcileRejectsUnknownAction(t *testing.T) {
	prevExec := executeAdminSyncCLI
	t.Cleanup(func() { executeAdminSyncCLI = prevExec })
	called := false
	executeAdminSyncCLI = func(_ context.Context, _, _, _, _ string, _ int) ([]byte, error) {
		called = true
		return []byte("{}"), nil
	}
	payload := runPluginHandler(t, syncReconcileHandler, `{"action":"definitely-not-a-mode"}`)
	if called {
		t.Fatal("unknown action must not invoke the sync CLI")
	}
	code, _ := payload["code"].(float64)
	msg := responseMessage(payload)
	if code == 0 || !strings.Contains(msg, "未知同步动作") {
		t.Fatalf("want fail-closed rejection, got code=%v msg=%q", payload["code"], msg)
	}
}

// TestTerminalRejectsUnknownAction 钉住终端接口的未知动作拒收：
// 早期实现把任意未知取值静默降级为只读 stream，调用方无法察觉动作写错。
func TestTerminalRejectsUnknownAction(t *testing.T) {
	resp := handleTerminalAction(t.TempDir(), TerminalRequest{Action: "Definitely-Not-An-Action"})
	if resp.ExitCode != 1 || len(resp.OutputLines) == 0 || !strings.Contains(resp.OutputLines[0], "未知终端动作") {
		t.Fatalf("want unknown-action error, got exit=%d lines=%v", resp.ExitCode, resp.OutputLines)
	}
}
