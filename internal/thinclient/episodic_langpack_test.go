// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rocareer/rolling-memory/internal/langpack"
	"github.com/rocareer/rolling-memory/internal/thinclient"
	_ "modernc.org/sqlite"
)

func TestLangpackVaultSupportsEpisodicCodecDiagnostics(t *testing.T) {
	for _, language := range []string{"", "en", "multi", "off"} {
		t.Run(language, func(t *testing.T) {
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "vault.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			vault, err := thinclient.NewVault(db, []byte("langpack-episodic-codec"))
			if err != nil {
				t.Fatal(err)
			}
			if err := langpack.ConfigureDefault(vault, language); err != nil {
				t.Fatal(err)
			}
			compatible, incompatible, missing, err := vault.EpisodicIndexDiagnostics()
			if err != nil || compatible != 0 || incompatible != 0 || missing != 0 {
				t.Fatalf("diagnostics = (%d, %d, %d, %v)", compatible, incompatible, missing, err)
			}
		})
	}
}
