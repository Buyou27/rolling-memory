// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/rocareer/rolling-memory/internal/text"
)

// NormalizeGap normalizes an unbound slot reason string.
func NormalizeGap(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// IsRolePlaceholder reports whether s is a generic dialog transcript role placeholder (delegates to internal/text).
func IsRolePlaceholder(s string) bool { return text.IsRolePlaceholder(s) }

// AliasSpec is one alias binding request on remember.
type AliasSpec struct {
	Of    string `json:"of"`
	Alias string `json:"alias"`
	Basis string `json:"basis"`
}

// AssertionDigestSHA256 computes a deterministic SHA-256 hex prefix over string-keyed slots.
func AssertionDigestSHA256(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// ConnectivityMetrics quantifies graph connectivity richness and entity/predicate reuse.
type ConnectivityMetrics struct {
	TotalAssertions      int     `json:"total_assertions"`
	ObjectNodeCount      int     `json:"object_node_count"`
	ObjectNodeRatio      float64 `json:"object_node_ratio"`
	EntityNodeCount      int     `json:"entity_node_count"`
	MultiEdgeEntityCount int     `json:"multi_edge_entity_count"`
	AvgEntityDegree      float64 `json:"avg_entity_degree"`
	MultiEdgeEntityRatio float64 `json:"multi_edge_entity_ratio"`
	RelationNodeCount    int     `json:"relation_node_count"`
	AvgRelationReuse     float64 `json:"avg_relation_reuse"`
	PhraseNodeSlots      int     `json:"phrase_node_slots"`
	ClauseReferentSlots  int     `json:"clause_referent_slots"`
}

// ComputeConnectivityMetrics computes deterministic graph connectivity and reuse metrics
// over a set of active assertions and their endpoint labels/kinds.
func ComputeConnectivityMetrics(assertions []Assertion, nodeKinds, nodeLabels map[string]string) ConnectivityMetrics {
	var m ConnectivityMetrics
	m.TotalAssertions = len(assertions)
	if m.TotalAssertions == 0 {
		return m
	}
	entityDeg := make(map[string]int, len(assertions))
	relSet := make(map[string]bool, 32)
	for _, a := range assertions {
		sID := normGraphID(a.SubjectID, a.NumSubjectID)
		rID := normGraphID(a.RelID, a.NumRelID)
		oID := normGraphID(a.ObjectID, a.NumObjectID)
		if sID != "" {
			entityDeg[sID]++
		}
		if oID != "" {
			m.ObjectNodeCount++
			entityDeg[oID]++
		} else if inlineRefs := ExtractInlineNodeRefs(a.ObjectLiteral); len(inlineRefs) > 0 {
			m.ObjectNodeCount++
			for _, refID := range inlineRefs {
				entityDeg[refID]++
			}
		}
		if rID != "" {
			relSet[rID] = true
		}
		sKind := nodeKinds[sID]
		oKind := nodeKinds[oID]
		sToks := CountTokens(nodeLabels[sID])
		oToks := CountTokens(nodeLabels[oID])
		if (sID != "" && (sKind == "" || sKind == "referent") && sToks >= TokenTier4) ||
			(oID != "" && (oKind == "" || oKind == "referent") && oToks >= TokenTier4) {
			m.PhraseNodeSlots++
		}
		if (sID != "" && (sKind == "" || sKind == "referent") && sToks >= 10) ||
			(oID != "" && (oKind == "" || oKind == "referent") && oToks >= 10) {
			m.ClauseReferentSlots++
		}
	}
	m.ObjectNodeRatio = float64(m.ObjectNodeCount) / float64(m.TotalAssertions)
	m.EntityNodeCount = len(entityDeg)
	if m.EntityNodeCount > 0 {
		totalDeg := 0
		multiCnt := 0
		for _, d := range entityDeg {
			totalDeg += d
			if d >= 2 {
				multiCnt++
			}
		}
		m.MultiEdgeEntityCount = multiCnt
		m.AvgEntityDegree = float64(totalDeg) / float64(m.EntityNodeCount)
		m.MultiEdgeEntityRatio = float64(multiCnt) / float64(m.EntityNodeCount)
	}
	m.RelationNodeCount = len(relSet)
	if m.RelationNodeCount > 0 {
		m.AvgRelationReuse = float64(m.TotalAssertions) / float64(m.RelationNodeCount)
	}
	return m
}

// MinEvidenceQuorumRows 最小证据量闸（架构契约）：
// max(断言数, 源轮数) 低于该值的微样本域禁评 S 级——满分也只能封顶 A（样本不足），
// 杜绝 n=1 域冒充「Theoretically Perfect」（读侧配额口径沿用）。
const MinEvidenceQuorumRows = 32
