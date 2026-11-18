// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package graph_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/graphmodel"
	"github.com/rocareer/rolling-memory/internal/memory/graph"
	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
	_ "modernc.org/sqlite"
)

// BenchmarkGraphViewCold measures one full snapshot + view build over a
// synthetic 2k-unit vault. Run with `-benchtime=1x` for a cold reading.
func BenchmarkGraphViewCold(b *testing.B) {
	db, err := sql.Open("sqlite", "file:graph_bench?mode=memory&cache=shared")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	vault, err := thinclient.NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		b.Fatal(err)
	}
	const docs = 2000
	vocab := []string{"alpha", "project", "graph", "memory", "cluster", "phrase", "unit", "citation", "evidence", "time"}
	for i := 0; i < docs; i++ {
		parts := make([]string, 0, 12)
		for j := 0; j < 8; j++ {
			parts = append(parts, vocab[(i+j)%len(vocab)])
		}
		text := fmt.Sprintf("Unit %d %s.", i, strings.Join(parts, " "))
		layer := "episodic"
		if i%17 == 0 {
			layer = "noetic"
		}
		if err := vault.SaveLocalPlaintext(fmt.Sprintf("ep_%05d", i), 1700000000+int64(i)*60, text, nil, thinclient.IngestResponse{Layer: layer}); err != nil {
			b.Fatal(err)
		}
	}
	builder := graph.NewBuilder(vault)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder.Invalidate()
		if _, err := builder.View(context.Background(), graphmodel.ViewRequest{Limit: 1024}); err != nil {
			b.Fatal(err)
		}
	}
}
