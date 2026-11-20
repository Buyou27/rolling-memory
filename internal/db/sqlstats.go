// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package db

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync"
	"sync/atomic"
)

// SQLStats counts the statements a measurement pool sends to SQLite. database/sql exposes
// no statement counter, and per-unit write amplitude is the number the storage work has to
// move, so measurement paths wrap the driver instead of inferring cost from wall clock.
type SQLStats struct {
	executes atomic.Int64
	queries  atomic.Int64
	prepares atomic.Int64

	mu     sync.Mutex
	record bool
	groups map[string]int64
}

// EnableGroups turns on per-statement attribution and clears any previous sample. It is a
// measurement aid: every statement is bucketed by its leading words, so a hot path can be
// attributed without guessing from wall clock. Keep it off outside a measured window.
func (s *SQLStats) EnableGroups(record bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record = record
	s.groups = nil
}

// Groups returns the recorded statement buckets, highest count first is left to the caller.
func (s *SQLStats) Groups() map[string]int64 {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.groups))
	for key, count := range s.groups {
		out[key] = count
	}
	return out
}

// observe buckets one statement by its leading words when attribution is enabled.
func (s *SQLStats) observe(query string) {
	s.mu.Lock()
	if !s.record {
		s.mu.Unlock()
		return
	}
	if s.groups == nil {
		s.groups = make(map[string]int64, 16)
	}
	s.groups[statementBucket(query)]++
	s.mu.Unlock()
}

// statementBucket keeps a stable, low-cardinality key: the leading words of the statement.
func statementBucket(query string) string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return "(empty)"
	}
	if len(fields) > 3 {
		fields = fields[:3]
	}
	return strings.Join(fields, " ")
}

// SQLStatsSnapshot is the counter set at one moment.
type SQLStatsSnapshot struct {
	Executes int64 `json:"executes"`
	Queries  int64 `json:"queries"`
	Prepares int64 `json:"prepares"`
}

// Statements is the total number of statements sent to SQLite.
func (s SQLStatsSnapshot) Statements() int64 { return s.Executes + s.Queries + s.Prepares }

// Snapshot reads the counters.
func (s *SQLStats) Snapshot() SQLStatsSnapshot {
	if s == nil {
		return SQLStatsSnapshot{}
	}
	return SQLStatsSnapshot{Executes: s.executes.Load(), Queries: s.queries.Load(), Prepares: s.prepares.Load()}
}

// Since returns the movement between two snapshots.
func (s SQLStatsSnapshot) Since(prev SQLStatsSnapshot) SQLStatsSnapshot {
	return SQLStatsSnapshot{
		Executes: s.Executes - prev.Executes,
		Queries:  s.Queries - prev.Queries,
		Prepares: s.Prepares - prev.Prepares,
	}
}

// countingConnector wraps a driver connector and hands out counting connections. Optional
// driver interfaces are forwarded explicitly on purpose: hiding them would make
// database/sql prepare every statement itself and change both the measured cost and the
// comparison with the production path.
type countingConnector struct {
	inner driver.Connector
	stats *SQLStats
}

func (c *countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: conn, stats: c.stats}, nil
}

func (c *countingConnector) Driver() driver.Driver { return c.inner.Driver() }

type countingConn struct {
	driver.Conn
	stats *SQLStats
}

func (c *countingConn) Prepare(query string) (driver.Stmt, error) {
	c.stats.prepares.Add(1)
	c.stats.observe(query)
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{Stmt: stmt, stats: c.stats, query: query}, nil
}

func (c *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if prep, ok := c.Conn.(driver.ConnPrepareContext); ok {
		c.stats.prepares.Add(1)
		c.stats.observe(query)
		stmt, err := prep.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return &countingStmt{Stmt: stmt, stats: c.stats, query: query}, nil
	}
	return c.Prepare(query)
}

func (c *countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.stats.executes.Add(1)
	c.stats.observe(query)
	if exec, ok := c.Conn.(driver.ExecerContext); ok {
		return exec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.stats.queries.Add(1)
	c.stats.observe(query)
	if queryer, ok := c.Conn.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

func (c *countingConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *countingConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

type countingStmt struct {
	driver.Stmt
	stats *SQLStats
	query string
}

func (s *countingStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.stats.executes.Add(1)
	s.stats.observe(s.query)
	return s.Stmt.Exec(args)
}

func (s *countingStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.stats.queries.Add(1)
	s.stats.observe(s.query)
	return s.Stmt.Query(args)
}

func (s *countingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.stats.executes.Add(1)
	s.stats.observe(s.query)
	if exec, ok := s.Stmt.(driver.StmtExecContext); ok {
		return exec.ExecContext(ctx, args)
	}
	return nil, driver.ErrSkip
}

func (s *countingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.stats.queries.Add(1)
	s.stats.observe(s.query)
	if queryer, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return queryer.QueryContext(ctx, args)
	}
	return nil, driver.ErrSkip
}

func (s *countingStmt) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := s.Stmt.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

func (s *countingStmt) ColumnConverter(idx int) driver.ValueConverter {
	if converter, ok := s.Stmt.(driver.ColumnConverter); ok {
		return converter.ColumnConverter(idx)
	}
	return driver.DefaultParameterConverter
}
