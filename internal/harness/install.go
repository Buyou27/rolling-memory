// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

// Install writes the skill, the MCP registration and, when requested, the hook
// events and the instruction block for every selected harness.
//
// The standard reference raises on an invalid server/skill name; Go reports it as
// an error action (the closed status set) so callers still get a report and the
// CLI can surface the message. Call ValidateInstallOptions first for the
// original stderr contract.
func Install(harnesses []Harness, options InstallOptions) *Report {
	report := newReport("install", options)
	if err := ValidateInstallOptions(report.Options); err != nil {
		report.Add("*", "binary", "-", StatusError, err.Error())
		return report
	}
	// A bare install (no --lang) resumes the persisted language preference so a
	// zh-CN user's re-run never silently flips the installed skill to English;
	// the first install on a fresh machine keeps the documented "en" default.
	report.Options.Lang = resolveInstallLang(report.Options)
	ensureBinary(report.Options, report)
	if len(report.Failures()) > 0 {
		return report
	}
	if report.Options.Lang != "" && !report.Options.MCPOnly {
		adminLang := AdminLangFor(report.Options.Lang)
		if !report.Options.DryRun && !report.Options.Project && options.Lang != "" {
			if written, err := WriteAdminDefaultLang(report.Options.Home, report.Options.Lang); err == nil && written != "" {
				adminLang = written
			}
		}
		if adminLang != "" {
			report.Extra["lang"] = report.Options.Lang
			report.Extra["default_lang"] = adminLang
		}
	}
	for _, harness := range harnesses {
		if !report.Options.MCPOnly {
			installSkill(harness, report.Options, report)
		}
		installMCP(harness, report.Options, report)
		if report.Options.Hooks && !report.Options.MCPOnly {
			installHooks(harness, report.Options, report)
		}
		if report.Options.Instructions && !report.Options.MCPOnly {
			installInstructions(harness, report.Options, report)
		}
	}
	if report.Options.DesktopShortcut && !report.Options.MCPOnly {
		installDesktopShortcut(report.Options, report)
	}
	return report
}

// Uninstall removes exactly what install wrote: matched entries, marker blocks
// and skill files whose frontmatter carries our name. Hooks and instruction
// blocks are removed whenever they are ours — not only when the caller repeats
// `--with-hooks` / `--with-instructions` — because a leftover hook re-runs
// `install --with-hooks` on the next prompt and would silently undo the
// uninstall; `--mcp-only` remains the narrowing knob for the MCP entry alone.
// The shim is left alone.
func Uninstall(harnesses []Harness, options InstallOptions) *Report {
	report := newReport("uninstall", options)
	if err := ValidateInstallOptions(report.Options); err != nil {
		report.Add("*", "binary", "-", StatusError, err.Error())
		return report
	}
	for _, harness := range harnesses {
		if !report.Options.MCPOnly {
			uninstallSkill(harness, report.Options, report)
		}
		start := len(report.Actions)
		uninstallMCP(harness, report.Options, report)
		if !report.Options.MCPOnly {
			failed := false
			for _, action := range report.Actions[start:] {
				if failedStatus(action.Status) {
					failed = true
					break
				}
			}
			if failed {
				report.Add(harness.ID, "hooks", "-", StatusSkipped, "retained because MCP uninstall failed")
				report.Add(harness.ID, "instructions", "-", StatusSkipped, "retained because MCP uninstall failed")
			} else {
				uninstallHooks(harness, report.Options, report)
				uninstallInstructions(harness, report.Options, report)
			}
		}
	}
	if !report.Options.MCPOnly && !report.Options.Project {
		// The desktop launcher is a user-level surface, not a project artifact.
		uninstallDesktopShortcut(report.Options, report)
	}
	return report
}

func newReport(mode string, options InstallOptions) *Report {
	options = options.normalized()
	return &Report{Mode: mode, Options: options, Extra: map[string]any{}}
}

// resolveInstallLang returns the explicit language, then the saved preference,
// then the documented English default. Install and Check share it so a check
// never compares an English skill against a Chinese installation.
func resolveInstallLang(options InstallOptions) string {
	if norm, err := NormalizeInstallLang(options.Lang); err == nil && norm != "" {
		return norm
	}
	if saved := ReadSavedInstallLang(options.Home); saved != "" {
		return saved
	}
	return "en"
}
