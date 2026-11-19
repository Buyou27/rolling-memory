// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package graph_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/rocareer/rolling-memory/internal/graphmodel"
	"github.com/rocareer/rolling-memory/internal/memory/graph"
	"github.com/rocareer/rolling-memory/internal/memory/localvault"
	_ "modernc.org/sqlite"
)

// TestGraphPhraseProjectionDropsOverlappingHanWindows pins the structural rule:
// two adjacent two-character windows of one Han run are one text run projected
// twice, so the phrase projection must not surface them. Retrieval posting and
// word-family labels keep the underlying tokens untouched.
func TestGraphPhraseProjectionDropsOverlappingHanWindows(t *testing.T) {
	db, err := sql.Open("sqlite", "file:graph_phrase_overlap_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	vault, err := localvault.NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	const text = "user 全部同意继续推进实施"
	for i := 0; i < 4; i++ {
		if err := vault.SaveLocalPlaintext(fmt.Sprintf("ep_%d", i), 1700000000+int64(i)*60, text, nil, localvault.IngestResponse{
			Layer: "episodic",
		}); err != nil {
			t.Fatal(err)
		}
	}

	builder := graph.NewBuilder(vault)
	resp, err := builder.View(context.Background(), graphmodel.ViewRequest{Limit: 256})
	if err != nil {
		t.Fatal(err)
	}
	phrases := 0
	for _, node := range resp.Nodes {
		if node.Kind != graphmodel.KindPhrase {
			continue
		}
		phrases++
		if left, right, ok := splitPhraseLabel(node.Label); ok && overlappingHanWindows(left, right) {
			t.Fatalf("overlapping Han windows leaked into the phrase projection: %q", node.Label)
		}
		if len(node.Phrases) == 1 {
			fragment := node.Phrases[0]
			if fragment.LeftDF <= 0 || fragment.RightDF <= 0 {
				t.Fatalf("phrase %q is missing member df: %+v", node.Label, fragment)
			}
			if fragment.Containment <= 0 || fragment.Containment > 1 {
				t.Fatalf("phrase %q containment out of range: %v", node.Label, fragment.Containment)
			}
			if fragment.Lift <= 0 {
				t.Fatalf("phrase %q lift out of range: %v", node.Label, fragment.Lift)
			}
		}
	}
	if phrases == 0 {
		t.Fatalf("expected at least one non-overlapping phrase node, nodes=%d", len(resp.Nodes))
	}
}

func splitPhraseLabel(label string) (string, string, bool) {
	parts := strings.SplitN(label, " · ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func overlappingHanWindows(left, right string) bool {
	l, r := []rune(left), []rune(right)
	if len(l) != 2 || len(r) != 2 {
		return false
	}
	if !unicode.Is(unicode.Han, l[0]) || !unicode.Is(unicode.Han, l[1]) ||
		!unicode.Is(unicode.Han, r[0]) || !unicode.Is(unicode.Han, r[1]) {
		return false
	}
	return l[1] == r[0] || r[1] == l[0]
}
