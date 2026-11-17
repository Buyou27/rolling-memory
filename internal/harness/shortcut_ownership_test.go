// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestShortcutOwnershipPredicate pins the ownership signature: only the exact
// launcher shape counts as ours, a file that merely mentions the product name
// does not.
func TestShortcutOwnershipPredicate(t *testing.T) {
	userNote := "#!/bin/sh\n# My Rolling-Memory Console entry\nprintf custom\n"
	if shortcutIsOurs(userNote, "darwin") {
		t.Fatal("user note was claimed as ours")
	}
	canonical := desktopShortcutSpec("darwin", "/home/user", "/opt/rmem").Content
	if !shortcutIsOurs(canonical, "darwin") {
		t.Fatalf("canonical darwin launcher not recognized: %q", canonical)
	}
	linux := desktopShortcutSpec("linux", "/home/user", "/opt/rmem").Content
	if !shortcutIsOurs(linux, "linux") {
		t.Fatalf("canonical linux launcher not recognized: %q", linux)
	}
	if shortcutIsOurs("[Desktop Entry]\nType=Application\nName=Other\n", "linux") {
		t.Fatal("foreign desktop entry was claimed as ours")
	}
}

// TestDesktopShortcutSparesUserFileWithProductName: a user file that merely
// mentions the product name must survive both the install refresh and the
// uninstall (the earlier substring test deleted it).
func TestDesktopShortcutSparesUserFileWithProductName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows launchers are .lnk files created through PowerShell")
	}
	home := t.TempDir()
	options := InstallOptions{Home: home, Name: DefaultName, SkillName: DefaultName}
	spec := desktopShortcutSpec(runtime.GOOS, home, options.ResolvedBinary())
	if err := os.MkdirAll(filepath.Dir(spec.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	user := "#!/bin/sh\n# My " + DesktopShortcutName + " entry\nprintf custom\n"
	if err := os.WriteFile(spec.Path, []byte(user), 0o755); err != nil {
		t.Fatal(err)
	}

	report := newReport("install", options)
	installDesktopShortcut(options, report)
	if action := firstAction(t, report, "*", "shortcut"); action.Status != StatusConflict {
		t.Fatalf("install status = %q, want %q", action.Status, StatusConflict)
	}
	if got := readFile(t, spec.Path); got != user {
		t.Fatalf("user file overwritten:\n%s", got)
	}

	report = newReport("uninstall", options)
	uninstallDesktopShortcut(options, report)
	if action := firstAction(t, report, "*", "shortcut"); action.Status != StatusConflict {
		t.Fatalf("uninstall status = %q, want %q", action.Status, StatusConflict)
	}
	if got := readFile(t, spec.Path); got != user {
		t.Fatalf("user file removed:\n%s", got)
	}
}
