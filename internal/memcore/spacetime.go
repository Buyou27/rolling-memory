// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import (
	"github.com/rocareer/rolling-memory/internal/text"
)

// PresenceCoordinates is a type alias to text.PresenceCoordinates to maintain
// backward compatibility while we refactor core primitives to the new package.
type PresenceCoordinates = text.PresenceCoordinates

// NormalizeHierarchicalPath normalizes a '/' or '\' separated hierarchical path,
// stripping redundant separators, '.', and folding '..' parent segments without domain-specific word lists.
func NormalizeHierarchicalPath(raw string) string {
	return text.NormalizeHierarchicalPath(raw)
}

// SplitHierarchicalSegments splits a hierarchical path into non-empty segments.
// ---- 时空坐标 / 工作区存在探测原语：唯一正本在 internal/text（薄转发）----

// 时空共振软势场权重（非退化：乘子 >= 1.0，delegates to internal/text）。
const (
	SpacetimeAlphaLocus   = text.SpacetimeAlphaLocus
	SpacetimeAlphaCarrier = text.SpacetimeAlphaCarrier
	SpacetimeBetaInc      = text.SpacetimeBetaInc
)

// SplitHierarchicalSegments splits a hierarchical path into non-empty segments (delegates to internal/text).
func SplitHierarchicalSegments(raw string) []string { return text.SplitHierarchicalSegments(raw) }

// HierarchicalLCPScore computes the language-agnostic hierarchical tree LCP affinity kernel (delegates to internal/text).
func HierarchicalLCPScore(pathA, pathB string) float64 {
	return text.HierarchicalLCPScore(pathA, pathB)
}

// IncarnationGenealogyScore computes the incarnation role hierarchy affinity kernel (delegates to internal/text).
func IncarnationGenealogyScore(incQuery, incAssertion string) float64 {
	return text.IncarnationGenealogyScore(incQuery, incAssertion)
}

// SpacetimeResonanceMultiplier computes the soft potential field multiplier (delegates to internal/text).
func SpacetimeResonanceMultiplier(query PresenceCoordinates, rowLocus, rowCarrier, rowIncarnation string) float64 {
	return text.SpacetimeResonanceMultiplier(query, rowLocus, rowCarrier, rowIncarnation)
}

// DualTrackLocusPrefixes returns dual-track locus prefixes (delegates to internal/text).
func DualTrackLocusPrefixes(rawLocus string) (prefixes []string, leafShortAlias string) {
	return text.DualTrackLocusPrefixes(rawLocus)
}

// IncarnationHierarchyPrefixes returns incarnation hierarchy prefixes (delegates to internal/text).
func IncarnationHierarchyPrefixes(rawIncarnation string) []string {
	return text.IncarnationHierarchyPrefixes(rawIncarnation)
}

// ResolveEffectiveLocus resolves the effective locus (delegates to internal/text).
func ResolveEffectiveLocus(explicitEventLocus, presenceLocus string, maxRunes int) string {
	return text.ResolveEffectiveLocus(explicitEventLocus, presenceLocus, maxRunes)
}

// DetectGitWorkspaceLocus detects the git workspace locus (delegates to internal/text).
func DetectGitWorkspaceLocus(cwd string) string { return text.DetectGitWorkspaceLocus(cwd) }

// DetectGitWorkspaceUser detects the git workspace user (delegates to internal/text).
func DetectGitWorkspaceUser(cwd string) string { return text.DetectGitWorkspaceUser(cwd) }

// CanonicalAssistantScopeVariants returns canonical assistant scope variants (delegates to internal/text).
func CanonicalAssistantScopeVariants(scope, assistantID string) []string {
	return text.CanonicalAssistantScopeVariants(scope, assistantID)
}
