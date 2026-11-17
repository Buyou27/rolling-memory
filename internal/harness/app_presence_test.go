// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestAppPresentHomeBundleFixture pins the home-directory app bundle probe:
// a bundle under <home>/Applications counts as installed software.
func TestAppPresentHomeBundleFixture(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("bundle fixture uses the macOS /Applications layout")
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Applications", "CodeBuddy.app"), 0o755); err != nil {
		t.Fatalf("mkdir bundle fixture: %v", err)
	}
	if !AppPresent("codebuddy", home) {
		t.Fatalf("home app bundle must count as present")
	}
}

// TestAppPresentBareStateDirIsNotInstallation pins the ghost case behind the
// console INSTALLED light: a leftover per-user data directory must not count
// as an installation. PATH is cleared so a real machine install of the same
// launcher name cannot leak into the assertion.
func TestAppPresentBareStateDirIsNotInstallation(t *testing.T) {
	t.Setenv("PATH", "")
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".workbuddy"), 0o755); err != nil {
		t.Fatalf("mkdir state fixture: %v", err)
	}
	if AppPresent("workbuddy", home) {
		t.Fatalf("a bare state directory must not count as an installation")
	}
	if AppPresent("no-such-harness", home) {
		t.Fatalf("unknown ids carry no evidence")
	}
}

// TestAppPresentPOSIXLauncherPath pins the CLI-host probe: a launcher under
// <home>/.local/bin counts as installed software.
func TestAppPresentPOSIXLauncherPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skipf("POSIX launcher layout")
	}
	t.Setenv("PATH", "")
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir bin fixture: %v", err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write launcher fixture: %v", err)
	}
	if !AppPresent("claude-code", home) {
		t.Fatalf("launcher under ~/.local/bin must count as present")
	}
}
