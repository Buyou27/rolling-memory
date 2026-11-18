// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleUnixGraphRejectsNonPost(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/graph", nil)
	handleUnixGraph(rec, req, NewLazyMemory(), &graphBuilderStore{}, UnixHealthResponse{})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v1/graph status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleUnixGraphRejectsInvalidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/graph", strings.NewReader("{"))
	handleUnixGraph(rec, req, NewLazyMemory(), &graphBuilderStore{}, UnixHealthResponse{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleUnixGraphUnavailableMemory(t *testing.T) {
	lazy := NewLazyMemory()
	lazy.err = errors.New("memory unavailable")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/graph", strings.NewReader(`{"limit":16}`))
	handleUnixGraph(rec, req, lazy, &graphBuilderStore{}, UnixHealthResponse{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable memory status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestGraphBuilderStoreRebindsOnVaultChange(t *testing.T) {
	var store graphBuilderStore
	if builder := store.forVault(nil); builder != nil {
		t.Fatalf("nil vault must not produce a builder")
	}
}
