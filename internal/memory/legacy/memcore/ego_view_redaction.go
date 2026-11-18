// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

// CanonicalSelfDesignator is the sole online-writer storage-core designator for the Assistant's own identity (origin="self").
// Explicit persona names are bound via aliases and projected on egress; governed display representatives use append-only merges.
const CanonicalSelfDesignator = "self"

// FallbackAssistantDesignator is the consumer-facing role label used when no unique persona name is known.
const FallbackAssistantDesignator = "assistant"

// EgressViewMode specifies how identity and roster labels are projected at the consumer boundary.
type EgressViewMode string

const (
	// EgressViewNeutral projects the canonical self token to a uniquely identified name or "assistant";
	// original roster labels and verbatim evidence stay untouched (default for eval/harness).
	EgressViewNeutral EgressViewMode = "neutral"
	// EgressViewPersona projects the canonical "self" token to the active persona name or "assistant".
	EgressViewPersona EgressViewMode = "persona"
	// EgressViewRedacted projects "self" to a generic role label ("assistant" or custom) and masks roster entities + aliases into deterministic pseudonyms ("Person_1", "Person_2", ...).
	EgressViewRedacted EgressViewMode = "redacted"
)

// EgressViewPolicy defines the consumer-side projection and de-identification parameters.
type EgressViewPolicy struct {
	Mode            EgressViewMode
	SelfDisplayName string              // Target display name for "self" (e.g. "湖伴" in EgressViewPersona, or "assistant" in EgressViewRedacted)
	SelfHistory     []string            // Historical or active persona names bound to "self" (used for evidence-text de-identification or normalization)
	RosterNodeIDs   map[uint64]bool     // Node IDs with origin="roster" (eligible for pseudonym masking in EgressViewRedacted)
	NodeAliases     map[uint64][]string // All known aliases per node ID (from memory_alias), enabling synchronized masking across structured bands and raw [E#k] slices
}
