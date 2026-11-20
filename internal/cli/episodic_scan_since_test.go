// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseEpisodicScanSince(t *testing.T) {
	rfc := "2026-11-20T17:00:00+08:00"
	want, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := parseEpisodicScanSince(rfc); err != nil || got != want.Unix() {
		t.Fatalf("rfc3339 = %d, %v; want %d", got, err, want.Unix())
	}

	day, err := time.Parse("2006-01-02", "2026-11-20")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := parseEpisodicScanSince("2026-11-20"); err != nil || got != day.Unix() {
		t.Fatalf("date = %d, %v; want %d", got, err, day.Unix())
	}

	if got, err := parseEpisodicScanSince(""); err != nil || got != 0 {
		t.Fatalf("empty = %d, %v; want 0, nil", got, err)
	}

	// A typo must fail instead of silently admitting the whole history.
	if _, err := parseEpisodicScanSince("last tuesday"); err == nil {
		t.Fatal("garbage value must be rejected")
	}
}

func TestEpisodicScanSinceReadsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "[core]\nscope = \"mem:self\"\n\n[sync]\n# lower bound for episodic ingest\nauto_watch = true\nsince = \"2026-11-20T17:00:00+08:00\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := time.Parse(time.RFC3339, "2026-11-20T17:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	got, err := EpisodicScanSince(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != want.Unix() {
		t.Fatalf("since = %d, want %d", got, want.Unix())
	}

	// Absent key keeps the current behaviour: no bound.
	plain := filepath.Join(dir, "plain.toml")
	if err := os.WriteFile(plain, []byte("[sync]\nauto_watch = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := EpisodicScanSince(plain); err != nil || got != 0 {
		t.Fatalf("absent since = %d, %v; want 0, nil", got, err)
	}
}

func TestEpisodicScanSinceEnvOverrideAndFailClosed(t *testing.T) {
	t.Setenv("ROLLING_MEMORY_SYNC_SINCE", "2026-11-20T17:00:00+08:00")
	want, err := time.Parse(time.RFC3339, "2026-11-20T17:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := EpisodicScanSince(filepath.Join(t.TempDir(), "missing.toml")); err != nil || got != want.Unix() {
		t.Fatalf("env override = %d, %v; want %d", got, err, want.Unix())
	}

	t.Setenv("ROLLING_MEMORY_SYNC_SINCE", "not-a-time")
	if _, err := EpisodicScanSince(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
		t.Fatal("invalid bound must fail closed")
	}
}

// Sources older than the bound must not be enumerated at all: skipping turns
// inside the walk would still read the whole historical transcript.
func TestFilterEpisodicScanSourcesSince(t *testing.T) {
	sources := []episodicScanSource{
		{SessionID: "old", LastActivity: 100},
		{SessionID: "new", LastActivity: 300},
		{SessionID: "unknown"},
	}
	kept := filterEpisodicScanSourcesSince(sources, 200)
	if len(kept) != 2 || kept[0].SessionID != "new" || kept[1].SessionID != "unknown" {
		t.Fatalf("kept = %v, want [new unknown]", kept)
	}
	if all := filterEpisodicScanSourcesSince(sources, 0); len(all) != 3 {
		t.Fatalf("unbounded kept %d sources, want 3", len(all))
	}
}

// The bound is in seconds. A millisecond timestamp compared raw looks like the
// far future, which is how an out-of-bound turn slipped into the vault once.
func TestEpisodicOccurredBefore(t *testing.T) {
	const since = 1795104000
	if !episodicOccurredBefore(1794724362, since) {
		t.Fatal("older second timestamp must be out of bound")
	}
	if !episodicOccurredBefore(1794724362000, since) {
		t.Fatal("older millisecond timestamp must be out of bound")
	}
	if episodicOccurredBefore(1795166047, since) {
		t.Fatal("newer timestamp must be in bound")
	}
	if episodicOccurredBefore(1795166047000, since) {
		t.Fatal("newer millisecond timestamp must be in bound")
	}
	if !episodicOccurredBefore(0, since) {
		t.Fatal("unknown time must be out of bound when a bound is set")
	}
	if episodicOccurredBefore(0, 0) {
		t.Fatal("no bound admits everything")
	}
}
