// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// AppPresent reports whether the harness's own software (app bundle, program,
// or launcher) can be found on this machine. Display surfaces use it to tell a
// live install apart from leftover state: a per-user data directory survives
// an uninstall and can even be created by our own wiring, so directory
// existence alone is not proof the host is still installed here.
//
// The evidence list is deliberately explicit and short; a miss only leaves a
// freshly installed, never-used host unlit until its first session, while a
// host in real use always lights through its own transcripts.
func AppPresent(id, home string) bool {
	if home == "" {
		if resolved, err := os.UserHomeDir(); err == nil {
			home = resolved
		}
	}
	bundles, launchers := appEvidence(id)
	for _, candidate := range appCandidates(bundles, launchers, home) {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	for _, launcher := range launchers {
		if _, err := exec.LookPath(launcher); err == nil {
			return true
		}
	}
	return false
}

// appCandidates resolves platform-specific bundle / program / launcher paths
// under home. Bundle names are base names without the platform suffix.
func appCandidates(bundles, launchers []string, home string) []string {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		for _, bundle := range bundles {
			candidates = append(candidates,
				filepath.Join(string(filepath.Separator), "Applications", bundle+".app"),
				filepath.Join(home, "Applications", bundle+".app"),
			)
		}
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		for _, program := range bundles {
			candidates = append(candidates,
				filepath.Join(localAppData, "Programs", program, program+".exe"),
				filepath.Join(localAppData, program, program+".exe"),
			)
		}
	default:
		for _, bundle := range bundles {
			desktop := filepath.Join(home, ".local", "share", "applications", bundle+".desktop")
			candidates = append(candidates,
				desktop,
				filepath.Join(string(filepath.Separator), "usr", "share", "applications", bundle+".desktop"),
			)
		}
	}
	for _, launcher := range launchers {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", launcher),
			filepath.Join(string(filepath.Separator), "usr", "local", "bin", launcher),
			filepath.Join(string(filepath.Separator), "opt", "homebrew", "bin", launcher),
		)
	}
	return candidates
}

// appEvidence lists the display names (app bundles / programs) and POSIX
// launcher names per harness. A host without its own bundle or launcher (an
// editor extension, for example) keeps empty lists and lights through its
// session activity instead.
func appEvidence(id string) (bundles, launchers []string) {
	switch id {
	case "zcode":
		return []string{"ZCode"}, []string{"zcode"}
	case "claude-code":
		return nil, []string{"claude"}
	case "codex":
		return nil, []string{"codex"}
	case "opencode":
		return nil, []string{"opencode"}
	case "dsh":
		return nil, []string{"dsh"}
	case "codebuddy":
		return []string{"CodeBuddy"}, nil
	case "cursor":
		return []string{"Cursor"}, []string{"cursor"}
	case "windsurf":
		return []string{"Windsurf", "Devin"}, []string{"windsurf"}
	case "workbuddy":
		return []string{"WorkBuddy"}, nil
	case "doubao":
		return []string{"Doubao"}, nil
	}
	return nil, nil
}
