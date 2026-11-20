// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
)

const (
	perfMaxScales = 6
	perfMaxFiles  = 20000
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
	root := strings.TrimSpace(a.args.String("dir"))
	if root == "" {
		root, err = os.MkdirTemp("", "rm-perf-slope-")
		if err != nil {
			return 1, err
		}
		defer os.RemoveAll(root)
	} else if err := os.MkdirAll(root, 0o700); err != nil {
		return 1, err
	}

	report := perfSlopeReport{Profile: "local · wal_autocheckpoint=0 (measurement ledger)"}
	sizes := make([]float64, 0, len(scales))
	appendMS := make([]float64, 0, len(scales))
	statements := make([]float64, 0, len(scales))
	walBytes := make([]float64, 0, len(scales))
	for _, files := range scales {
		result, err := measurePerfScale(root, files)
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

// measurePerfScale ingests one fixed-size synthetic corpus and reports the append cost,
// statement count and WAL volume of that single pass.
func measurePerfScale(root string, files int) (perfScaleResult, error) {
	dir := filepath.Join(root, fmt.Sprintf("n%d", files))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return perfScaleResult{}, err
	}
	for i := 0; i < files; i++ {
		// Fixed-length bodies keep the chunk count per file constant, so the reading tracks
		// per-unit storage cost instead of document size.
		body := fmt.Sprintf("# 记录 %d\n第 %d 条挂载记录用于测量逐条追加成本，固定长度文本保持分块数恒定。\n", i, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("doc-%05d.md", i)), []byte(body), 0o600); err != nil {
			return perfScaleResult{}, err
		}
	}

	dbPath := filepath.Join(root, fmt.Sprintf("n%d.db", files))
	connection, stats, err := db.OpenCounting(dbPath, db.LocalProfile())
	if err != nil {
		return perfScaleResult{}, err
	}
	defer connection.Close()
	// Measurement-only pragmas: automatic checkpoints are disabled so the WAL file is the
	// written-byte ledger for this pass. The product profile keeps its own checkpoint.
	if _, err := connection.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return perfScaleResult{}, err
	}
	if _, err := connection.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		return perfScaleResult{}, err
	}
	vault, err := localvault.NewVault(connection, []byte("perf-slope-measurement"))
	if err != nil {
		return perfScaleResult{}, err
	}
	client := localvault.NewClient(vault, localvault.NewLocalVaultTransport(vault))
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
	a.printf("perf slope · %s\n", report.Profile)
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
