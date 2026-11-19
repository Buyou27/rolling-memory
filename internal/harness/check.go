// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rocareer/rolling-memory/internal/ids"
)

// Check inspects registrations and, when live, probes the selected launcher on
// a temporary database. The launcher probe is not a host registration probe.
func Check(harnesses []Harness, options InstallOptions, live bool) *Report {
	report := newReport("check", options)
	if err := ValidateInstallOptions(report.Options); err != nil {
		report.Add("*", "binary", "-", StatusError, err.Error())
		return report
	}
	report.Options.Lang = resolveInstallLang(report.Options)
	checkLauncher(report.Options, report)
	if live {
		server := Handshake(report.Options, 30*time.Second)
		report.Extra["server"] = server
		launcher, _ := server["launcher"].(string)
		if server["ok"] != true {
			detail, _ := server["error"].(string)
			report.Add("*", "server", launcher, StatusError, detail)
		} else {
			report.Add("*", "server", launcher, StatusSkipped, "selected launcher probe only; host registrations are not executed")
		}
	}
	for _, harness := range harnesses {
		start := len(report.Actions)
		if !report.Options.MCPOnly {
			checkSkill(harness, report.Options, report)
		}
		checkMCP(harness, report.Options, report)
		if !report.Options.MCPOnly {
			checkHooks(harness, report.Options, report)
			checkInstructions(harness, report.Options, report)
		}
		plugin, hasPlugin := activeNativePlugin(harness, report.Options)
		runtimeUnverified := false
		for i := start; i < len(report.Actions); i++ {
			action := &report.Actions[i]
			if action.Component == "mcp" && action.Status == StatusOK &&
				!(hasPlugin && action.Detail == "plugin "+plugin.ID) {
				if err := validateMCPRegistration(harness, report.Options, action.Path); err != nil {
					action.Status = StatusError
					action.Detail = "invalid registration: " + err.Error()
				} else {
					runtimeUnverified = true
					detail := action.Detail
					action.Detail = "configured; runtime not verified"
					if detail != "" {
						action.Detail += " (" + detail + ")"
					}
				}
			}
			if hasPlugin && (action.Detail == "plugin "+plugin.ID ||
				(action.Component == "hooks" && action.Detail == "1 event · plugin "+plugin.ID) ||
				(action.Status == StatusConflict && strings.Contains(action.Detail, "plugin "+plugin.ID))) {
				if action.Status == StatusOK {
					action.Status = StatusSkipped
				}
				if action.Component == "mcp" {
					runtimeUnverified = true
				}
				action.Detail += "; plugin " + plugin.ID + " declared, host loading and runtime not verified"
			}
		}
		if live && runtimeUnverified {
			report.Add(harness.ID, "mcp-runtime", "-", StatusSkipped, "host registration not executed; runtime unverified")
		}
	}
	return report
}

func resolveLauncherPath(value string) (string, error) {
	if filepath.IsAbs(value) || strings.ContainsAny(value, `/\`) {
		return value, nil
	}
	return exec.LookPath(value)
}

// checkLauncher reports whether the command every host registration points at
// is present and current. It is the static contract that used to pass when the
// launcher was missing; a live handshake is reported separately.
func checkLauncher(options InstallOptions, report *Report) {
	if options.Binary != "" {
		path := options.ResolvedBinary()
		resolved, resolveErr := resolveLauncherPath(path)
		if resolveErr != nil {
			report.Add("*", "binary", path, StatusError, "custom launcher unavailable: "+resolveErr.Error())
			return
		}
		info, err := os.Stat(resolved)
		if err != nil {
			report.Add("*", "binary", path, StatusError, "custom launcher unavailable: "+err.Error())
			return
		}
		if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
			report.Add("*", "binary", path, StatusError, "custom launcher is not an executable file")
			return
		}
		report.Add("*", "binary", path, StatusOK, "custom launcher")
		return
	}
	path := DefaultShimPath(options.Home)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			report.Add("*", "binary", path, StatusMissing, "launcher missing; run `rolling-memory install`")
			return
		}
		report.Add("*", "binary", path, StatusError, "cannot inspect launcher: "+err.Error())
		return
	}
	if !info.Mode().IsRegular() {
		report.Add("*", "binary", path, StatusConflict, "launcher is not a regular file")
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		report.Add("*", "binary", path, StatusError, "cannot read launcher: "+err.Error())
		return
	}
	if string(raw) == expectedShimText() {
		report.Add("*", "binary", path, StatusOK, "")
		return
	}
	if generatedShim(string(raw)) {
		report.Add("*", "binary", path, StatusOutdated, "launcher points at a different binary; run `rolling-memory install`")
		return
	}
	report.Add("*", "binary", path, StatusConflict, "file is not our shim; not touching it")
}

// validateMCPRegistration checks the stored registration without executing it.
// A successful validation says only that its static fields and launcher exist.
func validateMCPRegistration(harness Harness, options InstallOptions, path string) error {
	var entry any
	switch harness.MCPKind {
	case "json":
		data, err := loadJSONFile(path)
		if err != nil {
			return err
		}
		keys := append(append([]string{}, harness.MCPKeys...), options.Name)
		var found bool
		entry, found = jsonLookup(data, keys)
		if !found {
			return fmt.Errorf("server entry not found")
		}
	case "toml":
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data, err := parseTOML(string(raw))
		if err != nil {
			return err
		}
		if len(harness.MCPKeys) == 0 {
			return fmt.Errorf("server table not found")
		}
		table, ok := data[harness.MCPKeys[0]].(map[string]any)
		if !ok {
			return fmt.Errorf("server table not found")
		}
		entry = table[options.Name]
	case "dsh":
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		begin, end := dshMarkers(options.Name)
		start, finish, found, malformed := dshMarkerRegion(text, begin, end)
		if !found || malformed || !dshRegionIsOurs(text[start:finish], options) {
			return fmt.Errorf("invalid profile registration")
		}
		lines := pySplitLines(text[start:finish])
		var command string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[7], "        command: ")), &command); err != nil {
			return fmt.Errorf("invalid command: %w", err)
		}
		return validateMCPExecutable(command, options.Cwd)
	default:
		return fmt.Errorf("unsupported registration format")
	}
	object, ok := asObject(entry)
	if !ok {
		return fmt.Errorf("server entry is not an object")
	}
	commandValue, _ := objectGet(object, "command")
	command, commandOK := commandValue.(string)
	argsValue, argsFound := objectGet(object, "args")
	args, argsOK := asArray(argsValue)
	if commandList, listOK := asArray(commandValue); listOK {
		if harness.ID != "opencode" || len(commandList) != 2 || commandList[1] != "mcp" || argsFound {
			return fmt.Errorf("command must be [executable, mcp]")
		}
		command = pyString(commandList[0])
		commandOK = true
		args = []any{"mcp"}
		argsOK = true
	}
	if !commandOK || strings.TrimSpace(command) == "" {
		return fmt.Errorf("command is missing")
	}
	if value, found := objectGet(object, "type"); found {
		text, ok := value.(string)
		want := "stdio"
		if harness.ID == "opencode" {
			want = "local"
		}
		if !ok || text != want {
			return fmt.Errorf("transport type is not %s", want)
		}
	} else if harness.ID == "opencode" {
		return fmt.Errorf("transport type is missing")
	}
	if value, found := objectGet(object, "transport"); found {
		text, ok := value.(string)
		if !ok || text != "stdio" {
			return fmt.Errorf("transport is not stdio")
		}
	}
	for _, key := range []string{"enabled", "disabled"} {
		if value, found := objectGet(object, key); found {
			flag, ok := value.(bool)
			if !ok || (key == "enabled" && !flag) || (key == "disabled" && flag) {
				return fmt.Errorf("server %s state is invalid or disabled", key)
			}
		}
	}
	if !argsFound && harness.ID != "opencode" || !argsOK || len(args) != 1 {
		if harness.ID == "opencode" {
			return fmt.Errorf("expected command [executable, mcp]")
		}
		return fmt.Errorf("expected args [mcp]")
	}
	arg, ok := args[0].(string)
	if !ok || arg != "mcp" {
		return fmt.Errorf("expected args [mcp]")
	}
	envKey := "env"
	if harness.ID == "opencode" {
		envKey = "environment"
	}
	if value, found := objectGet(object, envKey); found {
		env, ok := asObject(value)
		if !ok {
			return fmt.Errorf("env is not an object")
		}
		for _, item := range env {
			text, ok := item.Value.(string)
			if !ok {
				return fmt.Errorf("env %s is not a string", item.Key)
			}
			switch item.Key {
			case "ROLLING_MEMORY_MODE":
				if text != "local" {
					return fmt.Errorf("env %s must be local", item.Key)
				}
			case "ROLLING_MEMORY_DB", "ROLLING_MEMORY_SCOPE":
				if text == "" {
					return fmt.Errorf("env %s is empty", item.Key)
				}
			}
		}
	}
	return validateMCPExecutable(command, options.Cwd)
}

func validateMCPExecutable(command, cwd string) error {
	path := command
	if !filepath.IsAbs(path) && !strings.ContainsAny(path, `/\\`) {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return fmt.Errorf("command not found: %s", command)
		}
		path = resolved
	} else if !filepath.IsAbs(path) {
		if cwd == "" {
			return fmt.Errorf("relative command has no working directory")
		}
		path = filepath.Join(cwd, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("command unavailable: %w", err)
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
		return fmt.Errorf("command is not an executable file: %s", command)
	}
	return nil
}

// SelectHarnesses resolves explicit ids (unknown ids are an error) or filters
// the registry by detection.
func SelectHarnesses(registry []Harness, names []string, detectedOnly bool) ([]Harness, error) {
	if len(names) > 0 {
		index := harnessIndex(registry)
		var extIndex map[string]Harness
		if len(registry) > 0 {
			home := filepath.Dir(registry[0].Detect)
			extIndex = harnessIndex(BuildExtendedHarnesses(home))
		}
		for _, name := range names {
			if _, ok := index[name]; ok {
				continue
			}
			if extIndex != nil {
				if _, ok := extIndex[name]; ok {
					continue
				}
			}
			known := make([]string, 0, len(registry))
			for _, harness := range registry {
				known = append(known, harness.ID)
			}
			return nil, fmt.Errorf("unknown harness %s; known: %s", ids.QuotePyString(name), strings.Join(known, ", "))
		}
		selected := make([]Harness, 0, len(names))
		for _, name := range names {
			if h, ok := index[name]; ok {
				selected = append(selected, h)
			} else if extIndex != nil {
				if h, ok := extIndex[name]; ok {
					selected = append(selected, h)
				}
			}
		}
		return selected, nil
	}
	if detectedOnly {
		selected := make([]Harness, 0, len(registry))
		for _, harness := range registry {
			if fileExists(harness.Detect) {
				selected = append(selected, harness)
			}
		}
		return selected, nil
	}
	return append([]Harness(nil), registry...), nil
}

// InstalledHarnesses returns the harnesses where an install of options.Name is
// visible (skill file present, or the MCP config mentions the name).
func InstalledHarnesses(harnesses []Harness, options InstallOptions) []Harness {
	options = options.normalized()
	result := make([]Harness, 0, len(harnesses))
	for _, harness := range harnesses {
		found := false
		if !options.MCPOnly {
			for _, target := range skillTargets(harness, options) {
				if fileExists(target) {
					found = true
					break
				}
			}
		}
		if !found {
			for _, path := range mcpTargetPaths(harness, options) {
				if !fileExists(path) {
					continue
				}
				text := ""
				if raw, err := os.ReadFile(path); err == nil {
					text = strings.ToValidUTF8(string(raw), "\uFFFD")
				}
				if strings.Contains(text, options.Name) {
					found = true
					break
				}
			}
		}
		if !found && !options.MCPOnly {
			_, found = activeNativePlugin(harness, options)
		}
		if found {
			result = append(result, harness)
		}
	}
	return result
}

// DescribeHarnesses returns the --list rows.
func DescribeHarnesses(harnesses []Harness, options InstallOptions) []map[string]any {
	options = options.normalized()
	rows := make([]map[string]any, 0, len(harnesses))
	for _, harness := range harnesses {
		skill := "-"
		if options.Project && harness.SkillProject != "" {
			skill = filepath.Join(options.Cwd, filepath.FromSlash(harness.SkillProject))
		} else if harness.SkillUser != nil {
			skill = *harness.SkillUser
		}
		mcp := "-"
		if path, ok := mcpTargetPath(harness, options); ok {
			mcp = path
		}
		hooks := "-"
		if harness.HooksKind != "" && harness.HooksPath != nil {
			hooks = *harness.HooksPath
		}
		instructions := "-"
		if options.Project && harness.InstructionsProject != "" {
			instructions = filepath.Join(options.Cwd, filepath.FromSlash(harness.InstructionsProject))
		} else if harness.InstructionsUser != nil {
			instructions = *harness.InstructionsUser
		}
		rows = append(rows, map[string]any{
			"id":           harness.ID,
			"label":        harness.Label,
			"detected":     fileExists(harness.Detect),
			"detect":       harness.Detect,
			"skill":        skill,
			"mcp":          mcp,
			"hooks":        hooks,
			"instructions": instructions,
			"notes":        harness.Notes,
		})
	}
	return rows
}
