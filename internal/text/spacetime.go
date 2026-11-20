// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package text

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rocareer/rolling-memory/internal/hostid"
)

// PresenceCoordinates holds the 4D presence probe coordinates (World x Carrier x Locus x Incarnation)
// plus optional SelfLabel (the Assistant's first-class ego identity, origin="self") and PeerLabel
// (the bound host user identity, origin="roster"), injected automatically by gateway or harness hooks (0-LLM).
type PresenceCoordinates struct {
	World       string // Modality world: actual / fictional / hypothetical / dream
	Carrier     string // Physical or host channel: virtual / real / harness:zcode / dialog:web
	Locus       string // Hierarchical spatial path: org/repo/pkg/memory or country/city/district
	Incarnation string // Assistant incarnation role URI: dialog/main / coding/zcode/main
	SelfLabel   string // Optional assistant self-referent label (origin="self")
	PeerLabel   string // Optional bound host user label (origin="roster")
}

// IsEmpty reports whether all coordinates are unset.
func (p PresenceCoordinates) IsEmpty() bool {
	return p.World == "" && p.Carrier == "" && p.Locus == "" && p.Incarnation == "" && p.SelfLabel == "" && p.PeerLabel == ""
}

// Normalized returns a copy with normalized hierarchical paths and trimmed fields.
func (p PresenceCoordinates) Normalized() PresenceCoordinates {
	return PresenceCoordinates{
		World:       strings.TrimSpace(p.World),
		Carrier:     strings.TrimSpace(p.Carrier),
		Locus:       NormalizeHierarchicalPath(p.Locus),
		Incarnation: NormalizeHierarchicalPath(p.Incarnation),
		SelfLabel:   strings.TrimSpace(p.SelfLabel),
		PeerLabel:   strings.TrimSpace(p.PeerLabel),
	}
}

// NormalizeHierarchicalPath normalizes a '/' or '\' separated hierarchical path,
// stripping redundant separators, '.', and folding '..' parent segments without domain-specific word lists.
func NormalizeHierarchicalPath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\\", "/")
	parts := strings.Split(s, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "/")
}

// gitRepoRoot walks up from dir (at most 16 levels) to the enclosing `.git`
// marker. root is the repository root directory (the directory containing the
// marker); gitDir is the resolved git directory — the marker itself when it is
// a directory, or the `gitdir:` pointer target for linked worktrees and
// submodules; fromPointer reports the pointer case. All empty when no marker
// is found.
func gitRepoRoot(dir string) (root, gitDir string, fromPointer bool) {
	curr := dir
	for i := 0; i < 16; i++ {
		candidate := filepath.Join(curr, ".git")
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return curr, candidate, false
			}
			if data, readErr := os.ReadFile(candidate); readErr == nil {
				// Worktree or submodule gitdir pointer: "gitdir: /path/to/.git/worktrees/..."
				line := strings.TrimSpace(string(data))
				if strings.HasPrefix(line, "gitdir:") {
					gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
					if !filepath.IsAbs(gitDir) {
						gitDir = filepath.Join(curr, gitDir)
					}
					return curr, gitDir, true
				}
			}
			return curr, "", false
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return "", "", false
}

// DetectGitWorkspaceName returns the canonical workspace identity for a
// directory: the basename of the enclosing git repository root. It never
// appends relative subdirectories and never falls back to the raw directory
// name, so subdirectories, the home directory, runtime-data directories and
// temporary directories can never surface as workspaces. A linked worktree
// resolves through its gitdir pointer to the main repository, so every
// worktree of one repository keeps the repository's single identity.
// Returns "" when the directory is outside any git repository.
func DetectGitWorkspaceName(cwd string) string {
	cleaned := filepath.Clean(strings.TrimSpace(cwd))
	if cleaned == "" || cleaned == "." || cleaned == string(filepath.Separator) {
		return ""
	}
	homeClean := ""
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		homeClean = filepath.Clean(home)
		if cleaned == homeClean {
			return ""
		}
		for _, runtimeSub := range []string{
			filepath.Join(homeClean, ".local", "share", "rolling-memory"),
			filepath.Join(homeClean, ".local", "state", "rolling-memory"),
		} {
			if cleaned == runtimeSub || strings.HasPrefix(cleaned, runtimeSub+string(filepath.Separator)) {
				return ""
			}
		}
	}
	root, gitDir, fromPointer := gitRepoRoot(cleaned)
	// A dotfiles-style repository rooted at $HOME is not a workspace, and a
	// repository rooted at the filesystem root is a system artifact: neither
	// may lend its name to arbitrary directories below it.
	if root == "" || (homeClean != "" && filepath.Clean(root) == homeClean) {
		return ""
	}
	if fromPointer {
		if mainRoot := linkedWorktreeRoot(gitDir); mainRoot != "" {
			return filepath.Base(mainRoot)
		}
	}
	return filepath.Base(root)
}

// linkedWorktreeRoot maps a linked-worktree gitdir pointer
// ("<main>/.git/worktrees/<name>") to the main repository root so a worktree
// stamps the main repository's workspace name. Submodule pointers
// ("<super>/.git/modules/<name>") keep their own identity and return "".
func linkedWorktreeRoot(gitDir string) string {
	cleaned := filepath.Clean(strings.TrimSpace(gitDir))
	if cleaned == "" || cleaned == "." || cleaned == string(filepath.Separator) {
		return ""
	}
	if filepath.Base(filepath.Dir(cleaned)) != "worktrees" {
		return ""
	}
	gitEntry := filepath.Dir(filepath.Dir(cleaned)) // <main>/.git
	if filepath.Base(gitEntry) != ".git" || filepath.Dir(gitEntry) == gitEntry {
		return ""
	}
	return filepath.Dir(gitEntry)
}

// DetectHarnessPresence synthesizes PresenceCoordinates from a workspace cwd, optional harness name,
// and optional agent subrole (0-LLM, deterministic). The locus is the canonical workspace identity
// (DetectGitWorkspaceName): the enclosing repository root's name, or "" outside any repository.
//
// An undetected host leaves Carrier and Incarnation empty. The historical
// "harness:cli" fallback turned every detection failure into a concrete
// identity, which then received carrier and incarnation resonance and was
// written into assertion metadata. Unknown is now a first-class state: only
// the locus can still contribute resonance, and the console can say so.
func DetectHarnessPresence(cwd, harnessName, subrole string) PresenceCoordinates {
	locus := DetectGitWorkspaceName(cwd)
	h := hostid.Normalize(harnessName)
	if h == hostid.Unknown {
		h = hostid.Normalize(hostid.DetectFromEnv())
	}
	coords := PresenceCoordinates{
		Locus:     locus,
		PeerLabel: DetectGitWorkspaceUser(cwd),
	}
	if h != hostid.Unknown && h != "" {
		role := strings.ToLower(strings.TrimSpace(subrole))
		if role == "" {
			role = "main"
		}
		coords.Carrier = "harness:" + string(h)
		coords.Incarnation = NormalizeHierarchicalPath("coding/" + string(h) + "/" + role)
	}
	if coords.Carrier != "" || coords.Locus != "" {
		coords.World = "actual"
	}
	return coords
}

// DetectGitWorkspaceUser inspects the git config (local repository or ~/.gitconfig) to extract
// the configured user.name with 0 external process spawns.
func DetectGitWorkspaceUser(cwd string) string {
	cleaned := filepath.Clean(strings.TrimSpace(cwd))
	if cleaned != "" && cleaned != "." && cleaned != string(filepath.Separator) {
		curr := cleaned
		for i := 0; i < 16; i++ {
			candidate := filepath.Join(curr, ".git", "config")
			if rawCfg, err := os.ReadFile(candidate); err == nil {
				if name := parseUserNameFromGitConfig(string(rawCfg)); name != "" {
					return name
				}
				break
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rawCfg, err := os.ReadFile(filepath.Join(home, ".gitconfig")); err == nil {
			if name := parseUserNameFromGitConfig(string(rawCfg)); name != "" {
				return name
			}
		}
	}
	return ""
}

func parseUserNameFromGitConfig(content string) string {
	lines := strings.Split(content, "\n")
	inUser := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sec := strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			inUser = sec == "user"
			continue
		}
		if inUser && strings.HasPrefix(strings.ToLower(trimmed), "name") {
			if eq := strings.Index(trimmed, "="); eq >= 0 {
				val := strings.TrimSpace(trimmed[eq+1:])
				val = strings.Trim(val, `"'`)
				if val != "" {
					return val
				}
			}
		}
	}
	return ""
}

// DetectHarnessFromEnv returns the harness fingerprint carried by the process
// SavePresenceFile records a hook heartbeat in the shared attachment ledger.
// It is no longer a last-writer-wins document: each session keeps its own
// entry, and a hook carries a short lease rather than claiming that its
// short-lived process is still online. The document schema and locking live in
// internal/hostid.
func SavePresenceFile(stateDir string, coords PresenceCoordinates, session string, updatedAt int64) error {
	if strings.TrimSpace(stateDir) == "" || coords.IsEmpty() {
		return nil
	}
	host := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(coords.Carrier)), "harness:")
	if host == "" {
		host = string(hostid.Unknown)
	}
	attachment := hostid.Attachment{
		Host:       host,
		Label:      hostid.Label(hostid.Normalize(host)),
		SessionID:  session,
		Role:       incarnationRole(coords.Incarnation),
		Locus:      coords.Locus,
		Evidence:   "hook",
		Mode:       hostid.ModeHeartbeat,
		LastSeenAt: updatedAt,
	}
	return hostid.RecordAt(stateDir, attachment, updatedAt)
}

func incarnationRole(incarnation string) string {
	parts := strings.Split(strings.TrimSpace(incarnation), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[len(parts)-1]
}
