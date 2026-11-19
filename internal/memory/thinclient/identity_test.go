// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
	_ "modernc.org/sqlite"
)

// TestClientStampsPersonaIdentity verifies the persona coordinate contract for unlimited
// parallel incarnations: whatever the caller sets on the client reaches the locally saved row,
// so any incarnation can later answer when / where / what without a wider envelope.
func TestClientStampsPersonaIdentity(t *testing.T) {
	db, err := sql.Open("sqlite", "file:identity_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := thinclient.NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	client := thinclient.NewClient(vault, thinclient.NewLocalVaultTransport(vault))
	client.SetIdentity("reviewer", "inc-07", "Rolling-Memory")

	if _, err := client.RememberWithLayer(
		context.Background(), "ep_idtest", "并行分身身份坐标写入验证。", 1793102400, thinclient.LayerEpisodic, nil,
	); err != nil {
		t.Fatalf("RememberWithLayer: %v", err)
	}

	var storedAssistant, storedIncarnation, storedLocus string
	if err := db.QueryRow(
		`SELECT assistant_id, incarnation_id, locus FROM thin_vault_docs WHERE doc_id = 'ep_idtest'`,
	).Scan(&storedAssistant, &storedIncarnation, &storedLocus); err != nil {
		t.Fatalf("local row persona: %v", err)
	}
	if storedAssistant != "reviewer" || storedIncarnation != "inc-07" || storedLocus != "Rolling-Memory" {
		t.Fatalf("local persona = assistant=%q incarnation=%q locus=%q", storedAssistant, storedIncarnation, storedLocus)
	}
}

// TestNoeticRecordKeepsIncarnation verifies the L3 Noetic row keeps the crystallizing
// incarnation, so two parallel incarnations answering the same question stay distinguishable.
func TestNoeticRecordKeepsIncarnation(t *testing.T) {
	db, err := sql.Open("sqlite", "file:noetic_identity_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	vault, err := thinclient.NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	client := thinclient.NewClient(vault, thinclient.NewLocalVaultTransport(vault))
	client.SetIdentity("reviewer", "inc-09", "Rolling-Memory")

	if err := client.RememberNoetic("nt_idtest", "何时改了 vault.go", nil, nil, "已确认。", 1793102400); err != nil {
		t.Fatalf("RememberNoetic: %v", err)
	}
	var storedAssistant, storedIncarnation string
	if err := db.QueryRow(
		`SELECT assistant_id, incarnation_id FROM rm_noetic WHERE noetic_id = 'nt_idtest'`,
	).Scan(&storedAssistant, &storedIncarnation); err != nil {
		t.Fatalf("noetic persona: %v", err)
	}
	if storedAssistant != "reviewer" || storedIncarnation != "inc-09" {
		t.Fatalf("noetic persona = assistant=%q incarnation=%q", storedAssistant, storedIncarnation)
	}
}
