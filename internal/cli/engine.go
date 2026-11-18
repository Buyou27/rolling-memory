// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/graphmodel"
	"github.com/rocareer/rolling-memory/internal/ids"
	"github.com/rocareer/rolling-memory/internal/mcp"
)

// engineMaxRequestBytes caps one engine request body, mirroring the daemon's own HTTP
// body limit (internal/mcp maxHTTPBody) so the channel never buffers more than the
// daemon would accept.
const engineMaxRequestBytes = 8 << 20

// runEngine serves the quiet machine channel the local web console and automation use
// to reach the shared local daemon through the verified client: one request JSON on
// stdin, one response JSON on stdout, exit 0 on success and 1 on failure. Every op
// rejects a daemon that fails peer and identity verification, so callers never consume
// an unverified local socket, and no op falls back to a raw socket connection.
func (a *application) runEngine() int {
	switch a.args.String("op") {
	case "recall":
		return a.runEngineRecall()
	case "hydrate":
		return a.runEngineHydrate()
	case "documents":
		return a.runEngineDocuments()
	case "graph":
		return a.runEngineGraph()
	case "invalidate":
		return a.runEngineInvalidate()
	}
	return a.fail(fmt.Errorf("unknown engine operation %q", a.args.String("op")))
}

// readEngineRequest reads one JSON request body from stdin (bounded).
func (a *application) readEngineRequest() ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(a.stdin, engineMaxRequestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > engineMaxRequestBytes {
		return nil, errors.New("engine request exceeds the size limit")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, errors.New("engine operation requires a JSON request on stdin")
	}
	return body, nil
}

// loadEngineConfig resolves the configuration the daemon identity is verified against.
// Commands that keep custom databases in-process never route through the shared daemon,
// so the engine channel refuses them the same way `recall` does.
func (a *application) loadEngineConfig() (*config.Config, error) {
	cfg, err := config.Load(config.LoadOptions{
		Path:  a.args.StringPtr("config"),
		DB:    a.args.StringPtr("db"),
		Mode:  a.args.StringPtr("mode"),
		Scope: a.args.StringPtr("scope"),
	})
	if err != nil {
		return nil, err
	}
	if !mcp.ShouldUseUnixDaemon(cfg.DB) {
		return nil, errors.New("engine requires the local daemon channel (daemon routing is disabled for this configuration)")
	}
	return cfg, nil
}

// engineRelayValue converts one relayed daemon response into the generic JSON shape the
// renderer emits, preserving number literals.
func engineRelayValue(payload any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var generic map[string]any
	if err := decoder.Decode(&generic); err != nil {
		return nil, err
	}
	return generic, nil
}

// runEngineRecall relays one recall request over the verified daemon connection,
// reusing the exact call path `recall` uses. An engine-level failure is relayed in the
// daemon's own envelope (ok=false, engine_error=true) so callers can distinguish
// "engine said no" from "daemon unreachable"; both exit 1.
func (a *application) runEngineRecall() int {
	request, err := a.readEngineRequest()
	if err != nil {
		return a.fail(err)
	}
	var payload mcp.UnixRecallPayload
	if err := json.Unmarshal(request, &payload); err != nil {
		return a.fail(fmt.Errorf("decode engine request: %w", err))
	}
	cfg, err := a.loadEngineConfig()
	if err != nil {
		return a.fail(err)
	}
	if strings.TrimSpace(payload.DB) == "" {
		payload.DB = cfg.DB
	}
	resp, err := mcp.CallUnixDaemonRecall(context.Background(), "", cfg, payload)
	if err != nil {
		var engineErr *mcp.EngineRecallError
		if errors.As(err, &engineErr) {
			envelope := map[string]any{"ok": false, "engine_error": true, "error": ids.Redact(engineErr.Msg)}
			if dumpErr := a.printJSON(envelope); dumpErr != nil {
				return a.fail(dumpErr)
			}
			return 1
		}
		return a.fail(err)
	}
	envelope, err := engineRelayValue(resp)
	if err != nil {
		return a.fail(err)
	}
	if err := a.printJSON(envelope); err != nil {
		return a.fail(err)
	}
	return 0
}

// runEngineHydrate relays one display-hydration batch (same shape as the daemon's
// `POST /v1/hydrate` payload) so the console reads the canonical segmentation through
// the same handler the daemon serves, never through a second implementation.
func (a *application) runEngineHydrate() int {
	request, err := a.readEngineRequest()
	if err != nil {
		return a.fail(err)
	}
	var payload mcp.UnixHydratePayload
	if err := json.Unmarshal(request, &payload); err != nil {
		return a.fail(fmt.Errorf("decode engine request: %w", err))
	}
	cfg, err := a.loadEngineConfig()
	if err != nil {
		return a.fail(err)
	}
	if strings.TrimSpace(payload.DB) == "" {
		payload.DB = cfg.DB
	}
	resp, err := mcp.CallUnixDaemonHydrate(context.Background(), "", cfg, payload)
	if err != nil {
		return a.fail(err)
	}
	envelope, err := engineRelayValue(resp)
	if err != nil {
		return a.fail(err)
	}
	if err := a.printJSON(envelope); err != nil {
		return a.fail(err)
	}
	return 0
}

// runEngineDocuments relays one local-document read batch (same shape as the daemon's
// `POST /v1/documents` payload) over the verified connection.
func (a *application) runEngineDocuments() int {
	request, err := a.readEngineRequest()
	if err != nil {
		return a.fail(err)
	}
	var payload mcp.UnixDocumentPayload
	if err := json.Unmarshal(request, &payload); err != nil {
		return a.fail(fmt.Errorf("decode engine request: %w", err))
	}
	cfg, err := a.loadEngineConfig()
	if err != nil {
		return a.fail(err)
	}
	if strings.TrimSpace(payload.DB) == "" {
		payload.DB = cfg.DB
	}
	resp, err := mcp.CallUnixDaemonDocuments(context.Background(), "", cfg, payload)
	if err != nil {
		return a.fail(err)
	}
	envelope, err := engineRelayValue(resp)
	if err != nil {
		return a.fail(err)
	}
	if err := a.printJSON(envelope); err != nil {
		return a.fail(err)
	}
	return 0
}

// runEngineGraph relays one read-only memory-graph view (same shape as the
// daemon's `POST /v1/graph` payload) over the verified connection. The graph is
// built from the local vault and the immutable core snapshot; no cloud endpoint
// is consulted.
func (a *application) runEngineGraph() int {
	request, err := a.readEngineRequest()
	if err != nil {
		return a.fail(err)
	}
	var payload graphmodel.ViewRequest
	if err := json.Unmarshal(request, &payload); err != nil {
		return a.fail(fmt.Errorf("decode engine request: %w", err))
	}
	cfg, err := a.loadEngineConfig()
	if err != nil {
		return a.fail(err)
	}
	resp, err := mcp.CallUnixDaemonGraph(context.Background(), "", cfg, payload)
	if err != nil {
		return a.fail(err)
	}
	envelope, err := engineRelayValue(resp)
	if err != nil {
		return a.fail(err)
	}
	if err := a.printJSON(envelope); err != nil {
		return a.fail(err)
	}
	return 0
}

// runEngineInvalidate tells a running daemon to drop its cached snapshots after an
// out-of-process write. It reads no request body; with no daemon socket present there
// is nothing to invalidate and the operation reports notified=false as success.
func (a *application) runEngineInvalidate() int {
	cfg, err := a.loadEngineConfig()
	if err != nil {
		return a.fail(err)
	}
	notified, err := mcp.InvalidateUnixDaemonCachesVerified(context.Background(), "", cfg)
	if err != nil {
		return a.fail(err)
	}
	if err := a.printJSON(map[string]any{"ok": true, "notified": notified}); err != nil {
		return a.fail(err)
	}
	return 0
}
