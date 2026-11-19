// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rocareer/rolling-memory/internal/updatechannel"
)

func TestStableAndBetaChannelSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/rolling-memory/releases":
			_, _ = w.Write([]byte(`[
				{"tag_name":"0.0.6-beta.1","draft":false,"prerelease":true,"assets":[]},
				{"tag_name":"v0.0.5","draft":false,"prerelease":false,"assets":[]}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stable, err := Check(context.Background(), Options{
		Channel:        updatechannel.Stable,
		ReleaseBase:    server.URL + "/release",
		APIBase:        server.URL,
		Repo:           "acme/rolling-memory",
		CurrentVersion: "0.0.5",
		Timeout:        2 * time.Second,
	})
	if err != nil {
		t.Fatalf("stable Check: %v", err)
	}
	if stable.LatestTag != "0.0.5" {
		t.Fatalf("stable latest = %q, want 0.0.5", stable.LatestTag)
	}
	if stable.UpdateAvailable {
		t.Fatalf("stable must not offer a prerelease: %#v", stable.State)
	}

	beta, err := Check(context.Background(), Options{
		Channel:        updatechannel.Beta,
		ReleaseBase:    server.URL + "/release",
		APIBase:        server.URL,
		Repo:           "acme/rolling-memory",
		CurrentVersion: "0.0.5",
		Timeout:        2 * time.Second,
	})
	if err != nil {
		t.Fatalf("beta Check: %v", err)
	}
	if beta.LatestTag != "0.0.6-beta.1" || !beta.UpdateAvailable {
		t.Fatalf("beta result = %#v", beta.State)
	}
}
