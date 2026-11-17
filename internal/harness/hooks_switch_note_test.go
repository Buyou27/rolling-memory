// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/ids"
)

// TestZCodeHookSwitchNote: the check keeps reporting our on-disk wiring (it
// must not flip the host's global switch and must not fail on a fresh config),
// but the detail must tell the truth that configuration-file hooks stay
// disabled while hooks.enabled is unset.
func TestZCodeHookSwitchNote(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("HOME", fx.home)
	options := fx.optionsWith(func(options *InstallOptions) { options.Hooks = true })
	zcode := harnessIndex(fx.registry())["zcode"]
	path := *zcode.HooksPath
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	report := newReport("install", options)
	installHooks(zcode, options, report)
	if len(report.Failures()) != 0 {
		t.Fatalf("install failed: %#v", report.Failures())
	}
	if _, found := objectAt(t, readJSONObject(t, path), "hooks")["enabled"]; found {
		t.Fatal("install must not add the host's global hook switch")
	}

	check := newReport("check", options)
	checkHooks(zcode, options, check)
	action := firstAction(t, check, "zcode", "hooks")
	if action.Status != StatusOK {
		t.Fatalf("status = %q, want %q", action.Status, StatusOK)
	}
	if !strings.Contains(action.Detail, "hooks.enabled is not set") {
		t.Fatalf("detail misses the disabled-by-default note: %q", action.Detail)
	}

	// With the host switch explicitly on, the note disappears.
	data, err := loadJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = jsonTableWith(data, []string{"hooks"}, true, func(table ids.OrderedMap) ids.OrderedMap {
		return objectSet(table, "enabled", true)
	})
	text, err := dumpJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeText(path, text); err != nil {
		t.Fatal(err)
	}
	check = newReport("check", options)
	checkHooks(zcode, options, check)
	action = firstAction(t, check, "zcode", "hooks")
	if action.Status != StatusOK || strings.Contains(action.Detail, "not set") {
		t.Fatalf("switch-on check = %#v", action)
	}
}
