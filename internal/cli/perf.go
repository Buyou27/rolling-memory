// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
	"github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

const (
	perfMaxScales  = 6
	perfMaxFiles   = 20000
	perfMaxQueries = 200
)

// perfScaleResult is one corpus size measured on its own throwaway vault. Every field is a
// measurement of the product path (the same mount sync the CLI and daemon run), not of a
// separate benchmark arm.
type perfScaleResult struct {
	Files             int     `json:"files"`
	Chunks            int     `json:"chunks"`
	Terms             int     `json:"terms"`
	AppendMS          float64 `json:"append_ms"`
	AppendMSPerFile   float64 `json:"append_ms_per_file"`
	Statements        int64   `json:"statements"`
	StatementsPerFile float64 `json:"statements_per_file"`
	TermsPerFile      float64 `json:"terms_per_file"`
	StatementsPerTerm float64 `json:"statements_per_term"`
	WALBytes          int64   `json:"wal_bytes"`
	WALBytesPerFile   float64 `json:"wal_bytes_per_file"`
	DBBytes           int64   `json:"db_bytes"`
}

// perfSlopeReport is the multi-scale receipt: per-scale readings plus the log-log slope of
// each total against corpus size (1.00 = linear in size, 0.00 = independent of size).
type perfSlopeReport struct {
	Profile         string            `json:"profile"`
	Ablation        string            `json:"ablation,omitempty"`
	Scales          []perfScaleResult `json:"scales"`
	AppendMSSlope   float64           `json:"append_ms_loglog_slope"`
	StatementsSlope float64           `json:"statements_loglog_slope"`
	WALBytesSlope   float64           `json:"wal_bytes_loglog_slope"`
}

// runPerfCommand dispatches the measurement harness.
func (a *application) runPerfCommand() (int, error) {
	switch a.args.Subcommand() {
	case "slope":
		return a.runPerfSlope()
	case "recall":
		return a.runPerfRecall()
	default:
		return 1, fmt.Errorf("perf: unknown subcommand %q", a.args.Subcommand())
	}
}

// runPerfSlope measures per-unit storage cost at several corpus sizes. It never opens the
// configured vault: each scale gets a fresh throwaway database, so the reading describes the
// code under measurement and cannot touch user memory.
func (a *application) runPerfSlope() (int, error) {
	scales, err := parsePerfScales(a.args.String("files"))
	if err != nil {
		return 1, err
	}
	ablation := strings.TrimSpace(a.args.String("ablate"))
	if ablation != "" && ablation != perfAblationRevisionTriggers {
		return 1, fmt.Errorf("perf: unknown ablation %q (want %q)", ablation, perfAblationRevisionTriggers)
	}
	root, cleanup, err := perfRoot(a.args.String("dir"))
	if err != nil {
		return 1, err
	}
	defer cleanup()

	report := perfSlopeReport{Profile: "local · wal_autocheckpoint=0 (measurement ledger)", Ablation: ablation}
	sizes := make([]float64, 0, len(scales))
	appendMS := make([]float64, 0, len(scales))
	statements := make([]float64, 0, len(scales))
	walBytes := make([]float64, 0, len(scales))
	for _, files := range scales {
		result, err := measurePerfScale(root, files, ablation)
		if err != nil {
			return 1, err
		}
		report.Scales = append(report.Scales, result)
		sizes = append(sizes, float64(files))
		appendMS = append(appendMS, result.AppendMS)
		statements = append(statements, float64(result.Statements))
		walBytes = append(walBytes, float64(result.WALBytes))
	}
	report.AppendMSSlope = logLogSlope(sizes, appendMS)
	report.StatementsSlope = logLogSlope(sizes, statements)
	report.WALBytesSlope = logLogSlope(sizes, walBytes)
	return 0, a.emit(report, func() { a.printPerfSlope(report) })
}

// perfAblationRevisionTriggers drops the per-row revision triggers inside the throwaway
// vault only, so their cost can be attributed before any production change. It is an
// ablation, never a product mode: a vault without them would not advance its revision.
const perfAblationRevisionTriggers = "revision-triggers"

// Perf profile names select storage pragmas inside the throwaway vault only. The product
// keeps LocalProfile; the alternatives exist so a proposed change can be measured first.
const (
	perfProfileLocal = "local"
	perfProfileCache = "cache64"
	perfProfileMmap  = "mmap256"
	perfProfileBoth  = "cache64+mmap256"

	perfCacheSizeKiB = -65536 // negative means KiB: a 64 MiB page cache
	perfMmapBytes    = 256 << 20
)

// perfStorageProfile resolves the measurement profile. Only the measured copy changes; the
// product's LocalProfile and the daemon memory budget are untouched.
func perfStorageProfile(name string) (db.StorageProfile, error) {
	profile := db.LocalProfile()
	switch strings.TrimSpace(name) {
	case "", perfProfileLocal:
		return profile, nil
	case perfProfileCache:
		profile.CacheSize = perfCacheSizeKiB
	case perfProfileMmap:
		profile.MmapSize = perfMmapBytes
	case perfProfileBoth:
		profile.CacheSize = perfCacheSizeKiB
		profile.MmapSize = perfMmapBytes
	default:
		return profile, fmt.Errorf("perf: unknown profile %q (want %s, %s, %s or %s)",
			name, perfProfileLocal, perfProfileCache, perfProfileMmap, perfProfileBoth)
	}
	return profile, nil
}

// perfVault is one throwaway measurement database plus the handles the traces need.
type perfVault struct {
	connection *sql.DB
	stats      *db.SQLStats
	vault      *localvault.Vault
	client     *localvault.Client
}

// openPerfVault opens a counting pool on a throwaway path and applies the product's
// canonical harmonics, so both traces describe the default product assembly rather than a
// simpler fixture. Automatic checkpoints are disabled: the WAL file is the written-byte
// ledger for the write trace.
func openPerfVault(dbPath string, profile db.StorageProfile) (*perfVault, error) {
	connection, stats, err := db.OpenCounting(dbPath, profile)
	if err != nil {
		return nil, err
	}
	cleanup := func(cause error) (*perfVault, error) {
		_ = connection.Close()
		return nil, cause
	}
	if _, err := connection.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return cleanup(err)
	}
	if _, err := connection.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		return cleanup(err)
	}
	vault, err := localvault.NewVault(connection, []byte("perf-measurement"))
	if err != nil {
		return cleanup(err)
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("ROLLING_MEMORY_HARMONICS")), "off") {
		vault.SetCanonicalHarmonics(plain.CanonicalHarmonicOptions{
			EnableStructuredDates: true,
			Expander:              plain.CanonicalCompanionTags,
		})
		vault.SetSegmenterOptions(localvault.SegmenterPackOptions{HarmonicOverlay: true})
	}
	return &perfVault{
		connection: connection,
		stats:      stats,
		vault:      vault,
		client:     localvault.NewClient(vault, localvault.NewLocalVaultTransport(vault)),
	}, nil
}

// writePerfCorpus writes one fixed-shape synthetic corpus. withMarker adds a unique token
// per file so the read trace can address one document by name.
func writePerfCorpus(dir string, files int, withMarker bool) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for i := 0; i < files; i++ {
		// Fixed-length bodies keep the chunk count per file constant, so the reading tracks
		// per-unit storage cost instead of document size.
		marker := ""
		if withMarker {
			marker = fmt.Sprintf("标记%05d ", i)
		}
		body := fmt.Sprintf("# 记录 %d\n%s第 %d 条挂载记录用于测量逐条追加成本，固定长度文本保持分块数恒定。\n", i, marker, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("doc-%05d.md", i)), []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// measurePerfScale ingests one fixed-size synthetic corpus and reports the append cost,
// statement count and WAL volume of that single pass.
func measurePerfScale(root string, files int, ablation string) (perfScaleResult, error) {
	dir := filepath.Join(root, fmt.Sprintf("n%d", files))
	if err := writePerfCorpus(dir, files, false); err != nil {
		return perfScaleResult{}, err
	}

	dbPath := filepath.Join(root, fmt.Sprintf("n%d.db", files))
	opened, err := openPerfVault(dbPath, db.LocalProfile())
	if err != nil {
		return perfScaleResult{}, err
	}
	connection, stats, client := opened.connection, opened.stats, opened.client
	defer connection.Close()
	if ablation == perfAblationRevisionTriggers {
		if err := dropRevisionTriggers(connection); err != nil {
			return perfScaleResult{}, err
		}
	}
	if _, err := client.AddBackgroundDir(dir); err != nil {
		return perfScaleResult{}, err
	}

	before := stats.Snapshot()
	started := time.Now()
	synced, err := client.SyncBackgroundDirs(context.Background())
	elapsed := time.Since(started)
	if err != nil {
		return perfScaleResult{}, err
	}
	delta := stats.Snapshot().Since(before)
	if synced.IngestedFiles != files {
		return perfScaleResult{}, fmt.Errorf("perf: ingested %d of %d files", synced.IngestedFiles, files)
	}
	// Terms are counted after the snapshot so the reading itself does not join the delta.
	// They are the path-independent denominator: the reader coordinate inside each chunk
	// carries the absolute path, so a longer root yields more terms per file.
	var terms int
	if err := connection.QueryRow(`SELECT COUNT(*) FROM rm_terms`).Scan(&terms); err != nil {
		return perfScaleResult{}, err
	}

	result := perfScaleResult{
		Files:      files,
		Chunks:     synced.IngestedChunks,
		Terms:      terms,
		AppendMS:   float64(elapsed.Microseconds()) / 1000.0,
		Statements: delta.Statements(),
		WALBytes:   fileSizeOrZero(dbPath + "-wal"),
		DBBytes:    fileSizeOrZero(dbPath),
	}
	result.AppendMSPerFile = result.AppendMS / float64(files)
	result.StatementsPerFile = float64(result.Statements) / float64(files)
	result.TermsPerFile = float64(result.Terms) / float64(files)
	if result.Terms > 0 {
		result.StatementsPerTerm = float64(result.Statements) / float64(result.Terms)
	}
	result.WALBytesPerFile = float64(result.WALBytes) / float64(files)
	return result, nil
}

// dropRevisionTriggers removes the revision triggers from a throwaway vault.
func dropRevisionTriggers(connection *sql.DB) error {
	rows, err := connection.Query(`SELECT name FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'rm_rev_%'`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := connection.Exec(`DROP TRIGGER "` + name + `"`); err != nil {
			return err
		}
	}
	return nil
}

// perfStatementGroup is one leading-words bucket of the statements a query issues.
type perfStatementGroup struct {
	Statement string `json:"statement"`
	Count     int64  `json:"count"`
}

// perfRecallScale is one corpus size measured on the read path.
type perfRecallScale struct {
	Files              int                  `json:"files"`
	Terms              int                  `json:"terms"`
	Queries            int                  `json:"queries"`
	ColdMS             float64              `json:"cold_query_ms"`
	P50MS              float64              `json:"p50_ms"`
	P95MS              float64              `json:"p95_ms"`
	P99MS              float64              `json:"p99_ms"`
	StatementsPerQuery float64              `json:"statements_per_query"`
	HitsPerQuery       float64              `json:"hits_per_query"`
	StatementGroups    []perfStatementGroup `json:"statement_groups,omitempty"`
}

// perfRecallReport is the read-side receipt. Percentiles exclude the first (cold) query.
type perfRecallReport struct {
	Profile  string            `json:"profile"`
	Scales   []perfRecallScale `json:"scales"`
	P50Slope float64           `json:"p50_loglog_slope"`
}

// runPerfRecall measures the read path: cold first query, warm percentiles, statements per
// query and hits per query at several corpus sizes.
func (a *application) runPerfRecall() (int, error) {
	scales, err := parsePerfScales(a.args.String("files"))
	if err != nil {
		return 1, err
	}
	queries := a.args.Int("queries")
	if queries < 1 {
		queries = 1
	}
	if queries > perfMaxQueries {
		queries = perfMaxQueries
	}
	profileName := strings.TrimSpace(a.args.String("profile"))
	profile, err := perfStorageProfile(profileName)
	if err != nil {
		return 1, err
	}
	if profileName == "" {
		profileName = perfProfileLocal
	}
	root, cleanup, err := perfRoot(a.args.String("dir"))
	if err != nil {
		return 1, err
	}
	defer cleanup()

	report := perfRecallReport{Profile: "local · product harmonics · profile=" + profileName + " · one cold query then warm percentiles"}
	sizes := make([]float64, 0, len(scales))
	p50s := make([]float64, 0, len(scales))
	for _, files := range scales {
		result, err := measurePerfRecall(root, files, queries, profile)
		if err != nil {
			return 1, err
		}
		report.Scales = append(report.Scales, result)
		sizes = append(sizes, float64(files))
		p50s = append(p50s, result.P50MS)
	}
	report.P50Slope = logLogSlope(sizes, p50s)
	return 0, a.emit(report, func() { a.printPerfRecall(report) })
}

// perfFetchK mirrors the product's delivery margin: three times the delivery depth, at
// least 20, and a tighter margin once the corpus passes the large-vault threshold.
func perfFetchK(topK, corpusSize int) int {
	fetchK := topK * 3
	if fetchK < 20 {
		fetchK = 20
	}
	if corpusSize >= 2048 {
		fetchK = topK + 4
		if fetchK < 12 {
			fetchK = 12
		}
	}
	return fetchK
}

// measurePerfRecall ingests one corpus, checkpoints to the product's steady layout, then
// runs the queries through the same RecallWithOptions entry the delivery path uses.
func measurePerfRecall(root string, files, queries int, profile db.StorageProfile) (perfRecallScale, error) {
	dir := filepath.Join(root, fmt.Sprintf("r%d", files))
	if err := writePerfCorpus(dir, files, true); err != nil {
		return perfRecallScale{}, err
	}
	dbPath := filepath.Join(root, fmt.Sprintf("r%d.db", files))
	opened, err := openPerfVault(dbPath, profile)
	if err != nil {
		return perfRecallScale{}, err
	}
	defer opened.connection.Close()
	ctx := context.Background()
	if _, err := opened.client.AddBackgroundDir(dir); err != nil {
		return perfRecallScale{}, err
	}
	if _, err := opened.client.SyncBackgroundDirs(ctx); err != nil {
		return perfRecallScale{}, err
	}
	// A long-lived vault checkpoints; reads then hit the main database instead of a WAL the
	// product would have folded back.
	if _, err := opened.connection.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return perfRecallScale{}, err
	}
	var terms int
	if err := opened.connection.QueryRow(`SELECT COUNT(*) FROM rm_terms`).Scan(&terms); err != nil {
		return perfRecallScale{}, err
	}

	const topK = 10
	fetchK := perfFetchK(topK, files)
	now := clock.Unix()
	latencies := make([]float64, 0, queries)
	var hitTotal, statementTotal int64
	coldMS := 0.0
	opened.stats.EnableGroups(true)
	for i := 0; i < queries; i++ {
		index := i * files / queries
		cue := fmt.Sprintf("标记%05d", index)
		before := opened.stats.Snapshot()
		started := time.Now()
		hits, err := opened.client.RecallWithOptions(ctx, cue, localvault.BlindQueryOptions{TopK: fetchK, TimeRef: now})
		elapsed := time.Since(started)
		if err != nil {
			return perfRecallScale{}, err
		}
		elapsedMS := float64(elapsed.Microseconds()) / 1000.0
		latencies = append(latencies, elapsedMS)
		if i == 0 {
			coldMS = elapsedMS
		}
		hitTotal += int64(len(hits))
		statementTotal += opened.stats.Snapshot().Since(before).Statements()
	}
	warm := latencies
	if len(warm) > 1 {
		warm = warm[1:]
	}
	sorted := append([]float64(nil), warm...)
	sort.Float64s(sorted)
	groups := opened.stats.Groups()
	opened.stats.EnableGroups(false)
	return perfRecallScale{
		Files:              files,
		Terms:              terms,
		Queries:            queries,
		ColdMS:             coldMS,
		P50MS:              perfPercentile(sorted, 0.50),
		P95MS:              perfPercentile(sorted, 0.95),
		P99MS:              perfPercentile(sorted, 0.99),
		StatementsPerQuery: float64(statementTotal) / float64(queries),
		HitsPerQuery:       float64(hitTotal) / float64(queries),
		StatementGroups:    topStatementGroups(groups, 8),
	}, nil
}

// topStatementGroups orders the attribution buckets by count and keeps the leading ones.
func topStatementGroups(groups map[string]int64, limit int) []perfStatementGroup {
	out := make([]perfStatementGroup, 0, len(groups))
	for statement, count := range groups {
		out = append(out, perfStatementGroup{Statement: statement, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Statement < out[j].Statement
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// perfPercentile returns the nearest-rank percentile of an ascending sample.
func perfPercentile(sorted []float64, fraction float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if fraction <= 0 {
		return sorted[0]
	}
	if fraction >= 1 {
		return sorted[len(sorted)-1]
	}
	index := int(math.Ceil(fraction*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

// perfRoot resolves the artifact root, removing a default temp root when the run ends.
func perfRoot(dir string) (string, func(), error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		root, err := os.MkdirTemp("", "rm-perf-")
		if err != nil {
			return "", nil, err
		}
		return root, func() { _ = os.RemoveAll(root) }, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	return dir, func() {}, nil
}

// parsePerfScales reads the comma-separated corpus sizes, rejects duplicates and keeps the
// list ordered so the slope is computed over increasing sizes.
func parsePerfScales(raw string) ([]int, error) {
	seen := map[int]bool{}
	sizes := make([]int, 0, perfMaxScales)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		size, err := strconv.Atoi(part)
		if err != nil || size <= 0 || size > perfMaxFiles {
			return nil, fmt.Errorf("perf: invalid corpus size %q (want 1-%d)", part, perfMaxFiles)
		}
		if seen[size] {
			continue
		}
		seen[size] = true
		sizes = append(sizes, size)
	}
	if len(sizes) < 2 {
		return nil, fmt.Errorf("perf: at least two distinct corpus sizes are required")
	}
	if len(sizes) > perfMaxScales {
		sizes = sizes[:perfMaxScales]
	}
	sort.Ints(sizes)
	return sizes, nil
}

// logLogSlope fits y = a·n^b over positive samples and returns b. It reports how a total
// grows with corpus size: 1.00 is linear (per-unit cost constant), 0.00 is flat.
func logLogSlope(xs, ys []float64) float64 {
	var sumX, sumY, sumXX, sumXY float64
	n := 0
	for i := range xs {
		if i >= len(ys) || xs[i] <= 0 || ys[i] <= 0 {
			continue
		}
		x, y := math.Log(xs[i]), math.Log(ys[i])
		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
		n++
	}
	if n < 2 {
		return 0
	}
	denominator := float64(n)*sumXX - sumX*sumX
	if denominator == 0 {
		return 0
	}
	return (float64(n)*sumXY - sumX*sumY) / denominator
}

func fileSizeOrZero(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (a *application) printPerfSlope(report perfSlopeReport) {
	if report.Ablation != "" {
		a.printf("perf slope · %s · ablation=%s (throwaway vault only)\n", report.Profile, report.Ablation)
	} else {
		a.printf("perf slope · %s\n", report.Profile)
	}
	a.printf("%6s %10s %10s %10s %10s %10s %9s %10s\n",
		"files", "append ms", "ms/file", "stmts", "stmt/file", "terms/file", "stmt/term", "wal KB/file")
	for _, scale := range report.Scales {
		a.printf("%6d %10.1f %10.2f %10d %10.1f %10.1f %9.2f %10.1f\n",
			scale.Files, scale.AppendMS, scale.AppendMSPerFile, scale.Statements,
			scale.StatementsPerFile, scale.TermsPerFile, scale.StatementsPerTerm,
			float64(scale.WALBytesPerFile)/1024)
	}
	a.printf("log-log slope vs corpus size (1.00 = linear, 0.00 = flat): append %.2f · statements %.2f · wal %.2f\n",
		report.AppendMSSlope, report.StatementsSlope, report.WALBytesSlope)
}

func (a *application) printPerfRecall(report perfRecallReport) {
	a.printf("perf recall · %s\n", report.Profile)
	a.printf("%6s %9s %9s %9s %9s %9s %11s %8s\n",
		"files", "terms", "cold ms", "p50 ms", "p95 ms", "p99 ms", "stmt/query", "hits/q")
	for _, scale := range report.Scales {
		a.printf("%6d %9d %9.1f %9.2f %9.2f %9.2f %11.1f %8.1f\n",
			scale.Files, scale.Terms, scale.ColdMS, scale.P50MS, scale.P95MS, scale.P99MS,
			scale.StatementsPerQuery, scale.HitsPerQuery)
	}
	a.printf("log-log p50 slope vs corpus size: %.2f\n", report.P50Slope)
	if len(report.Scales) > 0 {
		largest := report.Scales[len(report.Scales)-1]
		if len(largest.StatementGroups) > 0 {
			a.printf("statement attribution at %d files (leading words, prepare and execute both count):\n", largest.Files)
			for _, group := range largest.StatementGroups {
				a.printf("  %6d  %s\n", group.Count, group.Statement)
			}
		}
	}
}
