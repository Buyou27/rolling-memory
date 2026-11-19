package rollingmemory

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestConsoleGuardActualMountAvailability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ROLLING_MEMORY_SCOPE", "")
	t.Setenv("ROLLING_MEMORY_DB", filepath.Join(home, "missing.sqlite"))
	router := gin.New()
	mount(router.Group("/admin/plugin/rolling-memory"))

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/ai/config", http.StatusOK},
		{"/ai/save", http.StatusOK},
		{"/background/select", http.StatusOK},
		{"/status", http.StatusForbidden},
		{"/eval/overview", http.StatusOK},
	} {
		t.Setenv("ROLLING_MEMORY_MODE", "local")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/plugin/rolling-memory"+tc.path, strings.NewReader(`{}`)))
		if tc.want == http.StatusForbidden {
			if w.Code != tc.want || !strings.Contains(w.Body.String(), "local_plain vault scope verification failed") {
				t.Errorf("%s: status=%d body=%s", tc.path, w.Code, w.Body.String())
			}
		} else if strings.Contains(w.Body.String(), "local_plain vault scope verification failed") {
			t.Errorf("%s unexpectedly hit vault guard", tc.path)
		}
	}
}

func TestConsoleVaultScopeGuardAdmitsBoundLegacyScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ROLLING_MEMORY_SCOPE", "")
	t.Setenv("ROLLING_MEMORY_MODE", "local")

	cfgDir := filepath.Join(home, ".config", "rolling-memory")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[core]\nscope = \"mem:self\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(home, "legacy.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE rm_meta (k TEXT PRIMARY KEY, v); INSERT INTO rm_meta VALUES ('local_plain_scope', 'mem:self')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	t.Setenv("ROLLING_MEMORY_DB", dbPath)

	if err := checkConsoleVaultScope(home); err != nil {
		t.Fatalf("bound legacy scope should pass: %v", err)
	}
	if got := resolveBoundVaultScope(dbPath, UserScope); got != LegacyUserScope {
		t.Fatalf("resolveBoundVaultScope(dbPath, %q) = %q, want %q", UserScope, got, LegacyUserScope)
	}

	router := gin.New()
	group := router.Group("/admin/plugin/rolling-memory")
	group.Use(consoleVaultScopeGuard())
	group.POST("/dashboard", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	for _, body := range []string{`{}`, `{"scope":"default"}`, `{"scope":"mem:self"}`} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/plugin/rolling-memory/dashboard", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("request %s should be admitted on bound legacy vault: status=%d body=%s", body, w.Code, w.Body.String())
		}
	}

	// A vault bound to a different scope than the configured one stays rejected.
	db2, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db2.Exec(`UPDATE rm_meta SET v = 'default' WHERE k = 'local_plain_scope'`); err != nil {
		t.Fatal(err)
	}
	db2.Close()
	if err := checkConsoleVaultScope(home); err == nil {
		t.Fatal("binding/config mismatch must stay rejected")
	}
}

func TestPublicLocomoRoutesRejectPrivateData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_MODE", "local")
	t.Setenv("ROLLING_MEMORY_EVAL_DATA", filepath.Join(home, "empty-eval"))
	dbPath := filepath.Join(home, "user.sqlite")
	t.Setenv("ROLLING_MEMORY_DB", dbPath)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE thin_vault_docs (doc_id TEXT, raw_text TEXT); INSERT INTO thin_vault_docs VALUES ('private-id', 'vault-secret-marker')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	evalDir := filepath.Join(home, ".local", "state", "rolling-memory", "eval")
	if err := os.MkdirAll(evalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	vault := `{"private-id":{"doc_id":"private-id","raw_text":"eval-secret-marker","address_uri":"cell#1"}}`
	if err := os.WriteFile(filepath.Join(evalDir, "eval-locomo.json"), []byte(vault), 0o600); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd); InvalidateLocomoPublicCorpusCache() })
	InvalidateLocomoPublicCorpusCache()
	router := gin.New()
	mountPublic(router.Group(""))
	checkRoutes := func() {
		t.Helper()
		for _, tc := range []struct{ path, body string }{
			{"/graph", `{"limit":10}`},
			{"/expand", `{"seed_keys":["unit:private-id"],"depth":1}`},
			{"/recall", `{"q":"secret","top_k":1}`},
		} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/plugin/rolling-memory/locomo"+tc.path, strings.NewReader(tc.body)))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"code":1`) || !strings.Contains(w.Body.String(), "LoCoMo public sample unavailable") || strings.Contains(w.Body.String(), "secret-marker") {
				t.Fatalf("%s exposed private data or failed to reject: status=%d body=%s", tc.path, w.Code, w.Body.String())
			}
		}
	}
	checkRoutes()

	snapshot := `{"units":{"private-id":{"doc_id":"private-id","raw_text":"snapshot-secret-marker"}},"nodes":[{"node_key":"unit:private-id","kind":"unit","layer":"episodic","span":"snapshot-secret-marker"}]}`
	if err := os.WriteFile(filepath.Join(evalDir, "locomo-static-snapshot.json"), []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLocomoCorpus(); err != nil {
		t.Fatal(err)
	}
	checkRoutes()
}

func TestLocomoExternalSnapshotsAndCacheInvalidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_EVAL_DATA", filepath.Join(home, "empty-eval"))
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd); InvalidateLocomoPublicCorpusCache() })
	InvalidateLocomoPublicCorpusCache()

	fixture := func(marker string) []byte {
		t.Helper()
		raw, err := json.Marshal(LocomoDataDump{
			ActiveScope: "synthetic-scope",
			Units:       map[string]locomoVaultEntry{"fixture": {DocID: "fixture", RawText: marker}},
			Nodes:       []GraphNodeItem{{ID: 1, NodeKey: "unit:fixture", Kind: "unit", Layer: "episodic", Summary: marker, Span: marker}},
			AtomTotal:   1,
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	write := func(path string, raw []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gzipFixture := func(raw []byte) []byte {
		t.Helper()
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	load := func(want string) {
		t.Helper()
		corpus, err := loadLocomoCorpus()
		if err != nil {
			t.Fatal(err)
		}
		if got := corpus.units["fixture"].RawText; got != want {
			t.Fatalf("snapshot content = %q, want %q", got, want)
		}
	}

	if _, err := loadLocomoCorpus(); err == nil || !strings.Contains(err.Error(), "read eval-locomo.json") {
		t.Fatalf("missing snapshot and vault must return the existing error, got %v", err)
	}
	statePath := filepath.Join(home, ".local", "state", "rolling-memory", "eval", "locomo-static-snapshot.json")
	repoJSON := filepath.Join(home, "Rolling-Memory", "admin", "internal", "rmconsole", "testdata", "locomo_home_snapshot.json")
	repoGzip := repoJSON + ".gz"
	write(repoGzip, gzipFixture(fixture("gzip-marker")))
	load("gzip-marker")
	if _, err := loadLocomoCorpusWithSnapshots(false); err == nil || !strings.Contains(err.Error(), "read eval-locomo.json") {
		t.Fatalf("snapshot-free load must not reuse the cached snapshot: %v", err)
	}
	load("gzip-marker")

	write(statePath, fixture("state-json-marker"))
	load("state-json-marker")
	write(repoJSON, fixture("repository-json-marker"))
	load("repository-json-marker")
	write(repoJSON, fixture("updated-repository-json-marker"))
	load("updated-repository-json-marker")
	if err := os.Remove(repoJSON); err != nil {
		t.Fatal(err)
	}
	load("state-json-marker")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	load("gzip-marker")
	InvalidateLocomoPublicCorpusCache()
	load("gzip-marker")
	write(repoGzip, gzipFixture(fixture("updated-gzip-marker")))
	load("updated-gzip-marker")
	write(repoGzip, []byte("invalid gzip"))
	if _, err := loadLocomoCorpus(); err == nil || !strings.Contains(err.Error(), "read eval-locomo.json") {
		t.Fatalf("invalid snapshot and missing vault must return the existing error, got %v", err)
	}
	write(statePath, []byte("invalid json"))
	if _, err := loadLocomoCorpus(); err == nil || !strings.Contains(err.Error(), "read eval-locomo.json") {
		t.Fatalf("invalid snapshots and missing vault must return the existing error, got %v", err)
	}
	vaultPath := filepath.Join(home, ".local", "state", "rolling-memory", "eval", "eval-locomo.json")
	write(vaultPath, []byte(`{"fixture":{"doc_id":"fixture","raw_text":"vault-fallback-marker","address_uri":"cell#1"}}`))
	load("vault-fallback-marker")
}

func TestWriteNewLocomoSnapshotDoesNotOverwrite(t *testing.T) {
	raw := []byte(`{"nodes":[{"node_key":"unit:synthetic","kind":"unit"}]}`)
	for _, name := range []string{"snapshot.json", "snapshot.json.gz"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := writeNewLocomoSnapshot(path, raw); err != nil {
				t.Fatal(err)
			}
			dump, err := readLocomoSnapshot(path)
			if err != nil || len(dump.Nodes) != 1 || dump.Nodes[0].NodeKey != "unit:synthetic" {
				t.Fatalf("snapshot round trip: dump=%+v err=%v", dump, err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeNewLocomoSnapshot(path, []byte("replacement")); !errors.Is(err, os.ErrExist) {
				t.Fatalf("existing output must be rejected: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("existing output changed: %v", err)
			}
		})
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "existing.json")
	link := filepath.Join(dir, "snapshot.json")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeNewLocomoSnapshot(link, raw); !errors.Is(err, os.ErrExist) {
		t.Fatalf("symlink output must be rejected: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "existing" {
		t.Fatalf("symlink target changed: %q, %v", got, err)
	}
}

func TestLocalPlainEvalAgentsTransmitContextToConfiguredModel(t *testing.T) {
	t.Setenv("ROLLING_MEMORY_MODE", "local")
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests = append(requests, body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"yes"}}]}`))
	}))
	defer srv.Close()
	question := EvalQuestionItem{ID: "test", Question: "question-marker", Answer: "answer-marker", EvidenceTexts: []string{"eval-context-marker"}}
	cfg := AIModelConfig{BaseURL: srv.URL, AnswerModel: "model", JudgeModel: "model", TimeoutSec: 2}
	_, _, err := executeSingleQuestionEval(context.Background(), "", "", question, 0, EvalArmOracle, 1, true, cfg)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || !strings.Contains(string(encoded), "eval-context-marker") || !strings.Contains(string(encoded), "question-marker") {
		t.Fatalf("unexpected model requests: %s", encoded)
	}
}
