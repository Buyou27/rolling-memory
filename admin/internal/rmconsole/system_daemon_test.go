// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNormalizeSystemDaemonAction(t *testing.T) {
	if action, err := normalizeSystemDaemonAction(""); err != nil || action != "status" {
		t.Fatalf("empty action must default to status, got %q err=%v", action, err)
	}
	if action, err := normalizeSystemDaemonAction(" RESTART "); err != nil || action != "restart" {
		t.Fatalf("action must normalize case/space, got %q err=%v", action, err)
	}
	for _, action := range []string{"status", "start", "stop", "restart"} {
		if got, err := normalizeSystemDaemonAction(action); err != nil || got != action {
			t.Fatalf("action %q must pass through, got %q err=%v", action, got, err)
		}
	}
	if _, err := normalizeSystemDaemonAction("kill-all"); err == nil {
		t.Fatalf("unknown action must be rejected")
	}
}

func TestReadConfigMemoryLimit(t *testing.T) {
	original := systemCLIRunner
	t.Cleanup(func() { systemCLIRunner = original })
	systemCLIRunner = func(ctx context.Context, bin string, args []string) ([]byte, error) {
		if strings.Join(args, " ") != "config show" {
			t.Fatalf("unexpected args: %v", args)
		}
		return []byte(`{"daemon":{"memory_limit":"2GiB"},"mode":"local"}`), nil
	}
	value, err := readConfigMemoryLimit(context.Background(), "/bin/rolling-memory")
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if value != "2GiB" {
		t.Fatalf("memory_limit = %q, want 2GiB", value)
	}
}

func TestRunSystemCLIRequiresExplicitBinaryUnderTest(t *testing.T) {
	t.Setenv("ROLLING_MEMORY_BIN", "")
	if _, err := runSystemCLI(context.Background(), os.Args[0], []string{"daemon", "status", "--json"}); err == nil {
		t.Fatalf("test binaries must refuse to run without an explicit ROLLING_MEMORY_BIN")
	}
}
