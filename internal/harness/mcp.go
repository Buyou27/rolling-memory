// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
	"github.com/rocareer/rolling-memory/internal/ids"
	"gopkg.in/yaml.v3"
)

// The MCP surface has three writers: a JSON deep merge (zcode / claude-code /
// codebuddy), a TOML section replacement (codex) and a marker block inside the
// dsh profile patch.

func mcpEnv(options InstallOptions) ids.OrderedMap {
	env := make(ids.OrderedMap, 0, 2)
	if options.DB != "" {
		env = append(env, ids.JSONPair{Key: "ROLLING_MEMORY_DB", Value: options.DB})
	}
	if options.Scope != "" {
		env = append(env, ids.JSONPair{Key: "ROLLING_MEMORY_SCOPE", Value: options.Scope})
	}
	return env
}

// mcpEntry is the JSON shape written into zcode / claude-code / codebuddy.
func mcpEntry(options InstallOptions) ids.OrderedMap {
	entry := ids.OrderedMap{
		{Key: "type", Value: "stdio"},
		{Key: "command", Value: options.ResolvedBinary()},
		{Key: "args", Value: []any{"mcp"}},
	}
	if env := mcpEnv(options); len(env) > 0 {
		entry = append(entry, ids.JSONPair{Key: "env", Value: env})
	}
	return entry
}

func tomlBlock(options InstallOptions) (string, error) {
	return tomlBlockWithTimeout(options, int64(30))
}

func tomlBlockWithTimeout(options InstallOptions, timeout int64) (string, error) {
	command, err := pyJSONString(options.ResolvedBinary())
	if err != nil {
		return "", err
	}
	name := options.Name
	if strings.Contains(name, ".") {
		name, err = pyJSONString(name)
		if err != nil {
			return "", err
		}
	}
	lines := []string{
		"[mcp_servers." + name + "]",
		"command = " + command,
		`args = ["mcp"]`,
		fmt.Sprintf("startup_timeout_sec = %d", timeout),
	}
	if env := mcpEnv(options); len(env) > 0 {
		lines = append(lines, "", "[mcp_servers."+name+".env]")
		for _, pair := range env {
			value, err := pyJSONString(pyString(pair.Value))
			if err != nil {
				return "", err
			}
			lines = append(lines, pair.Key+" = "+value)
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func dshMarkers(name string) (string, string) {
	return fmt.Sprintf("# >>> rolling-memory (%s) >>>", name),
		fmt.Sprintf("# <<< rolling-memory (%s) <<<", name)
}

func dshBlock(options InstallOptions) (string, error) {
	begin, end := dshMarkers(options.Name)
	command, err := pyJSONString(options.ResolvedBinary())
	if err != nil {
		return "", err
	}
	lines := []string{
		begin,
		"- insert:",
		"    - id: " + options.Name + "-mcp",
		"      name: '@deepseek-ai/dsh-mcp-client'",
		"      config:",
		"        serverName: " + options.Name,
		"        transport: stdio",
		"        command: " + command,
		"        args: ['mcp']",
		"        failOnStartupError: false",
		end,
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// isOurBinary reports whether value is the resolved launcher, the bare command
// name, or a launcher shim path for this platform.
func isOurBinary(value string, options InstallOptions) bool {
	resolved := options.ResolvedBinary()
	return value == resolved || value == "rolling-memory" || value == "rmem" || value == DefaultShimPath(options.Home)
}
func hasShimSuffixFor(value, goos string) bool {
	name := shimNameFor(goos)
	return strings.HasSuffix(value, "/"+name) || strings.HasSuffix(value, "\\"+name)
}

func entryIsOurs(entry any, options InstallOptions) bool {
	object, ok := asObject(entry)
	if !ok {
		return false
	}
	argsValue, _ := objectGet(object, "args")
	args, ok := asArray(argsValue)
	if !ok || len(args) != 1 || args[0] != "mcp" {
		return false
	}
	command, _ := objectGet(object, "command")
	return isOurBinary(pyString(command), options)
}

func jsonEntryIsManaged(entry any, options InstallOptions) bool {
	if !entryIsOurs(entry, options) {
		return false
	}
	object, ok := asObject(entry)
	if !ok {
		return false
	}
	for _, pair := range object {
		switch pair.Key {
		case "command", "args":
		case "type":
			if pair.Value != "stdio" {
				return false
			}
		case "env":
			env, ok := asObject(pair.Value)
			if !ok {
				return false
			}
			for _, item := range env {
				if (item.Key != "ROLLING_MEMORY_DB" && item.Key != "ROLLING_MEMORY_SCOPE") || item.Value == nil {
					return false
				}
				if _, ok := item.Value.(string); !ok {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func entryCommand(entry any) string {
	if object, ok := asObject(entry); ok {
		if command, found := objectGet(object, "command"); found {
			return pyString(command)
		}
	}
	return "None"
}

func hookIsOurs(command string, options InstallOptions) bool {
	for _, sub := range []string{"recall", "start", "stop"} {
		suffix := " hook " + sub + " --json"
		if !strings.HasSuffix(command, suffix) {
			continue
		}
		binary := strings.TrimSuffix(command, suffix)
		return binary == shlexQuote(options.ResolvedBinary()) || binary == "rolling-memory" || binary == "rmem"
	}
	return false
}

// regionIsOurs: ours only when a binary/launcher marker is present -- the name
// alone is not proof.
func regionIsOurs(text string, options InstallOptions) bool {
	return strings.Contains(text, options.ResolvedBinary()) ||
		strings.Contains(text, "rolling_memory") ||
		strings.Contains(text, ShimName) ||
		strings.Contains(text, windowsShimName)
}

func mcpTargetPath(harness Harness, options InstallOptions) (string, bool) {
	if harness.MCPKind == "none" || harness.MCPKind == "" {
		return "", false
	}
	if harness.MCPKind == "dsh" {
		if options.Project || harness.ProfilesDir == nil || validateDSHProfile(options.withDefaults().DSHProfile) != nil {
			return "", false
		}
		return filepath.Join(*harness.ProfilesDir, options.withDefaults().DSHProfile, "cordis.patch.yml"), true
	}
	if options.Project {
		if harness.MCPProjectPath == nil {
			return "", false
		}
		return filepath.Join(options.Cwd, filepath.FromSlash(*harness.MCPProjectPath)), true
	}
	return harness.MCPPath, true
}

// mcpTargetPaths returns every path install/uninstall/check must cover for a
// harness: the primary target plus the legacy path when the host's read point is
// not fully settled. WorkBuddy is the one such harness: its connectors document
// ~/.workbuddy/mcp.json as the user config while earlier host builds were
// observed reading the dotted ~/.workbuddy/.mcp.json. Every writer here merges
// into the existing table and removes only entries it recognises as its own, so
// whatever the host already keeps in either file survives untouched.
func dshPathError(harness Harness, path string) error {
	if harness.MCPKind != "dsh" || harness.ProfilesDir == nil {
		return nil
	}
	for _, candidate := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink %s", candidate)
		}
	}
	return nil
}

func mcpTargetPaths(harness Harness, options InstallOptions) []string {
	primary, ok := mcpTargetPath(harness, options)
	if !ok {
		return nil
	}
	paths := []string{primary}
	if harness.MCPLegacyPath != "" && harness.MCPLegacyPath != primary && !options.Project {
		paths = append(paths, harness.MCPLegacyPath)
	}
	return paths
}

// mcpSkipNote names the truthful reason a harness has no writable MCP target.
func mcpSkipNote(harness Harness) string {
	if harness.MCPSkipReason != "" {
		return harness.MCPSkipReason
	}
	return "no MCP target for this harness/scope"
}

func installMCP(harness Harness, options InstallOptions, report *Report) {
	paths := mcpTargetPaths(harness, options)
	if len(paths) == 0 {
		report.Add(harness.ID, "mcp", "-", StatusSkipped, mcpSkipNote(harness))
		return
	}
	for _, path := range paths {
		if err := dshPathError(harness, path); err != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict, err.Error())
			continue
		}
		switch harness.MCPKind {
		case "json":
			installMCPJSON(harness, options, path, report)
		case "toml":
			installMCPToml(harness, options, path, report)
		case "dsh":
			installMCPDsh(harness, options, path, report)
		default:
			report.Add(harness.ID, "mcp", path, StatusError, "unknown mcp kind "+pyRepr(harness.MCPKind))
		}
	}
}

func uninstallMCP(harness Harness, options InstallOptions, report *Report) {
	paths := mcpTargetPaths(harness, options)
	if len(paths) == 0 {
		report.Add(harness.ID, "mcp", "-", StatusSkipped, mcpSkipNote(harness))
		return
	}
	for _, path := range paths {
		if err := dshPathError(harness, path); err != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict, err.Error())
			continue
		}
		if !fileExists(path) {
			// A secondary target the user never had is not a missing piece of
			// the installation; report on the primary one only.
			if path == paths[0] {
				report.Add(harness.ID, "mcp", path, StatusSkipped, "config file not present")
			}
			continue
		}
		switch harness.MCPKind {
		case "json":
			uninstallMCPJSON(harness, options, path, report)
		case "toml":
			uninstallMCPToml(harness, options, path, report)
		default:
			uninstallMCPDsh(harness, options, path, report)
		}
	}
}

func checkMCP(harness Harness, options InstallOptions, report *Report) {
	paths := mcpTargetPaths(harness, options)
	if len(paths) == 0 {
		report.Add(harness.ID, "mcp", "-", StatusSkipped, mcpSkipNote(harness))
		return
	}
	plugin, hasPluginMCP := activeNativePlugin(harness, options)
	hasPluginMCP = hasPluginMCP && plugin.HasMCP
	for _, path := range paths {
		if err := dshPathError(harness, path); err != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict, err.Error())
			continue
		}
		if !fileExists(path) {
			if harness.MCPKind == "dsh" && options.Name == DefaultName {
				selected, err := dshBundleSelected(path)
				if err != nil {
					report.Add(harness.ID, "mcp", path, StatusConflict, "cannot determine selected DSH bundles: "+err.Error())
					continue
				}
				if selected {
					report.Add(harness.ID, "mcp", path, StatusSkipped, "DSH bundle selected; activation unverified")
					continue
				}
			}
			if hasPluginMCP {
				if path == paths[0] {
					report.Add(harness.ID, "mcp", path, StatusConflict, "plugin "+plugin.ID+" declares MCP; host activation unverified")
				}
				continue
			}
			if path == paths[0] {
				report.Add(harness.ID, "mcp", path, StatusMissing, "config file not present")
			}
			continue
		}
		switch harness.MCPKind {
		case "json":
			checkMCPJSON(harness, options, path, report)
		case "toml":
			checkMCPToml(harness, options, path, report)
		default:
			checkMCPDsh(harness, options, path, report)
		}
	}
}

func jsonPathIsObject(data ids.OrderedMap, keys []string) bool {
	var value any = data
	for _, key := range keys {
		object, ok := asObject(value)
		if !ok {
			return false
		}
		value, ok = objectGet(object, key)
		if !ok {
			return true
		}
	}
	_, ok := asObject(value)
	return ok
}

func installMCPJSON(harness Harness, options InstallOptions, path string, report *Report) {
	if plugin, ok := activeNativePlugin(harness, options); ok && plugin.HasMCP && !options.Force {
		data, err := loadJSONFile(path)
		if err != nil {
			report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
			return
		}
		keys := append(append([]string{}, harness.MCPKeys...), options.Name)
		if existing, found := jsonLookup(data, keys); found && existing != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict, "existing MCP entry retained; plugin "+plugin.ID+" is also active")
			return
		}
		report.Add(harness.ID, "mcp", path, StatusConflict, "plugin "+plugin.ID+" declares an MCP entry; host activation is unverified")
		return
	}
	data, err := loadJSONFile(path)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	if !jsonPathIsObject(data, harness.MCPKeys) {
		report.Add(harness.ID, "mcp", path, StatusConflict, "MCP path contains a non-object value")
		return
	}
	table, _, ok := jsonTable(data, harness.MCPKeys, true)
	if !ok {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: missing JSON table")
		return
	}
	existing, found := objectGet(table, options.Name)
	present := found && existing != nil
	if present && jsonEntryIsManaged(existing, options) {
		object, _ := asObject(existing)
		if value, found := objectGet(object, "env"); found {
			env, _ := asObject(value)
			if options.DB == "" {
				if db, found := objectGet(env, "ROLLING_MEMORY_DB"); found {
					options.DB, _ = db.(string)
				}
			}
			if options.Scope == "" {
				if scope, found := objectGet(env, "ROLLING_MEMORY_SCOPE"); found {
					options.Scope, _ = scope.(string)
				}
			}
		}
	}
	entry := mcpEntry(options)
	if present && jsonEqual(existing, entry) {
		report.Add(harness.ID, "mcp", path, StatusUnchanged, "")
		return
	}
	if present && !jsonEntryIsManaged(existing, options) && !options.Force {
		report.Add(harness.ID, "mcp", path, StatusConflict,
			"'"+options.Name+"' has unverified ownership or user fields; not touching it")
		return
	}
	if options.DryRun {
		detail := ""
		if preview, err := dumpJSON(ids.OrderedMap{{Key: options.Name, Value: entry}}); err == nil {
			detail = oneLineJSON(preview)
		}
		report.Add(harness.ID, "mcp", path, StatusPlanned, detail)
		return
	}
	updated, _ := jsonTableWith(data, harness.MCPKeys, true, func(current ids.OrderedMap) ids.OrderedMap {
		return objectSet(current, options.Name, entry)
	})
	text, err := dumpJSON(updated)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot encode: "+err.Error())
		return
	}
	if _, err := writeText(path, text); err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot write: "+err.Error())
		return
	}
	status := StatusInstalled
	if present {
		status = StatusUpdated
	}
	report.Add(harness.ID, "mcp", path, status, "")
}

func uninstallMCPJSON(harness Harness, options InstallOptions, path string, report *Report) {
	data, err := loadJSONFile(path)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	table, _, ok := jsonTable(data, harness.MCPKeys, false)
	if !ok {
		report.Add(harness.ID, "mcp", path, StatusSkipped, "entry not present")
		return
	}
	existing, found := objectGet(table, options.Name)
	if !found || existing == nil {
		report.Add(harness.ID, "mcp", path, StatusSkipped, "entry not present")
		return
	}
	if !jsonEntryIsManaged(existing, options) {
		report.Add(harness.ID, "mcp", path, StatusConflict, "entry has unverified ownership or user fields; not touching it")
		return
	}
	if options.DryRun {
		report.Add(harness.ID, "mcp", path, StatusPlanned, "")
		return
	}
	updated, ok := jsonTableWith(data, harness.MCPKeys, false, func(current ids.OrderedMap) ids.OrderedMap {
		return objectDelete(current, options.Name)
	})
	if !ok {
		report.Add(harness.ID, "mcp", path, StatusSkipped, "entry not present")
		return
	}
	text, err := dumpJSON(updated)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot encode: "+err.Error())
		return
	}
	if _, err := writeText(path, text); err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot write: "+err.Error())
		return
	}
	report.Add(harness.ID, "mcp", path, StatusRemoved, "")
}

func checkMCPJSON(harness Harness, options InstallOptions, path string, report *Report) {
	data, err := loadJSONFile(path)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	keys := append(append([]string{}, harness.MCPKeys...), options.Name)
	entry, found := jsonLookup(data, keys)
	if plugin, ok := activeNativePlugin(harness, options); ok && plugin.HasMCP {
		if found && entry != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict,
				"duplicate MCP: both "+filepath.Base(path)+" and plugin "+plugin.ID+" active (run `rolling-memory install "+harness.ID+"` to deduplicate)")
			return
		}
		report.Add(harness.ID, "mcp", path, StatusConflict, "plugin "+plugin.ID+" declares MCP; host activation unverified")
		return
	}
	if !found || entry == nil {
		report.Add(harness.ID, "mcp", path, StatusMissing,
			strings.Join(append(append([]string{}, harness.MCPKeys...), options.Name), "."))
		return
	}
	if entryIsOurs(entry, options) {
		report.Add(harness.ID, "mcp", path, StatusOK, entryCommand(entry))
		return
	}
	report.Add(harness.ID, "mcp", path, StatusConflict, "same-named entry is not ours")
}

type tomlSpan struct {
	start   int
	end     int
	comment string
}

func tomlComments(node *unstable.Node, raw string, out *strings.Builder) {
	for node != nil {
		if node.Kind == unstable.Comment {
			start := int(node.Raw.Offset)
			end := start + int(node.Raw.Length)
			out.WriteString(raw[start:end])
			out.WriteByte('\n')
		}
		tomlComments(node.Child(), raw, out)
		node = node.Next()
	}
}

// tomlTableSpans identifies only complete table headers and their assignments.
// Comments and all bytes outside those expressions remain untouched.
func tomlTableSpans(raw string, target []string) ([]tomlSpan, error) {
	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset([]byte(raw))
	current := false
	var spans []tomlSpan
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind != unstable.Table && node.Kind != unstable.ArrayTable && node.Kind != unstable.KeyValue {
			continue
		}
		iter := node.Key()
		var key []string
		var keyStart, keyEnd int
		for iter.Next() {
			if len(key) == 0 {
				keyStart = int(iter.Node().Raw.Offset)
			}
			keyEnd = int(iter.Node().Raw.Offset + iter.Node().Raw.Length)
			key = append(key, string(iter.Node().Data))
		}
		if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			current = len(key) >= len(target) && reflect.DeepEqual(key[:len(target)], target)
		}
		if !current {
			continue
		}
		start := keyStart
		end := keyEnd
		if node.Kind == unstable.KeyValue {
			start = int(node.Raw.Offset)
			end = start + int(node.Raw.Length)
		}
		lineStart := strings.LastIndex(raw[:start], "\n") + 1
		prefix := strings.TrimSpace(raw[lineStart:start])
		if prefix == "" || ((node.Kind == unstable.Table || node.Kind == unstable.ArrayTable) && (prefix == "[" || prefix == "[[")) {
			start = lineStart
		} else {
			return nil, fmt.Errorf("cannot safely locate TOML expression")
		}
		lineEnd := strings.IndexByte(raw[end:], '\n')
		var comments strings.Builder
		if node.Kind == unstable.KeyValue {
			tomlComments(node.Value().Child(), raw, &comments)
		}
		if lineEnd >= 0 {
			lineEnd += end
		} else {
			lineEnd = len(raw)
		}
		if index := strings.IndexByte(raw[end:lineEnd], '#'); index >= 0 {
			comments.WriteString(raw[end+index : lineEnd])
			if lineEnd < len(raw) {
				comments.WriteByte('\n')
			}
		}
		end = lineEnd
		if end < len(raw) {
			end++
		}
		spans = append(spans, tomlSpan{start: start, end: end, comment: comments.String()})
	}
	return spans, parser.Error()
}

func tomlWithoutSpans(raw string, spans []tomlSpan) string {
	var out strings.Builder
	last := 0
	for _, span := range spans {
		out.WriteString(raw[last:span.start])
		out.WriteString(span.comment)
		last = span.end
	}
	out.WriteString(raw[last:])
	return out.String()
}

func tomlEntry(data map[string]any, harness Harness, name string) (any, bool) {
	var value any = data
	for _, key := range append(append([]string{}, harness.MCPKeys...), name) {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func tomlOtherFieldsEqual(before, after map[string]any, harness Harness, name string) bool {
	for _, data := range []map[string]any{before, after} {
		value := data
		parents := []map[string]any{data}
		for _, key := range harness.MCPKeys {
			child, ok := value[key].(map[string]any)
			if !ok {
				value = nil
				break
			}
			value = child
			parents = append(parents, child)
		}
		if value != nil {
			delete(value, name)
			for i := len(harness.MCPKeys) - 1; i >= 0; i-- {
				if len(parents[i+1]) != 0 {
					break
				}
				delete(parents[i], harness.MCPKeys[i])
			}
		}
	}
	return reflect.DeepEqual(before, after)
}

func tomlTableName(harness Harness, options InstallOptions) string {
	if len(harness.MCPKeys) > 0 {
		return harness.MCPKeys[0] + "." + options.Name
	}
	return options.Name
}

func installMCPToml(harness Harness, options InstallOptions, path string, report *Report) {
	desired, err := tomlBlock(options)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot encode: "+err.Error())
		return
	}
	table := tomlTableName(harness, options)
	original := ""
	if fileExists(path) {
		raw, err := os.ReadFile(path)
		if err != nil {
			report.Add(harness.ID, "mcp", path, StatusError, "cannot read: "+err.Error())
			return
		}
		original = string(raw)
	}
	before, err := parseTOML(original)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	entry, present := tomlEntry(before, harness, options.Name)
	if present && tomlEntryIsOurs(entry, options) {
		object := entry.(map[string]any)
		if env, ok := object["env"].(map[string]any); ok {
			if options.DB == "" {
				options.DB, _ = env["ROLLING_MEMORY_DB"].(string)
			}
			if options.Scope == "" {
				options.Scope, _ = env["ROLLING_MEMORY_SCOPE"].(string)
			}
		}
		if timeout, found := object["startup_timeout_sec"]; found {
			value, ok := timeout.(int64)
			if !ok {
				report.Add(harness.ID, "mcp", path, StatusConflict, "startup_timeout_sec is not an integer; not touching it")
				return
			}
			desired, err = tomlBlockWithTimeout(options, value)
		} else {
			desired, err = tomlBlock(options)
		}
		if err != nil {
			report.Add(harness.ID, "mcp", path, StatusError, "cannot encode: "+err.Error())
			return
		}
	}
	spans, err := tomlTableSpans(original, append(append([]string{}, harness.MCPKeys...), options.Name))
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	status := StatusInstalled
	text := original
	if present {
		if !tomlEntryIsOurs(entry, options) {
			report.Add(harness.ID, "mcp", path, StatusConflict, "["+table+"] has unverified ownership or fields; not touching it")
			return
		}
		if len(spans) == 0 {
			report.Add(harness.ID, "mcp", path, StatusConflict, "entry is not represented by standalone tables")
			return
		}
		desiredData, _ := parseTOML(desired)
		desiredEntry, _ := tomlEntry(desiredData, harness, options.Name)
		if reflect.DeepEqual(entry, desiredEntry) {
			report.Add(harness.ID, "mcp", path, StatusUnchanged, "")
			return
		}
		remaining := tomlWithoutSpans(original, spans)
		text = remaining[:spans[0].start] + desired + remaining[spans[0].start:]
		status = StatusUpdated
	} else {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if strings.TrimSpace(text) != "" {
			text += "\n"
		}
		text += desired
	}
	after, err := parseTOML(text)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "refusing to write invalid TOML: "+err.Error())
		return
	}
	desiredData, _ := parseTOML(desired)
	desiredEntry, _ := tomlEntry(desiredData, harness, options.Name)
	actualEntry, found := tomlEntry(after, harness, options.Name)
	if !found || !reflect.DeepEqual(actualEntry, desiredEntry) {
		report.Add(harness.ID, "mcp", path, StatusConflict, "entry has fields outside standalone tables")
		return
	}
	before, _ = parseTOML(original)
	if !tomlOtherFieldsEqual(before, after, harness, options.Name) {
		report.Add(harness.ID, "mcp", path, StatusConflict, "refusing to change unrelated TOML fields")
		return
	}
	if options.DryRun {
		report.Add(harness.ID, "mcp", path, StatusPlanned, tomlPreview(desired))
		return
	}
	if _, err := writeText(path, text); err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot write: "+err.Error())
		return
	}
	report.Add(harness.ID, "mcp", path, status, "")
}

func tomlPreview(desired string) string {
	return strings.ReplaceAll(strings.TrimSpace(desired), "\n", " | ")
}

func uninstallMCPToml(harness Harness, options InstallOptions, path string, report *Report) {
	raw, err := os.ReadFile(path)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot read: "+err.Error())
		return
	}
	original := string(raw)
	before, err := parseTOML(original)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	entry, found := tomlEntry(before, harness, options.Name)
	if !found {
		report.Add(harness.ID, "mcp", path, StatusSkipped, "entry not present")
		return
	}
	if !tomlEntryIsOurs(entry, options) {
		report.Add(harness.ID, "mcp", path, StatusConflict, "entry is not ours; not touching it")
		return
	}
	spans, err := tomlTableSpans(original, append(append([]string{}, harness.MCPKeys...), options.Name))
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	if len(spans) == 0 {
		report.Add(harness.ID, "mcp", path, StatusConflict, "entry is not represented by standalone tables")
		return
	}
	text := tomlWithoutSpans(original, spans)
	after, err := parseTOML(text)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "refusing to write invalid TOML: "+err.Error())
		return
	}
	if _, found := tomlEntry(after, harness, options.Name); found {
		report.Add(harness.ID, "mcp", path, StatusConflict, "entry has fields outside standalone tables")
		return
	}
	before, _ = parseTOML(original)
	if !tomlOtherFieldsEqual(before, after, harness, options.Name) {
		report.Add(harness.ID, "mcp", path, StatusConflict, "refusing to change unrelated TOML fields")
		return
	}
	if options.DryRun {
		report.Add(harness.ID, "mcp", path, StatusPlanned, "")
		return
	}
	if _, err := writeText(path, text); err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot write: "+err.Error())
		return
	}
	report.Add(harness.ID, "mcp", path, StatusRemoved, "")
}

func checkMCPToml(harness Harness, options InstallOptions, path string, report *Report) {
	raw, err := os.ReadFile(path)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	data, err := parseTOML(string(raw))
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	var table any
	if len(harness.MCPKeys) > 0 {
		table = data[harness.MCPKeys[0]]
	}
	tableObject, _ := table.(map[string]any)
	entry, found := tableObject[options.Name]
	if !found || entry == nil {
		report.Add(harness.ID, "mcp", path, StatusMissing,
			tomlTableName(harness, options))
		return
	}
	if tomlEntryIsOurs(entry, options) {
		report.Add(harness.ID, "mcp", path, StatusOK, entryCommand(entry))
		return
	}
	report.Add(harness.ID, "mcp", path, StatusConflict, "same-named entry is not ours")
}

func tomlEntryIsOurs(entry any, options InstallOptions) bool {
	if !entryIsOurs(entry, options) {
		return false
	}
	object, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	for key, value := range object {
		switch key {
		case "command", "args", "startup_timeout_sec":
		case "env":
			env, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for name := range env {
				if name != "ROLLING_MEMORY_DB" && name != "ROLLING_MEMORY_SCOPE" {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func dshMarkerRegion(text, begin, end string) (int, int, bool, bool) {
	starts := strings.Count(text, begin)
	ends := strings.Count(text, end)
	if starts == 0 && ends == 0 {
		return 0, 0, false, false
	}
	if starts != 1 || ends != 1 {
		return 0, 0, false, true
	}
	start := strings.Index(text, begin)
	finish := strings.Index(text, end)
	if finish < start+len(begin) || (start > 0 && text[start-1] != '\n') ||
		(finish > 0 && text[finish-1] != '\n') ||
		(finish+len(end) < len(text) && text[finish+len(end)] != '\n' && text[finish+len(end)] != '\r') {
		return 0, 0, false, true
	}
	return start, finish + len(end), true, false
}

func dshRegionIsOurs(region string, options InstallOptions) bool {
	lines := pySplitLines(region)
	if len(lines) != 11 || !strings.HasPrefix(lines[7], "        command: ") {
		return false
	}
	var command string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[7], "        command: ")), &command); err != nil || !isOurBinary(command, options) {
		return false
	}
	copyOptions := options
	copyOptions.Binary = command
	expected, err := dshBlock(copyOptions)
	return err == nil && strings.TrimSpace(region) == strings.TrimSpace(expected)
}

func dshBundleSelected(path string) (bool, error) {
	manifestPath := filepath.Join(filepath.Dir(path), "package.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	manifest, err := loadJSONFile(manifestPath)
	if err != nil {
		return false, err
	}
	value, found := jsonLookup(manifest, []string{"dsh", "profile", "bundles"})
	if !found {
		if !jsonPathIsObject(manifest, []string{"dsh", "profile"}) {
			return false, fmt.Errorf("invalid dsh profile manifest")
		}
		return false, nil
	}
	bundles, ok := asArray(value)
	if !ok {
		return false, fmt.Errorf("dsh.profile.bundles is not a list")
	}
	for _, bundle := range bundles {
		name, ok := bundle.(string)
		if !ok {
			return false, fmt.Errorf("dsh.profile.bundles contains a non-string value")
		}
		if name == "@rocareer/dsh-rolling-memory" {
			return true, nil
		}
	}
	return false, nil
}

func dshYAMLNode(text string) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(text))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple YAML documents are not supported")
		}
		return nil, err
	}
	if len(document.Content) != 1 {
		return nil, fmt.Errorf("invalid YAML document")
	}
	root := document.Content[0]
	if root.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("DSH patch must be a sequence")
	}
	return root, nil
}

func dshSameServer(node *yaml.Node, name string) (bool, error) {
	if node.Kind == yaml.AliasNode {
		return false, fmt.Errorf("YAML aliases require manual review")
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" {
				return false, fmt.Errorf("dynamic YAML key requires manual review")
			}
			if key.Value == "serverName" {
				if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
					return false, fmt.Errorf("dynamic serverName requires manual review")
				}
				if value.Value == name {
					return true, nil
				}
			}
			found, err := dshSameServer(value, name)
			if found || err != nil {
				return found, err
			}
		}
		return false, nil
	}
	for _, child := range node.Content {
		found, err := dshSameServer(child, name)
		if found || err != nil {
			return found, err
		}
	}
	return false, nil
}

func dshEmptySequenceOffset(text string, node *yaml.Node) (int, bool) {
	if node == nil || len(node.Content) != 0 || node.Style&yaml.FlowStyle == 0 {
		return 0, false
	}
	lines := strings.Split(text, "\n")
	if node.Line < 1 || node.Line > len(lines) {
		return 0, false
	}
	offset := 0
	for _, line := range lines[:node.Line-1] {
		offset += len(line) + 1
	}
	line := lines[node.Line-1]
	column := node.Column - 1
	if column < 0 || column+2 > len(line) || line[column:column+2] != "[]" {
		return 0, false
	}
	return offset + column, true
}

func installMCPDsh(harness Harness, options InstallOptions, path string, report *Report) {
	if !fileExists(filepath.Dir(path)) {
		report.Add(harness.ID, "mcp", path, StatusError, "dsh profile dir not found: "+filepath.Dir(path))
		return
	}
	selected := false
	if options.Name == DefaultName {
		var err error
		selected, err = dshBundleSelected(path)
		if err != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict, "cannot determine selected DSH bundles: "+err.Error())
			return
		}
	}
	detail := ""
	if options.DB != "" || options.Scope != "" {
		detail = "dsh entry cannot carry env; set core.db/core.scope with `rolling-memory config set`"
	}
	begin, end := dshMarkers(options.Name)
	block, err := dshBlock(options)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot encode: "+err.Error())
		return
	}
	original := ""
	if fileExists(path) {
		raw, err := os.ReadFile(path)
		if err != nil {
			report.Add(harness.ID, "mcp", path, StatusError, "cannot read: "+err.Error())
			return
		}
		original = string(raw)
	}
	regionStart, regionEnd, found, malformed := dshMarkerRegion(original, begin, end)
	if malformed {
		report.Add(harness.ID, "mcp", path, StatusConflict, "malformed marker block; not touching it")
		return
	}
	if selected {
		status := StatusSkipped
		if found {
			status = StatusConflict
		}
		report.Add(harness.ID, "mcp", path, status, "DSH profile selects @rocareer/dsh-rolling-memory; manual entry retained without changes (bundle activation unverified)")
		return
	}
	root, err := dshYAMLNode(original)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusConflict, "cannot safely parse DSH patch: "+err.Error())
		return
	}
	outside := original
	if found {
		outside = original[:regionStart] + original[regionEnd:]
	}
	if strings.TrimSpace(outside) != "" {
		other, err := dshYAMLNode(outside)
		if err != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict, "cannot safely inspect DSH patch: "+err.Error())
			return
		}
		if other != nil {
			duplicate, err := dshSameServer(other, options.Name)
			if err != nil || duplicate {
				detail := "profile contains a same-named server; manual review required"
				if err != nil {
					detail = "cannot safely inspect DSH patch: " + err.Error()
				}
				report.Add(harness.ID, "mcp", path, StatusConflict, detail)
				return
			}
		}
	}
	if found {
		current := original[regionStart:regionEnd]
		if strings.TrimSpace(current) == strings.TrimSpace(block) {
			report.Add(harness.ID, "mcp", path, StatusUnchanged, detail)
			return
		}
		if !dshRegionIsOurs(current, options) {
			report.Add(harness.ID, "mcp", path, StatusConflict, "marker block is not ours; not touching it")
			return
		}
		if options.DryRun {
			report.Add(harness.ID, "mcp", path, StatusPlanned, detail)
			return
		}
		text := original[:regionStart] + strings.TrimRight(block, "\n") + original[regionEnd:]
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if _, err := dshYAMLNode(text); err != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict, "refusing to write invalid DSH patch: "+err.Error())
			return
		}
		if _, err := writeText(path, text); err != nil {
			report.Add(harness.ID, "mcp", path, StatusError, "cannot write: "+err.Error())
			return
		}
		report.Add(harness.ID, "mcp", path, StatusUpdated, detail)
		return
	}
	text := original
	if root != nil && root.Style&yaml.FlowStyle != 0 {
		offset, ok := dshEmptySequenceOffset(original, root)
		if !ok {
			report.Add(harness.ID, "mcp", path, StatusConflict, "flow-style DSH patch requires manual review")
			return
		}
		text = original[:offset] + original[offset+2:]
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if strings.TrimSpace(text) != "" {
		text += "\n"
	}
	text += block
	if _, err := dshYAMLNode(text); err != nil {
		report.Add(harness.ID, "mcp", path, StatusConflict, "refusing to write invalid DSH patch: "+err.Error())
		return
	}
	if options.DryRun {
		report.Add(harness.ID, "mcp", path, StatusPlanned, detail)
		return
	}
	if _, err := writeText(path, text); err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot write: "+err.Error())
		return
	}
	report.Add(harness.ID, "mcp", path, StatusInstalled, detail)
}

func uninstallMCPDsh(harness Harness, options InstallOptions, path string, report *Report) {
	begin, end := dshMarkers(options.Name)
	raw, err := os.ReadFile(path)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot read: "+err.Error())
		return
	}
	original := string(raw)
	if _, err := dshYAMLNode(original); err != nil {
		report.Add(harness.ID, "mcp", path, StatusConflict, "cannot safely parse DSH patch: "+err.Error())
		return
	}
	regionStart, regionEnd, found, malformed := dshMarkerRegion(original, begin, end)
	if malformed {
		report.Add(harness.ID, "mcp", path, StatusConflict, "malformed marker block; not touching it")
		return
	}
	if !found {
		report.Add(harness.ID, "mcp", path, StatusSkipped, "marker block not present")
		return
	}
	if !dshRegionIsOurs(original[regionStart:regionEnd], options) {
		report.Add(harness.ID, "mcp", path, StatusConflict, "marker block is not ours; not touching it")
		return
	}
	if options.DryRun {
		report.Add(harness.ID, "mcp", path, StatusPlanned, "")
		return
	}
	if strings.HasPrefix(original[regionEnd:], "\r\n") {
		regionEnd += 2
	} else if strings.HasPrefix(original[regionEnd:], "\n") {
		regionEnd++
	}
	combined := original[:regionStart] + original[regionEnd:]
	root, err := dshYAMLNode(combined)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusConflict, "refusing to write invalid DSH patch: "+err.Error())
		return
	}
	if root == nil {
		if combined != "" && !strings.HasSuffix(combined, "\n") {
			combined += "\n"
		}
		combined += "[]\n"
	}
	if _, err := writeText(path, combined); err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot write: "+err.Error())
		return
	}
	report.Add(harness.ID, "mcp", path, StatusRemoved, "")
}

func checkMCPDsh(harness Harness, options InstallOptions, path string, report *Report) {
	selected := false
	if options.Name == DefaultName {
		var err error
		selected, err = dshBundleSelected(path)
		if err != nil {
			report.Add(harness.ID, "mcp", path, StatusConflict, "cannot determine selected DSH bundles: "+err.Error())
			return
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		report.Add(harness.ID, "mcp", path, StatusError, "cannot parse: "+err.Error())
		return
	}
	text := string(raw)
	if _, err := dshYAMLNode(text); err != nil {
		report.Add(harness.ID, "mcp", path, StatusConflict, "cannot safely parse DSH patch: "+err.Error())
		return
	}
	begin, end := dshMarkers(options.Name)
	start, finish, found, malformed := dshMarkerRegion(text, begin, end)
	if malformed {
		report.Add(harness.ID, "mcp", path, StatusConflict, "marker block present but malformed")
		return
	}
	if selected {
		status := StatusSkipped
		if found {
			status = StatusConflict
		}
		report.Add(harness.ID, "mcp", path, status, "DSH bundle selected; activation unverified")
		return
	}
	if !found {
		report.Add(harness.ID, "mcp", path, StatusMissing, "no '"+options.Name+"' block")
		return
	}
	if dshRegionIsOurs(text[start:finish], options) {
		report.Add(harness.ID, "mcp", path, StatusOK, "")
		return
	}
	report.Add(harness.ID, "mcp", path, StatusConflict, "marker block is not ours")
}

func parseTOML(raw string) (map[string]any, error) {
	var data map[string]any
	if err := toml.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	return data, nil
}
