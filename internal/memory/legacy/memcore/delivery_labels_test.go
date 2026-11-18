// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeliveryLabelsSingleSourceOfTruthGate ensures that within internal/memcore production files,
// band headers and prospective loop tags are exclusively defined in delivery_labels*.go
// rather than duplicated as scattered string literals.
func TestDeliveryLabelsSingleSourceOfTruthGate(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob *.go: %v", err)
	}
	forbiddenLiterals := []string{
		`"【记忆带·"`,
		`"【Memory Band·"`,
		`"[待兑现]"`,
		`"[已兑现]"`,
		`"[已取消]"`,
		`"[已中止]"`,
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "delivery_labels.go" || f == "delivery_labels_replace.go" {
			continue
		}
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("read %s: %v", f, rerr)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if cIdx := strings.Index(trimmed, "//"); cIdx >= 0 {
				trimmed = trimmed[:cIdx]
			}
			for _, lit := range forbiddenLiterals {
				if strings.Contains(trimmed, lit) {
					t.Fatalf("file %s contains scattered hardcoded label literal %s; use delivery_labels.go single source of truth instead", f, lit)
				}
			}
		}
	}
}
