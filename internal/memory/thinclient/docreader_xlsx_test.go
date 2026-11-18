// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
)

func TestGroupSheetRowsIntoBlocksRendersHeaderLabelsAndSkipsEmptyRows(t *testing.T) {
	rows := [][]string{
		{"Name", "Role"},
		{"Alice", "Engineer"},
		{},
		{"Bob", "Designer"},
	}
	blocks := groupSheetRowsIntoBlocks("Sheet1", rows)
	if len(blocks) != 1 {
		t.Fatalf("expected one block, got %d", len(blocks))
	}
	got := blocks[0]
	wantLines := []string{
		"[Sheet1 Row 1] Name | Role",
		"[Sheet1 Row 2] Name: Alice | Role: Engineer",
		"[Sheet1 Row 4] Name: Bob | Role: Designer",
	}
	if strings.Join(got.lines, "\n") != strings.Join(wantLines, "\n") {
		t.Fatalf("block lines = %q, want %q", got.lines, wantLines)
	}
	if got.anchor != "Sheet1:r1-4" || got.lineStart != 1 || got.lineEnd != 4 {
		t.Fatalf("anchor/range = %q %d..%d, want Sheet1:r1-4 1..4", got.anchor, got.lineStart, got.lineEnd)
	}
}

func TestGroupSheetRowsIntoBlocksEnforcesExactBudget(t *testing.T) {
	header := []string{"Item", "Note"}
	rows := [][]string{header}
	for i := 0; i < 120; i++ {
		rows = append(rows, []string{fmt.Sprintf("item-%03d", i), "deterministic note for sheet budget testing"})
	}
	blocks := groupSheetRowsIntoBlocks("Sheet1", rows)
	if len(blocks) < 2 {
		t.Fatalf("expected multiple blocks at the 1024-token budget, got %d", len(blocks))
	}
	prevEnd := 0
	for i, block := range blocks {
		if len(block.lines) == 0 {
			t.Fatalf("block %d has no lines", i)
		}
		if block.lineStart != prevEnd+1 {
			t.Fatalf("block %d starts at %d, want %d", i, block.lineStart, prevEnd+1)
		}
		if block.lineEnd < block.lineStart {
			t.Fatalf("block %d range %d..%d is inverted", i, block.lineStart, block.lineEnd)
		}
		body := strings.Join(block.lines, "\n")
		if tokens := tokenzr.Count(body); tokens > TargetChunkTokens && len(block.lines) > 1 {
			t.Fatalf("block %d carries %d tokens over budget %d", i, tokens, TargetChunkTokens)
		}
		if !strings.Contains(block.anchor, fmt.Sprintf("r%d-%d", block.lineStart, block.lineEnd)) {
			t.Fatalf("block %d anchor %q does not match its row range %d..%d", i, block.anchor, block.lineStart, block.lineEnd)
		}
		prevEnd = block.lineEnd
	}
	if prevEnd != len(rows) {
		t.Fatalf("final row range ends at %d, want %d", prevEnd, len(rows))
	}
}

func TestGroupSheetRowsIntoBlocksKeepsSingleOverlongRowWhole(t *testing.T) {
	rows := [][]string{
		{"Note"},
		{strings.Repeat("payload token ", 2000)},
		{"tail"},
	}
	blocks := groupSheetRowsIntoBlocks("Sheet1", rows)
	if len(blocks) < 2 {
		t.Fatalf("expected the overlong row to be isolated, got %d block(s)", len(blocks))
	}
	var overlong rawChunkBlock
	found := false
	for _, block := range blocks {
		for _, line := range block.lines {
			if strings.Contains(line, "payload token") {
				overlong, found = block, true
			}
		}
	}
	if !found {
		t.Fatal("overlong row missing from the rendered blocks")
	}
	if len(overlong.lines) != 1 {
		t.Fatalf("overlong row shares its block with %d other line(s)", len(overlong.lines)-1)
	}
	if tokenzr.Count(overlong.lines[0]) <= TargetChunkTokens {
		t.Fatalf("fixture row is not over budget (%d tokens)", tokenzr.Count(overlong.lines[0]))
	}
}
