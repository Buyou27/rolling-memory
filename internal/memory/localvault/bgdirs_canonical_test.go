// Copyright 2026 Rocareer & Rolling PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBackgroundMountMirrorsCanonicalDirs pins the write-side hand-over for the
// background directory table: every mount, filter update and removal must land
// in rm_bg_dirs as well, so readers can move to the canonical table before the
// thin table is dropped.
func TestBackgroundMountMirrorsCanonicalDirs(t *testing.T) {
	db := migrationTestDB(t)
	v, err := NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("alpha project note"), 0o600); err != nil {
		t.Fatal(err)
	}

	mount, err := v.AddBackgroundDirWithExts(dir, []string{".md"})
	if err != nil {
		t.Fatalf("AddBackgroundDirWithExts: %v", err)
	}
	var root string
	if err := v.db.QueryRow(`SELECT root_path FROM rm_bg_dirs WHERE dir_id = ?`, mount.DirID).Scan(&root); err != nil {
		t.Fatalf("canonical mount missing: %v", err)
	}
	if root != mount.RootPath {
		t.Fatalf("canonical root = %q, want %q", root, mount.RootPath)
	}

	if _, err := v.AddBackgroundDirWithExts(dir, []string{".md", ".txt"}); err != nil {
		t.Fatalf("update filter: %v", err)
	}
	var exts string
	if err := v.db.QueryRow(`SELECT include_exts_json FROM rm_bg_dirs WHERE dir_id = ?`, mount.DirID).Scan(&exts); err != nil {
		t.Fatalf("read canonical filter: %v", err)
	}
	if exts == "" || exts == "[]" {
		t.Fatalf("canonical filter did not follow the update: %q", exts)
	}

	if _, err := v.RemoveBackgroundDir(dir); err != nil {
		t.Fatalf("RemoveBackgroundDir: %v", err)
	}
	var remaining int
	if err := v.db.QueryRow(`SELECT COUNT(*) FROM rm_bg_dirs WHERE dir_id = ?`, mount.DirID).Scan(&remaining); err != nil {
		t.Fatalf("count canonical mounts: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("canonical mount survived removal: %d", remaining)
	}
}
