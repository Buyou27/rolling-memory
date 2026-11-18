// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import (
	"strconv"
	"strings"
)

// ConnectivityDefectKind enumerates the closed set of structural and topological connectivity violations.
type ConnectivityDefectKind string

const (
	DefectMissingAssertionNode ConnectivityDefectKind = "missing_assertion_node"
	DefectOrphanEndpoint       ConnectivityDefectKind = "orphan_endpoint"
	DefectInvalidEndpointKind  ConnectivityDefectKind = "invalid_endpoint_kind"
	DefectPhantomRefLeak       ConnectivityDefectKind = "phantom_ref_leak"
	DefectOrphanParticipant    ConnectivityDefectKind = "orphan_participant"
	DefectOrphanLedgerRef      ConnectivityDefectKind = "orphan_ledger_ref"
	DefectOrphanAlias          ConnectivityDefectKind = "orphan_alias"
	DefectOrphanMetaEdge       ConnectivityDefectKind = "orphan_meta_edge"
	DefectMetaSelfLoop         ConnectivityDefectKind = "meta_self_loop"
	DefectSameAsCycleOrInvalid ConnectivityDefectKind = "same_as_cycle_or_invalid"
	DefectHierarchyCycle       ConnectivityDefectKind = "hierarchy_cycle"
	DefectIsolatedNode         ConnectivityDefectKind = "isolated_node"
)

// ConnectivityDefect describes a single deterministic connectivity or topological violation.
type ConnectivityDefect struct {
	Kind    ConnectivityDefectKind `json:"kind"`
	NodeID  string                 `json:"node_id,omitempty"`
	EdgeID  string                 `json:"edge_id,omitempty"`
	Slot    string                 `json:"slot,omitempty"`
	Target  string                 `json:"target,omitempty"`
	Message string                 `json:"message"`
}

// AssertionExtRef carries optional extended node-reference slots (such as IncarnationID)
// that may accompany an Assertion in multi-incarnation / spacetime-addressed graphs.
type AssertionExtRef struct {
	AssertionNodeID string `json:"assertion_node_id"`
	IncarnationID   string `json:"incarnation_id,omitempty"`
}

// GraphConnectivityInput holds the complete multi-channel graph snapshot for connectivity verification.
type GraphConnectivityInput struct {
	Nodes                   []Node            `json:"nodes"`
	Assertions              []Assertion       `json:"assertions"`
	AssertionExtRefs        []AssertionExtRef `json:"assertion_ext_refs,omitempty"`
	Participants            []Participant     `json:"participants,omitempty"`
	Aliases                 []NodeAlias       `json:"aliases,omitempty"`
	MetaEdges               []MetaEdge        `json:"meta_edges,omitempty"`
	LedgerRows              []RestateRow      `json:"ledger_rows,omitempty"`
	AllowPreRegisteredRoots bool              `json:"allow_pre_registered_roots,omitempty"`
	MaxDefectSamples        int               `json:"max_defect_samples,omitempty"`
}

// GraphConnectivityReport summarizes node-level, edge-level, and component-level connectivity health.
type GraphConnectivityReport struct {
	Clean                      bool                           `json:"clean"`
	TotalNodes                 int                            `json:"total_nodes"`
	LiveNodes                  int                            `json:"live_nodes"`
	MergedNodes                int                            `json:"merged_nodes"`
	RetiredNodes               int                            `json:"retired_nodes"`
	TotalAssertions            int                            `json:"total_assertions"`
	ActiveAssertions           int                            `json:"active_assertions"`
	ConnectedLiveNodes         int                            `json:"connected_live_nodes"`
	AnchoredLiveNodes          int                            `json:"anchored_live_nodes"`
	IsolatedLiveNodes          int                            `json:"isolated_live_nodes"`
	UnanchoredMetaIslandNodes  int                            `json:"unanchored_meta_island_nodes"`
	DanglingMergedNodes        int                            `json:"dangling_merged_nodes"`
	NodeConnectedRate          float64                        `json:"node_connected_rate"`
	NodeAnchoredRate           float64                        `json:"node_anchored_rate"`
	ConnectedComponents        int                            `json:"connected_components"`
	AnchoredComponents         int                            `json:"anchored_components"`
	LargestComponentNodes      int                            `json:"largest_component_nodes"`
	LargestComponentRatio      float64                        `json:"largest_component_ratio"`
	EffectiveEntityCount       int                            `json:"effective_entity_count"`
	MultiEdgeEntityCount       int                            `json:"multi_edge_entity_count"`
	AvgEffectiveEntityDegree   float64                        `json:"avg_effective_entity_degree"`
	MultiEdgeEntityRatio       float64                        `json:"multi_edge_entity_ratio"`
	TotalDefects               int                            `json:"total_defects"`
	DefectCounts               map[ConnectivityDefectKind]int `json:"defect_counts"`
	Defects                    []ConnectivityDefect           `json:"defects,omitempty"`
	CanonicalWinnerByNode      map[string]string              `json:"canonical_winner_by_node,omitempty"`
	AliveCanonicalAndConnected map[string]bool                `json:"alive_canonical_and_connected,omitempty"`
}

func normGraphID(strID string, numID uint64) string {
	s := strings.TrimSpace(strID)
	if s != "" {
		return s
	}
	if numID > 0 {
		return strconv.FormatUint(numID, 10)
	}
	return ""
}

// ExtractInlineNodeRefs extracts explicit numeric node references from a literal string:
//  1. Bracketed node references `[#123]` anywhere in the string;
//  2. Standalone bare `#123` when the entire trimmed string is a single `#<digits>` token (e.g. "#123").
//
// Natural-language ordinals inside phrases (e.g. "ranked #1 in the tournament", "song #2", "C#10", "#ff00aa")
// are intentionally excluded so they are never confused with graph node pointers.
func ExtractInlineNodeRefs(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "#") {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	addRef := func(raw string) {
		numStr := strings.TrimLeft(raw, "0")
		if numStr != "" && !seen[numStr] {
			seen[numStr] = true
			out = append(out, numStr)
		}
	}

	// Case 1: entire trimmed string is "#<digits>"
	if strings.HasPrefix(s, "#") && len(s) > 1 {
		allDigits := true
		for _, r := range s[1:] {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			addRef(s[1:])
			return out
		}
	}

	// Case 2: bracketed "[#<digits>]" anywhere in s
	runes := []rune(s)
	n := len(runes)
	for i := 0; i+3 < n; i++ {
		if runes[i] != '[' || runes[i+1] != '#' {
			continue
		}
		j := i + 2
		for j < n && runes[j] >= '0' && runes[j] <= '9' {
			j++
		}
		if j > i+2 && j < n && runes[j] == ']' {
			addRef(string(runes[i+2 : j]))
			i = j
		}
	}
	return out
}
