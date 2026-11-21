// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

const (
	// ingestWorkerLeaseKey is the cross-process single-flight row for the scan
	// worker. The existing autowatch lease still fences one pass; this row keeps
	// a single long-lived worker so restarts cannot stack scanners.
	ingestWorkerLeaseKey  = "ingest_worker_lease"
	ingestWorkerStatusKey = "ingest_worker_status"
	ingestWorkerLeaseTTL  = 20 * time.Second
	ingestWorkerHeartbeat = 5 * time.Second
	// ingestWorkerRetry is how long an extra worker waits for the holder to stop.
	ingestWorkerRetry = 5 * time.Second
	// ingestQueueDrainBatch is the claim slice one ProcessRememberQueue call
	// handles before the pass re-checks its remaining budget.
	ingestQueueDrainBatch = 256
	// ingestQueueDrainBudget bounds one scan pass's queue drain so the source
	// scan and the snapshot builder still run inside the 10s tick. At the
	// measured ~1.1ms per applied remember this drains roughly 3.5k items per
	// pass instead of the 32 the old fixed limit allowed.
	ingestQueueDrainBudget = 4 * time.Second
	// ingestQueueDrainLimit is the per-pass cap: a very large backlog cannot
	// monopolise the pass even when items apply faster than the budget.
	ingestQueueDrainLimit = 4096
	// ingestQueueRetryBackoff spaces retries of one failing queued remember so it
	// cannot block newer items in the same drain slice.
	ingestQueueRetryBackoff = 10 * time.Second
)

// ingestWorkerLease is the single-instance guard held by `rmem ingestd`.
type ingestWorkerLease struct {
	db     *sql.DB
	token  string
	mu     sync.Mutex
	value  string
	stop   chan struct{}
	done   chan struct{}
	lost   atomic.Bool
	lostCh chan struct{}
	once   sync.Once
}

// runIngestd is the background worker: it scans sources, heals pointers and
// supervises the snapshot builder. It owns no recall surface and exits when the
// parent's lifeline pipe closes.
func (a *application) runIngestd() int {
	a.setEnvFromArgs()
	memory, err := a.openMemory()
	if err != nil {
		return a.fail(err)
	}
	defer func() { _ = memory.Close() }()
	if memory.Config == nil {
		return a.fail(errors.New("ingest worker requires a configured scope"))
	}
	if memory.Config.Mode != "local" {
		return a.fail(errors.New("ingest worker requires local mode and local_plain"))
	}
	if !IsAutoSyncEnabled(memory.Config.Path) {
		a.printLine("ingest worker: auto-sync disabled")
		return 0
	}
	memory.SetSnapshotOnly(true)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	in := a.stdin
	if in == nil {
		in = os.Stdin
	}
	go func() {
		_, _ = io.Copy(io.Discard, in)
		stop()
	}()

	var lease *ingestWorkerLease
	for lease == nil {
		if ctx.Err() != nil {
			return 0
		}
		current, ok, acquireErr := tryAcquireIngestWorkerLease(memory.Store.DB(), ingestWorkerLeaseTTL)
		if acquireErr != nil {
			return a.fail(acquireErr)
		}
		if ok {
			lease = current
			break
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(ingestWorkerRetry):
		}
	}
	defer lease.release()
	lease.startHeartbeat(ctx)
	applyIngestWorkerQoS()
	if cores := runtime.NumCPU(); cores > 2 {
		runtime.GOMAXPROCS(2)
	}
	log.Printf("ingestd: worker started pid=%d db=%s", os.Getpid(), memory.Config.DB)

	runPass := func() {
		writeIngestWorkerStatus(memory.Store.DB(), "scanning")
		if applied, queueErr := drainRememberQueue(ctx, memory, ingestQueueDrainBudget); queueErr != nil {
			log.Printf("ingestd: remember queue: %v", queueErr)
		} else if applied > 0 {
			log.Printf("ingestd: applied %d queued remember(s)", applied)
		}
		RunAutoWatchWorkerTick(ctx, memory, bgSettleWindow, episodicQuiescenceWindow)
		if IsEpisodicSyncPaused(memory.Store.DB(), memory.Config.Path) {
			writeIngestWorkerStatus(memory.Store.DB(), "paused")
			return
		}
		writeIngestWorkerStatus(memory.Store.DB(), "idle")
		a.ensureSnapshotBuilder(ctx, memory)
		if pruned, pruneErr := memory.PruneIngestQueueApplied(ctx); pruneErr != nil {
			log.Printf("ingestd: prune remember queue: %v", pruneErr)
		} else if pruned > 0 {
			log.Printf("ingestd: pruned %d applied remember queue row(s)", pruned)
		}
		if processed, queueErr := memory.ProcessCrystallizationQueue(ctx, 1); queueErr != nil {
			log.Printf("ingestd: crystallization queue: %v", queueErr)
		} else if processed > 0 {
			log.Printf("ingestd: crystallized %d deferred pulse(s)", processed)
		}
	}
	// Bootstrap immediately: a fresh install should not wait a full tick before
	// the first page of turns and the first published generation exist.
	runPass()

	ticker := time.NewTicker(defaultAutoWatchTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-lease.lostCh:
			log.Printf("ingestd: worker lease lost; exiting")
			return 1
		case <-ticker.C:
			if !IsAutoSyncEnabled(memory.Config.Path) {
				log.Printf("ingestd: auto-sync disabled; exiting")
				return 0
			}
			runPass()
		}
	}
}

// rememberQueueDrainer is the slice of the memory facade the drain loop needs.
type rememberQueueDrainer interface {
	ProcessRememberQueue(context.Context, facade.IngestQueueProcessOptions) (int, error)
}

// drainRememberQueue applies queued remembers in bounded batches until the pass
// budget, the per-pass cap, or an empty queue. Deferred items carry their own
// retry backoff, so one failing item cannot block the rest of the batch.
func drainRememberQueue(ctx context.Context, drainer rememberQueueDrainer, budget time.Duration) (int, error) {
	if drainer == nil {
		return 0, nil
	}
	if budget <= 0 {
		budget = ingestQueueDrainBudget
	}
	deadline := time.Now().Add(budget)
	applied := 0
	var firstErr error
	for applied < ingestQueueDrainLimit {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return applied, err
			}
		}
		batch := ingestQueueDrainBatch
		if remaining := ingestQueueDrainLimit - applied; remaining < batch {
			batch = remaining
		}
		progress, err := drainer.ProcessRememberQueue(ctx, facade.IngestQueueProcessOptions{
			Limit:             batch,
			LeaseDuration:     ingestWorkerLeaseTTL,
			HeartbeatInterval: ingestWorkerHeartbeat,
			RetryBackoff:      ingestQueueRetryBackoff,
		})
		applied += progress
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if progress == 0 || time.Now().After(deadline) {
			break
		}
	}
	return applied, firstErr
}

// superviseIngestWorker keeps one `ingestd` child alive for the daemon. The
// child's stdin is a lifeline pipe: when the daemon exits (including SIGKILL),
// the pipe closes and the worker leaves on its own.
func (a *application) superviseIngestWorker(ctx context.Context, memory *facade.Memory) {
	if memory == nil || memory.Config == nil {
		return
	}
	cfg := *memory.Config
	failures := 0
	delay := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if !IsAutoSyncEnabled(cfg.Path) {
			if !sleepContext(ctx, 30*time.Second) {
				return
			}
			continue
		}
		appendIngestLog(cfg, "supervisor: starting ingest worker")
		started := time.Now()
		err := runIngestWorker(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			appendIngestLog(cfg, "supervisor: worker exited: %v", err)
		} else {
			appendIngestLog(cfg, "supervisor: worker exited cleanly")
		}
		if time.Since(started) > 10*time.Minute {
			failures = 0
		}
		failures++
		if failures > 5 {
			appendIngestLog(cfg, "supervisor: restart budget exhausted; staying down")
			return
		}
		if !sleepContext(ctx, delay) {
			return
		}
		if delay < time.Minute {
			delay *= 2
			if delay > time.Minute {
				delay = time.Minute
			}
		}
	}
}

// runIngestWorker spawns one worker and waits. The parent keeps the write end
// of the lifeline pipe; closing it is the portable "please exit" signal.
func runIngestWorker(ctx context.Context, cfg config.Config) error {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return fmt.Errorf("resolve executable: %w", err)
	}
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "ingestd", "--config", cfg.Path, "--db", cfg.DB, "--mode", cfg.Mode, "--scope", cfg.Scope)
	cmd.Stdin = readEnd
	logPath := cfg.Log + ".ingestd"
	if cfg.Log == "" {
		logPath = cfg.DB + ".ingestd.log"
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
	if handle, openErr := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); openErr == nil {
		cmd.Stdout = handle
		cmd.Stderr = handle
		defer func() { _ = handle.Close() }()
	}
	if err := cmd.Start(); err != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return err
	}
	_ = readEnd.Close()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	select {
	case waitErr := <-waitCh:
		_ = writeEnd.Close()
		return waitErr
	case <-ctx.Done():
		_ = writeEnd.Close()
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-waitCh
		}
		return ctx.Err()
	}
}

// tryAcquireIngestWorkerLease attempts one compare-and-swap acquisition. A live
// holder makes it return (nil, false, nil); the caller retries.
func tryAcquireIngestWorkerLease(db *sql.DB, ttl time.Duration) (*ingestWorkerLease, bool, error) {
	if db == nil {
		return nil, false, errors.New("ingest worker lease requires an open store")
	}
	if ttl <= 0 {
		ttl = ingestWorkerLeaseTTL
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS rm_meta (k TEXT PRIMARY KEY, v BLOB NOT NULL)`); err != nil {
		return nil, false, err
	}
	token := newIngestToken()
	value := fmt.Sprintf("%d:%d:%s", os.Getpid(), time.Now().Unix(), token)
	for attempt := 0; attempt < 3; attempt++ {
		var current string
		err := db.QueryRow(`SELECT CAST(v AS TEXT) FROM rm_meta WHERE k = ?`, ingestWorkerLeaseKey).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			result, insertErr := db.Exec(`INSERT INTO rm_meta(k, v) VALUES (?, ?) ON CONFLICT(k) DO NOTHING`, ingestWorkerLeaseKey, []byte(value))
			if insertErr != nil {
				return nil, false, insertErr
			}
			if affected, _ := result.RowsAffected(); affected == 1 {
				return newIngestWorkerLease(db, token, value), true, nil
			}
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if ingestLeaseLive(current, time.Now(), ttl) {
			return nil, false, nil
		}
		result, updateErr := db.Exec(`UPDATE rm_meta SET v = ? WHERE k = ? AND CAST(v AS TEXT) = ?`, []byte(value), ingestWorkerLeaseKey, current)
		if updateErr != nil {
			return nil, false, updateErr
		}
		if affected, _ := result.RowsAffected(); affected == 1 {
			return newIngestWorkerLease(db, token, value), true, nil
		}
	}
	return nil, false, nil
}

func newIngestWorkerLease(db *sql.DB, token, value string) *ingestWorkerLease {
	return &ingestWorkerLease{
		db:     db,
		token:  token,
		value:  value,
		lostCh: make(chan struct{}),
	}
}

func (l *ingestWorkerLease) startHeartbeat(ctx context.Context) {
	l.stop = make(chan struct{})
	l.done = make(chan struct{})
	go func() {
		defer close(l.done)
		ticker := time.NewTicker(ingestWorkerHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.stop:
				return
			case <-ticker.C:
				if err := l.heartbeatOnce(); err != nil {
					l.markLost()
					return
				}
			}
		}
	}()
}

func (l *ingestWorkerLease) heartbeatOnce() error {
	l.mu.Lock()
	current := l.value
	l.mu.Unlock()
	next := fmt.Sprintf("%d:%d:%s", os.Getpid(), time.Now().Unix(), l.token)
	result, err := l.db.Exec(`UPDATE rm_meta SET v = ? WHERE k = ? AND CAST(v AS TEXT) = ?`, []byte(next), ingestWorkerLeaseKey, current)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("ingest worker lease was taken over")
	}
	l.mu.Lock()
	l.value = next
	l.mu.Unlock()
	return nil
}

func (l *ingestWorkerLease) markLost() {
	l.lost.Store(true)
	l.once.Do(func() { close(l.lostCh) })
}

func (l *ingestWorkerLease) release() {
	if l == nil {
		return
	}
	if l.stop != nil {
		close(l.stop)
		<-l.done
	}
	l.mu.Lock()
	value := l.value
	l.mu.Unlock()
	_, _ = l.db.Exec(`DELETE FROM rm_meta WHERE k = ? AND CAST(v AS TEXT) = ?`, ingestWorkerLeaseKey, value)
}

func ingestLeaseLive(value string, now time.Time, ttl time.Duration) bool {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 3)
	if len(parts) < 2 {
		return false
	}
	stamp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	age := now.Unix() - stamp
	return age >= 0 && age <= int64(ttl/time.Second)
}

// writeIngestWorkerStatus publishes the worker phase for `rmem scan status`.
// It is diagnostic only; the lease heartbeat is the liveness signal.
func writeIngestWorkerStatus(db *sql.DB, phase string) {
	if db == nil {
		return
	}
	payload := fmt.Sprintf(`{"phase":%q,"pid":%d,"at":%d}`, phase, os.Getpid(), time.Now().Unix())
	_, _ = db.Exec(`INSERT INTO rm_meta(k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v`,
		ingestWorkerStatusKey, []byte(payload))
}

func newIngestToken() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(raw[:])
}

func appendIngestLog(cfg config.Config, format string, values ...any) {
	path := cfg.Log + ".ingestd"
	if cfg.Log == "" {
		path = cfg.DB + ".ingestd.log"
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf(format, values...)
		return
	}
	defer func() { _ = handle.Close() }()
	_, _ = fmt.Fprintf(handle, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, values...))
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
