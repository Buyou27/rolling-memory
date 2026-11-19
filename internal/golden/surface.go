// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package golden

import (
	"cmp"
	"os"
	"path/filepath"
	"strings"

	"github.com/rocareer/rolling-memory/internal/config"
)

// This file carries the fixture ops of the surface scenario (250-config-egress).
// config_load/config_egress/config_secret load a case configuration against
// case-level environment values without touching the network or host state.

// temporaryEnv sets process environment entries for one op and restores the
// previous values afterwards (the oracle's _temporary_env): config.ResolveSecret
// and the XDG lookups read os.Environ directly, so a case-level environment is
// the only faithful way to drive them.
func temporaryEnv(values map[string]string) func() {
	previous := map[string]*string{}
	for name, value := range values {
		if existing, ok := os.LookupEnv(name); ok {
			copied := existing
			previous[name] = &copied
		} else {
			previous[name] = nil
		}
		_ = os.Setenv(name, value)
	}
	return func() {
		for name, value := range previous {
			if value == nil {
				_ = os.Unsetenv(name)
				continue
			}
			_ = os.Setenv(name, *value)
		}
	}
}

// surfaceCaseDir returns (and creates) the per-case scratch directory of one
// surface op (cases/<prefix>/<case> under the scenario root).
func (r *Runner) surfaceCaseDir(prefix, name string) (string, error) {
	path := filepath.Join(r.ScenarioRoot, "cases", prefix, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// expandCaseValue substitutes {ROOT} (the scenario root) inside a case
// template string; other value types pass through untouched.
func (r *Runner) expandCaseValue(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	return strings.ReplaceAll(text, "{ROOT}", r.ScenarioRoot)
}

// opConfigLoad mirrors the driver's config_load: write the case files, then
// load one configuration with the case environment and flags (flags > env >
// file > defaults) and return its public dict or the ConfigError receipt.
func (r *Runner) opConfigLoad(args map[string]any) (any, error) {
	caseDir, err := r.surfaceCaseDir("config", cmp.Or(argString(args, "case"), "case"))
	if err != nil {
		return nil, err
	}
	for relative, text := range argMap(args, "files") {
		target := filepath.Join(caseDir, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(asString(text)), 0o644); err != nil {
			return nil, err
		}
	}
	env := map[string]string{}
	for name, value := range argMap(args, "env") {
		env[name] = asString(r.expandCaseValue(value))
	}
	options := config.LoadOptions{Env: env}
	// The oracle passes no path for most cases and falls back to
	// $ROLLING_MEMORY_CONFIG or the default config file of the synthetic HOME;
	// the scenario case directory is the hermetic stand-in for that default
	// (the receipt's path only normalizes to <CFG>, never compares).
	path := cmp.Or(env["ROLLING_MEMORY_CONFIG"], filepath.Join(caseDir, "config.toml"))
	if pathValue, ok := args["config_path"]; ok && pathValue != nil {
		path = filepath.Join(caseDir, asString(pathValue))
	}
	options.Path = &path
	flags := argMap(args, "flags")
	if value, ok := flags["db"]; ok {
		text := asString(r.expandCaseValue(value))
		options.DB = &text
	}
	if value, ok := flags["mode"]; ok {
		text := asString(r.expandCaseValue(value))
		options.Mode = &text
	}
	if value, ok := flags["scope"]; ok {
		text := asString(r.expandCaseValue(value))
		options.Scope = &text
	}
	if value, ok := flags["reader"]; ok {
		text := asString(r.expandCaseValue(value))
		options.Reader = &text
	}
	loaded, err := config.Load(options)
	if err != nil {
		return map[string]any{"ok": false, "error": "ConfigError", "message": err.Error()}, nil
	}
	return map[string]any{"ok": true, "config": loaded.PublicDict()}, nil
}

// opConfigEgress mirrors the driver's config_egress: load the case
// configuration in the requested mode and probe every URL through the egress
// policy, reporting denial instead of failing the op. A configuration that no
// longer validates (e.g., a retired mode left over from an upgrade) is pinned
// as a load-error receipt, the same fail-closed shape config_load records.
func (r *Runner) opConfigEgress(args map[string]any) (any, error) {
	caseDir, err := r.surfaceCaseDir("config", cmp.Or(argString(args, "case"), "egress"))
	if err != nil {
		return nil, err
	}
	mode := cmp.Or(argString(args, "mode"), "local")
	path := filepath.Join(caseDir, "config.toml")
	loaded, err := config.Load(config.LoadOptions{Path: &path, Mode: &mode, Env: map[string]string{}})
	if err != nil {
		return map[string]any{"ok": false, "error": "ConfigError", "message": err.Error()}, nil
	}
	if value, ok := args["allow_loopback_llm"]; ok {
		loaded.AllowLoopbackLLM = truthy(value)
	}
	entries := []any{}
	for _, raw := range argAnyList(args, "urls") {
		url := asString(raw)
		entry := map[string]any{"url": url}
		if err := loaded.CheckEgress(url); err != nil {
			entry["denied"] = true
			entry["error"] = err.Error()
		} else {
			entry["denied"] = false
		}
		entries = append(entries, entry)
	}
	return map[string]any{
		"mode":               loaded.Mode,
		"allow_loopback_llm": loaded.AllowLoopbackLLM,
		"cases":              entries,
	}, nil
}

// opConfigSecret mirrors the driver's config_secret: write the case key file
// (default text golden-key-value, default mode 0600), point the eval key slots
// at it, and report status plus the resolved value under the case environment.
func (r *Runner) opConfigSecret(args map[string]any) (any, error) {
	caseName := cmp.Or(argString(args, "case"), "secret")
	caseDir, err := r.surfaceCaseDir("config", caseName)
	if err != nil {
		return nil, err
	}
	filePath := ""
	if relative := argString(args, "file"); relative != "" {
		filePath = filepath.Join(caseDir, relative)
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return nil, err
		}
		text := cmp.Or(argString(args, "file_text"), "golden-key-value\n")
		if err := os.WriteFile(filePath, []byte(text), 0o600); err != nil {
			return nil, err
		}
		if err := os.Chmod(filePath, os.FileMode(argIntDefault(args, "mode", 0o600))); err != nil {
			return nil, err
		}
	}
	path := filepath.Join(caseDir, "config.toml")
	loaded, err := config.Load(config.LoadOptions{Path: &path, Env: map[string]string{}})
	if err != nil {
		return nil, err
	}
	envName := argString(args, "env_name")
	loaded.Eval.APIKeyEnv = envName
	loaded.Eval.APIKeyFile = filePath
	env := map[string]string{}
	if envName != "" {
		env[envName] = argStringDefault(args, "env_value", "")
	}
	restore := temporaryEnv(env)
	defer restore()
	result := map[string]any{"case": caseName, "status": loaded.SecretStatus()}
	value, err := loaded.EvalKey()
	if err != nil {
		result["error"] = "ConfigError"
		result["message"] = err.Error()
		return result, nil
	}
	result["value"] = value
	return result, nil
}
