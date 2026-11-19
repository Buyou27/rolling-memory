// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFixture(t *testing.T, fx *fixture, options InstallOptions) {
	t.Helper()
	assertNoFailures(t, Install(fx.registry(), options))
}

func TestCheckReportsOutdatedSkill(t *testing.T) {
	fx := newFixture(t)
	options := fx.options()
	installFixture(t, fx, options)

	updated := "---\nname: rolling-memory\ndescription: synthetic test skill\n---\n\n# newer body\n"
	if err := os.WriteFile(*options.SkillFile, []byte(updated), 0o644); err != nil {
		t.Fatalf("rewrite skill source: %v", err)
	}
	report := Check(fx.registry(), options, false)
	action := firstAction(t, report, "zcode", "skill")
	if action.Status != StatusOutdated {
		t.Fatalf("zcode skill check = %#v, want outdated", action)
	}
	if ok, _ := report.ToDict()["ok"].(bool); ok {
		t.Fatalf("outdated skill must make the check fail")
	}
}

func TestCheckReportsModifiedSkillAsConflict(t *testing.T) {
	fx := newFixture(t)
	options := fx.options()
	installFixture(t, fx, options)

	target := filepath.Join(fx.home, ".zcode", "skills", options.SkillName, "SKILL.md")
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if err := os.WriteFile(target, append(raw, []byte("\nuser edit\n")...), 0o644); err != nil {
		t.Fatalf("modify installed skill: %v", err)
	}
	report := Check(fx.registry(), options, false)
	action := firstAction(t, report, "zcode", "skill")
	if action.Status != StatusConflict {
		t.Fatalf("modified skill check = %#v, want conflict", action)
	}
}

func TestCheckReportsMissingLauncher(t *testing.T) {
	fx := newFixture(t)
	report := Check(fx.registry(), fx.options(), false)
	action := firstAction(t, report, "*", "binary")
	if action.Status != StatusMissing {
		t.Fatalf("launcher check = %#v, want missing", action)
	}
	if ok, _ := report.ToDict()["ok"].(bool); ok {
		t.Fatalf("missing launcher must make the check fail")
	}
}

func TestCheckReportsOutdatedLauncher(t *testing.T) {
	fx := newFixture(t)
	options := fx.options()
	shim := DefaultShimPath(fx.home)
	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		t.Fatalf("mkdir shim dir: %v", err)
	}
	previous := shimTextFor("darwin", filepath.Join(fx.home, "old", "rolling-memory"))
	if err := os.WriteFile(shim, []byte(previous), 0o755); err != nil {
		t.Fatalf("write generated shim: %v", err)
	}
	report := Check(fx.registry(), options, false)
	action := firstAction(t, report, "*", "binary")
	if action.Status != StatusOutdated {
		t.Fatalf("launcher check = %#v, want outdated", action)
	}
}

func TestResolveInstallLangUsesSavedPreference(t *testing.T) {
	home := t.TempDir()
	if _, err := WriteAdminDefaultLang(home, "zh-CN"); err != nil {
		t.Fatalf("write saved language: %v", err)
	}
	options := DefaultInstallOptions(home, home)
	if got := resolveInstallLang(options); got != "zh-CN" {
		t.Fatalf("saved language = %q, want zh-CN", got)
	}
	options.Lang = "en"
	if got := resolveInstallLang(options); got != "en" {
		t.Fatalf("explicit language = %q, want en", got)
	}
}

func TestInstallRestoresOwnershipMarkerForCurrentContent(t *testing.T) {
	fx := newFixture(t)
	options := fx.options()
	installFixture(t, fx, options)
	target := filepath.Join(fx.home, ".zcode", "skills", options.SkillName, "SKILL.md")
	marker := filepath.Join(filepath.Dir(target), skillOwnerMarker)
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove ownership marker: %v", err)
	}
	check := Check(fx.registry(), options, false)
	action := firstAction(t, check, "zcode", "skill")
	if action.Status != StatusOK || !strings.Contains(action.Detail, "ownership marker") {
		t.Fatalf("current content without marker = %#v", action)
	}
	install := Install(fx.registry(), options)
	if action := firstAction(t, install, "zcode", "skill"); action.Status != StatusUnchanged {
		t.Fatalf("marker repair = %#v, want unchanged", action)
	}
	if !fileExists(marker) {
		t.Fatalf("ownership marker was not restored")
	}
	assertNoFailures(t, Check(fx.registry(), options, false))
}

func TestCheckAcceptsBareCustomLauncher(t *testing.T) {
	fx := newFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(executable)+string(os.PathListSeparator)+os.Getenv("PATH"))
	options := fx.options()
	options.Binary = filepath.Base(executable)
	action := firstAction(t, Check(fx.registry(), options, false), "*", "binary")
	if action.Status != StatusOK || action.Detail != "custom launcher" {
		t.Fatalf("bare custom launcher = %#v", action)
	}
}
