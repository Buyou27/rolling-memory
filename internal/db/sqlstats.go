// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package db

import (
	"context"
	"database/sql/driver"
	"sync/atomic"
)

// SQLStats counts the statements a measurement pool sends to SQLite. database/sql exposes
// no statement counter, and per-unit write amplitude is the number the storage work has to
// move, so measurement paths wrap the driver instead of inferring cost from wall clock.
type SQLStats struct {
	executes atomic.Int64
	queries  atomic.Int64
	prepares atomic.Int64
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
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{Stmt: stmt, stats: c.stats}, nil
}

func (c *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if prep, ok := c.Conn.(driver.ConnPrepareContext); ok {
		c.stats.prepares.Add(1)
		stmt, err := prep.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return &countingStmt{Stmt: stmt, stats: c.stats}, nil
	}
	return c.Prepare(query)
}

func (c *countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.stats.executes.Add(1)
	if exec, ok := c.Conn.(driver.ExecerContext); ok {
		return exec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.stats.queries.Add(1)
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
}

func (s *countingStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.stats.executes.Add(1)
	return s.Stmt.Exec(args)
}

func (s *countingStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.stats.queries.Add(1)
	return s.Stmt.Query(args)
}

func (s *countingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.stats.executes.Add(1)
	if exec, ok := s.Stmt.(driver.StmtExecContext); ok {
		return exec.ExecContext(ctx, args)
	}
	return nil, driver.ErrSkip
}

func (s *countingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.stats.queries.Add(1)
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
