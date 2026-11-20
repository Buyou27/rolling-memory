// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func assertKeys(t *testing.T, label string, result map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(result))
	for key := range result {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s keys = %v, want %v", label, got, want)
	}
}

// writeTranscript drops one synthetic transcript in a temp dir.
func writeTranscript(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
