// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSystemFramePayload(t *testing.T) {
	original := systemCLIRunner
	t.Cleanup(func() { systemCLIRunner = original })
	var gotArgs []string
	systemCLIRunner = func(_ context.Context, bin string, args []string) ([]byte, error) {
		gotArgs = append([]string{bin}, args...)
		return []byte(`{"ok":true,"online":true,"pid":123,"uptime_sec":42,"heap_alloc_bytes":1024,"heap_sys_bytes":2048,"total_ram_bytes":4096,"mem_limit_bytes":2147483648,"mem_limit_source":"explicit","configured_memory_limit":"2GiB","config_matches":true}`), nil
	}
	payload := systemFramePayload(context.Background(), "/bin/rolling-memory", "/tmp/vault.db")
	if strings.Join(gotArgs, " ") != "/bin/rolling-memory daemon status --db /tmp/vault.db --json" {
		t.Fatalf("unexpected CLI args: %v", gotArgs)
	}
	if payload["online"] != true || payload["pid"] != float64(123) {
		t.Fatalf("online/pid not merged from the report: %#v", payload)
	}
	if payload["mem_limit_source"] != "explicit" || payload["mem_limit_bytes"] != float64(2147483648) {
		t.Fatalf("memory cap fields missing: %#v", payload)
	}
	if sampled, _ := payload["sampled_at_ms"].(int64); sampled <= 0 {
		t.Fatalf("sampled_at_ms missing: %#v", payload)
	}
}

func TestSystemFramePayloadSamplingFailure(t *testing.T) {
	original := systemCLIRunner
	t.Cleanup(func() { systemCLIRunner = original })
	systemCLIRunner = func(context.Context, string, []string) ([]byte, error) {
		return nil, errors.New("daemon socket unavailable")
	}
	payload := systemFramePayload(context.Background(), "/bin/rolling-memory", "/tmp/vault.db")
	if payload["ok"] != false || payload["online"] != false {
		t.Fatalf("failed sample must be reported as offline: %#v", payload)
	}
	if msg, _ := payload["health_error"].(string); !strings.Contains(msg, "daemon socket unavailable") {
		t.Fatalf("health_error not surfaced: %#v", payload)
	}
	if sampled, _ := payload["sampled_at_ms"].(int64); sampled <= 0 {
		t.Fatalf("sampled_at_ms missing on the failure frame: %#v", payload)
	}
}

func TestSystemRoomSpecAndPermissions(t *testing.T) {
	key, events, ok := parseWSRoomSpec(wsRoomSystem)
	if !ok || key.kind != streamKindSystem || len(events) != 1 || events[0] != "system" {
		t.Fatalf("rm/system room spec = %#v %v %v", key, events, ok)
	}
	if _, _, ok := parseWSRoomSpec("rm/system:extra"); ok {
		t.Fatal("rm/system must be an exact room name")
	}
	rules, op := wsRoomPermissionRules(streamKindSystem)
	if op != "OR" || len(rules) != 2 ||
		rules[0] != "plugin/rolling-memory/system/read" || rules[1] != "plugin/rolling-memory/system/daemon" {
		t.Fatalf("system room permissions = %v %s", rules, op)
	}
}

func TestSystemStreamCadence(t *testing.T) {
	hub := newSystemStreamHub(streamKey{kind: streamKindSystem}, t.TempDir())
	if hub == nil || len(hub.producers) != 1 {
		t.Fatalf("unexpected hub shape: %#v", hub)
	}
	producer := hub.producers[0]
	if producer.event != "system" || !producer.alwaysPush || producer.minInterval != systemStreamInterval {
		t.Fatalf("producer cadence = %#v", producer)
	}
	if hub.interval != systemStreamInterval || systemStreamInterval != 5*time.Second {
		t.Fatalf("hub interval = %s, want %s", hub.interval, systemStreamInterval)
	}
}
