// plugin: rolling-memory v0.0.58
package rollingmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// startFakeHydrateDaemon 在临时 Unix socket 上模拟引擎守护进程的 `/v1/hydrate` 端点，
// 返回 socket 路径、收到的请求体通道与调用计数。
func startFakeHydrateDaemon(t *testing.T, reply func(refs []string) map[string]string) (string, *atomic.Int64, *[]map[string]any) {
	t.Helper()
	sockDir, err := os.MkdirTemp("/tmp", "rm-admin-hyd-")
	if err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	sockPath := filepath.Join(sockDir, "engine.sock")
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	var calls atomic.Int64
	var payloadMu sync.Mutex
	var payloads []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/hydrate", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		payloadMu.Lock()
		payloads = append(payloads, payload)
		payloadMu.Unlock()
		var refs []string
		var items []hydrateRef
		if rawItems, ok := payload["items"].([]any); ok {
			for _, item := range rawItems {
				if m, ok := item.(map[string]any); ok {
					ref := asString(m["ref"])
					refs = append(refs, ref)
					items = append(items, hydrateRef{DocID: asString(m["doc_id"]), Ref: ref})
				}
			}
		}
		texts := reply(refs)
		out := map[string]any{"ok": true}
		list := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if text, ok := texts[item.Ref]; ok && text != "" && item.DocID != "" {
				list = append(list, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "actual_ref": item.Ref, "text": text, "verified": true})
			} else {
				list = append(list, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "error": "unavailable"})
			}
		}
		out["texts"] = list
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = os.RemoveAll(sockDir)
	})
	return sockPath, &calls, &payloads
}

func startHydrateContractDaemon(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rm-admin-contract-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "engine.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/hydrate", handler)
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); _ = os.RemoveAll(dir) })
	return path
}

// startFakeDocumentsDaemon 在临时 Unix socket 上模拟引擎守护进程的 `/v1/documents` 端点，
// 返回 socket 路径与调用计数。reply 中出现的 unit id 即为守护进程裁决可读（答案正文），
// 未出现即裁决不可读（应答 error，控制台按「正文暂不可读」降级）。
func startFakeDocumentsDaemon(t *testing.T, reply func(unitIDs []string) map[string]string) (string, *atomic.Int64) {
	t.Helper()
	sockDir, err := os.MkdirTemp("/tmp", "rm-admin-doc-")
	if err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	sockPath := filepath.Join(sockDir, "engine.sock")
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/documents", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var payload struct {
			Items []struct {
				UnitID string `json:"unit_id"`
			} `json:"items"`
		}
		_ = json.Unmarshal(body, &payload)
		var unitIDs []string
		for _, item := range payload.Items {
			unitIDs = append(unitIDs, item.UnitID)
		}
		texts := reply(unitIDs)
		list := make([]map[string]any, 0, len(unitIDs))
		for _, id := range unitIDs {
			if text, ok := texts[id]; ok {
				list = append(list, map[string]any{"unit_id": id, "raw_text": text})
				continue
			}
			list = append(list, map[string]any{"unit_id": id, "error": "source unavailable"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "docs": list})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = os.RemoveAll(sockDir)
	})
	return sockPath, &calls
}

// TestBackgroundChunkRowUsesDaemonBody 锁定 L1 分块列表行的正文口径：
// 批量预热经守护进程 /v1/documents 一次解析并把源文件正文写进 content，
// 未裁决的行（源文件被移动/删除）保留来源位置预览并显式标记 body_unavailable。
func TestBackgroundChunkRowUsesDaemonBody(t *testing.T) {
	readableID := "bg:readable#0"
	goneID := "bg:gone#0"
	sockPath, calls := startFakeDocumentsDaemon(t, func(unitIDs []string) map[string]string {
		out := map[string]string{}
		for _, id := range unitIDs {
			if id == readableID {
				out[id] = "# Emergency Failover Runbook\nExecute command `aurora-ctl drain --graceful`."
			}
		}
		return out
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)
	home := t.TempDir()

	readableRow := map[string]any{
		"src": "chunk", "id": readableID, "layer": "background", "status": "active",
		"rel_path": "docs/runbook.md", "anchor": "L1-7", "abs_path": "/ws/docs/runbook.md", "occurred_at": 1762380000,
	}
	goneRow := map[string]any{
		"src": "chunk", "id": goneID, "layer": "background", "status": "active",
		"rel_path": "docs/retired.md", "anchor": "L1-3", "abs_path": "/ws/docs/retired.md", "occurred_at": 1762380000,
	}
	rows := []map[string]any{readableRow, goneRow}
	warmHydrateLocalDocumentsBatch(home, rows)

	row := layerRowFromSQL(readableRow, home)
	if row.BodyUnavailable || !strings.Contains(row.Content, "aurora-ctl drain --graceful") {
		t.Fatalf("readable chunk row = %+v, want the daemon-resolved chunk body", row)
	}
	if row.Title != "docs/runbook.md #L1-7" {
		t.Fatalf("chunk row title = %q, want the file location", row.Title)
	}
	rowGone := layerRowFromSQL(goneRow, home)
	if !rowGone.BodyUnavailable || rowGone.Content != "[docs/retired.md#L1-3] (/ws/docs/retired.md)" {
		t.Fatalf("source-gone chunk row = %+v, want location preview flagged body_unavailable", rowGone)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("daemon calls = %d, want a single warm batch", got)
	}
	// 缓存命中后重复构造行不得再打守护进程。
	_ = layerRowFromSQL(readableRow, home)
	if got := calls.Load(); got != 1 {
		t.Fatalf("cached chunk row must not call the daemon again, calls = %d", got)
	}
}

// TestLayersGetBackgroundChunkReturnsDaemonBody 锁定 L1 分块详情（layers/get）：
// 正文可读时 content 为源文件正文且不带不可读标记；读取引擎不可用时退化为位置预览 + body_unavailable。
func TestLayersGetBackgroundChunkReturnsDaemonBody(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bg_detail.db")
	db, err := openWritableVaultDB(dbPath)
	if err != nil {
		t.Fatalf("openWritableVaultDB: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO thin_vault_bg_chunks(unit_id, abs_path, rel_path, chunk_index, anchor, occurred_at, address_uri)
		VALUES ('bg:detail#0', '/ws/docs/plan.md', 'docs/plan.md', 0, 'L1-9', 1762380000, 'C6:L1:BG#00')`); err != nil {
		_ = db.Close()
		t.Fatalf("insert bg_chunk: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	t.Setenv("ROLLING_MEMORY_DB", dbPath)
	// 固定 HOME：分块正文缓存按 (flavor, home, unit_id) 分键，本测试两个阶段必须落在同一键上，
	// 阶段 ② 才会真正重建「引擎不可用」路径。
	home := t.TempDir()
	t.Setenv("HOME", home)

	decodeRow := func(raw string) ThreeLayerRow {
		t.Helper()
		var envelope struct {
			Code int `json:"code"`
			Data struct {
				Row ThreeLayerRow `json:"row"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			t.Fatalf("decode layers/get: %v", err)
		}
		if envelope.Code != 0 {
			t.Fatalf("layers/get returned business error: %s", raw)
		}
		return envelope.Data.Row
	}

	// ① 守护进程可用：content 为分块正文。
	sockPath, _ := startFakeDocumentsDaemon(t, func(unitIDs []string) map[string]string {
		return map[string]string{"bg:detail#0": "# Deployment Plan\nStep 1: drain the aurora gateway."}
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)
	row := decodeRow(callPluginHandler(t, layersGetHandler,
		"/admin/plugin/rolling-memory/layers/get", `{"id":"bg:detail#0"}`))
	if row.BodyUnavailable || !strings.Contains(row.Content, "drain the aurora gateway") {
		t.Fatalf("layers/get chunk row = %+v, want the canonical chunk body", row)
	}
	if row.RelPath != "docs/plan.md" || row.Anchor != "L1-9" {
		t.Fatalf("layers/get chunk row lost its source location: %+v", row)
	}

	// ② 读取引擎不可用（socket 缺失且守护进程未起）：位置预览 + body_unavailable，绝不冒充正文。
	// 先清掉 ① 写入的正文缓存，否则这里会命中缓存拿不到「不可读」路径。
	hydrateMu.Lock()
	delete(hydrateCache, hydrateCacheKey(hydrateFlavorLocalDoc, home, "bg:detail#0"))
	hydrateMu.Unlock()
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", "")
	t.Setenv("ROLLING_MEMORY_DAEMON", "0")
	row = decodeRow(callPluginHandler(t, layersGetHandler,
		"/admin/plugin/rolling-memory/layers/get", `{"id":"bg:detail#0"}`))
	if !row.BodyUnavailable || row.Content != "[docs/plan.md#L1-9] (/ws/docs/plan.md)" {
		t.Fatalf("engine-down chunk row = %+v, want location preview flagged body_unavailable", row)
	}
}

// TestMemoryDetailBackgroundChunkReturnsDaemonBody 锁定检索调试台详情（memories/detail）的
// L1 分块分支与列表同一口径：正文可读时给出正文，不可读时以位置预览兜底。
func TestMemoryDetailBackgroundChunkReturnsDaemonBody(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bg_mem_detail.db")
	db, err := openWritableVaultDB(dbPath)
	if err != nil {
		t.Fatalf("openWritableVaultDB: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO thin_vault_bg_chunks(unit_id, abs_path, rel_path, chunk_index, anchor, occurred_at, address_uri)
		VALUES ('bg:mem#0', '/ws/docs/notes.md', 'docs/notes.md', 0, 'L1-5', 1762380000, 'C6:L1:BG#01')`); err != nil {
		_ = db.Close()
		t.Fatalf("insert bg_chunk: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	t.Setenv("ROLLING_MEMORY_DB", dbPath)

	sockPath, _ := startFakeDocumentsDaemon(t, func(unitIDs []string) map[string]string {
		return map[string]string{"bg:mem#0": "Meeting notes: rotate the optical transceivers on Friday."}
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)
	home := t.TempDir()
	t.Setenv("HOME", home)

	det, err := queryMemoryDetailFromDB(dbPath, "bg:mem#0")
	if err != nil {
		t.Fatalf("queryMemoryDetailFromDB: %v", err)
	}
	if !strings.Contains(det.TraceText, "rotate the optical transceivers") ||
		!strings.Contains(det.Item.Object, "rotate the optical transceivers") {
		t.Fatalf("memory detail must carry the canonical chunk body: %+v", det)
	}
	if det.TraceAnchor != "[docs/notes.md#L1-5] (/ws/docs/notes.md)" {
		t.Fatalf("memory detail trace anchor = %q, want the source location", det.TraceAnchor)
	}
}

// TestHydratedPointerPrefersDaemonCanonicalText verifies each read through the daemon.
func TestHydratedPointerPrefersDaemonCanonicalText(t *testing.T) {
	ref := "@ref:zcode:sess_fixture:msg_turn_1#1"
	sockPath, calls, payloads := startFakeHydrateDaemon(t, func(refs []string) map[string]string {
		out := map[string]string{}
		for _, r := range refs {
			if r == ref {
				out[r] = "[assistant] 规范分段正文"
			}
		}
		return out
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)
	home := t.TempDir()

	text, ok := hydratedEpisodicPointerLive(home, filepath.Join(home, "memory.db"), "doc-one", ref)
	if !ok || text != "[assistant] 规范分段正文" {
		t.Fatalf("daemon hydration = (%q, %v), want canonical text", text, ok)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("daemon calls = %d, want 1", got)
	}
	if _, ok := hydratedEpisodicPointerLive(home, filepath.Join(home, "memory.db"), "doc-one", ref); !ok {
		t.Fatalf("second verified read failed")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("each read must reverify the source, calls = %d", got)
	}
	if len(*payloads) != 2 {
		t.Fatalf("payloads = %d, want 2", len(*payloads))
	}
	items, _ := (*payloads)[0]["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item, _ := items[0].(map[string]any)
	if item["doc_id"] != "doc-one" || item["ref"] != ref {
		t.Fatalf("request identity = %+v", item)
	}
}

// TestWarmHydrateBatchVerifiesEachDocument tests mixed, duplicate, and missing identities.
func TestWarmHydrateBatchVerifiesEachDocument(t *testing.T) {
	ref := "@ref:zcode:sess:msg#0"
	sockPath, calls, payloads := startFakeHydrateDaemon(t, func(refs []string) map[string]string {
		return map[string]string{ref: "[user] verified"}
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)
	home := t.TempDir()
	rows := []map[string]any{
		{"id": "doc-a", "content": ref},
		{"id": "doc-a", "content": ref},
		{"id": "doc-b", "content": ref},
		{"content": ref},
		{"id": "doc-missing", "content": "@ref:jsonl:/missing:0#0"},
	}
	warmHydrateZCodePointersBatch(home, filepath.Join(home, "memory.db"), rows)
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	for i := 0; i < 3; i++ {
		if text, ok := hydratedPointerRowText(rows[i]); !ok || text != "[user] verified" {
			t.Fatalf("row %d = (%q, %v)", i, text, ok)
		}
	}
	for _, row := range rows[3:] {
		if text, ok := hydratedPointerRowText(row); ok || text != "" {
			t.Fatalf("unavailable row = %q", text)
		}
	}
	items, _ := (*payloads)[0]["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %+v, want distinct doc/ref pairs", items)
	}
}

func TestHydrateFailsClosedWhenDaemonMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", filepath.Join(home, "missing.sock"))
	ref := "@ref:jsonl:" + filepath.Join(home, "transcript.jsonl") + ":0#0"
	if err := os.WriteFile(filepath.Join(home, "transcript.jsonl"), []byte(`{"type":"user","message":{"role":"user","content":"secret"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if text, ok := hydratedEpisodicPointerLive(home, filepath.Join(home, "memory.db"), "doc", ref); ok || text != "" {
		t.Fatalf("missing daemon = (%q, %v)", text, ok)
	}
}

func TestHydrateDaemonRejectsUntrustedResults(t *testing.T) {
	ref := "@ref:zcode:session:message#0"
	var calls atomic.Int64
	path := startHydrateContractDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			Items []hydrateRef `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var results []map[string]any
		for _, item := range req.Items {
			result := map[string]any{"doc_id": item.DocID, "ref": item.Ref, "actual_ref": item.Ref, "text": "secret", "verified": true}
			switch item.DocID {
			case "good":
			case "unverified":
				delete(result, "verified")
			case "error":
				result["error"] = "mismatch"
			case "wrong-id":
				result["doc_id"] = "other"
			case "wrong-ref":
				result["ref"] = "@ref:zcode:other:msg#0"
			case "no-actual":
				delete(result, "actual_ref")
			case "relocated":
				result["actual_ref"] = "@ref:zcode:session:message#1"
			case "duplicate":
				results = append(results, result)
			}
			results = append(results, result)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "texts": results})
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", path)
	home := t.TempDir()
	dbPath := filepath.Join(home, "vault.db")
	if got, ok := hydrateRefsViaDaemon(home, dbPath, nil); ok || len(got) != 0 || calls.Load() != 0 {
		t.Fatalf("empty batch = %v, %v", got, ok)
	}
	ids := []string{"good", "unverified", "error", "wrong-id", "wrong-ref", "no-actual", "relocated", "duplicate", ""}
	items := make([]hydrateRef, 0, len(ids))
	for _, id := range ids {
		items = append(items, hydrateRef{DocID: id, Ref: ref})
	}
	got, ok := hydrateRefsViaDaemon(home, dbPath, items)
	if !ok || len(got) != 1 || got[hydrateRef{DocID: "good", Ref: ref}] != "secret" {
		t.Fatalf("verified results = %+v, handled=%v", got, ok)
	}
	if calls.Load() != 1 {
		t.Fatalf("batch calls = %d", calls.Load())
	}
}

func TestHydrateRevalidatesChangedSourceAndIdentity(t *testing.T) {
	ref := "@ref:zcode:session:message#0"
	var valid atomic.Bool
	valid.Store(true)
	path := startHydrateContractDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Items []hydrateRef `json:"items"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var results []map[string]any
		for _, item := range req.Items {
			if item.DocID == "doc-a" && item.Ref == ref && valid.Load() {
				results = append(results, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "actual_ref": ref, "text": "indexed original", "verified": true})
			} else {
				results = append(results, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "error": "content or identity mismatch"})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "texts": results})
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", path)
	home := t.TempDir()
	dbPath := filepath.Join(home, "memory.db")
	if text, ok := hydratedEpisodicPointerLive(home, dbPath, "doc-a", ref); !ok || text != "indexed original" {
		t.Fatalf("valid source = (%q, %v)", text, ok)
	}
	for _, item := range []hydrateRef{{DocID: "doc-b", Ref: ref}, {DocID: "doc-a", Ref: ref + "wrong"}, {Ref: ref}} {
		if text, ok := hydratedEpisodicPointerLive(home, dbPath, item.DocID, item.Ref); ok || text != "" {
			t.Fatalf("wrong identity = (%q, %v)", text, ok)
		}
	}
	valid.Store(false)
	if text, ok := hydratedEpisodicPointerLive(home, dbPath, "doc-a", ref); ok || text != "" {
		t.Fatalf("changed source reused text = (%q, %v)", text, ok)
	}
	t.Setenv("ROLLING_MEMORY_DAEMON", "0")
	if text, ok := hydratedEpisodicPointerLive(home, dbPath, "doc-a", ref); ok || text != "" {
		t.Fatalf("disabled daemon reused text = (%q, %v)", text, ok)
	}
}

func TestCachedROIBundleRevalidatesEpisodicEvidence(t *testing.T) {
	ref := "@ref:zcode:session:message#0"
	var valid atomic.Bool
	valid.Store(true)
	path := startHydrateContractDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Items []hydrateRef `json:"items"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var results []map[string]any
		for _, item := range req.Items {
			if valid.Load() && item.DocID == "doc-a" && item.Ref == ref {
				results = append(results, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "actual_ref": ref, "text": "authenticated source", "verified": true})
			} else {
				results = append(results, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "error": "source changed"})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "texts": results})
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", path)
	t.Setenv("HOME", t.TempDir())
	memoryPath := filepath.Join(t.TempDir(), "vault.db")
	bundle := vaultAnalyticsBundle{
		vault:         &roiFactsVault{},
		topCitedUnits: []roiTopCitedUnit{{UnitID: "doc-a", IsZeroCopyRef: true, storedRef: ref, fallbackPreview: "citation preview", FullText: "old cached text", IsHydrated: true}},
		noeticStream:  []roiNoeticEvent{{CitedUnits: []roiCitedUnitDetail{{UnitID: "doc-a", StoredText: ref, IsZeroCopyRef: true, FullText: "old cached text", IsHydrated: true}}}},
	}
	roiVaultBundleMu.Lock()
	roiVaultBundleCache[memoryPath] = &roiVaultBundleEntry{sig: roiVaultBundleSignature(memoryPath, time.Now()), bundle: bundle}
	roiVaultBundleMu.Unlock()
	t.Cleanup(func() { roiVaultBundleMu.Lock(); delete(roiVaultBundleCache, memoryPath); roiVaultBundleMu.Unlock() })
	first := readROIFactsVaultBundle(memoryPath)
	if first.topCitedUnits[0].FullText != "authenticated source" || first.noeticStream[0].CitedUnits[0].FullText != "authenticated source" {
		t.Fatalf("verified bundle = %+v %+v", first.topCitedUnits, first.noeticStream)
	}
	valid.Store(false)
	second := readROIFactsVaultBundle(memoryPath)
	if second.topCitedUnits[0].FullText != "" || second.topCitedUnits[0].IsHydrated || second.topCitedUnits[0].Preview != "citation preview" || second.noeticStream[0].CitedUnits[0].FullText != "" || second.noeticStream[0].CitedUnits[0].IsHydrated {
		t.Fatalf("stale bundle = %+v %+v", second.topCitedUnits, second.noeticStream)
	}
	if first.topCitedUnits[0].FullText != "authenticated source" {
		t.Fatal("revalidation mutated previous response")
	}
}

func BenchmarkHydrateDaemonBatch(b *testing.B) {
	dir, err := os.MkdirTemp("/tmp", "rm-admin-bench-")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "engine.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		b.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Items []hydrateRef `json:"items"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		results := make([]map[string]any, 0, len(req.Items))
		for _, item := range req.Items {
			results = append(results, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "actual_ref": item.Ref, "text": "verified body", "verified": true})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "texts": results})
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	b.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", path)
	b.Setenv("ROLLING_MEMORY_DAEMON", "1")
	refs := make([]hydrateRef, 64)
	for i := range refs {
		refs[i] = hydrateRef{DocID: fmt.Sprintf("doc-%d", i), Ref: fmt.Sprintf("@ref:zcode:session:msg-%d#0", i)}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, ok := hydrateRefsViaDaemon(dir, filepath.Join(dir, "vault.db"), refs)
		if !ok || len(result) != len(refs) {
			b.Fatalf("results=%d handled=%v", len(result), ok)
		}
	}
}

func TestPointerRowSnapshotDoesNotCacheVerifiedText(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, "vault.db")
	ref := "@ref:zcode:session:message#0"
	seedVault(t, dbPath, `INSERT INTO thin_vault_docs(doc_id, layer, occurred_at, raw_text, address_uri) VALUES ('doc-a', 'episodic', 1, '`+ref+`', 'addr')`)
	sock, _, _ := startFakeHydrateDaemon(t, func([]string) map[string]string { return map[string]string{ref: "verified"} })
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sock)
	first, err := cachedVaultPointerDocRows(dbPath, layerListQuery{})
	if err != nil || len(first) != 1 {
		t.Fatalf("pointer rows = %+v, %v", first, err)
	}
	warmHydrateZCodePointersBatch(home, dbPath, first)
	if text, ok := hydratedPointerRowText(first[0]); !ok || text != "verified" {
		t.Fatalf("first row = %q, %v", text, ok)
	}
	second, err := cachedVaultPointerDocRows(dbPath, layerListQuery{})
	if err != nil || len(second) != 1 {
		t.Fatalf("second rows = %+v, %v", second, err)
	}
	if text, ok := hydratedPointerRowText(second[0]); ok || text != "" {
		t.Fatalf("snapshot retained text = %q, %v", text, ok)
	}
}

func TestLargePointerSearchFindsLateVerifiedMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "vault.db")
	seedVault(t, dbPath)
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 513; i++ {
		if _, err := tx.Exec(`INSERT INTO thin_vault_docs(doc_id, layer, occurred_at, raw_text, address_uri) VALUES (?, 'episodic', ?, ?, '')`, fmt.Sprintf("doc-%03d", i), i, fmt.Sprintf("@ref:zcode:session:msg-%03d#0", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	targetRef := "@ref:zcode:session:msg-512#0"
	sock, calls, _ := startFakeHydrateDaemon(t, func(refs []string) map[string]string {
		out := make(map[string]string, len(refs))
		for _, ref := range refs {
			out[ref] = "ordinary"
		}
		out[targetRef] = "needle verified"
		return out
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sock)
	t.Setenv("ROLLING_MEMORY_DB", dbPath)
	rows, total, err := queryLayerRows(dbPath, layerListQuery{layer: episodicLayer, keyword: "needle", limit: 20}, home)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != "doc-512" {
		t.Fatalf("large search = %+v, %d, %v", rows, total, err)
	}
	if calls.Load() != 9 {
		t.Fatalf("batch calls=%d, want 9", calls.Load())
	}
	memories, err := queryMemoriesFromDB(dbPath, MemoriesQueryRequest{Layer: episodicLayer, Keyword: "needle", Page: 1, PageSize: 20})
	if err != nil || memories.Total != 1 || len(memories.Items) != 1 || memories.Items[0].ID != "doc-512" {
		t.Fatalf("large memories search = %+v, %v", memories, err)
	}
}

func TestListSearchAndDashboardDoNotReuseStalePointerText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "vault.db")
	t.Setenv("ROLLING_MEMORY_DB", dbPath)
	ref := "@ref:zcode:session:message#0"
	seedVault(t, dbPath, `INSERT INTO thin_vault_docs(doc_id, layer, occurred_at, raw_text, address_uri) VALUES ('doc-a', 'episodic', 1700000000, '`+ref+`', 'addr')`)
	var valid atomic.Bool
	valid.Store(true)
	sock := startHydrateContractDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Items []hydrateRef `json:"items"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var results []map[string]any
		for _, item := range req.Items {
			if valid.Load() && item.DocID == "doc-a" && item.Ref == ref {
				results = append(results, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "actual_ref": ref, "text": "unique searchable source", "verified": true})
			} else {
				results = append(results, map[string]any{"doc_id": item.DocID, "ref": item.Ref, "error": "source changed"})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "texts": results})
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sock)
	query := layerListQuery{layer: episodicLayer, keyword: "searchable", limit: 20}
	rows, total, err := queryLayerRows(dbPath, query, home)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Content != "unique searchable source" {
		t.Fatalf("initial search = %+v, %d, %v", rows, total, err)
	}
	cached := DashboardResponse{RecentAssertions: []RecentAssertion{{ID: "doc-a", Rendered: ref}}}
	if got := hydrateDashboardRecent(home, dbPath, cached); got.RecentAssertions[0].Rendered != "unique searchable source" {
		t.Fatalf("dashboard = %+v", got.RecentAssertions)
	}
	valid.Store(false)
	rows, total, err = queryLayerRows(dbPath, query, home)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("stale search = %+v, %d, %v", rows, total, err)
	}
	rows, total, err = queryLayerRows(dbPath, layerListQuery{layer: episodicLayer, limit: 20}, home)
	if err != nil || total != 1 || len(rows) != 1 || !rows[0].BodyUnavailable || rows[0].Content != vaultRefGapMarker {
		t.Fatalf("stale listing = %+v, %d, %v", rows, total, err)
	}
	if got := hydrateDashboardRecent(home, dbPath, cached); got.RecentAssertions[0].Rendered != vaultRefGapMarker {
		t.Fatalf("stale dashboard = %+v", got.RecentAssertions)
	}
	if cached.RecentAssertions[0].Rendered != ref {
		t.Fatal("dashboard snapshot was mutated")
	}
}

// TestTerminalStreamLinesHydratePointers 锁定 Live Console 终端回显：
// 最近写入单元的 `@ref:` 指针必须展示水合后的正文，而不是指针字符串本身。
func TestTerminalStreamLinesHydratePointers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ref := "@ref:zcode:sess_term:msg_term_1#0"
	sockPath, _, _ := startFakeHydrateDaemon(t, func(refs []string) map[string]string {
		out := map[string]string{}
		for _, r := range refs {
			if r == ref {
				out[r] = "[user] 终端回显正文"
			}
		}
		return out
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open vault fixture: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE thin_vault_docs (doc_id TEXT, layer TEXT, occurred_at INTEGER, address_uri TEXT, raw_text TEXT);`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO thin_vault_docs (doc_id, layer, occurred_at, address_uri, raw_text) VALUES ('ep_term_1', 'episodic', 1700000000, 'addr-1', ?);`, ref); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	_ = db.Close()

	lines := buildTerminalStreamLines(dbPath, filepath.Join(t.TempDir(), "missing.log"), 4)
	for _, line := range lines {
		if line.ID != "unit-ep_term_1" {
			continue
		}
		if strings.Contains(line.Text, "@ref:") {
			t.Fatalf("terminal echo still prints the raw pointer: %q", line.Text)
		}
		if !strings.Contains(line.Text, "终端回显正文") {
			t.Fatalf("terminal echo missing hydrated text: %q", line.Text)
		}
		return
	}
	t.Fatalf("terminal stream lines did not include the seeded unit: %+v", lines)
}

// TestGraphDisplayTextHydratesPointers 锁定图谱节点摘要/跨度：
// 指针行取规范水合正文，非指针行维持「蒸馏优先、原文兜底」口径。
func TestGraphDisplayTextHydratesPointers(t *testing.T) {
	ref := "@ref:zcode:sess_graph:msg_graph_1#1"
	sockPath, _, _ := startFakeHydrateDaemon(t, func(refs []string) map[string]string {
		out := map[string]string{}
		for _, r := range refs {
			if r == ref {
				out[r] = "[assistant] 图谱规范正文"
			}
		}
		return out
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)
	home := t.TempDir()

	txt, span := hydrateEpisodicDisplayText(home, filepath.Join(home, "memory.db"), "doc-one", ref, "")
	if txt != "[assistant] 图谱规范正文" || span != "[assistant] 图谱规范正文" {
		t.Fatalf("pointer row display = (%q, %q), want hydrated text", txt, span)
	}
	if txt, span := hydrateEpisodicDisplayText(home, filepath.Join(home, "memory.db"), "doc-one", "plain text", "distilled text"); txt != "distilled text" || span != "plain text" {
		t.Fatalf("plain row display = (%q, %q), want distilled/raw passthrough", txt, span)
	}
}

// TestVaultFollowupDocsWarmUncachedPointers 锁定采纳审计语料：
// 冷缓存的零拷贝指针行必须先走规范批量水合再入语料，早期实现只读缓存会把这行静默丢掉。
func TestVaultFollowupDocsWarmUncachedPointers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ref := "@ref:zcode:sess_follow:msg_follow_1#1"
	sockPath, _, _ := startFakeHydrateDaemon(t, func(refs []string) map[string]string {
		out := map[string]string{}
		for _, r := range refs {
			if r == ref {
				out[r] = "[assistant] 审计语料正文"
			}
		}
		return out
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open vault fixture: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE thin_vault_docs (doc_id TEXT, layer TEXT, occurred_at INTEGER, address_uri TEXT, raw_text TEXT, sentences_json TEXT, locus TEXT);`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO thin_vault_docs (doc_id, layer, occurred_at, address_uri, raw_text, sentences_json, locus) VALUES ('ep_follow_1', 'episodic', 1700000200, 'addr-f', ?, '[]', 'locus-f');`, ref); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	rows, _ := loadVaultFollowupDocs(context.Background(), db, t.TempDir(), dbPath, 0)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want the uncached pointer row to survive hydration", len(rows))
	}
	if rows[0].text != "[assistant] 审计语料正文" || rows[0].docID != "ep_follow_1" {
		t.Fatalf("row = %+v, want canonical hydrated text", rows[0])
	}
	if _, err := db.Exec(`UPDATE thin_vault_docs SET sentences_json = '["unverified fallback"]' WHERE doc_id = 'ep_follow_1'`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROLLING_MEMORY_DAEMON", "0")
	rows, _ = loadVaultFollowupDocs(context.Background(), db, t.TempDir(), dbPath, 0)
	if len(rows) != 0 {
		t.Fatalf("unverified pointer entered followup corpus: %+v", rows)
	}
}

// TestUnresolvableRefShowsGapMarker 锁定诚实占位：无法水合的零拷贝指针绝不以正文身份上屏，
// 展示面回「来源缺口」占位（与产品 recent 同一措辞）。
func TestUnresolvableRefShowsGapMarker(t *testing.T) {
	ref := "@ref:zcode:sess_pruned:msg_pruned_1#0"
	sockPath, _, _ := startFakeHydrateDaemon(t, func(refs []string) map[string]string {
		out := map[string]string{}
		for _, r := range refs {
			out[r] = "" // 规范判定：宿主原文已清理，不可水合
		}
		return out
	})
	t.Setenv("ROLLING_MEMORY_DAEMON_SOCKET", sockPath)
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()

	if got := hydrateVaultTextForDisplay(filepath.Join(home, "memory.db"), "doc-one", ref); got != vaultRefGapMarker {
		t.Fatalf("display = %q, want gap marker", got)
	}
	if txt, span := hydrateEpisodicDisplayText(home, filepath.Join(home, "memory.db"), "doc-one", ref, ""); txt != vaultRefGapMarker || span != vaultRefGapMarker {
		t.Fatalf("graph display = (%q, %q), want gap marker", txt, span)
	}
	if got := hydrateVaultTextForDisplay(filepath.Join(home, "memory.db"), "doc-one", "普通正文"); got != "普通正文" {
		t.Fatalf("plain text must pass through, got %q", got)
	}
}
