// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEpisodicSourceScanRetriesFailedSourceAfterOtherProgress(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "a.jsonl")
	available := filepath.Join(dir, "b.jsonl")
	writeScanFixture(t, available, "available", 1)
	sources := []episodicScanSource{
		{HarnessID: "claude-code", Kind: "jsonl", Path: missing},
		{HarnessID: "claude-code", Kind: "jsonl", Path: available},
	}
	seen := map[string]bool{}
	visit := func(turn candidateEpisodicTurn) error { seen[turn.Text] = true; return nil }
	cursor, _, err := visitEpisodicSources(context.Background(), sources, "mem:test", episodicSourceCursor{}, 10, visit)
	if err == nil || !seen["[user] available 0"] {
		t.Fatalf("first pass err=%v seen=%v", err, seen)
	}
	writeScanFixture(t, missing, "recovered", 1)
	_, _, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 10, visit)
	if err != nil || !seen["[user] recovered 0"] {
		t.Fatalf("recovered source was not retried: err=%v seen=%v cursor=%+v", err, seen, cursor)
	}
}

func TestEpisodicSourceScanRetriesUnreadableSourceAfterPagedProgress(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "a.jsonl")
	available := filepath.Join(dir, "b.jsonl")
	writeScanFixture(t, available, "available", 7)
	sources := []episodicScanSource{
		{HarnessID: "claude-code", Kind: "jsonl", Path: missing},
		{HarnessID: "claude-code", Kind: "jsonl", Path: available},
	}
	seen := map[string]bool{}
	visit := func(turn candidateEpisodicTurn) error { seen[turn.Text] = true; return nil }
	cursor, more, err := visitEpisodicSources(context.Background(), sources, "mem:test", episodicSourceCursor{}, 2, visit)
	if err == nil || !more || !cursor.RetrySources {
		t.Fatalf("first page err=%v more=%v cursor=%+v", err, more, cursor)
	}
	writeScanFixture(t, missing, "recovered", 1)
	for page := 0; page < 8 && !seen["[user] recovered 0"]; page++ {
		cursor, _, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 2, visit)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
	}
	for i := 0; i < 7; i++ {
		if !seen[fmt.Sprintf("[user] available %d", i)] {
			t.Errorf("available item %d was starved", i)
		}
	}
	if !seen["[user] recovered 0"] {
		t.Fatalf("recovered source was skipped after paged progress: cursor=%+v", cursor)
	}
}

func TestEpisodicSourceScanRetriesSourceChangedDuringPage(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jsonl")
	second := filepath.Join(dir, "b.jsonl")
	writeScanFixture(t, first, "original", 2)
	writeScanFixture(t, second, "second", 5)
	sources := []episodicScanSource{
		{HarnessID: "claude-code", Kind: "jsonl", Path: first},
		{HarnessID: "claude-code", Kind: "jsonl", Path: second},
	}
	seen := map[string]bool{}
	changed := false
	visit := func(turn candidateEpisodicTurn) error {
		seen[turn.Text] = true
		if !changed {
			changed = true
			writeScanFixture(t, first, "replacement", 2)
		}
		return nil
	}
	cursor, more, err := visitEpisodicSources(context.Background(), sources, "mem:test", episodicSourceCursor{}, 10, visit)
	if err == nil || !more || (cursor.Source != "" && !cursor.RetrySources) {
		t.Fatalf("changed source err=%v more=%v cursor=%+v", err, more, cursor)
	}
	for page := 0; page < 10 && (!seen["[user] replacement 0"] || !seen["[user] second 4"]); page++ {
		cursor, _, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 2, visit)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
	}
	if !seen["[user] replacement 0"] || !seen["[user] replacement 1"] || !seen["[user] second 4"] {
		t.Fatalf("changed or later source was skipped: seen=%v cursor=%+v", seen, cursor)
	}
}

func TestEpisodicSourceScanRepeatedUnavailableStaysUnknown(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	sources := []episodicScanSource{{HarnessID: "claude-code", Kind: "jsonl", Path: missing}}
	cursor := episodicSourceCursor{}
	for i := 0; i < 3; i++ {
		var more bool
		var err error
		cursor, more, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 2, func(candidateEpisodicTurn) error { return nil })
		if err == nil || !more || !cursor.RetrySources || !cursor.RetryCycle {
			t.Fatalf("pass %d cursor=%+v more=%v err=%v", i, cursor, more, err)
		}
	}
	writeScanFixture(t, missing, "recovered", 1)
	cursor, more, err := visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 2, func(candidateEpisodicTurn) error { return nil })
	if err != nil || more || cursor.RetrySources {
		t.Fatalf("recovered cursor=%+v more=%v err=%v", cursor, more, err)
	}
}

func TestEpisodicSourceScanItemFailureContinuesAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeScanFixture(t, path, "item", 3)
	sources := []episodicScanSource{{HarnessID: "claude-code", Kind: "jsonl", Path: path}}
	bad := true
	seen := map[string]bool{}
	visit := func(turn candidateEpisodicTurn) error {
		seen[turn.Text] = true
		if bad && turn.Text == "[user] item 0" {
			return fmt.Errorf("%w: fixture", errEpisodicSourceItem)
		}
		return nil
	}
	cursor, more, err := visitEpisodicSources(context.Background(), sources, "mem:test", episodicSourceCursor{}, 2, visit)
	if err == nil || !more || !cursor.RetrySources || cursor.ItemErrors != 1 || !seen["[user] item 1"] {
		t.Fatalf("first page cursor=%+v more=%v seen=%v err=%v", cursor, more, seen, err)
	}
	cursor, more, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 2, visit)
	if err != nil || !more || !seen["[user] item 2"] || cursor.Source != "" || cursor.ItemErrors != 1 {
		t.Fatalf("second page cursor=%+v more=%v seen=%v err=%v", cursor, more, seen, err)
	}
	bad = false
	seen = map[string]bool{}
	for page := 0; page < 3; page++ {
		cursor, more, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 2, visit)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	if !seen["[user] item 0"] || !seen["[user] item 2"] || more {
		t.Fatalf("recovered item not retried: cursor=%+v more=%v seen=%v", cursor, more, seen)
	}
}

func TestEpisodicSourceScanSaveFailureStopsBeforeOtherSource(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jsonl")
	second := filepath.Join(dir, "b.jsonl")
	writeScanFixture(t, first, "first", 1)
	writeScanFixture(t, second, "second", 1)
	sources := []episodicScanSource{
		{HarnessID: "claude-code", Kind: "jsonl", Path: first},
		{HarnessID: "claude-code", Kind: "jsonl", Path: second},
	}
	failure := errors.New("system write failure")
	var seen []string
	cursor, more, err := visitEpisodicSources(context.Background(), sources, "mem:test", episodicSourceCursor{}, 10, func(turn candidateEpisodicTurn) error {
		seen = append(seen, turn.Text)
		return failure
	})
	if !errors.Is(err, failure) || !more || len(seen) != 1 || seen[0] != "[user] first 0" {
		t.Fatalf("save failure err=%v more=%v seen=%v", err, more, seen)
	}
	var retried string
	_, _, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 1, func(turn candidateEpisodicTurn) error {
		retried = turn.Text
		return nil
	})
	if err != nil || retried != "[user] first 0" {
		t.Fatalf("failed save not retried: err=%v retried=%q", err, retried)
	}
}

func TestEpisodicSourceScanTenThousandAndLaterSource(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jsonl")
	second := filepath.Join(dir, "b.jsonl")
	writeScanFixture(t, first, "large", 10000)
	writeScanFixture(t, second, "later", 1)
	sources := []episodicScanSource{
		{HarnessID: "claude-code", Kind: "jsonl", Path: first},
		{HarnessID: "claude-code", Kind: "jsonl", Path: second},
	}
	start := time.Now()
	cursor := episodicSourceCursor{}
	seen := 0
	later := false
	pages := 0
	for {
		var more bool
		var err error
		cursor, more, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, reconcileDefaultLimit, func(turn candidateEpisodicTurn) error {
			seen++
			later = later || turn.Text == "[user] later 0"
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if !more {
			break
		}
		if pages > 10001 {
			t.Fatal("scan failed to converge")
		}
	}
	if seen != 10001 || !later {
		t.Fatalf("seen=%d later=%v pages=%d", seen, later, pages)
	}
	t.Logf("10k source: %d pages, elapsed %s", pages, time.Since(start))
}

func TestEpisodicSourceScanGrowingSourceDoesNotStarveLaterSource(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jsonl")
	second := filepath.Join(dir, "b.jsonl")
	writeScanFixture(t, first, "growing", 4)
	writeScanFixture(t, second, "later", 1)
	sources := []episodicScanSource{
		{HarnessID: "claude-code", Kind: "jsonl", Path: first},
		{HarnessID: "claude-code", Kind: "jsonl", Path: second},
	}
	cursor := episodicSourceCursor{}
	seenLater := false
	for page := 0; page < 8 && !seenLater; page++ {
		var err error
		cursor, _, err = visitEpisodicSources(context.Background(), sources, "mem:test", cursor, 2, func(turn candidateEpisodicTurn) error {
			seenLater = seenLater || strings.Contains(turn.Text, "later")
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(first, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		var writeErr error
		for i := 0; i < 2; i++ {
			if _, writeErr = fmt.Fprintf(f, `{"type":"user","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"appended %d %d"}}`+"\n", page, i); writeErr != nil {
				break
			}
		}
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("append err=%v close=%v", writeErr, closeErr)
		}
	}
	if !seenLater {
		t.Fatal("later source starved while earlier source grew")
	}
}
