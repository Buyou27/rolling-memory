// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/rocareer/rolling-memory/internal/mcp"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
)

// snapshotBuildBudget bounds one full-corpus materialisation. The builder runs
// outside the recall daemon, so a slow vault only delays publication.
const snapshotBuildBudget = 30 * time.Minute

// runBuild materialises the full-corpus disk cluster and records the published
// corpus generation. It is safe to run while the recall daemon is serving: the
// daemon keeps its current snapshot and picks the new cluster up on the next
// freshness check.
func (a *application) runBuild() int {
	a.setEnvFromArgs()
	if os.Getenv("ROLLING_MEMORY_BUILD_BACKGROUND") == "1" {
		applyIngestWorkerQoS()
	}
	memory, err := a.openMemory()
	if err != nil {
		return a.fail(err)
	}
	defer func() { _ = memory.Close() }()
	memory.SetSnapshotOnly(false)
	memory.SetColdBuildMemoryLimit(mcp.ResolveColdBuildMemoryLimit(memory.Config))
	release, acquired, err := localvault.AcquireBuildLock(memory.Config.DB, 45*time.Minute)
	if err != nil {
		return a.fail(err)
	}
	if !acquired {
		return a.fail(errors.New("another snapshot build is already running"))
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), snapshotBuildBudget)
	defer cancel()
	started := time.Now()
	builtDocsRev, err := memory.RebuildSnapshot(ctx)
	if err != nil {
		return a.fail(err)
	}
	gap, known := memory.SnapshotBuildGap(ctx)
	result := map[string]any{
		"ok":             true,
		"db":             memory.Config.DB,
		"scope":          memory.Config.Scope,
		"duration_ms":    time.Since(started).Milliseconds(),
		"published":      memory.PublishedSnapshotReady(),
		"built_docs_rev": builtDocsRev,
		"gap":            gap,
		"gap_known":      known,
	}
	writeSnapshotBuildStatus(memory.Store.DB(), 0, "")
	if err := a.emit(result, func() {
		a.printf("snapshot ready: %s (%d ms, docs_rev=%d)\n", memory.Config.DB, result["duration_ms"], builtDocsRev)
	}); err != nil {
		return a.fail(err)
	}
	return 0
}
