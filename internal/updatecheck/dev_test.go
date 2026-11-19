// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/updatechannel"
)

func TestCheckDevChannelTracksMainCommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/rolling-memory/commits/main" {
			_, _ = w.Write([]byte(`{"sha":"abcdef1234567890","commit":{"message":"head","author":{"date":"2026-11-19T00:00:00Z"}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	installDir := filepath.Join(data, "rolling-memory")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "install.json"), []byte(`{"method":"source","commit":"oldcommit"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Check(context.Background(), Options{
		Channel:        updatechannel.Dev,
		APIBase:        server.URL,
		Repo:           "acme/rolling-memory",
		CurrentVersion: "0.0.6",
		Timeout:        2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Channel != updatechannel.Dev || result.LatestCommitShort != "abcdef123456" {
		t.Fatalf("unexpected dev result: %#v", result.State)
	}
	if !result.UpdateAvailable || result.Installable {
		t.Fatalf("dev update flags = %#v", result.State)
	}

	if err := os.WriteFile(filepath.Join(installDir, "install.json"), []byte(`{"method":"source","commit":"abcdef1234567890"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := Check(context.Background(), Options{
		Channel:        updatechannel.Dev,
		APIBase:        server.URL,
		Repo:           "acme/rolling-memory",
		CurrentVersion: "0.0.6",
		Timeout:        2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if current.UpdateAvailable {
		t.Fatalf("matching main commit must be up to date: %#v", current.State)
	}
}

func TestDevWithoutSourceCommitDoesNotHitTheNetwork(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	result, err := Check(context.Background(), Options{
		Channel:        updatechannel.Dev,
		APIBase:        "http://127.0.0.1:1",
		Repo:           "acme/rolling-memory",
		CurrentVersion: "0.0.6",
		Timeout:        time.Second,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.NeedsSourceBaseline || result.UpdateAvailable {
		t.Fatalf("expected baseline-only state: %#v", result.State)
	}
	if result.Source != "baseline-required" {
		t.Fatalf("source = %q, want baseline-required", result.Source)
	}
}
