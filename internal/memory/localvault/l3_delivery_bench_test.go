// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkSearchClientSDKDefaultWithNoetics measures the delivery path with
// many matched traces: cue matching, evidence coverage, and the noetic merge.
func BenchmarkSearchClientSDKDefaultWithNoetics(b *testing.B) {
	db, err := sql.Open("sqlite", vaultTestDSN(filepath.Join(b.TempDir(), "l3-delivery-bench.db")))
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := NewVault(db, []byte("l3-delivery-bench-secret"))
	if err != nil {
		b.Fatalf("NewVault: %v", err)
	}
	for _, doc := range []struct{ id, text string }{
		{"u1", "alpha beta gamma"},
		{"u2", "alpha beta delta"},
		{"u3", "alpha beta epsilon"},
	} {
		if err := vault.SaveLocalPlaintext(doc.id, 100, doc.text, nil, IngestResponse{Layer: "episodic"}); err != nil {
			b.Fatalf("SaveLocalPlaintext %s: %v", doc.id, err)
		}
	}
	for i := 0; i < 200; i++ {
		if err := vault.SaveLocalNoetic(LocalNoeticRecord{
			NoeticID:     fmt.Sprintf("nt_bench_%d", i),
			OccurredAt:   int64(200 + i),
			QueryText:    fmt.Sprintf("alpha beta bench%d", i),
			AnswerText:   "ans",
			CitedUnitIDs: []string{"u2", fmt.Sprintf("missing_%d", i)},
		}); err != nil {
			b.Fatalf("SaveLocalNoetic %d: %v", i, err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := vault.SearchClientSDKDefault("alpha beta", 10); err != nil {
			b.Fatalf("SearchClientSDKDefault: %v", err)
		}
	}
}
