// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

// WithExternalCoCitations returns a derived Engine view whose co-citation CSR
// also materializes caller-declared citation records whose citing unit is not
// an index node. A client-side recollection trace cites evidence that is
// already in the corpus; the trace itself is not a document, but the pairing it
// asserts is a declaration exactly like DocMeta.CitedIDs, so the kernel can
// materialize the second-order co-citation without ingesting the trace.
//
// The receiver is never mutated. The returned engine shares the same index and
// every immutable cluster field, and owns only a cloned state with a rebuilt
// citation/co-citation CSR. The caller keeps ownership of the base engine: the
// derived view must not be closed independently because it shares the base
// mmap lifetime.
//
// An engine carrying a delta overlay returns itself unchanged: the delta child
// engine owns a separate cluster state, and silently applying base-only edges
// would make the two views disagree.
func (e *Engine) WithExternalCoCitations(citationSets [][]string) *Engine {
	if e == nil || e.Index == nil {
		return e
	}
	if e.HasDelta() {
		return e
	}
	base := e.ensureClusterState()
	if base == nil {
		return e
	}
	if len(citationSets) == 0 && len(base.externalCitationSets) == 0 {
		return e
	}
	derived := *e
	cloned := *base
	cloned.externalCitationSets = cloneCitationSets(citationSets)
	cloned.buildCitationCSR(e.Index)
	derived.clusterState = &cloned
	// The incremental basis belongs to the previous state; a clone must not
	// pretend to extend it.
	derived.prevClusterState = nil
	derived.prevDrops = nil
	derived.ResetScratch()
	return &derived
}

// cloneCitationSets deep-copies the caller's declared records so later mutation
// of the input slices cannot change a cached derived engine.
func cloneCitationSets(sets [][]string) [][]string {
	if len(sets) == 0 {
		return nil
	}
	out := make([][]string, 0, len(sets))
	for _, set := range sets {
		if len(set) == 0 {
			continue
		}
		out = append(out, append([]string(nil), set...))
	}
	return out
}
