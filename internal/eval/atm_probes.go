// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
	local "github.com/rocareer/rolling-memory/sdk/go/local"
)

// 对抗探针档（atm-probes）：
//
// 该档在冻结 ATM 语料上运行「附加只读」的自建对抗题集，输出检索级统计。
// 探针目录由调用方经 --probes 指定（MANIFEST.json + 缺席/冲突 JSONL），
// 与 evaldata/** 物理分离：语料与官方题集一个字节都不动。
//
// 两档语义：
//   - absence：查询所问的实体在语料中零记载（缺席判据由探针构建器全库扫描确认）。
//     检索级指标读数 = 静默率（自适应交付 K=0）、交付条数分布、缺席词面泄漏（契约守卫）、
//     首位能量与窗口能量落差；
//   - conflict：同一事实槽存在旧记录与后发更新记录。检索级指标读数 = 双记录入窗率、
//     新旧各自入窗率、新记录压过旧记录的比例、交付窗口内旧值/新值的篇幅压力。
const (
	// SuiteATMProbes 是自建对抗探针档的 suite 名。
	SuiteATMProbes = "atm-probes"
	// TierProbeAbsence 是缺席探针档。
	TierProbeAbsence = "absence"
	// TierProbeConflict 是冲突探针档。
	TierProbeConflict = "conflict"
	// ProbeManifestName 是探针目录清单文件名。
	ProbeManifestName = "MANIFEST.json"
)

// ProbeTierNames 是探针档的固定顺序。
var ProbeTierNames = []string{TierProbeAbsence, TierProbeConflict}

// NormalizeProbeTier 归一探针档名；空值取缺席档，未知档返回空串。
func NormalizeProbeTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "", TierProbeAbsence:
		return TierProbeAbsence
	case TierProbeConflict:
		return TierProbeConflict
	}
	return ""
}

// ATMProbe 是缺席与冲突两类探针的并集结构（按 group 取用各自字段）。
type ATMProbe struct {
	ID       string `json:"id"`
	Group    string `json:"group"`
	Subtype  string `json:"subtype"`
	Question string `json:"question"`
	QType    string `json:"qtype"`
	Expected string `json:"expected,omitempty"`
	// 缺席档：absent_terms 为全库零命中词面；cooc_terms 为各自在库但联合零共现的词面。
	AbsentTerms []string `json:"absent_terms,omitempty"`
	CoocTerms   []string `json:"cooc_terms,omitempty"`
	// 冲突档：evidence_ids 依时间升序 [旧, 新]。
	ExpectedCurrent string   `json:"expected_current,omitempty"`
	ExpectedStale   string   `json:"expected_stale,omitempty"`
	EvidenceIDs     []string `json:"evidence_ids,omitempty"`
	EvidenceTS      []string `json:"evidence_ts,omitempty"`
	GapDays         int      `json:"gap_days,omitempty"`
}

type probeManifestSuite struct {
	Count  int    `json:"count"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type probeManifest struct {
	CorpusPin struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"corpus_pin"`
	Suites map[string]probeManifestSuite `json:"suites"`
}

// LoadATMProbes 读取探针目录中指定档位的 JSONL，并按清单中的 sha256 校验数据件。
// 清单的 corpus_pin 必须与官方 ATM 语料 pin 一致，防止探针在漂流语料上被解释。
func LoadATMProbes(probeDir, tier string) ([]ATMProbe, probeManifestSuite, error) {
	dir := strings.TrimSpace(probeDir)
	if dir == "" {
		return nil, probeManifestSuite{}, errors.New("探针目录为空：用 --probes <dir> 指定（MANIFEST.json + 缺席/冲突 JSONL）")
	}
	manifestPath := filepath.Join(expandUser(dir), ProbeManifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, probeManifestSuite{}, fmt.Errorf("读取探针清单失败: %w", err)
	}
	manifest := probeManifest{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, probeManifestSuite{}, fmt.Errorf("%s 解析失败: %w", ProbeManifestName, err)
	}
	if manifest.CorpusPin.SHA256 != "" && manifest.CorpusPin.SHA256 != PinATMCorpusSHA256 {
		return nil, probeManifestSuite{}, fmt.Errorf("探针清单的 corpus_pin=%s 与官方 ATM 语料 pin=%s 不一致：探针必须建在冻结语料上",
			manifest.CorpusPin.SHA256, PinATMCorpusSHA256)
	}
	entry, ok := manifest.Suites[tier]
	if !ok || strings.TrimSpace(entry.File) == "" {
		return nil, probeManifestSuite{}, fmt.Errorf("探针清单没有档位 %q（可用：%s）", tier, strings.Join(ProbeTierNames, " / "))
	}
	path := filepath.Join(filepath.Dir(manifestPath), entry.File)
	if err := VerifyFile(path, entry.SHA256); err != nil {
		return nil, probeManifestSuite{}, err
	}
	handle, err := os.Open(path)
	if err != nil {
		return nil, probeManifestSuite{}, err
	}
	defer handle.Close()
	probes := make([]ATMProbe, 0, entry.Count)
	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		probe := ATMProbe{}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			return nil, probeManifestSuite{}, fmt.Errorf("%s 第 %d 行解析失败: %w", entry.File, len(probes)+1, err)
		}
		if strings.TrimSpace(probe.ID) == "" || strings.TrimSpace(probe.Question) == "" {
			return nil, probeManifestSuite{}, fmt.Errorf("%s 第 %d 行缺少 id/question", entry.File, len(probes)+1)
		}
		probes = append(probes, probe)
	}
	if err := scanner.Err(); err != nil {
		return nil, probeManifestSuite{}, err
	}
	if entry.Count > 0 && len(probes) != entry.Count {
		return nil, probeManifestSuite{}, fmt.Errorf("%s 行数 %d 与清单 count=%d 不一致", entry.File, len(probes), entry.Count)
	}
	return probes, entry, nil
}

// probeDist 是整数读数分布（条数、名次等）。
type probeDist struct {
	Min    int     `json:"min"`
	Median float64 `json:"median"`
	Max    int     `json:"max"`
	Mean   float64 `json:"mean"`
}

// probeFloatDist 是浮点读数分布（能量 Nats）。
type probeFloatDist struct {
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
}

func probeFloatDistribution(values []float64) probeFloatDist {
	if len(values) == 0 {
		return probeFloatDist{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	return probeFloatDist{
		Min:    PyRound(sorted[0], 3),
		Median: PyRound(median, 3),
		Max:    PyRound(sorted[len(sorted)-1], 3),
		Mean:   PyRound(sum/float64(len(sorted)), 3),
	}
}

func probeDistribution(values []int) probeDist {
	if len(values) == 0 {
		return probeDist{}
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	median := float64(sorted[len(sorted)/2])
	if len(sorted)%2 == 0 {
		median = float64(sorted[len(sorted)/2-1]+sorted[len(sorted)/2]) / 2
	}
	var sum int
	for _, v := range sorted {
		sum += v
	}
	return probeDist{
		Min:    sorted[0],
		Median: median,
		Max:    sorted[len(sorted)-1],
		Mean:   PyRound(float64(sum)/float64(len(sorted)), 2),
	}
}

func probeRate(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return PyRound(100*float64(part)/float64(total), 2)
}

// normalizeProbeText 做大小写、印刷引号与空白归一（与探针构建器的字面量判据同构）。
func normalizeProbeText(text string) string {
	lower := strings.ToLower(text)
	replaced := strings.NewReplacer(
		"\u2018", "'", "\u2019", "'",
		"\u201c", "\"", "\u201d", "\"",
		"\u00a0", " ",
	).Replace(lower)
	return strings.Join(strings.Fields(replaced), " ")
}

func probeTextContains(text, term string) bool {
	needle := normalizeProbeText(term)
	if needle == "" {
		return false
	}
	return strings.Contains(normalizeProbeText(text), needle)
}

func probeEnergy(hit local.ResolvedHit) float64 {
	if hit.DirectWeight > 0 {
		return hit.DirectWeight
	}
	return hit.FinalWeight
}

// probeRanks 把命中顺序转成 1-based 名次（重复 ID 取首次出现）。
func probeRanks(ids []string) map[string]int {
	ranks := make(map[string]int, len(ids))
	for at, id := range ids {
		if _, exists := ranks[id]; !exists {
			ranks[id] = at + 1
		}
	}
	return ranks
}

// probeEvidenceRanks 取冲突探针的旧、新记录名次（evidence_ids 依时间升序）。
func probeEvidenceRanks(probe ATMProbe, ranks map[string]int) (stale, current int) {
	if len(probe.EvidenceIDs) >= 2 {
		stale = ranks[probe.EvidenceIDs[0]]
		current = ranks[probe.EvidenceIDs[1]]
	}
	return stale, current
}

// probeMentions 统计窗口内提到旧值/新值的交付单元数。
func probeMentions(probe ATMProbe, texts []string) (stale, current int) {
	for _, text := range texts {
		if probeTextContains(text, probe.ExpectedStale) {
			stale++
		}
		if probeTextContains(text, probe.ExpectedCurrent) {
			current++
		}
	}
	return stale, current
}

// conflictRawStats 是冲突档在「裸 raw_limit 窗口」上的同口径读数，
// 用于与历史首轮（直查 raw-limit 64、未经自适应交付）逐项对齐；主档以自适应交付窗口为准。
type conflictRawStats struct {
	BothInWindow         int `json:"both_in_window"`
	StaleInWindow        int `json:"stale_in_window"`
	CurrentInWindow      int `json:"current_in_window"`
	CurrentOutranksStale int `json:"current_outranks_stale"`
	StaleMentions        int `json:"stale_mentions"`
	CurrentMentions      int `json:"current_mentions"`
	StaleDominates       int `json:"stale_dominates"`
}

// absenceProbeStats 是缺席档的检索级汇总。
type absenceProbeStats struct {
	N                  int            `json:"n"`
	Delivered          probeDist      `json:"delivered"`
	RawHits            probeDist      `json:"raw_hits"`
	Truncated          int            `json:"truncated"`
	TruncatedRate      float64        `json:"truncated_rate_pct"`
	Silent             int            `json:"silent"`
	SilentRate         float64        `json:"silent_rate_pct"`
	AbsentTermLeak     int            `json:"absent_term_leak"`
	AbsentTermLeakRate float64        `json:"absent_term_leak_rate_pct"`
	Top1Energy         probeFloatDist `json:"top1_energy"`
	EnergyDrop         probeFloatDist `json:"energy_drop"`
	SubtypeBreakdown   map[string]int `json:"subtype_breakdown"`
}

// conflictProbeStats 是冲突档的检索级汇总。
type conflictProbeStats struct {
	N                        int              `json:"n"`
	Delivered                probeDist        `json:"delivered"`
	RawHits                  probeDist        `json:"raw_hits"`
	Truncated                int              `json:"truncated"`
	TruncatedRate            float64          `json:"truncated_rate_pct"`
	Silent                   int              `json:"silent"`
	SilentRate               float64          `json:"silent_rate_pct"`
	BothInWindow             int              `json:"both_in_window"`
	BothRate                 float64          `json:"both_in_window_rate_pct"`
	StaleInWindow            int              `json:"stale_in_window"`
	StaleRate                float64          `json:"stale_in_window_rate_pct"`
	CurrentInWindow          int              `json:"current_in_window"`
	CurrentRate              float64          `json:"current_in_window_rate_pct"`
	CurrentOutranksStale     int              `json:"current_outranks_stale"`
	CurrentOutranksStaleRate float64          `json:"current_outranks_stale_rate_pct"`
	StaleMentions            int              `json:"stale_mentions"`
	CurrentMentions          int              `json:"current_mentions"`
	StaleDominates           int              `json:"stale_dominates"`
	StaleDominatesRate       float64          `json:"stale_dominates_rate_pct"`
	SubtypeBreakdown         map[string]int   `json:"subtype_breakdown"`
	Raw64                    conflictRawStats `json:"raw64"`
}

type atmProbeSummary struct {
	Suite                      string              `json:"suite"`
	Tier                       string              `json:"tier"`
	Arm                        string              `json:"arm"`
	PulseMode                  string              `json:"pulse_mode"`
	Transport                  string              `json:"transport"`
	Scope                      string              `json:"scope"`
	LocalFallback              bool                `json:"local_fallback"`
	RawLimit                   int                 `json:"raw_limit"`
	N                          int                 `json:"n"`
	ProbesSHA256               string              `json:"probes_sha256"`
	CorpusSHA256               string              `json:"corpus_sha256"`
	OfficialCorpusPinsVerified bool                `json:"official_corpus_pins_verified"`
	Ingest                     map[string]any      `json:"ingest"`
	Absence                    *absenceProbeStats  `json:"absence,omitempty"`
	Conflict                   *conflictProbeStats `json:"conflict,omitempty"`
	Errors                     int                 `json:"errors"`
	ElapsedSeconds             float64             `json:"elapsed_seconds"`
}

// RunATMProbes 在冻结 ATM 语料上运行自建对抗探针档，并输出检索级统计。
func RunATMProbes(cfg *config.Config, opts Options) (map[string]any, error) {
	logf := opts.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	started := time.Now()
	if !IsValidPulseMode(opts.PulseMode) {
		return nil, fmt.Errorf("invalid pulse_mode %q (only %q is supported)", opts.PulseMode, PulseModeLocalPlain)
	}
	tier := NormalizeProbeTier(opts.Tier)
	if tier == "" {
		return nil, fmt.Errorf("未知探针档 %q（可用：%s）", opts.Tier, strings.Join(ProbeTierNames, " / "))
	}
	if strings.TrimSpace(opts.ProbeDir) == "" {
		return nil, errors.New("atm-probes 需要 --probes <dir>：指向含 MANIFEST.json 与缺席/冲突 JSONL 的探针目录")
	}
	if opts.Answerer == "llm" || opts.Judge == "llm" {
		return nil, errors.New("atm-probes 是检索级档位：不支持 answerer/judge（探针不携带可判分答案）")
	}

	dataDir, err := ResolveATMDataDir(opts.DataDir)
	if err != nil {
		return nil, err
	}
	officialCorpusPinsVerified := false
	if !opts.NoVerify {
		if err := VerifyATMCorpusPins(dataDir); err != nil {
			return nil, err
		}
		officialCorpusPinsVerified = true
	}
	probes, entry, err := LoadATMProbes(opts.ProbeDir, tier)
	if err != nil {
		return nil, err
	}

	opts.Suite = SuiteATMProbes
	opts.PulseMode = NormalizePulseMode(opts.PulseMode)
	// 探针与 ATM 主档共用冻结语料与同一世代 scope：索引一次建成、零重复灌库。
	scope := ScopeForSuiteAndMode(SuiteATM, evalUserID(), opts.PulseMode)
	engine, err := NewLocalEngine(cfg, SuiteATM, expandUser(opts.StorePath), WithPulseTokenizer(opts.PulseTokenizer), WithPulseMode(opts.PulseMode))
	if err != nil {
		return nil, err
	}
	items, byID, _, err := LoadATMSGMItems(dataDir, scope, opts.CorpusLimit)
	if err != nil {
		return nil, err
	}
	prepared, ingestSec, err := IngestATMCorpus(engine, items, opts.Rebuild, opts.ReusePrepare, logf)
	if err != nil {
		return nil, err
	}
	defer engine.CloseGeneration(context.Background(), logf)
	scope = engine.Scope

	rawLimit := opts.RawLimit
	if rawLimit <= 0 {
		rawLimit = 64
	}
	workers := opts.Workers
	if workers < 1 {
		workers = 8
	}
	if workers > 32 {
		workers = 32
	}

	logf("[atm-probes] suite=%s tier=%s pulse_mode=%s probes=%d raw_limit=%d scope=%s",
		SuiteATMProbes, tier, opts.PulseMode, len(probes), rawLimit, scope)

	rows := make([]map[string]any, len(probes))
	errs := make([]error, len(probes))
	jobs := make(chan int, len(probes))
	for i := range probes {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				probe := probes[idx]
				retIDs, hits, recallErr := RecallATMQuestion(engine, byID, probe.Question, rawLimit)
				if recallErr != nil {
					errs[idx] = recallErr
					continue
				}
				delivered := SelectATMAdaptiveDelivery(hits, retIDs, rawLimit)
				if coverageFloorEnabled() && delivered > 0 {
					if firstCoverRanks := engine.salientFirstCoverRanks(probe.Question, retIDs, rawLimit); len(firstCoverRanks) > 0 {
						delivered = SelectATMAdaptiveDeliveryCoverage(hits, retIDs, rawLimit, firstCoverRanks)
					}
				}
				if delivered > len(hits) {
					delivered = len(hits)
				}
				textOf := func(hit local.ResolvedHit) string {
					if text := hit.RawPlaintext; strings.TrimSpace(text) != "" {
						return text
					}
					if item := byID[hit.UnitID]; item != nil {
						return item.FullText
					}
					return ""
				}
				deliveredTexts := make([]string, 0, delivered)
				for _, hit := range hits[:delivered] {
					deliveredTexts = append(deliveredTexts, textOf(hit))
				}
				row := map[string]any{
					"id":          probe.ID,
					"group":       probe.Group,
					"subtype":     probe.Subtype,
					"question":    probe.Question,
					"delivered_n": delivered,
					"raw_n":       len(retIDs),
					"truncated":   delivered < len(retIDs),
					"silent":      delivered == 0,
				}
				switch tier {
				case TierProbeAbsence:
					leak := false
					for _, text := range deliveredTexts {
						for _, term := range probe.AbsentTerms {
							if probeTextContains(text, term) {
								leak = true
							}
						}
					}
					top1, drop := 0.0, 0.0
					if delivered > 0 {
						top1 = probeEnergy(hits[0])
						if delivered > 1 {
							drop = top1 - probeEnergy(hits[delivered-1])
						}
					}
					row["absent_term_leak"] = leak
					row["top1_energy"] = PyRound(top1, 3)
					row["energy_drop"] = PyRound(drop, 3)
				case TierProbeConflict:
					staleRank, currentRank := probeEvidenceRanks(probe, probeRanks(retIDs[:delivered]))
					rawStaleRank, rawCurrentRank := probeEvidenceRanks(probe, probeRanks(retIDs))
					staleMentions, currentMentions := probeMentions(probe, deliveredTexts)
					rawTexts := make([]string, 0, len(hits))
					for _, hit := range hits {
						rawTexts = append(rawTexts, textOf(hit))
					}
					rawStaleMentions, rawCurrentMentions := probeMentions(probe, rawTexts)
					row["stale_rank"] = staleRank
					row["current_rank"] = currentRank
					row["both_in_window"] = staleRank > 0 && currentRank > 0
					row["current_outranks_stale"] = staleRank > 0 && currentRank > 0 && currentRank < staleRank
					row["stale_mentions"] = staleMentions
					row["current_mentions"] = currentMentions
					row["stale_dominates"] = staleMentions > currentMentions
					row["raw_both_in_window"] = rawStaleRank > 0 && rawCurrentRank > 0
					row["raw_current_outranks_stale"] = rawStaleRank > 0 && rawCurrentRank > 0 && rawCurrentRank < rawStaleRank
					row["raw_stale_in_window"] = rawStaleRank > 0
					row["raw_current_in_window"] = rawCurrentRank > 0
					row["raw_stale_mentions"] = rawStaleMentions
					row["raw_current_mentions"] = rawCurrentMentions
					row["raw_stale_dominates"] = rawStaleMentions > rawCurrentMentions
				}
				rows[idx] = row
			}
		}()
	}
	wg.Wait()

	errCount := 0
	for _, err := range errs {
		if err != nil {
			errCount++
		}
	}
	if errCount > 0 {
		return nil, fmt.Errorf("atm-probes 检索失败 %d/%d 题：%w", errCount, len(probes), errs[firstNonNilIndex(errs)])
	}

	summary := atmProbeSummary{
		Suite:                      SuiteATMProbes,
		Tier:                       tier,
		Arm:                        "atm-probes-" + tier + "-" + ArmModeForPulseMode(opts.PulseMode),
		PulseMode:                  opts.PulseMode,
		Transport:                  TransportForPulseMode(opts.PulseMode),
		Scope:                      scope,
		LocalFallback:              false,
		RawLimit:                   rawLimit,
		N:                          len(probes),
		ProbesSHA256:               entry.SHA256,
		CorpusSHA256:               PinATMCorpusSHA256,
		OfficialCorpusPinsVerified: officialCorpusPinsVerified,
		Ingest: map[string]any{
			"corpus_rows": len(items), "inserted": insertedUnits(prepared), "reused": prepared.Reused,
			"ingest_seconds": PyRound(ingestSec, 2), "scope": scope,
		},
		Errors:         errCount,
		ElapsedSeconds: PyRound(time.Since(started).Seconds(), 1),
	}
	if tier == TierProbeAbsence {
		summary.Absence = aggregateAbsenceProbes(rows)
	} else {
		summary.Conflict = aggregateConflictProbes(rows)
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, err
	}
	artifact := summary.Arm + runWindowSuffix(opts)
	if err := writeResults(filepath.Join(opts.OutDir, "results-"+artifact+".jsonl"), rows); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(opts.OutDir, "summary-"+artifact+".json"), summary); err != nil {
		return nil, err
	}
	return probeSummaryMap(summary)
}

func insertedUnits(prepared EvalManifest) int {
	if prepared.Reused {
		return 0
	}
	return prepared.InsertedUnits
}

func firstNonNilIndex(errs []error) int {
	for i, err := range errs {
		if err != nil {
			return i
		}
	}
	return 0
}

func aggregateAbsenceProbes(rows []map[string]any) *absenceProbeStats {
	stats := &absenceProbeStats{SubtypeBreakdown: map[string]int{}}
	delivered := make([]int, 0, len(rows))
	raw := make([]int, 0, len(rows))
	top1 := make([]float64, 0, len(rows))
	drop := make([]float64, 0, len(rows))
	for _, row := range rows {
		n := intFromAny(row["delivered_n"])
		rawN := intFromAny(row["raw_n"])
		delivered = append(delivered, n)
		raw = append(raw, rawN)
		if n < rawN {
			stats.Truncated++
		}
		if n == 0 {
			stats.Silent++
		}
		if leaked, ok := row["absent_term_leak"].(bool); ok && leaked {
			stats.AbsentTermLeak++
		}
		top1 = append(top1, floatFromAny(row["top1_energy"]))
		drop = append(drop, floatFromAny(row["energy_drop"]))
		if subtype, ok := row["subtype"].(string); ok && subtype != "" {
			stats.SubtypeBreakdown[subtype]++
		}
	}
	stats.N = len(rows)
	stats.Delivered = probeDistribution(delivered)
	stats.RawHits = probeDistribution(raw)
	stats.TruncatedRate = probeRate(stats.Truncated, stats.N)
	stats.SilentRate = probeRate(stats.Silent, stats.N)
	stats.AbsentTermLeakRate = probeRate(stats.AbsentTermLeak, stats.N)
	stats.Top1Energy = probeFloatDistribution(top1)
	stats.EnergyDrop = probeFloatDistribution(drop)
	return stats
}

func aggregateConflictProbes(rows []map[string]any) *conflictProbeStats {
	stats := &conflictProbeStats{SubtypeBreakdown: map[string]int{}}
	delivered := make([]int, 0, len(rows))
	raw := make([]int, 0, len(rows))
	for _, row := range rows {
		n := intFromAny(row["delivered_n"])
		rawN := intFromAny(row["raw_n"])
		delivered = append(delivered, n)
		raw = append(raw, rawN)
		if n < rawN {
			stats.Truncated++
		}
		if n == 0 {
			stats.Silent++
		}
		staleRank := intFromAny(row["stale_rank"])
		currentRank := intFromAny(row["current_rank"])
		if staleRank > 0 {
			stats.StaleInWindow++
		}
		if currentRank > 0 {
			stats.CurrentInWindow++
		}
		if staleRank > 0 && currentRank > 0 {
			stats.BothInWindow++
			if currentRank < staleRank {
				stats.CurrentOutranksStale++
			}
		}
		stats.StaleMentions += intFromAny(row["stale_mentions"])
		stats.CurrentMentions += intFromAny(row["current_mentions"])
		if dominates, ok := row["stale_dominates"].(bool); ok && dominates {
			stats.StaleDominates++
		}
		if truthy(row["raw_stale_in_window"]) {
			stats.Raw64.StaleInWindow++
		}
		if truthy(row["raw_current_in_window"]) {
			stats.Raw64.CurrentInWindow++
		}
		if truthy(row["raw_both_in_window"]) {
			stats.Raw64.BothInWindow++
		}
		if truthy(row["raw_current_outranks_stale"]) {
			stats.Raw64.CurrentOutranksStale++
		}
		stats.Raw64.StaleMentions += intFromAny(row["raw_stale_mentions"])
		stats.Raw64.CurrentMentions += intFromAny(row["raw_current_mentions"])
		if truthy(row["raw_stale_dominates"]) {
			stats.Raw64.StaleDominates++
		}
		if subtype, ok := row["subtype"].(string); ok && subtype != "" {
			stats.SubtypeBreakdown[subtype]++
		}
	}
	stats.N = len(rows)
	stats.Delivered = probeDistribution(delivered)
	stats.RawHits = probeDistribution(raw)
	stats.TruncatedRate = probeRate(stats.Truncated, stats.N)
	stats.SilentRate = probeRate(stats.Silent, stats.N)
	stats.BothRate = probeRate(stats.BothInWindow, stats.N)
	stats.StaleRate = probeRate(stats.StaleInWindow, stats.N)
	stats.CurrentRate = probeRate(stats.CurrentInWindow, stats.N)
	stats.CurrentOutranksStaleRate = probeRate(stats.CurrentOutranksStale, stats.N)
	stats.StaleDominatesRate = probeRate(stats.StaleDominates, stats.N)
	return stats
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	}
	return 0
}

func floatFromAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	}
	return 0
}

// probeSummaryMap 把结构化 summary 转成与其它 suite 一致的 map 回执。
func probeSummaryMap(summary atmProbeSummary) (map[string]any, error) {
	encoded, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}
