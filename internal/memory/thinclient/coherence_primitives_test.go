// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestVaultSearchClientSDKDefault_CloneInflationImmunity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clone_immunity.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	v, err := NewVault(db, []byte("clone-immunity-test-secret"))
	if err != nil {
		t.Fatal(err)
	}

	_ = v.SaveLocalPlaintext("gold-target", 1700000000, "zephyrion alpha quantum telescope calibration", nil, IngestResponse{})
	for i := 0; i < 25; i++ {
		_ = v.SaveLocalPlaintext(
			fmt.Sprintf("clone-%02d", i),
			1700000010+int64(i*5),
			fmt.Sprintf("zephyrion observatory burst frame telemetry log %d", i%2),
			nil,
			IngestResponse{},
		)
	}
	for i := 0; i < 15; i++ {
		_ = v.SaveLocalPlaintext(
			fmt.Sprintf("distractor-alpha-%02d", i),
			1690000000+int64(i*86400),
			fmt.Sprintf("alpha routine maintenance report %d", i),
			nil,
			IngestResponse{},
		)
	}
	for i := 0; i < 19; i++ {
		_ = v.SaveLocalPlaintext(
			fmt.Sprintf("filler-%02d", i),
			1680000000+int64(i*86400),
			fmt.Sprintf("background archive entry number %d", i),
			nil,
			IngestResponse{},
		)
	}

	hits, err := v.SearchClientSDKDefault("zephyrion alpha", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].UnitID != "gold-target" {
		t.Fatalf("expected gold-target at rank #1 under 25x burst clone inflation, got %+v", hits)
	}
}
