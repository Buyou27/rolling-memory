// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
)

func TestLocalModeNeverContactsBrain(t *testing.T) {
	for _, credential := range []string{"explicit", "stored"} {
		t.Run(credential, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected request", http.StatusInternalServerError)
			}))
			defer server.Close()
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			t.Setenv("ROLLING_MEMORY_BRAIN_URL", "")
			t.Setenv("ROLLING_MEMORY_BRAIN_KEY", "")
			memory := newMemory(t)
			if credential == "explicit" {
				t.Setenv("ROLLING_MEMORY_BRAIN_URL", server.URL+"/open/memory/v1")
				t.Setenv("ROLLING_MEMORY_BRAIN_KEY", "test-key")
			} else {
				account := filepath.Join(home, ".config", "rolling-memory", "cloud-account.json")
				if err := os.MkdirAll(filepath.Dir(account), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(account, []byte(`{"base_url":"`+server.URL+`","api_key":"test-account"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			client, err := memory.ThinClient()
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := client.Transport.(*thinclient.LocalVaultTransport); !ok {
				t.Fatalf("transport = %T", client.Transport)
			}
			if _, err := client.Remember(context.Background(), "local-sentinel", "local sentinel content", 1); err != nil {
				t.Fatal(err)
			}
			if got := requests.Load(); got != 0 {
				t.Fatalf("local mode made %d remote requests", got)
			}
		})
	}
}
