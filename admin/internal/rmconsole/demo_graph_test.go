// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSyntheticDemoRoutesIgnorePrivateSources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_EVAL_DATA", filepath.Join(home, "evaldata"))
	t.Setenv("ROLLING_MEMORY_DB", filepath.Join(home, "vault.sqlite"))

	router := gin.New()
	mountPublic(router.Group(""))
	request := func(path, body string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/plugin/rolling-memory/demo/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		var envelope struct {
			Code int            `json:"code"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || w.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("%s: status=%d body=%s err=%v", path, w.Code, w.Body.String(), err)
		}
		if envelope.Data["synthetic_demo"] != true || strings.Contains(w.Body.String(), "private-marker") {
			t.Fatalf("%s returned non-demo or private data: %s", path, w.Body.String())
		}
		return envelope.Data
	}
	graph := request("graph", `{"limit":256}`)
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/plugin/rolling-memory/demo/") && route.Method != http.MethodPost {
			t.Fatalf("unexpected demo method: %+v", route)
		}
	}
	expand := request("expand", `{"seed_keys":["unit:demo-box"],"depth":2}`)
	recall := request("recall", `{"question_id":"demo-box"}`)
	limited := request("graph", `{"limit":1}`)
	if len(limited["nodes"].([]any)) != 1 || len(limited["edges"].([]any)) != 0 || limited["truncated"].(map[string]any)["nodes"] != true {
		t.Fatalf("demo limit not respected: %v", limited)
	}
	if len(graph["nodes"].([]any)) == 0 || len(expand["nodes"].([]any)) == 0 || recall["answer"] != "In a blue box beside the window." {
		t.Fatalf("missing fictional examples: graph=%v expand=%v recall=%v", graph, expand, recall)
	}
	if _, ok := recall["score"]; ok {
		t.Fatal("demo response must not claim a score")
	}
	for _, item := range recall["hits"].([]any) {
		if _, ok := item.(map[string]any)["score"]; ok {
			t.Fatal("demo links must not claim retrieval scores")
		}
	}

	evalDir := filepath.Join(home, ".local", "state", "rolling-memory", "eval")
	if err := os.MkdirAll(evalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"eval-locomo.json":            `{"private-id":{"raw_text":"vault-private-marker"}}`,
		"locomo-static-snapshot.json": `{"nodes":[{"node_key":"private-marker","span":"snapshot-private-marker"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(evalDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	questionsDir := filepath.Join(home, "evaldata", "tiers")
	if err := os.MkdirAll(questionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(questionsDir, "questions-100.json"), []byte(`[{"question":"benchmark-private-marker"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, "vault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE thin_vault_docs (doc_id TEXT, raw_text TEXT); INSERT INTO thin_vault_docs VALUES ('private-id', 'sqlite-private-marker')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	locomoCorpusMu.Lock()
	oldCache := locomoCorpusCache
	locomoCorpusCache = &locomoCachedCorpus{loadedAt: time.Now(), nodesByKey: map[string]*GraphNodeItem{
		"unit:private-marker": {NodeKey: "unit:private-marker", Span: "cache-private-marker"},
	}}
	locomoCorpusMu.Unlock()
	t.Cleanup(func() {
		locomoCorpusMu.Lock()
		locomoCorpusCache = oldCache
		locomoCorpusMu.Unlock()
	})

	for _, tc := range []struct {
		path, body string
		want       map[string]any
	}{
		{"graph", `{"limit":256}`, graph},
		{"expand", `{"seed_keys":["unit:demo-box"],"depth":2}`, expand},
		{"recall", `{"question_id":"demo-box"}`, recall},
	} {
		if got := request(tc.path, tc.body); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s changed after private fixtures were introduced", tc.path)
		}
	}
	unknown := request("recall", `{"question_id":"private-marker","q":"private-marker"}`)
	if unknown["answer"] != "" || len(unknown["hits"].([]any)) != 0 || unknown["q"] != "" {
		t.Fatalf("unknown question must not echo input or invent an answer: %v", unknown)
	}
	privateExpand := request("expand", `{"seed_keys":["unit:private-marker"],"depth":3}`)
	if len(privateExpand["nodes"].([]any)) != 0 || len(privateExpand["edges"].([]any)) != 0 {
		t.Fatalf("unknown seed must not expose private data: %v", privateExpand)
	}
}

func TestSyntheticDemoExpandLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	mountPublic(router.Group(""))
	request := func(body string) demoGraphResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/plugin/rolling-memory/demo/expand", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		var envelope struct {
			Code int               `json:"code"`
			Data demoGraphResponse `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || w.Code != http.StatusOK || envelope.Code != 0 {
			t.Fatalf("status=%d body=%s err=%v", w.Code, w.Body.String(), err)
		}
		return envelope.Data
	}
	full := request(`{"seed_keys":["unit:demo-box"],"depth":2}`)
	if !full.SyntheticDemo || len(full.Nodes) <= 1 || full.Truncated.Nodes || full.Truncated.Edges {
		t.Fatalf("complete neighborhood incorrectly truncated: %+v", full)
	}
	limited := request(`{"seed_keys":["unit:demo-box"],"depth":2,"limit":1}`)
	if len(limited.Nodes) != 1 || !limited.Truncated.Nodes || limited.Truncated.Edges || limited.Stats.NodeShown != 1 || limited.Stats.EdgeShown != len(limited.Edges) {
		t.Fatalf("limited neighborhood metadata mismatch: %+v", limited)
	}
	exact := request(`{"seed_keys":["unit:demo-box"],"depth":2,"limit":` + strconv.Itoa(len(full.Nodes)) + `}`)
	if exact.Truncated.Nodes || !reflect.DeepEqual(exact.Nodes, full.Nodes) || !reflect.DeepEqual(exact.Edges, full.Edges) {
		t.Fatalf("exact limit must preserve complete neighborhood: %+v", exact)
	}
	empty := request(`{"seed_keys":["unknown"],"depth":3,"limit":1}`)
	if len(empty.Nodes) != 0 || len(empty.Edges) != 0 || empty.Truncated.Nodes || empty.Truncated.Edges {
		t.Fatalf("unknown seed must produce a complete empty neighborhood: %+v", empty)
	}
	many := make([]string, 20000)
	for i := range many {
		many[i] = "unknown-" + strconv.Itoa(i)
	}
	many = append(many, "unit:demo-box", "unit:demo-box")
	body, err := json.Marshal(GraphExpandRequest{SeedKeys: many, Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := request(string(body)); !reflect.DeepEqual(got.Nodes, full.Nodes) || !reflect.DeepEqual(got.Edges, full.Edges) || got.Truncated.Nodes {
		t.Fatalf("unknown or duplicate seeds changed neighborhood: %+v", got)
	}
}
