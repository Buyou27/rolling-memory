// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import (
	"strings"
)

// Prospective Loop Axis (Axiom 9 — Orthogonal Loop State & Target-Time Zeigarnik Field):
//   - loop_state ∈ {'', 'open', 'fulfilled', 'waived', 'broken'} tracks prospective open loops.
//   - Write-side state transitions strictly follow explicit closure (closes_prior=true):
//     implicit cross-predicate evolution (e.g., "plan to go to Beijing" -> "arrived in Beijing")
//     is naturally represented by the [SO] same-object chronological chain (chain head = current state)
//     without guessing predicate entailment in code or mutating prior rows.
//   - Read-side Zeigarnik tension boost applies ONLY when an active open loop carries an explicit
//     resolved target/due timestamp (t_event > 0) within the bounded 14-day horizon, preventing
//     undated performative greetings/chitchat from outranking factual assertions.

const (
	LoopStateNone      = ""
	LoopStateOpen      = "open"
	LoopStateFulfilled = "fulfilled"
	LoopStateWaived    = "waived"
	LoopStateBroken    = "broken"
)

var validLoopStates = map[string]bool{
	LoopStateOpen:      true,
	LoopStateFulfilled: true,
	LoopStateWaived:    true,
	LoopStateBroken:    true,
}

// InitialLoopState determines the initial loop_state on assertion write:
//   - If an explicit valid loopState is supplied, it is returned normalized;
//   - Otherwise, if warrant == "performative", it defaults to "open";
//   - Otherwise, it returns "" (retrospective fact).
func InitialLoopState(warrant, explicitLoopState string) string {
	norm := strings.ToLower(strings.TrimSpace(explicitLoopState))
	if validLoopStates[norm] {
		return norm
	}
	if strings.EqualFold(strings.TrimSpace(warrant), "performative") {
		return LoopStateOpen
	}
	return LoopStateNone
}
