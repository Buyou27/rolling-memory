// plugin: rolling-memory v0.0.58
// eval_export.go —— 评测运行「复现包」导出端点（/eval/export）。
//
// 面向两类读者一次成包：
//   - ATM-Bench 官方 / 外部复核者：自包含 Markdown 报告（官方 gold-set-mean 指标表、
//     与封印基线的配对差值、复现命令、语料 SHA-256 Pin、环境指纹）；
//   - 机器消费方：同内容的结构化 JSON（run 记录 + 基线 + 逐题明细），可直接入
//     paper/reproduce 或外部审计管线。
//
// 只读端点：仅读测评任务表与 manifest，绝不触发任何评测阶段。
package rollingmemory

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ai-go-hub/ai-go-admin/internal/kit/httpx"
	"github.com/ai-go-hub/ai-go-admin/internal/version"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var evalMetricKeys = []string{
	"evidence_hit", "full_evidence_hit", "r10", "r20", "r50", "any_hit_10", "all_hit_10",
	"answer_in_context", "judge_rate", "avg_recall_ms", "p95_recall_ms",
	"avg_delivered_tokens", "total_delivered_tokens", "avg_delivered_items", "ingest_ms",
}

func copyEvalMetricAvailability(src map[string]bool) map[string]bool {
	out := make(map[string]bool, len(evalMetricKeys))
	for _, key := range evalMetricKeys {
		out[key] = src[key]
	}
	return out
}

// setLegacyEvalMetricAvailability preserves only measurements supported by the stored payload.
// Legacy zero-valued retrieval metrics were omitted, and evidence-free runs used zero placeholders.
func setLegacyEvalMetricAvailability(rec *EvalRunRecord, raw []byte) {
	if rec.MetricsAvailable != nil {
		if !rec.IsBaseline && !rec.MetricEvidenceExplicit {
			for _, key := range []string{"evidence_hit", "r10", "r20", "r50", "any_hit_10", "all_hit_10", "answer_in_context", "avg_recall_ms", "p95_recall_ms", "avg_delivered_items", "avg_delivered_tokens", "total_delivered_tokens"} {
				rec.MetricsAvailable[key] = false
			}
		}
		rec.MetricsAvailable = copyEvalMetricAvailability(rec.MetricsAvailable)
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return
	}
	rec.MetricsAvailable = copyEvalMetricAvailability(nil)
	if rec.ScoredQuestions <= 0 {
		return
	}
	for _, key := range evalMetricKeys {
		value, ok := fields[key]
		if !ok || string(value) == "null" {
			continue
		}
		var number float64
		if json.Unmarshal(value, &number) != nil {
			continue
		}
		switch key {
		case "judge_rate":
			rec.MetricsAvailable[key] = rec.JudgeScored > 0
		default:
			rec.MetricsAvailable[key] = number != 0
		}
	}
}

func evalRunMetricAvailability(items []EvalQuestionResultItem, suite string, ingestMS int64) map[string]bool {
	available := copyEvalMetricAvailability(nil)
	if len(items) == 0 {
		return available
	}
	// The cached ingest time belongs to a prior prepare, not necessarily this run.
	_ = ingestMS
	for _, key := range []string{"evidence_hit", "r10", "r20", "r50", "any_hit_10", "all_hit_10", "answer_in_context", "avg_recall_ms", "p95_recall_ms", "avg_delivered_items", "avg_delivered_tokens", "total_delivered_tokens"} {
		available[key] = true
		for _, item := range items {
			if !item.MetricsAvailable[key] {
				available[key] = false
				break
			}
		}
	}
	for _, item := range items {
		if suite == SuiteATM && item.QSScored || suite != SuiteATM && (item.JudgeVerdict == "correct" || item.JudgeVerdict == "wrong") {
			available["judge_rate"] = true
		}
	}
	return available
}

func evalMetricAvailable(rec *EvalRunRecord, key string) bool {
	return rec != nil && rec.MetricsAvailable != nil && rec.MetricsAvailable[key]
}

type evalExportRequest struct {
	Suite  string `json:"suite"`
	RunID  string `json:"run_id"`
	Format string `json:"format"` // markdown（默认）| json
}

func findEvalExportRecordOn(db *gorm.DB, suite, runID string) (*EvalRunRecord, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, nil
	}
	pool := referenceBaselines()
	if suite == SuiteATM {
		pool = atmReferenceBaselines()
	}
	for i := range pool {
		if pool[i].ID == runID {
			return &pool[i], nil
		}
	}
	history, err := readEvalTaskRowsOn(db, suite)
	if err != nil {
		return nil, err
	}
	for i := range history {
		if history[i].ID == runID {
			return &history[i], nil
		}
	}
	return nil, nil
}

// evalExportHandler 只导出明确指定的运行或封印基线。
func evalExportHandler(c *gin.Context) {
	var req evalExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, httpx.WithMessage("参数错误: "+err.Error()))
		return
	}
	wantID := strings.TrimSpace(req.RunID)
	if wantID == "" {
		httpx.Fail(c, httpx.WithCode(http.StatusBadRequest), httpx.WithMessage("必须指定要导出的 run_id"))
		return
	}
	suiteName := strings.ToLower(strings.TrimSpace(req.Suite))
	if suiteName != "locomo" && suiteName != "atm" && suiteName != "atmbench" && suiteName != "atm-bench" {
		httpx.Fail(c, httpx.WithCode(http.StatusBadRequest), httpx.WithMessage("必须指定有效的评测套件"))
		return
	}
	suite := normalizeSuite(suiteName)
	db, ledgerErr := evalLedger()
	run, err := findEvalExportRecordOn(db, suite, wantID)
	if err != nil {
		if ledgerErr != nil {
			err = ledgerErr
		}
		httpx.Fail(c, httpx.WithMessage("读取测评任务台账失败: "+err.Error()))
		return
	}
	if run == nil {
		httpx.Fail(c, httpx.WithCode(http.StatusNotFound), httpx.WithMessage("未找到指定套件的运行记录（run_id="+wantID+"）"))
		return
	}

	baseline := matchExportBaseline(suite, run.Tier)
	if run.IsBaseline && baseline != nil && run.ID == baseline.ID {
		baseline = nil
	}
	items := []EvalQuestionResultItem{}
	var manifest EvalManifest
	if !run.IsBaseline {
		var err error
		items, err = readEvalTaskItemRowsOn(db, run.ID)
		if err != nil {
			httpx.Fail(c, httpx.WithMessage("读取逐题明细失败: "+err.Error()))
			return
		}
	}
	// 运行级证据来自该运行的台账，而不是可能已被后续运行替换的当前 manifest。
	var prov evalTaskProvenance
	if !run.IsBaseline {
		var err error
		prov, err = readEvalTaskProvenanceOn(db, run.ID)
		if err != nil {
			httpx.Fail(c, httpx.WithMessage("读取运行来源失败: "+err.Error()))
			return
		}
	}
	manifest.Scope = prov.Scope
	manifest.CorpusSHA256 = prov.CorpusSHA256
	if len(items) > 0 {
		manifest.Transport = items[0].EngineTransport
	}
	corpusSHA := prov.CorpusSHA256

	payload := gin.H{
		"generated_at": time.Now().Format(time.RFC3339),
		"suite":        suite,
		"run":          run,
		"baseline":     baseline,
		"delta":        exportDeltaRows(run, baseline),
		"environment": gin.H{
			"software_version": version.Get().Version,
			"go_version":       runtime.Version(),
			"os_arch":          runtime.GOOS + "/" + runtime.GOARCH,
			"corpus_sha256":    corpusSHA,
			"scope":            manifest.Scope,
			"documents":        nil,
			"transport":        manifest.Transport,
		},
		"items":           items,
		"items_available": len(items) > 0,
	}
	if strings.EqualFold(strings.TrimSpace(req.Format), "json") {
		httpx.Success(c, httpx.WithData(payload))
		return
	}
	payload["markdown"] = renderEvalReproMarkdown(suite, run, baseline, manifest, corpusSHA, len(items))
	httpx.Success(c, httpx.WithData(payload))
}

// matchExportBaseline 取同套件同题集档位的封印基线（我方实测行优先），供配对差值。
func matchExportBaseline(suite, tier string) *EvalRunRecord {
	var pool []EvalRunRecord
	if suite == SuiteATM {
		pool = atmReferenceBaselines()
	} else {
		pool = referenceBaselines()
	}
	for i := range pool {
		if pool[i].Tier == tier && strings.Contains(pool[i].ID, "-rm") {
			return &pool[i]
		}
	}
	for i := range pool {
		if pool[i].Tier == tier {
			return &pool[i]
		}
	}
	return nil
}

type exportDeltaRow struct {
	Metric   string   `json:"metric"`
	Run      *float64 `json:"run"`
	Baseline *float64 `json:"baseline"`
	DeltaPP  *float64 `json:"delta_pp"`
}

func evalComparisonFingerprintsMatch(a, b *EvalComparisonFingerprint) bool {
	if a == nil || b == nil {
		return false
	}
	for _, pair := range [][2]string{
		{a.QuestionsSHA256, b.QuestionsSHA256},
		{a.CorpusSHA256, b.CorpusSHA256},
		{a.ScoringSHA256, b.ScoringSHA256},
	} {
		decoded, err := hex.DecodeString(pair[0])
		if err != nil || len(decoded) != 32 || pair[0] != pair[1] {
			return false
		}
	}
	return true
}

// exportDeltaRows 仅在两边均有测量和一致的比较证据时计算百分点差值。
func exportDeltaRows(run, baseline *EvalRunRecord) []exportDeltaRow {
	metrics := []struct {
		key  string
		name string
		pick func(*EvalRunRecord) float64
	}{
		{"r10", "R@10", func(r *EvalRunRecord) float64 { return r.R10 }},
		{"r20", "R@20", func(r *EvalRunRecord) float64 { return r.R20 }},
		{"r50", "R@50", func(r *EvalRunRecord) float64 { return r.R50 }},
		{"any_hit_10", "Any-Hit@10", func(r *EvalRunRecord) float64 { return r.AnyHit10 }},
		{"all_hit_10", "All-Hit@10", func(r *EvalRunRecord) float64 { return r.AllHit10 }},
		{"judge_rate", "Hybrid QS / Judge", func(r *EvalRunRecord) float64 { return r.JudgeRate }},
	}
	rows := make([]exportDeltaRow, 0, len(metrics))
	for _, m := range metrics {
		row := exportDeltaRow{Metric: m.name}
		if evalMetricAvailable(run, m.key) {
			value := roundFloat(m.pick(run), 4)
			row.Run = &value
		}
		if evalMetricAvailable(baseline, m.key) {
			value := roundFloat(m.pick(baseline), 4)
			row.Baseline = &value
		}
		comparable := run != nil && baseline != nil && (run.IsBaseline || run.Status == evalTaskStatusCompleted) &&
			(baseline.IsBaseline || baseline.Status == evalTaskStatusCompleted) &&
			run.Suite != "" && run.Suite == baseline.Suite && run.Tier == baseline.Tier && run.Protocol != "" && run.Protocol == baseline.Protocol &&
			run.ScoredQuestions > 0 && run.ScoredQuestions == baseline.ScoredQuestions &&
			evalComparisonFingerprintsMatch(run.Comparison, baseline.Comparison)
		if comparable && run.Suite != SuiteATM {
			// LoCoMo's ranked text-evidence calculation is not ATM's ID-based official recall.
			comparable = m.key == "judge_rate"
		}
		if m.key == "judge_rate" && (run == nil || baseline == nil || run.Model == "" || run.Model != baseline.Model || run.JudgeScored != baseline.JudgeScored) {
			comparable = false
		}
		if comparable && row.Run != nil && row.Baseline != nil {
			value := roundFloat((*row.Run-*row.Baseline)*100, 2)
			row.DeltaPP = &value
		}
		rows = append(rows, row)
	}
	return rows
}

// renderEvalReproMarkdown 生成自包含复现报告（ATM 官方评测器口径）。
func renderEvalReproMarkdown(suite string, run, baseline *EvalRunRecord, manifest EvalManifest, corpusSHA string, itemCount int) string {
	var b strings.Builder
	suiteName := "LoCoMo"
	if suite == SuiteATM {
		suiteName = "ATM-Bench"
	}
	fmt.Fprintf(&b, "# %s Reproducibility Report · %s\n\n", suiteName, run.ID)
	fmt.Fprintf(&b, "- Generated: %s · Software v%s · %s/%s · Go %s\n", time.Now().Format("2006-01-02 15:04:05"), version.Get().Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Fprintf(&b, "- Run: **%s** · Tier `%s` · Arm `%s` · TopK %d · %d scored questions · %.1fs end-to-end\n", run.Label, run.Tier, run.Arm, run.TopK, run.ScoredQuestions, run.ElapsedSeconds)
	if run.Status != "" {
		fmt.Fprintf(&b, "- Status: `%s`\n", run.Status)
	}
	if run.Error != "" {
		fmt.Fprintf(&b, "- Error: %s\n", run.Error)
	}
	fmt.Fprintf(&b, "- Corpus: scope `%s` · SHA-256 `%s`\n\n", manifest.Scope, corpusSHA)

	b.WriteString("## Metrics vs Sealed Baseline\n\n")
	b.WriteString("| Metric | This Run | Baseline | Δ (pp) |\n|---|---:|---:|---:|\n")
	for _, row := range exportDeltaRows(run, baseline) {
		value := func(v *float64) string {
			if v == nil {
				return "—"
			}
			return fmt.Sprintf("%.2f%%", *v*100)
		}
		delta := "—"
		if row.DeltaPP != nil {
			delta = fmt.Sprintf("%+.2f", *row.DeltaPP)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", row.Metric, value(row.Run), value(row.Baseline), delta)
	}
	if baseline != nil {
		fmt.Fprintf(&b, "\nBaseline: `%s`（%s）\n", baseline.ID, baseline.Label)
	} else {
		b.WriteString("\nBaseline: none available for comparison.\n")
	}

	b.WriteString("\n## Performance Telemetry\n\n")
	if evalMetricAvailable(run, "avg_recall_ms") {
		fmt.Fprintf(&b, "- Retrieval latency: mean %.1f ms", run.AvgRecallMS)
		if evalMetricAvailable(run, "p95_recall_ms") {
			fmt.Fprintf(&b, " · P95 %.1f ms", run.P95RecallMS)
		}
		b.WriteString("\n")
	}
	if evalMetricAvailable(run, "ingest_ms") {
		fmt.Fprintf(&b, "- Corpus ingest + index build: %.1f s\n", float64(run.IngestMS)/1000)
	}
	if evalMetricAvailable(run, "avg_delivered_items") && evalMetricAvailable(run, "avg_delivered_tokens") {
		fmt.Fprintf(&b, "- Delivered payload: %.1f items / %d tokens per question (tokenz-r)\n", run.AvgDeliveredItems, run.AvgDeliveredTokens)
	}

	if len(run.BucketCounts) > 0 {
		b.WriteString("\n## Failure Bucket Attribution\n\n")
		keys := make([]string, 0, len(run.BucketCounts))
		for k := range run.BucketCounts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "- %s: %d\n", k, run.BucketCounts[k])
		}
	}

	b.WriteString("\n## Reproduce\n\n")
	fmt.Fprintf(&b, "```bash\n%s\n```\n", run.ReproCLI)
	fmt.Fprintf(&b, "\nPer-question details: %d items archived in the admin ledger (`/eval/run` mode=detail · run_id=%s).\n", itemCount, run.ID)
	if suite == SuiteATM {
		fmt.Fprintf(&b, "\nScoring formula (official gold-set-mean): R@k = mean over questions of |set(GT) ∩ set(Top-k)| / |set(GT)|.\n")
	}
	return b.String()
}
