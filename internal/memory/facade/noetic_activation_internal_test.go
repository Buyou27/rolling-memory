// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade

import (
	"reflect"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/localvault"
)

// Only a handle that actually took part in the delivery is reinforced: a
// delivered handle band, or a handle whose cited evidence survived the caps.
// A matched-but-unused recollection and the just-crystallized row never count.
func TestDeliveredNoeticActivationsCountsOnlyDeliveredHandles(t *testing.T) {
	records := []localvault.LocalNoeticRecord{
		{NoeticID: "nt_handle_only", TraceKey: "trace-handle"},
		{NoeticID: "nt_cited", TraceKey: "trace-cited", CitedUnitIDs: []string{"doc-1"}},
		{NoeticID: "nt_unused", TraceKey: "trace-unused", CitedUnitIDs: []string{"doc-404"}},
		{NoeticID: "nt_just_created", TraceKey: "trace-new", CitedUnitIDs: []string{"doc-1"}},
		{NoeticID: "nt_legacy", CitedUnitIDs: []string{"doc-1"}},
	}
	bands := []map[string]any{
		{"kind": "cell_band", "nodes": []map[string]any{{"id": "doc-1"}}},
		{"kind": "noetic_band", "nodes": []map[string]any{{"id": "nt_handle_only"}}},
	}
	got := deliveredNoeticActivations(records, bands, "trace-new")
	want := []string{"nt_handle_only", "nt_cited", "nt_legacy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activated handles = %v, want %v", got, want)
	}
}
