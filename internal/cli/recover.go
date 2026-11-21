// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"os"
	"time"
)

// runRecover runs the episodic source scan repeatedly without reconcile, heal or
// prune. It is an additive recovery path: existing units are only superseded by
// an exact turn rewrite, never bulk-retired.
func (a *application) runRecover() int {
	a.setEnvFromArgs()
	memory, err := a.openMemory()
	if err != nil {
		return a.fail(err)
	}
	defer func() { _ = memory.Close() }()
	memory.SetSnapshotOnly(true)
	home, _ := os.UserHomeDir()
	passes := a.args.Int("passes")
	if passes <= 0 {
		passes = 10
	}
	limit := a.args.Int("limit")
	if limit <= 0 {
		limit = 500
	}
	budget := a.args.Int("budget_sec")
	if budget <= 0 {
		budget = 60
	}
	db := memory.Store.DB()
	idlePasses := 0
	for pass := 1; pass <= passes; pass++ {
		var before, after int64
		_ = db.QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&before)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(budget)*time.Second)
		scanErr := runEpisodicSourceScanTickWithLimit(ctx, memory, home, limit)
		cancel()
		_ = db.QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&after)
		a.printf("pass %d: docs %d -> %d (%+d) err=%v\n", pass, before, after, after-before, scanErr)
		if after == before {
			idlePasses++
			if idlePasses >= 2 {
				break
			}
		} else {
			idlePasses = 0
		}
	}
	return 0
}
