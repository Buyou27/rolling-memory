// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// This adapter owns the worker pool for the intra-event endorsement pass. The
// portable core subset (core_portability_test.go) stays free of runtime and
// sync imports; unit_relations.go reaches this pool through the
// endorsePairDispatch hook and keeps a serial fallback when the hook is absent.

// endorseWorkersOverride pins the worker count for determinism tests; 0 uses
// the runtime default (GOMAXPROCS).
var endorseWorkersOverride int

// endorseUnitEvaluations counts evaluated event-unit indices when a test pins
// the worker count, so tests can assert the parallel path actually ran.
var endorseUnitEvaluations atomic.Int64

func init() {
	endorsePairDispatch = dispatchEndorsePairs
}

// endorseDispatchProbe is installed only by measurement tests; nil in production.
var endorseDispatchProbe func(n int)

// SetEndorseDispatchProbe installs the dispatch-size hook; nil removes it.
func SetEndorseDispatchProbe(fn func(n int)) { endorseDispatchProbe = fn }

func dispatchEndorsePairs(n int, eval endorsePairEval) []float64 {
	if endorseDispatchProbe != nil {
		endorseDispatchProbe(n)
	}
	out := make([]float64, n)
	if n <= 1 {
		return out
	}
	workers := endorseWorkersOverride
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > n-1 {
		workers = n - 1
	}
	if workers < 1 {
		workers = 1
	}
	// Below four evaluations per worker the fixed goroutine spawn and wakeup
	// cost dominates the parallel gain; keep identical coverage in one loop.
	if n <= 4*workers {
		local := &rmQueryScratch{}
		for i := 1; i < n; i++ {
			out[i] = eval(i, local)
			if endorseWorkersOverride > 0 {
				endorseUnitEvaluations.Add(1)
			}
		}
		return out
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := &rmQueryScratch{}
			for {
				i := int(next.Add(1))
				if i >= n {
					return
				}
				out[i] = eval(i, local)
				if endorseWorkersOverride > 0 {
					endorseUnitEvaluations.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	return out
}
