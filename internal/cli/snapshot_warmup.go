// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"log"
	"time"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// snapshotWarmupInterval is a read-only preload cadence. It keeps the reader on
// the newest published generation without putting the sidecar load on the first
// query after a build.
const snapshotWarmupInterval = 15 * time.Second

// backgroundSnapshotWarmup periodically refreshes the read-only corpus snapshot.
// In snapshot-only mode Warmup never builds; it only opens the published slot,
// loads its docs sidecar and applies the bounded change log.
func (a *application) backgroundSnapshotWarmup(ctx context.Context, memory *facade.Memory) {
	if memory == nil || !memory.SnapshotOnly() {
		return
	}
	ticker := time.NewTicker(snapshotWarmupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := memory.Warmup(ctx); err != nil && ctx.Err() == nil {
			log.Printf("snapshot warmup: %v", err)
		}
	}
}
