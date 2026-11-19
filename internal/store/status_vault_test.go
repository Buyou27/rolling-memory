// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package store

import "testing"

// The local-vault status branch reports the one supported transport: every
// database is loopback-only, even if a caller sets a non-local mode.
func TestVaultStatusReportsConfiguredMode(t *testing.T) {
	store := newTestStore(t, testScope)
	if _, err := store.conn.Exec(`DROP TABLE assertion`); err != nil {
		t.Fatalf("drop legacy assertion table: %v", err)
	}

	store.Config.Mode = "local"
	status, err := store.Status(testScope)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status["mode"] != "local" {
		t.Fatalf("mode = %v, want local", status["mode"])
	}
	if status["egress"] != "loopback-only" {
		t.Fatalf("egress = %v, want loopback-only", status["egress"])
	}

	store.Config.Mode = "cloud"
	status, err = store.Status(testScope)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status["egress"] != "loopback-only" {
		t.Fatalf("egress = %v, want loopback-only (cloud mode is retired)", status["egress"])
	}
}
