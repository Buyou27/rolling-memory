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

// SplitHierarchicalSegments splits a hierarchical path into non-empty segments.
func SplitHierarchicalSegments(raw string) []string {
	norm := NormalizeHierarchicalPath(raw)
	if norm == "" {
		return nil
	}
	parts := strings.Split(norm, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// HierarchicalLCPScore computes the language-agnostic hierarchical tree Longest Common Prefix (LCP)
// affinity kernel in [0, 1]:
//
//	K_space(A, B) = 2 * LCP(seg(A), seg(B)) / (|seg(A)| + |seg(B)|)
//
// Returns 0 when there is no common prefix or either path is empty (zero penalty for global facts).
func HierarchicalLCPScore(pathA, pathB string) float64 {
	segsA := SplitHierarchicalSegments(pathA)
	segsB := SplitHierarchicalSegments(pathB)
	if len(segsA) == 0 || len(segsB) == 0 {
		return 0
	}
	maxK := len(segsA)
	if len(segsB) < maxK {
		maxK = len(segsB)
	}
	lcp := 0
	for i := 0; i < maxK; i++ {
		if !strings.EqualFold(segsA[i], segsB[i]) {
			break
		}
		lcp++
	}
	if lcp == 0 {
		return 0
	}
	return float64(2*lcp) / float64(len(segsA)+len(segsB))
}

// IncarnationGenealogyScore computes the incarnation role hierarchy affinity kernel in [0, 1]:
//
//	K_inc(I_q, I_a) = LCP(seg(I_q), seg(I_a)) / max(|seg(I_q)|, |seg(I_a)|)
func IncarnationGenealogyScore(incQuery, incAssertion string) float64 {
	segsQ := SplitHierarchicalSegments(incQuery)
	segsA := SplitHierarchicalSegments(incAssertion)
	if len(segsQ) == 0 || len(segsA) == 0 {
		return 0
	}
	minLen, maxLen := len(segsQ), len(segsA)
	if minLen > maxLen {
		minLen, maxLen = maxLen, minLen
	}
	lcp := 0
	for i := 0; i < minLen; i++ {
		if !strings.EqualFold(segsQ[i], segsA[i]) {
			break
		}
		lcp++
	}
	if lcp == 0 {
		return 0
	}
	return float64(lcp) / float64(maxLen)
}

// Spacetime resonance soft potential field weights (non-degrading: multiplier >= 1.0).
const (
	SpacetimeAlphaLocus   = 0.45 // Max boost (+45%) for exact hierarchical locus match
	SpacetimeAlphaCarrier = 0.10 // Boost (+10%) for matching carrier channel
	SpacetimeBetaInc      = 0.25 // Max boost (+25%) for matching incarnation genealogy
)

// SpacetimeResonanceMultiplier computes the soft potential field multiplier (>= 1.0):
//
//	M = 1 + alpha_locus * K_space(L_q, L_a) + alpha_carrier * I(c_q == c_a) + beta_inc * K_inc(I_q, I_a)
func SpacetimeResonanceMultiplier(query PresenceCoordinates, rowLocus, rowCarrier, rowIncarnation string) float64 {
	mult := 1.0
	if query.Locus != "" && rowLocus != "" {
		if kSpace := HierarchicalLCPScore(query.Locus, rowLocus); kSpace > 0 {
			mult += SpacetimeAlphaLocus * kSpace
		}
	}
	if query.Carrier != "" && rowCarrier != "" && CarrierMatches(query.Carrier, rowCarrier) {
		mult += SpacetimeAlphaCarrier
	}
	if query.Incarnation != "" && rowIncarnation != "" {
		if kInc := IncarnationGenealogyScore(query.Incarnation, rowIncarnation); kInc > 0 {
			mult += SpacetimeBetaInc * kInc
		}
	}
	return mult
}

// CarrierMatches reports whether two carrier strings match (exact case-insensitive or canonical family).
func CarrierMatches(a, b string) bool {
	ca := strings.ToLower(strings.TrimSpace(a))
	cb := strings.ToLower(strings.TrimSpace(b))
	if ca == "" || cb == "" {
		return false
	}
	if ca == cb {
		return true
	}
	return CanonicalSchemaCarrier(ca) == CanonicalSchemaCarrier(cb) && (ca == "virtual" || cb == "virtual" || ca == "real" || cb == "real")
}

// CanonicalSchemaCarrier maps rich carrier descriptors (such as "harness:zcode" or "device:laptop-1")
// to the base schema CHECK domain {"virtual", "real"} when writing to strict columns.
func CanonicalSchemaCarrier(raw string) string {
	c := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case c == "real" || c == "virtual":
		return c
	case strings.HasPrefix(c, "device:") || strings.HasPrefix(c, "physical:") || strings.HasPrefix(c, "real:"):
		return "real"
	default:
		return "virtual"
	}
}

// ParseGitRemoteSlug extracts "<org>/<repo>" from standard SSH or HTTPS git remote URLs.
func ParseGitRemoteSlug(remoteURL string) string {
	u := strings.TrimSpace(remoteURL)
	if u == "" {
		return ""
	}
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	// Handle SCP-style git@host:org/repo
	if colonIdx := strings.Index(u, ":"); colonIdx > 0 && !strings.Contains(u, "://") {
		pathPart := strings.TrimPrefix(u[colonIdx+1:], "/")
		segs := SplitHierarchicalSegments(pathPart)
		if len(segs) >= 2 {
			return segs[len(segs)-2] + "/" + segs[len(segs)-1]
		}
		if len(segs) == 1 {
			return segs[0]
		}
	}
	// Handle scheme://host/org/repo
	if schemeIdx := strings.Index(u, "://"); schemeIdx >= 0 {
		rest := u[schemeIdx+3:]
		if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
			segs := SplitHierarchicalSegments(rest[slashIdx+1:])
			if len(segs) >= 2 {
				return segs[len(segs)-2] + "/" + segs[len(segs)-1]
			}
			if len(segs) == 1 {
				return segs[0]
			}
		}
	}
	return ""
}

// DetectGitWorkspaceLocus inspects cwd (walking up to find .git/config) and synthesizes
// a hierarchical spatial locus "<org_or_parent>/<repo>/<rel_subdir>" with 0 external process spawns.
// It is the spatial-coordinate synthesizer (the relative subdirectory is part of the
// coordinate); workspace identity stamping (ingest, presence, anchors) uses
// DetectGitWorkspaceName instead, so one repository keeps exactly one workspace name.
func DetectGitWorkspaceLocus(cwd string) string {
	cleaned := filepath.Clean(strings.TrimSpace(cwd))
	if cleaned == "" || cleaned == "." || cleaned == string(filepath.Separator) {
		return ""
	}
	repoRoot, gitDir, fromPointer := gitRepoRoot(cleaned)
	gitConfigPath := ""
	if gitDir != "" {
		if !fromPointer {
			gitConfigPath = filepath.Join(gitDir, "config")
		} else if _, err := os.Stat(filepath.Join(gitDir, "config")); err == nil {
			gitConfigPath = filepath.Join(gitDir, "config")
		} else if parent := filepath.Dir(filepath.Dir(gitDir)); parent != gitDir {
			if _, err := os.Stat(filepath.Join(parent, "config")); err == nil {
				gitConfigPath = filepath.Join(parent, "config")
			}
		}
	}

	if repoRoot != "" {
		slug := ""
		if gitConfigPath != "" {
			if rawCfg, err := os.ReadFile(gitConfigPath); err == nil {
				slug = parseRemoteURLFromGitConfig(string(rawCfg))
			}
		}
		repoBase := filepath.Base(repoRoot)
		if slug == "" {
			parentBase := filepath.Base(filepath.Dir(repoRoot))
			if parentBase != "" && parentBase != "." && parentBase != string(filepath.Separator) {
				slug = parentBase + "/" + repoBase
			} else {
				slug = repoBase
			}
		} else if repoBase != "" {
			// Ensure the local workspace repo directory name is preserved when it differs in casing or fork suffix
			slugSegs := SplitHierarchicalSegments(slug)
			if len(slugSegs) >= 2 {
				slug = slugSegs[0] + "/" + repoBase
			}
		}
		rel, err := filepath.Rel(repoRoot, cleaned)
		if err == nil && rel != "." && rel != "" && !strings.HasPrefix(rel, "..") {
			return NormalizeHierarchicalPath(slug + "/" + filepath.ToSlash(rel))
		}
		return NormalizeHierarchicalPath(slug)
	}

	// Non-git directory fallback: take up to the last 2 directory segments (<parent>/<dir>)
	base := filepath.Base(cleaned)
	parent := filepath.Base(filepath.Dir(cleaned))
	if parent != "" && parent != "." && parent != string(filepath.Separator) && parent != base {
		return NormalizeHierarchicalPath(parent + "/" + base)
	}
	return NormalizeHierarchicalPath(base)
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

func parseRemoteURLFromGitConfig(content string) string {
	lines := strings.Split(content, "\n")
	var firstURL, originURL string
	inOrigin := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sec := strings.ToLower(trimmed)
			inOrigin = strings.Contains(sec, `remote "origin"`) || strings.Contains(sec, `remote "upstream"`)
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "url") {
			if eq := strings.Index(trimmed, "="); eq >= 0 {
				u := strings.TrimSpace(trimmed[eq+1:])
				if firstURL == "" {
					firstURL = u
				}
				if inOrigin && originURL == "" {
					originURL = u
				}
			}
		}
	}
	chosen := originURL
	if chosen == "" {
		chosen = firstURL
	}
	return ParseGitRemoteSlug(chosen)
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
// environment, or "" when no host-specific session marker is present. The
// registry and the strength ladder live in internal/hostid; this wrapper keeps
// the historical text-package call site.
func DetectHarnessFromEnv() string {
	return hostid.DetectFromEnv()
}

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
