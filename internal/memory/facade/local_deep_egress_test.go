// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
)

func TestLocalLegacyDeepEnvironmentDoesNotContactSentinel(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_MODE", "local")
	t.Setenv("ROLLING_MEMORY_LLM_PROVIDER", "openai")
	t.Setenv("ROLLING_MEMORY_LLM_BASE_URL", server.URL)
	t.Setenv("ROLLING_MEMORY_LLM_API_KEY_ENV", "ROLLING_MEMORY_TEST_LLM_KEY")
	t.Setenv("ROLLING_MEMORY_TEST_LLM_KEY", "test-key")
	t.Setenv("ROLLING_MEMORY_BRAIN_URL", server.URL+"/open/memory/v1")
	t.Setenv("ROLLING_MEMORY_BRAIN_KEY", "test-key")
	configPath := filepath.Join(home, "config.toml")
	dbPath := filepath.Join(home, "memory.db")
	scope := "mem:test"
	cfg, err := config.Load(config.LoadOptions{Path: &configPath, DB: &dbPath, Scope: &scope})
	if err != nil {
		t.Fatal(err)
	}
	memory, err := facade.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	// Fresh production databases bootstrap thin; this test exercises the
	// legacy assertion-graph deep path, so apply its schema.
	if err := db.EnsureLegacyGraphSchema(memory.Store.DB()); err != nil {
		t.Fatal(err)
	}
	rememberOne(t, memory, "local sentinel", "local", "contains", map[string]any{"label": "sentinel"})
	if !memory.Store.HasLegacyGraph() {
		t.Fatal("fixture did not exercise the legacy graph")
	}
	result, err := memory.Recall(facade.RecallRequest{Cue: "local sentinel", Deep: true})
	if err != nil {
		t.Fatal(err)
	}
	deep, ok := result.Meta["deep"].(map[string]any)
	if !ok || deep["requested"] != true || deep["used"] != false {
		t.Fatalf("deep metadata = %#v", result.Meta["deep"])
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("local legacy deep sent %d requests to sentinel", got)
	}
}
