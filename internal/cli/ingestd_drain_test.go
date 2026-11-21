// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

// fakeRememberQueue returns the scripted batch sizes in order and records the
// options the drain loop passes down.
type fakeRememberQueue struct {
	progress []int
	err      error
	calls    int
	options  []facade.IngestQueueProcessOptions
}

func (f *fakeRememberQueue) ProcessRememberQueue(_ context.Context, opts facade.IngestQueueProcessOptions) (int, error) {
	f.calls++
	f.options = append(f.options, opts)
	if f.calls <= len(f.progress) {
		return f.progress[f.calls-1], f.err
	}
	return 0, f.err
}

func TestDrainRememberQueueDrainsBatchesUntilCap(t *testing.T) {
	batch, cap := ingestQueueDrainBatch, ingestQueueDrainLimit
	progress := make([]int, cap/batch)
	for i := range progress {
		progress[i] = batch
	}
	queue := &fakeRememberQueue{progress: progress}
	applied, err := drainRememberQueue(context.Background(), queue, time.Second)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if applied != cap {
		t.Fatalf("applied=%d want cap=%d", applied, cap)
	}
	if queue.calls != cap/batch {
		t.Fatalf("calls=%d want %d", queue.calls, cap/batch)
	}
	for _, opts := range queue.options {
		if opts.Limit != batch {
			t.Fatalf("batch limit=%d want %d", opts.Limit, batch)
		}
		if opts.LeaseDuration != ingestWorkerLeaseTTL || opts.HeartbeatInterval != ingestWorkerHeartbeat || opts.RetryBackoff != ingestQueueRetryBackoff {
			t.Fatalf("drain options drifted: %+v", opts)
		}
	}
}

func TestDrainRememberQueueStopsWhenEmpty(t *testing.T) {
	queue := &fakeRememberQueue{progress: []int{200, 0}}
	applied, err := drainRememberQueue(context.Background(), queue, time.Second)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if applied != 200 || queue.calls != 2 {
		t.Fatalf("applied=%d calls=%d want 200/2", applied, queue.calls)
	}
}

func TestDrainRememberQueueKeepsFirstError(t *testing.T) {
	boom := errors.New("boom")
	queue := &fakeRememberQueue{progress: []int{100, 0}, err: boom}
	applied, err := drainRememberQueue(context.Background(), queue, time.Second)
	if applied != 100 {
		t.Fatalf("applied=%d want 100", applied)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v want boom", err)
	}
	if queue.calls != 2 {
		t.Fatalf("calls=%d want 2", queue.calls)
	}
}

func TestDrainRememberQueueHonorsBudget(t *testing.T) {
	queue := &fakeRememberQueue{progress: []int{7, 7, 7, 7}}
	applied, err := drainRememberQueue(context.Background(), queue, time.Nanosecond)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if applied != 7 || queue.calls != 1 {
		t.Fatalf("applied=%d calls=%d want 7/1 after an exhausted budget", applied, queue.calls)
	}
}

func TestDrainRememberQueueNilDrainer(t *testing.T) {
	if applied, err := drainRememberQueue(context.Background(), nil, time.Second); applied != 0 || err != nil {
		t.Fatalf("nil drainer applied=%d err=%v", applied, err)
	}
}
