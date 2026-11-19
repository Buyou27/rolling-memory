// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package updatesource defines the closed set of release sources used by the
// updater. The source selects the repository whose releases and tags define the
// available version; it is independent from the release channel (stable, beta,
// dev) and from an explicit mirror URL.
package updatesource

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	GitHub = "github"
	Gitee  = "gitee"

	// Default follows the official GitHub repository.
	Default = GitHub

	GitHubRepo = "rocareer/rolling-memory"
	GiteeRepo  = "rocareer/Rolling-Memory"

	GitHubReleaseBase = "https://github.com/rocareer/rolling-memory/releases/latest/download"
	GiteeReleaseBase  = "https://gitee.com/rocareer/Rolling-Memory/releases/latest/download"
	GiteeAPIBase      = "https://gitee.com/api/v5"
)

// Normalize returns the canonical source name.
func Normalize(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "":
		return Default, nil
	case GitHub:
		return GitHub, nil
	case Gitee:
		return Gitee, nil
	default:
		return "", fmt.Errorf("update source must be one of github, gitee; got %q", raw)
	}
}

// ReleaseBase returns the default flat release base for a source. An unknown
// value falls back to GitHub so a malformed setting never disables updates.
func ReleaseBase(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case Gitee:
		return GiteeReleaseBase
	default:
		return GitHubReleaseBase
	}
}

// IsGiteeBase reports whether a release base addresses the Gitee host.
func IsGiteeBase(base string) bool {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "gitee.com")
}

// GiteeRepoFromBase extracts <owner>/<repo> from a Gitee release URL. A URL
// that does not identify a repository falls back to the official mirror.
func GiteeRepoFromBase(base string) string {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || !strings.EqualFold(parsed.Hostname(), "gitee.com") {
		return GiteeRepo
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return GiteeRepo
	}
	return parts[0] + "/" + parts[1]
}

// GiteeReleaseDownloadBase returns the tag-specific download base. Gitee does
// not expose a GitHub-style `latest/download` route, so callers resolve the tag
// first and then fetch the manifest and assets from this base.
func GiteeReleaseDownloadBase(repo, tag string) string {
	if strings.TrimSpace(repo) == "" {
		repo = GiteeRepo
	}
	return "https://gitee.com/" + strings.Trim(repo, "/") + "/releases/download/" + url.PathEscape(strings.TrimSpace(tag))
}

// RepoURL returns the clone URL for a source.
func RepoURL(source string) string {
	if normalized, err := Normalize(source); err == nil && normalized == Gitee {
		return "https://gitee.com/" + GiteeRepo + ".git"
	}
	return "https://github.com/" + GitHubRepo + ".git"
}
