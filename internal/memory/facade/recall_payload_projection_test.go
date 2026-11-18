// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade

import (
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/ids"
	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// The MCP probe receipt is encoded with the Python-compatible dumper, which
// rejects struct values. Delivery meta must therefore carry plain JSON shapes.
func TestRecallMetaPayloadIsPyJSONEncodable(t *testing.T) {
	meta := map[string]any{
		"void_certificates": voidCertificatePayload([]plain.VoidCertificate{{
			Kind:  plain.VoidKindOOVProper,
			Terms: []string{"aspen"},
			DFs:   []int{0},
		}}),
		"coverage": coverageReportPayload(plain.CoverageReport{
			FullMass:     14.5,
			OOVMass:      2.5,
			CoveredMass:  9.0,
			Completeness: 0.6206896551724138,
			ActiveTerms:  3,
		}),
	}
	text, err := ids.PyJSONDumpIndent(meta, false, true, 2)
	if err != nil {
		t.Fatalf("probe payload must stay encodable: %v", err)
	}
	for _, want := range []string{
		`"kind": "oov_proper"`,
		`"terms": [`,
		`"aspen"`,
		`"full_mass": 14.5`,
		`"active_terms": 3`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q:\n%s", want, text)
		}
	}
}

func TestVoidCertificatePayloadKeepsNilAsNull(t *testing.T) {
	if got := voidCertificatePayload(nil); got != nil {
		t.Fatalf("nil certificates must stay nil so JSON renders null, got %#v", got)
	}
}
