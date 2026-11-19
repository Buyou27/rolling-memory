// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package updatesource

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":        GitHub,
		"github":  GitHub,
		" GitHub": GitHub,
		"gitee":   Gitee,
		" GITEE ": Gitee,
	}
	for input, want := range cases {
		got, err := Normalize(input)
		if err != nil || got != want {
			t.Fatalf("Normalize(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := Normalize("gitlab"); err == nil {
		t.Fatal("unknown source must be rejected")
	}
}

func TestReleaseBaseAndRepo(t *testing.T) {
	if got := ReleaseBase(Gitee); got != GiteeReleaseBase {
		t.Fatalf("ReleaseBase(gitee) = %q", got)
	}
	if got := ReleaseBase(GitHub); got != GitHubReleaseBase {
		t.Fatalf("ReleaseBase(github) = %q", got)
	}
	if got := RepoURL(Gitee); got != "https://gitee.com/rocareer/Rolling-Memory.git" {
		t.Fatalf("RepoURL(gitee) = %q", got)
	}
	if got := RepoURL(GitHub); got != "https://github.com/rocareer/rolling-memory.git" {
		t.Fatalf("RepoURL(github) = %q", got)
	}
}

func TestGiteeBaseParsing(t *testing.T) {
	base := "https://gitee.com/acme/Rolling-Memory/releases/download/v1.2.3"
	if !IsGiteeBase(base) {
		t.Fatal("gitee base was not recognized")
	}
	if got := GiteeRepoFromBase(base); got != "acme/Rolling-Memory" {
		t.Fatalf("repo = %q", got)
	}
	if got := GiteeReleaseDownloadBase("acme/Rolling-Memory", "v1.2.3"); got != base {
		t.Fatalf("download base = %q, want %q", got, base)
	}
	if IsGiteeBase("https://github.com/rocareer/rolling-memory/releases/latest/download") {
		t.Fatal("GitHub base was classified as Gitee")
	}
}
