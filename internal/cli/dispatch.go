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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/eval"
	"github.com/rocareer/rolling-memory/internal/hooks"
	"github.com/rocareer/rolling-memory/internal/ids"
	"github.com/rocareer/rolling-memory/internal/mcp"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
	"github.com/rocareer/rolling-memory/internal/text"
	"github.com/rocareer/rolling-memory/internal/version"
)

// Main is the process entry point shared by cmd/rolling-memory and cmd/rmem.
// CLI parser usage errors exit 2, processed application errors exit 1 and a
// broken stdout pipe exits 0.
func Main(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	parser := BuildParser()
	args, err := parser.Parse(argv)
	if err != nil {
		var help *HelpRequest
		if errors.As(err, &help) {
			fmt.Fprint(stdout, help.Text)
			return 0
		}
		var usage *UsageError
		if errors.As(err, &usage) {
			fmt.Fprintf(stderr, "%s\n%s: error: %s\n", usage.Usage, parser.Prog, usage.Message)
			return 2
		}
		fmt.Fprintf(stderr, "error: %s\n", ids.Redact(err.Error()))
		return 1
	}
	if args.version {
		fmt.Fprintf(stdout, "rolling-memory %s\n", version.Version)
		return 0
	}
	app := &application{stdin: stdin, stdout: stdout, stderr: stderr, args: args}
	code := app.dispatch()
	if app.writeErr != nil {
		if isBrokenPipe(app.writeErr) {
			return 0
		}
		return 1
	}
	return code
}

// application carries the parsed command line and the three stdio streams.
type application struct {
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	args     *Args
	writeErr error
	// snapshotBuilder is the single-flight guard for the background full-corpus
	// builder spawned by a long-lived recall surface.
	snapshotBuildRunning  atomic.Bool
	snapshotBuildLastNS   atomic.Int64
	snapshotBuildFailures atomic.Int32
}

func (a *application) printf(format string, values ...any) {
	if a.writeErr != nil {
		return
	}
	if _, err := fmt.Fprintf(a.stdout, format, values...); err != nil {
		a.writeErr = err
	}
}

func (a *application) printLine(text string) { a.printf("%s\n", text) }

// printJSON mirrors json.dumps(value, ensure_ascii=False, indent=2).
func (a *application) printJSON(value any) error {
	text, err := ids.PyJSONDumpIndent(value, false, true, 2)
	if err != nil {
		// 结构体等 PyJSON 不直接支持的形态经 encoding/json 归一后再编码；
		// 解码必须保留数字字面（UseNumber），否则 int 字段会退化成 float64 并渲染成 "0.0"，
		// 破坏下游（管理后台、脚本）按整数解析 CLI 输出的契约。
		if raw, mErr := json.Marshal(value); mErr == nil {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			var generic any
			if decoder.Decode(&generic) == nil {
				text, err = ids.PyJSONDumpIndent(generic, false, true, 2)
			}
		}
	}
	if err != nil {
		return err
	}
	a.printf("%s\n", text)
	return nil
}

// emit writes JSON when --json was given, or invokes human() otherwise.
func (a *application) emit(result any, human func()) error {
	if a.args.Flag("json") {
		return a.printJSON(result)
	}
	human()
	return nil
}

// fail renders one processed application error.
func (a *application) fail(err error) int {
	if isBrokenPipe(err) {
		return 0
	}
	message := ids.Redact(err.Error())
	if a.args.Flag("json") {
		envelope := ids.OrderedMap{
			{Key: "ok", Value: false},
			{Key: "error", Value: message},
		}
		// 携带稳定错误码的错误原样透出 error_code：调用方按码分类，不匹配文案。
		var coded interface{ ErrorCode() string }
		if errors.As(err, &coded) {
			envelope = append(envelope, ids.JSONPair{Key: "error_code", Value: coded.ErrorCode()})
		}
		text, dumpErr := ids.PyJSONDump(envelope, false, false, false)
		if dumpErr == nil {
			a.printf("%s\n", text)
		}
	} else {
		fmt.Fprintf(a.stderr, "error: %s\n", message)
	}
	return 1
}

func (a *application) errorf(format string, values ...any) {
	fmt.Fprintf(a.stderr, format, values...)
}

func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}

// ------------------------------------------------------------------ dispatch

func (a *application) dispatch() int {
	switch a.args.Command() {
	case "mcp":
		return a.runMCP()
	case "build":
		return a.runBuild()
	case "ingestd":
		return a.runIngestd()
	case "scan":
		return a.runScan()
	case "perf":
		// Measurement runs on its own throwaway vaults, so it must not require (or open)
		// a configured vault first.
		code, err := a.runPerfCommand()
		if err != nil {
			return a.fail(err)
		}
		return code
	case "hook":
		return a.runHook()
	case "init":
		return a.runInit()
	case "mode":
		return a.runMode()
	case "config":
		return a.runConfig()
	case "install":
		return a.runInstall()
	case "panel":
		return a.runPanel()
	case "update":
		return a.runUpdate()
	case "engine":
		return a.runEngine()
	case "daemon":
		return a.runDaemon()
	case "eval":
		return a.runEval()
	case "recall":
		scope := a.args.String("scope")
		if eval.IsEvalScope(scope) {
			return a.runLocalEvalRecall()
		}
		if err := rejectRetiredEvalScope(scope); err != nil {
			return a.fail(err)
		}
		if code, handled := a.tryDaemonRecall(); handled {
			return code
		}
	}
	return a.withMemory(a.runWithMemory)
}

func (a *application) withMemory(fn func(*facade.Memory) (int, error)) int {
	memory, err := a.openMemory()
	if err != nil {
		return a.fail(err)
	}
	defer memory.Close()
	code, err := fn(memory)
	if err != nil {
		return a.fail(err)
	}
	return code
}

func (a *application) openMemory() (*facade.Memory, error) {
	cfg, err := config.Load(config.LoadOptions{
		Path:  a.args.StringPtr("config"),
		DB:    a.args.StringPtr("db"),
		Mode:  a.args.StringPtr("mode"),
		Scope: a.args.StringPtr("scope"),
	})
	if err != nil {
		return nil, err
	}
	return facade.New(cfg)
}

func (a *application) runWithMemory(memory *facade.Memory) (int, error) {
	switch a.args.Command() {
	case "recall":
		return a.runRecall(memory)
	case "recent":
		result, err := memory.Recent(nil, a.args.StringPtr("reader"), a.args.Int("limit"))
		if err != nil {
			return 1, err
		}
		return 0, a.emit(result, func() { a.printRecent(result) })
	case "import":
		result, err := memory.ImportFile(a.args.String("path"), nil, a.args.StringPtr("session"),
			a.args.String("audience"), false)
		if err != nil {
			return 1, err
		}
		return 0, a.emit(result, func() { a.printImport(result) })
	case "background":
		return a.runBackground(memory)
	case "sync":
		return a.runSyncCommand(memory)
	case "status":
		result, err := memory.Status(memory.Config.Scope)
		if err != nil {
			return 1, err
		}
		result = attachUpdateHint(result)
		return 0, a.emit(result, func() { a.printStatus(result) })
	case "doctor":
		result, err := memory.Doctor()
		if err != nil {
			return 1, err
		}
		result = attachUpdateHint(result)
		return 0, a.emit(result, func() { a.printDoctor(result) })
	case "check":
		result, err := memory.Check()
		if err != nil {
			return 1, err
		}
		if err := a.emit(result, func() { a.printCheck(result) }); err != nil {
			return 1, err
		}
		if ok, _ := result["ok"].(bool); ok {
			return 0, nil
		}
		return 1, nil
	case "maintain":
		return a.runMaintainCommand(memory)
	case "eject":
		result, err := memory.Eject()
		if err != nil {
			return 1, err
		}
		if err := a.emit(result, func() { a.printSummary("eject", result) }); err != nil {
			return 1, err
		}
		if ok, _ := result["ok"].(bool); ok {
			return 0, nil
		}
		return 1, nil
	case "efficacy":
		rep, err := memory.Efficacy(a.args.StringPtr("scope"), a.args.String("harness"), a.args.Int("limit"), a.args.Int("budget_seconds"))
		if err != nil {
			return 1, err
		}
		m := rep.ToMap()
		if a.args.Flag("card") && !a.args.Flag("json") {
			a.printEfficacyCard(m)
			return 0, nil
		}
		return 0, a.emit(m, func() { a.printEfficacy(m) })
	}
	return 1, fmt.Errorf("unknown command %q", a.args.Command())
}

// --------------------------------------------------------------------- read

func (a *application) tryDaemonRecall() (int, bool) {
	if a.args.Flag("stdin_batch") {
		return 0, false
	}
	cfg, err := config.Load(config.LoadOptions{
		Path:  a.args.StringPtr("config"),
		DB:    a.args.StringPtr("db"),
		Mode:  a.args.StringPtr("mode"),
		Scope: a.args.StringPtr("scope"),
	})
	if err != nil {
		return 0, false
	}
	if !mcp.ShouldUseUnixDaemon(cfg.DB) {
		return 0, false
	}
	if limit := a.args.Int("raw_limit"); limit < 1 || limit > 64 {
		return a.fail(errors.New("--raw-limit must be between 1 and 64")), true
	}
	if bands := a.args.IntPtr("bands"); bands != nil && (*bands < 0 || *bands > 64) {
		return a.fail(errors.New("--bands must be between 0 and 64")), true
	}
	cue := a.args.String("cue")
	if cue == "" && a.args.String("result_mode") != "recent" {
		return a.fail(errors.New("recall requires --q")), true
	}
	var timeRef any
	if value, ok := a.args.LookupString("time_ref"); ok {
		timeRef = value
	}
	var presence *text.PresenceCoordinates
	pLocus := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_PRESENCE_LOCUS"))
	pCarrier := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_PRESENCE_CARRIER"))
	pInc := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_PRESENCE_INCARNATION"))
	if pLocus != "" || pCarrier != "" || pInc != "" {
		coords := text.PresenceCoordinates{
			World:       "actual",
			Carrier:     pCarrier,
			Locus:       pLocus,
			Incarnation: pInc,
		}.Normalized()
		if !coords.IsEmpty() {
			presence = &coords
		}
	}
	scopePtr := a.args.StringPtr("scope")
	if scopePtr == nil && a.args.StringPtr("config") != nil && strings.TrimSpace(cfg.Scope) != "" {
		s := strings.TrimSpace(cfg.Scope)
		scopePtr = &s
	}
	readerPtr := a.args.StringPtr("reader")
	if readerPtr == nil && a.args.StringPtr("config") != nil && strings.TrimSpace(cfg.Reader) != "" {
		r := strings.TrimSpace(cfg.Reader)
		readerPtr = &r
	}
	isJSONOrProbe := a.args.Flag("json") || a.args.String("result_mode") == "probe"
	renderMaxChars := 0
	if chars := a.args.IntPtr("chars"); chars != nil && *chars != 0 {
		renderMaxChars = *chars
	}
	payload := mcp.UnixRecallPayload{
		DB:             cfg.DB,
		Cue:            cue,
		Scope:          scopePtr,
		Reader:         readerPtr,
		Mode:           a.args.String("result_mode"),
		CapsBands:      a.args.IntPtr("bands"),
		CapsNodes:      a.args.IntPtr("nodes"),
		CapsChars:      a.args.IntPtr("chars"),
		Offset:         a.args.Int("offset"),
		IncludeRaw:     a.args.FlagPtr("include_raw"),
		BudgetMS:       a.args.IntPtr("budget_ms"),
		RawLimit:       a.args.Int("raw_limit"),
		TimeRef:        timeRef,
		Presence:       presence,
		NoCrystallize:  a.args.Flag("no_crystallize"),
		RenderMaxChars: renderMaxChars,
		NeedRendered:   !isJSONOrProbe,
	}
	resp, err := mcp.CallUnixDaemonRecall(context.Background(), "", cfg, payload)
	if err != nil {
		var notSent *mcp.DaemonRecallNotSentError
		if errors.As(err, &notSent) {
			return 0, false
		}
		return a.fail(err), true
	}
	if isJSONOrProbe {
		if a.args.String("result_mode") == "probe" {
			if err := a.printJSON(resp.Probe); err != nil {
				return a.fail(err), true
			}
			return 0, true
		}
		if err := a.printJSON(resp.Result); err != nil {
			return a.fail(err), true
		}
		return 0, true
	}
	a.printf("%s\n", resp.Rendered)
	return 0, true
}

func (a *application) runRecall(memory *facade.Memory) (int, error) {
	if a.args.Flag("stdin_batch") {
		return a.runRecallStdinBatch(memory)
	}
	if limit := a.args.Int("raw_limit"); limit < 1 || limit > 64 {
		return 1, errors.New("--raw-limit must be between 1 and 64")
	}
	if bands := a.args.IntPtr("bands"); bands != nil && (*bands < 0 || *bands > 64) {
		return 1, errors.New("--bands must be between 0 and 64")
	}
	cue := a.args.String("cue")
	// The time-ordered recent primitive is cue-less by design (pure time order).
	if cue == "" && a.args.String("result_mode") != "recent" {
		return 1, errors.New("recall requires --q")
	}
	var timeRef any
	if value, ok := a.args.LookupString("time_ref"); ok {
		timeRef = value
	}
	var presence *text.PresenceCoordinates
	pLocus := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_PRESENCE_LOCUS"))
	pCarrier := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_PRESENCE_CARRIER"))
	pInc := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_PRESENCE_INCARNATION"))
	if pLocus != "" || pCarrier != "" || pInc != "" {
		coords := text.PresenceCoordinates{
			World:       "actual",
			Carrier:     pCarrier,
			Locus:       pLocus,
			Incarnation: pInc,
		}.Normalized()
		if !coords.IsEmpty() {
			presence = &coords
		}
	}
	result, err := memory.Recall(facade.RecallRequest{
		Cue:           cue,
		Reader:        a.args.StringPtr("reader"),
		Mode:          a.args.String("result_mode"),
		CapsBands:     a.args.IntPtr("bands"),
		CapsNodes:     a.args.IntPtr("nodes"),
		CapsChars:     a.args.IntPtr("chars"),
		Offset:        a.args.Int("offset"),
		IncludeRaw:    a.args.FlagPtr("include_raw"),
		BudgetMS:      a.args.IntPtr("budget_ms"),
		RawLimit:      a.args.Int("raw_limit"),
		TimeRef:       timeRef,
		Presence:      presence,
		NoCrystallize: a.args.Flag("no_crystallize"),
	})
	if err != nil {
		return 1, err
	}
	// A one-shot process must not exit before the L3 crystallization pulse it scheduled:
	// the wait is bounded and normally instant, and dropping it would leave the engine
	// without the noetic node this recall just produced.
	defer facade.WaitForPendingNoeticPulses(facade.NoeticPulseBudget)
	if a.args.Flag("json") || a.args.String("result_mode") == "probe" {
		payload := result.ToDict()
		if a.args.String("result_mode") == "probe" {
			payload = result.ToProbe()
		}
		return 0, a.printJSON(payload)
	}
	maxChars := memory.Config.Recall.Chars
	if chars := a.args.IntPtr("chars"); chars != nil && *chars != 0 {
		maxChars = *chars
	}
	a.printf("%s\n", result.Render(maxChars))
	return 0, nil
}

// ------------------------------------------------------------------- config

func (a *application) runInit() int {
	if portableDir := strings.TrimSpace(a.args.String("portable")); portableDir != "" {
		return a.runInitPortable(portableDir)
	}
	db := a.args.StringPtr("db")
	if a.args.Flag("project") && (db == nil || *db == "") {
		cwd, err := os.Getwd()
		if err != nil {
			return a.fail(err)
		}
		value := filepath.Join(cwd, ".rolling-memory", "memory.db")
		db = &value
	}
	cfg, err := config.Load(config.LoadOptions{
		Path:  a.args.StringPtr("config"),
		DB:    db,
		Mode:  a.args.StringPtr("mode"),
		Scope: a.args.StringPtr("scope"),
	})
	if err != nil {
		return a.fail(err)
	}
	if err := cfg.Ensure(); err != nil {
		return a.fail(err)
	}
	if a.args.Flag("write_config") {
		if _, err := config.WriteDefault(cfg.Path); err != nil {
			return a.fail(err)
		}
		if db != nil && *db != "" {
			if _, err := config.SetConfigValue(cfg.Path, "core.db", cfg.DB); err != nil {
				return a.fail(err)
			}
		}
		if scope, ok := a.args.LookupString("scope"); ok && scope != "" {
			if _, err := config.SetConfigValue(cfg.Path, "core.scope", cfg.Scope); err != nil {
				return a.fail(err)
			}
		}
	}
	memory, err := facade.New(cfg)
	if err != nil {
		return a.fail(err)
	}
	// 初始化即物化端侧金库 schema（含与 RB 存储同构的 rm_docs / rm_terms 规范表）：
	// 交付面的数据库标准在 init 当刻即确定，不推迟到首次真正使用（import/recall）才创建。
	if _, err := memory.LocalVault(); err != nil {
		return a.fail(err)
	}
	status, statusErr := memory.Status(cfg.Scope)
	closeErr := memory.Close()
	if statusErr != nil {
		return a.fail(statusErr)
	}
	if closeErr != nil {
		return a.fail(closeErr)
	}
	result := map[string]any{
		"ok":             true,
		"db":             cfg.DB,
		"config":         cfg.Path,
		"scope":          cfg.Scope,
		"mode":           cfg.Mode,
		"schema_version": status["schema_version"],
	}
	if err := a.emit(result, func() { a.printInit(result) }); err != nil {
		return a.fail(err)
	}
	return 0
}

func (a *application) runMode() int {
	cfg, err := config.Load(config.LoadOptions{
		Path:  a.args.StringPtr("config"),
		DB:    a.args.StringPtr("db"),
		Mode:  a.args.StringPtr("mode"),
		Scope: a.args.StringPtr("scope"),
	})
	if err != nil {
		return a.fail(err)
	}
	if name := a.args.String("name"); name != "" {
		if _, err := config.SetConfigValue(cfg.Path, "mode.name", name); err != nil {
			return a.fail(err)
		}
		mode := name
		cfg, err = config.Load(config.LoadOptions{
			Path:  a.args.StringPtr("config"),
			DB:    a.args.StringPtr("db"),
			Mode:  &mode,
			Scope: a.args.StringPtr("scope"),
		})
		if err != nil {
			return a.fail(err)
		}
	}
	result := map[string]any{"ok": true, "mode": cfg.Mode, "egress": "loopback-only", "config": cfg.Path}
	if err := a.emit(result, func() {
		a.printf("mode: %s (loopback-only)\n", cfg.Mode)
	}); err != nil {
		return a.fail(err)
	}
	return 0
}

func (a *application) runConfig() int {
	cfg, err := config.Load(config.LoadOptions{
		Path:  a.args.StringPtr("config"),
		DB:    a.args.StringPtr("db"),
		Mode:  a.args.StringPtr("mode"),
		Scope: a.args.StringPtr("scope"),
	})
	if err != nil {
		return a.fail(err)
	}
	switch a.args.Subcommand() {
	case "path":
		a.printLine(cfg.Path)
		return 0
	case "set":
		path, err := config.SetConfigValue(cfg.Path, a.args.String("key"), a.args.String("value"))
		if err != nil {
			return a.fail(err)
		}
		a.printf("updated %s: %s = %s\n", path, a.args.String("key"), a.args.String("value"))
		return 0
	default:
		if err := a.printJSON(cfg.PublicDict()); err != nil {
			return a.fail(err)
		}
		return 0
	}
}

// -------------------------------------------------------------- surface faces

func (a *application) runMCP() int {
	a.setEnvFromArgs()
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		return a.fail(err)
	}
	if cfg.Mode != "local" {
		return a.fail(errors.New("mcp requires local mode and local_plain"))
	}
	if a.args.Flag("daemon") {
		mem, err := a.openMemory()
		if err != nil {
			return a.fail(err)
		}
		defer func() { _ = mem.Close() }()
		mem.SetSnapshotOnly(true)
		if err := mcp.ServeUnix(mcp.DefaultUnixSocketPath(), mem, func(ctx context.Context) {
			go a.backgroundSnapshotWarmup(ctx, mem)
			a.superviseIngestWorker(ctx, mem)
		}); err != nil {
			a.errorf("error: %s\n", ids.Redact(err.Error()))
			return 1
		}
		return 0
	}
	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	if a.args.Flag("http") {
		go a.backgroundMCPAutoWatch(watchCtx)
		// The URL transports keep one long-lived process: the lazy memory opens
		// on the first tool call and reopens if the database file is replaced.
		if err := mcp.ServeHTTP(a.args.String("addr"), a.args.String("path"), nil); err != nil {
			a.errorf("error: %s\n", ids.Redact(err.Error()))
			return 1
		}
		return 0
	}
	if mcp.ShouldUseUnixDaemon(a.args.String("db")) {
		if err := mcp.ServeWithDaemonProxyAt(a.stdin, a.stdout, config.DefaultStateDir()); err != nil {
			if isBrokenPipe(err) {
				return 0
			}
			a.errorf("error: %s\n", ids.Redact(err.Error()))
			return 1
		}
		return 0
	}
	go a.backgroundMCPAutoWatch(watchCtx)
	if err := mcp.ServeAt(a.stdin, a.stdout, nil, config.DefaultStateDir()); err != nil {
		if isBrokenPipe(err) {
			return 0
		}
		// stdout carries the protocol; diagnostics only go to stderr.
		a.errorf("error: %s\n", ids.Redact(err.Error()))
		return 1
	}
	return 0
}

func (a *application) runHook() int {
	if value := a.args.String("config"); value != "" {
		os.Setenv("ROLLING_MEMORY_CONFIG", config.ExpandUserPath(value))
	}
	hookArgs := []string{a.args.String("event")}
	if a.args.Flag("json") {
		hookArgs = append(hookArgs, "--json")
	}
	if budget := a.args.IntPtr("budget_ms"); budget != nil && *budget != 0 {
		hookArgs = append(hookArgs, "--budget-ms", strconv.Itoa(*budget))
	}
	return hooks.HookMain(hookArgs, a.stdin, a.stdout, a.stderr)
}

// setEnvFromArgs exports CLI flag overrides into ROLLING_MEMORY_* environment variables.
func (a *application) setEnvFromArgs() {
	for _, pair := range []struct{ name, env string }{
		{"config", "ROLLING_MEMORY_CONFIG"},
		{"db", "ROLLING_MEMORY_DB"},
		{"scope", "ROLLING_MEMORY_SCOPE"},
		{"mode", "ROLLING_MEMORY_MODE"},
	} {
		if value := a.args.String(pair.name); value != "" {
			os.Setenv(pair.env, value)
		}
	}
}
