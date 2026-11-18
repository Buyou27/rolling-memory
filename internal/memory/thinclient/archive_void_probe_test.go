// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"

	_ "modernc.org/sqlite"
)

func hasVoidKind(certs []plain.VoidCertificate, kind string) bool {
	for _, c := range certs {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

// 全档零记载专名触发 C1 且 OOV 质量严格落在完备度内部；全在库查询零凭证、零 OOV。
func TestVaultArchiveVoidProbeCertificatesAndCoverage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "void_probe.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	v, err := NewVault(db, []byte("void-probe-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	// 12 篇小库：船记录 3 篇、住宿记录 3 篇、填充 6 篇——使 N/e≈4.4 高于常见词档频。
	for i := 0; i < 3; i++ {
		_ = v.SaveLocalPlaintext(fmt.Sprintf("boat-%d", i), 1700000000+int64(i), "boat tour trip ticket harbour cruise paid fifty pounds", nil, IngestResponse{})
	}
	for i := 0; i < 3; i++ {
		_ = v.SaveLocalPlaintext(fmt.Sprintf("hotel-%d", i), 1700001000+int64(i), "where did i stay in the hotel for the weekend booked", nil, IngestResponse{})
	}
	for i := 0; i < 6; i++ {
		_ = v.SaveLocalPlaintext(fmt.Sprintf("filler-%d", i), 1700002000+int64(i), fmt.Sprintf("alpha beta gamma delta epsilon zeta eta theta %d", i), nil, IngestResponse{})
	}

	certs, report, ok := v.ArchiveVoidProbe("How much did I pay for the boat tour during my trip to Aspen?")
	if !ok {
		t.Fatalf("probe unavailable on a populated vault")
	}
	if !hasVoidKind(certs, plain.VoidKindOOVProper) {
		t.Fatalf("absent proper noun must fire C1, got %+v", certs)
	}
	if report.FullMass <= 0 || report.OOVMass <= 0 || report.OOVMass >= report.FullMass {
		t.Fatalf("partial void must show OOV mass strictly inside full mass, got %+v", report)
	}

	cleanCerts, cleanReport, ok := v.ArchiveVoidProbe("Where did I stay in the hotel for the weekend?")
	if !ok {
		t.Fatalf("probe unavailable on clean query")
	}
	if hasVoidKind(cleanCerts, plain.VoidKindOOVProper) || hasVoidKind(cleanCerts, plain.VoidKindBrokenCompound) {
		t.Fatalf("fully recorded query must not fire blocking certificates, got %+v", cleanCerts)
	}
	if cleanReport.OOVMass != 0 {
		t.Fatalf("fully recorded query must carry zero OOV mass, got %+v", cleanReport)
	}
}

// 空查询与空金库安全返回不可用，不 panic。
func TestVaultArchiveVoidProbeUnavailablePaths(t *testing.T) {
	if _, _, ok := (*Vault)(nil).ArchiveVoidProbe("anything"); ok {
		t.Fatalf("nil vault must report unavailable")
	}
	dbPath := filepath.Join(t.TempDir(), "void_empty.sqlite")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := NewVault(db, []byte("void-empty-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := v.ArchiveVoidProbe("How much did I pay for the boat tour in Aspen?"); ok {
		t.Fatalf("empty vault must report unavailable")
	}
	if _, _, ok := v.ArchiveVoidProbe("   "); ok {
		t.Fatalf("blank query must report unavailable")
	}
}
