// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildFakeMCP compiles the synthetic MCP server. The handshake must run a real
// child process; the go test binary itself is not a launcher.
func buildFakeMCP(t *testing.T) string {
	t.Helper()
	name := "fakemcp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	output := filepath.Join(t.TempDir(), name)
	command := exec.Command("go", "build", "-o", output, "./testdata/fakemcp")
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fake MCP server: %v\n%s", err, data)
	}
	return output
}

func fakeHandshakeOptions(t *testing.T) InstallOptions {
	t.Helper()
	options := DefaultInstallOptions(t.TempDir(), t.TempDir())
	options.Binary = buildFakeMCP(t)
	options.Scope = "mem:test"
	return options
}

func TestHandshakeOK(t *testing.T) {
	t.Setenv("RM_FAKE_MCP_MODE", "ok")
	result := Handshake(fakeHandshakeOptions(t), 10*time.Second)
	if result["ok"] != true {
		t.Fatalf("handshake = %#v", result)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 1 || tools[0] != "recall" {
		t.Fatalf("tools = %#v", result["tools"])
	}
	server, ok := result["server"].(map[string]any)
	if !ok || server["name"] != "rolling-memory" {
		t.Fatalf("server = %#v", result["server"])
	}
	if _, exists := result["error"]; exists {
		t.Fatalf("unexpected error: %v", result["error"])
	}
}

func TestHandshakeUnexpectedTools(t *testing.T) {
	t.Setenv("RM_FAKE_MCP_MODE", "wrong-tools")
	result := Handshake(fakeHandshakeOptions(t), 10*time.Second)
	if result["ok"] != false {
		t.Fatalf("handshake should fail: %#v", result)
	}
	if message, _ := result["error"].(string); !strings.Contains(message, "unexpected tool set: ['remember']") {
		t.Fatalf("error = %v", result["error"])
	}
}

func TestHandshakeRejectsInitializeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test launcher")
	}
	launcher := filepath.Join(t.TempDir(), "fake-mcp")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32603,\"message\":\"failed\"}}' '{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"recall\"}]}}'\n"
	if err := os.WriteFile(launcher, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	options := DefaultInstallOptions(t.TempDir(), t.TempDir())
	options.Binary = launcher
	if result := Handshake(options, 10*time.Second); result["ok"] != false {
		t.Fatalf("initialize error must fail the handshake: %#v", result)
	}
}

func TestHandshakeRejectsInvalidResponsesAndExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test launcher")
	}
	tests := []struct {
		name       string
		initialize string
		tools      string
		exit       string
	}{
		{"missing result", `{"jsonrpc":"2.0","id":1}`, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"recall"}]}}`, "0"},
		{"invalid version", `{"jsonrpc":"1.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"rolling-memory"}}}`, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"recall"}]}}`, "0"},
		{"non-scalar version", `{"jsonrpc":[],"id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"rolling-memory"}}}`, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"recall"}]}}`, "0"},
		{"missing server info", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"recall"}]}}`, "0"},
		{"tools error", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"rolling-memory"}}}`, `{"jsonrpc":"2.0","id":2,"error":{"code":-32603}}`, "0"},
		{"nonzero exit", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"rolling-memory"}}}`, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"recall"}]}}`, "1"},
		{"duplicate initialize", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"rolling-memory"}}}` + "\n" + `{"jsonrpc":"2.0","id":1,"error":{"code":-32603}}`, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"recall"}]}}`, "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := filepath.Join(t.TempDir(), "fake-mcp")
			script := "#!/bin/sh\nprintf '%s\\n' '" + tt.initialize + "' '" + tt.tools + "'\nexit " + tt.exit + "\n"
			if err := os.WriteFile(launcher, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			options := DefaultInstallOptions(t.TempDir(), t.TempDir())
			options.Binary = launcher
			if result := Handshake(options, 10*time.Second); result["ok"] != false {
				t.Fatalf("invalid handshake accepted: %#v", result)
			}
		})
	}
}

func TestCheckDoesNotTreatUnrelatedLauncherAsRegisteredServer(t *testing.T) {
	fx := newFixture(t)
	options := fx.options()
	options.Binary = buildFakeMCP(t)
	t.Setenv("RM_FAKE_MCP_MODE", "ok")
	zcode, err := SelectHarnesses(fx.registry(), []string{"zcode"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeText(fx.zcodeConfig(), `{"mcp":{"servers":{"rmem":{"type":"stdio","command":"/missing/rolling-memory.sh","args":["mcp"]}}}}`); err != nil {
		t.Fatal(err)
	}
	options.MCPOnly = true
	report := Check(zcode, options, true)
	found := false
	for _, action := range report.Actions {
		if action.Harness == "zcode" && action.Component == "mcp" {
			found = true
		}
	}
	if !found || len(report.Failures()) == 0 {
		t.Fatalf("unrelated launcher handshake must not validate the broken registration: %#v", report.ToDict())
	}
}

func TestCheckRejectsBrokenStaticRegistrations(t *testing.T) {
	fx := newFixture(t)
	options := fx.options()
	options.Binary = buildFakeMCP(t)
	options.MCPOnly = true
	zcode, err := SelectHarnesses(fx.registry(), []string{"zcode"}, false)
	if err != nil {
		t.Fatal(err)
	}
	command := strings.ReplaceAll(options.Binary, `\`, `\\`)
	tests := []struct {
		name  string
		entry string
		valid bool
	}{
		{"valid", `{"command":"` + command + `","type":"stdio","args":["mcp"]}`, true},
		{"independent scope", `{"command":"` + command + `","args":["mcp"],"env":{"ROLLING_MEMORY_SCOPE":"project:other"}}`, true},
		{"wrong arguments", `{"command":"` + command + `","args":["mcp","extra"]}`, false},
		{"disabled", `{"command":"` + command + `","args":["mcp"],"disabled":true}`, false},
		{"wrong transport", `{"command":"` + command + `","args":["mcp"],"type":"http"}`, false},
		{"bad env", `{"command":"` + command + `","args":["mcp"],"env":{"ROLLING_MEMORY_MODE":"remote"}}`, false},
		{"invalid env type", `{"command":"` + command + `","args":["mcp"],"env":[]}`, false},
		{"directory", `{"command":"` + strings.ReplaceAll(filepath.Dir(command), `\`, `\\`) + `","args":["mcp"]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := writeText(fx.zcodeConfig(), `{"mcp":{"servers":{"rmem":`+tt.entry+`}}}`); err != nil {
				t.Fatal(err)
			}
			report := Check(zcode, options, false)
			if got := len(report.Failures()) == 0; got != tt.valid {
				t.Fatalf("valid=%v, report=%#v", got, report.ToDict())
			}
			if tt.valid {
				action := firstAction(t, report, "zcode", "mcp")
				if action.Status != StatusOK || !strings.Contains(action.Detail, "runtime not verified") {
					t.Fatalf("static evidence is not labeled: %#v", action)
				}
			}
		})
	}
}

func TestCheckReportsStaticRegistrationSeparatelyFromLauncherProbe(t *testing.T) {
	fx := newFixture(t)
	options := fx.options()
	options.Binary = buildFakeMCP(t)
	options.MCPOnly = true
	t.Setenv("RM_FAKE_MCP_MODE", "ok")
	zcode, err := SelectHarnesses(fx.registry(), []string{"zcode"}, false)
	if err != nil {
		t.Fatal(err)
	}
	command, err := json.Marshal(options.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeText(fx.zcodeConfig(), `{"mcp":{"servers":{"rmem":{"command":`+string(command)+`,"args":["mcp"]}}}}`); err != nil {
		t.Fatal(err)
	}
	report := Check(zcode, options, true)
	assertNoFailures(t, report)
	configured := firstAction(t, report, "zcode", "mcp")
	if configured.Status != StatusOK || !strings.Contains(configured.Detail, "runtime not verified") {
		t.Fatalf("static registration = %#v", configured)
	}
	runtime := firstAction(t, report, "zcode", "mcp-runtime")
	if runtime.Status != StatusSkipped || !strings.Contains(runtime.Detail, "not executed") {
		t.Fatalf("runtime evidence = %#v", runtime)
	}
	server, _ := report.Extra["server"].(map[string]any)
	if server["ok"] != true || !strings.Contains(pyString(server["evidence"]), "selected launcher only") {
		t.Fatalf("launcher evidence = %#v", server)
	}
}

func TestStaticRegistrationValidationAcrossFormats(t *testing.T) {
	for _, kind := range []string{"json", "toml", "dsh"} {
		t.Run(kind, func(t *testing.T) {
			options := DefaultInstallOptions(t.TempDir(), t.TempDir())
			options.Binary = buildFakeMCP(t)
			path := filepath.Join(t.TempDir(), "config")
			h := Harness{MCPKind: kind}
			var content string
			var err error
			switch kind {
			case "json":
				h.MCPKeys = []string{"mcpServers"}
				command, err := json.Marshal(options.Binary)
				if err != nil {
					t.Fatal(err)
				}
				content = `{"mcpServers":{"rolling-memory":{"command":` + string(command) + `,"args":["mcp"]}}}`
			case "toml":
				h.MCPKeys = []string{"mcp_servers"}
				content, err = tomlBlock(options)
			case "dsh":
				content, err = dshBlock(options)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := validateMCPRegistration(h, options, path); err != nil {
				t.Fatalf("valid %s registration: %v", kind, err)
			}
			if err := os.Remove(options.Binary); err != nil {
				t.Fatal(err)
			}
			if err := validateMCPRegistration(h, options, path); err == nil {
				t.Fatalf("%s registration accepted missing launcher", kind)
			}
		})
	}
}

func TestStaticRegistrationDoesNotRunCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test launcher")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	launcher := filepath.Join(dir, "rolling-memory")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nprintf executed > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	options := DefaultInstallOptions(t.TempDir(), t.TempDir())
	options.Binary = launcher
	h := Harness{MCPKind: "json", MCPKeys: []string{"mcpServers"}}
	path := filepath.Join(dir, "mcp.json")
	command, err := json.Marshal(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"rolling-memory":{"command":`+string(command)+`,"args":["mcp"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateMCPRegistration(h, options, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("static validation executed registered command: %v", err)
	}
}

func TestHandshakeNoResponse(t *testing.T) {
	t.Setenv("RM_FAKE_MCP_MODE", "silent")
	result := Handshake(fakeHandshakeOptions(t), 10*time.Second)
	if result["ok"] != false {
		t.Fatalf("handshake should fail: %#v", result)
	}
	if message, _ := result["error"].(string); !strings.Contains(message, "no initialize/tools-list response (exit 0); stderr: -") {
		t.Fatalf("error = %v", result["error"])
	}
}

func TestHandshakeDoesNotFallbackWhenShimMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher fixture")
	}
	fx := newFixture(t)
	options := fx.options()
	options.Binary = ""
	options.MCPOnly = true
	fake := buildFakeMCP(t)
	pathDir := t.TempDir()
	pathCommand := filepath.Join(pathDir, "rmem")
	data, err := os.ReadFile(fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathCommand, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)
	t.Setenv("RM_FAKE_MCP_MODE", "ok")
	shim := options.ResolvedBinary()
	if fileExists(shim) {
		t.Fatalf("fixture unexpectedly has shim %s", shim)
	}
	zcode, err := SelectHarnesses(fx.registry(), []string{"zcode"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeText(fx.zcodeConfig(), `{"mcp":{"servers":{"rmem":{"command":"rmem","args":["mcp"]}}}}`); err != nil {
		t.Fatal(err)
	}
	report := Check(zcode, options, true)
	server, _ := report.Extra["server"].(map[string]any)
	action := firstAction(t, report, "*", "server")
	if server["ok"] != false || server["launcher"] != shim || action.Path != shim || action.Status != StatusError {
		t.Fatalf("missing shim must fail without fallback: server=%#v action=%#v", server, action)
	}
	if message, _ := server["error"].(string); !strings.Contains(message, "launcher not found") {
		t.Fatalf("missing shim error = %#v", server)
	}
	if configured := firstAction(t, report, "zcode", "mcp"); configured.Status != StatusOK {
		t.Fatalf("temporary PATH registration = %#v", configured)
	}
	if runtime := firstAction(t, report, "zcode", "mcp-runtime"); runtime.Status != StatusSkipped {
		t.Fatalf("host runtime falsely verified: %#v", runtime)
	}

	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, data, 0o755); err != nil {
		t.Fatal(err)
	}
	report = Check(zcode, options, true)
	server, _ = report.Extra["server"].(map[string]any)
	action = firstAction(t, report, "*", "server")
	if server["ok"] != true || server["launcher"] != shim || action.Path != shim {
		t.Fatalf("present shim must be the actual probe: server=%#v action=%#v", server, action)
	}
	if _, exists := server["fallback_reason"]; exists {
		t.Fatalf("present shim must not report fallback: %#v", server)
	}
	if runtime := firstAction(t, report, "zcode", "mcp-runtime"); runtime.Status != StatusSkipped {
		t.Fatalf("successful launcher probe must not verify host runtime: %#v", runtime)
	}
}

func TestHandshakeMissingLauncher(t *testing.T) {
	options := DefaultInstallOptions(t.TempDir(), t.TempDir())
	options.Binary = filepath.Join(t.TempDir(), "does-not-exist")
	result := Handshake(options, 10*time.Second)
	if result["ok"] != false {
		t.Fatalf("handshake should fail: %#v", result)
	}
	if message, _ := result["error"].(string); !strings.Contains(message, "launcher not found") {
		t.Fatalf("error = %v", result["error"])
	}
	if result["launcher"] != options.Binary {
		t.Fatalf("missing explicit launcher = %#v", result)
	}
	if _, exists := result["fallback_reason"]; exists {
		t.Fatalf("explicit launcher must not fall back: %#v", result)
	}
	report := Check(nil, options, true)
	if action := firstAction(t, report, "*", "server"); action.Status != StatusError || action.Path != options.Binary {
		t.Fatalf("missing launcher action = %#v", action)
	}
}

func TestHandshakeTimeout(t *testing.T) {
	t.Setenv("RM_FAKE_MCP_MODE", "sleep")
	result := Handshake(fakeHandshakeOptions(t), 100*time.Millisecond)
	if result["ok"] != false {
		t.Fatalf("handshake should time out: %#v", result)
	}
	if message, _ := result["error"].(string); !strings.Contains(message, "timed out") {
		t.Fatalf("error = %v", result["error"])
	}
}

func TestLauncherCommand(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	options := DefaultInstallOptions(t.TempDir(), t.TempDir())
	options.Binary = "/opt/rm/rolling-memory"
	if command := launcherCommand(options); len(command) != 1 || command[0] != options.Binary {
		t.Fatalf("custom binary command = %#v", command)
	}

	options.Binary = ""
	if command := launcherCommand(options); len(command) != 1 || command[0] != executablePath() {
		t.Fatalf("fallback command = %#v", command)
	}

	shim := DefaultShimPath(options.Home)
	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(shim, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	if command := launcherCommand(options); len(command) != 1 || command[0] != shim {
		t.Fatalf("shim command = %#v", command)
	}
}
