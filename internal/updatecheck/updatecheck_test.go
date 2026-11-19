// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package updatecheck

import (
	"context"

	"github.com/rocareer/rolling-memory/internal/updatechannel"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckKeepsTagWhenReleaseAssetsAreMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/rolling-memory/releases":
			_, _ = w.Write([]byte(`[
				{"tag_name":"v0.0.6","draft":false,"prerelease":false,"assets":[]},
				{"tag_name":"v0.0.4","draft":false,"prerelease":false,"assets":[{"name":"SHA256SUMS"}]}
			]`))
		case "/release/SHA256SUMS", "/release/VERSION":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	result, err := Check(context.Background(), Options{
		Channel:        updatechannel.Stable,
		ReleaseBase:    server.URL + "/release",
		APIBase:        server.URL,
		Repo:           "acme/rolling-memory",
		CurrentVersion: "0.0.5",
		Timeout:        2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.LatestTag != "0.0.6" {
		t.Fatalf("latest_tag = %q, want 0.0.6", result.LatestTag)
	}
	if !result.UpdateAvailable {
		t.Fatal("update_available = false, want true")
	}
	if result.AssetsAvailable || result.Installable {
		t.Fatalf("assets must be unavailable: %#v", result.State)
	}
	if result.Source != "github-api" {
		t.Fatalf("source = %q, want github-api", result.Source)
	}
}

func TestCheckUsesManifestVersionWhenAPIIsOff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release/SHA256SUMS":
			_, _ = w.Write([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  rolling-memory-darwin-arm64\n"))
		case "/release/VERSION":
			_, _ = w.Write([]byte("0.0.7\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("ROLLING_MEMORY_UPDATE_API_BASE", "off")
	result, err := Check(context.Background(), Options{
		Channel:        updatechannel.Stable,
		ReleaseBase:    server.URL + "/release",
		CurrentVersion: "0.0.5",
		Timeout:        2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.LatestTag != "0.0.7" || !result.UpdateAvailable {
		t.Fatalf("unexpected result: %#v", result.State)
	}
	if !result.AssetsChecked {
		t.Fatal("assets_checked = false, want true")
	}
}
