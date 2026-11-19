// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"time"
)

// PendingTraceSummary summarizes an unencoded raw trace for query-aware barrier evaluation and scheduling.
type PendingTraceSummary struct {
	ID         uint64
	Session    string
	OccurredAt int64
	SearchText string
}

// SessionScheduleMeta holds metadata for ordering multi-session encoding queues.
type SessionScheduleMeta struct {
	Session   string
	TurnCount int
	MinAt     int64
	MaxAt     int64
	MinID     uint64
}

// AdaptiveBarrierSnapshot captures the real-time progress and relevance state of a scope during WaitEnriched.
type AdaptiveBarrierSnapshot struct {
	TotalPendingTraces     int
	RelevantPendingTraces  int
	InitialRelevantPending int
	AdvancedAny            bool
	HasActiveHeartbeat     bool
	Elapsed                time.Duration
	IdleSinceProgress      time.Duration
	BaseTimeout            time.Duration
	MaxTotalWait           time.Duration
	ClientSafeWindow       time.Duration
	VelocityTracesPerSec   float64
}

// AdaptiveBarrierDecision represents the convergence verdict of the query-aware enrich barrier.
type AdaptiveBarrierDecision struct {
	ShouldRelease bool
	TimedOut      bool
	Reason        string
}

const (
	// BaseAttentionTokenBudget Short-turn high-attention baseline budget (<= 1024 tokens).
	BaseAttentionTokenBudget = 1024
	// MaxWholeTurnTokenBudget Maximum token budget to keep a single medium/long turn intact in one working context.
	MaxWholeTurnTokenBudget = 4096
	// OversizedTurnSliceTarget Target slice token budget when a single extreme turn exceeds MaxWholeTurnTokenBudget.
	OversizedTurnSliceTarget = 2048
	// DefaultSliceOverlapTokens Sentence-boundary overlap tokens when an extreme turn (> 4096 tokens) is sliced.
	DefaultSliceOverlapTokens = 128
	// DefaultSliceMinTailTokens Minimum tail tokens before merging into the preceding slice.
	DefaultSliceMinTailTokens = 256
)

// AdaptiveTurnSliceSpec describes the slice and batch token budgets for a turn of given token count.
type AdaptiveTurnSliceSpec struct {
	ShouldSlice      bool
	SliceTarget      int
	OverlapTokens    int
	MinTailTokens    int
	BatchTokenBudget int
}
