// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackgroundSyncSkipsSymlinkTargetOutsideMount pins the mount boundary: a
// symlink inside a mounted directory must not pull files from outside the
// canonical mount root into the index (the earlier behavior indexed them and
// recall served their content). An inside symlink still indexes its target.
func TestBackgroundSyncSkipsSymlinkTargetOutsideMount(t *testing.T) {
	root := newBackgroundTestDir(t)
	outside := newBackgroundTestDir(t)
	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("outside mount secret document"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}
	// Control: an inside symlink (target also inside the mount) keeps indexing.
	inside := filepath.Join(root, "inside.md")
	if err := os.WriteFile(inside, []byte("inside mount document body"), 0o600); err != nil {
		t.Fatal(err)
	}
	insideLink := filepath.Join(root, "inside-link.md")
	if err := os.Symlink(inside, insideLink); err != nil {
		t.Fatal(err)
	}

	client, db := newBackgroundTestClient(t, &backgroundTransportStub{})
	if _, err := client.AddBackgroundDir(root); err != nil {
		t.Fatalf("AddBackgroundDir: %v", err)
	}
	report, err := client.SyncBackgroundDirs(context.Background())
	if err != nil {
		t.Fatalf("SyncBackgroundDirs: %v", err)
	}
	if got := countBackgroundRows(t, db, `SELECT COUNT(*) FROM rm_bg_chunks WHERE abs_path = ?`, secret); got != 0 {
		t.Fatalf("external symlink target entered the index: %d", got)
	}
	if got := countBackgroundRows(t, db, `SELECT COUNT(*) FROM rm_bg_chunks WHERE abs_path = ?`, inside); got == 0 {
		t.Fatalf("inside symlink target was not indexed")
	}
	sawOutsideSkip := false
	for _, skip := range report.SkippedFiles {
		if strings.Contains(skip.Reason, "outside the mount root") {
			sawOutsideSkip = true
		}
	}
	if !sawOutsideSkip {
		t.Fatalf("outside symlink skip not reported: %+v", report)
	}
}
