// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package api_test

import (
	"testing"

	"github.com/rocareer/rolling-memory/internal/api"
	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// 产品 recall 面必须随交付携带全档零记载凭证与完备度：缺席专名触发 C1，
// 在库查询零阻断凭证。两处都是事实陈述，不改变交付条数。
func TestLocalRecallEmitsVoidCertificatesAndCoverage(t *testing.T) {
	memory := newVaultOnlyMemory(t)
	docs := []string{
		"boat tour trip ticket harbour cruise paid fifty pounds",
		"boat tour trip ticket harbour cruise paid fifty pounds again",
		"boat tour trip ticket harbour cruise paid fifty pounds return",
		"where did i stay in the hotel for the weekend booked",
		"where did i stay in the hotel for the weekend booked again",
		"where did i stay in the hotel for the weekend booked return",
		"alpha beta gamma delta epsilon zeta eta theta one",
		"alpha beta gamma delta epsilon zeta eta theta two",
		"alpha beta gamma delta epsilon zeta eta theta three",
		"alpha beta gamma delta epsilon zeta eta theta four",
		"alpha beta gamma delta epsilon zeta eta theta five",
		"alpha beta gamma delta epsilon zeta eta theta six",
	}
	for i, content := range docs {
		body := content
		if _, err := memory.Remember(api.RememberRequest{Content: &body, Session: "s1"}); err != nil {
			t.Fatalf("remember %d: %v", i, err)
		}
	}

	result, err := memory.Recall(api.RecallRequest{
		Cue: "How much did I pay for the boat tour during my trip to Aspen?", NoCrystallize: true,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	certs, ok := result.Meta["void_certificates"].([]plain.VoidCertificate)
	if !ok || len(certs) == 0 {
		t.Fatalf("meta must carry void certificates, got %T %v", result.Meta["void_certificates"], result.Meta["void_certificates"])
	}
	found := false
	for _, c := range certs {
		if c.Kind == plain.VoidKindOOVProper {
			found = true
		}
	}
	if !found {
		t.Fatalf("absent proper noun must fire C1, got %+v", certs)
	}
	report, ok := result.Meta["coverage"].(plain.CoverageReport)
	if !ok {
		t.Fatalf("meta must carry coverage report, got %T", result.Meta["coverage"])
	}
	if report.FullMass <= 0 || report.OOVMass <= 0 || report.OOVMass >= report.FullMass {
		t.Fatalf("partial void must show OOV mass strictly inside full mass, got %+v", report)
	}

	clean, err := memory.Recall(api.RecallRequest{
		Cue: "Where did I stay in the hotel for the weekend?", NoCrystallize: true,
	})
	if err != nil {
		t.Fatalf("clean recall: %v", err)
	}
	cleanCerts, _ := clean.Meta["void_certificates"].([]plain.VoidCertificate)
	for _, c := range cleanCerts {
		if c.Kind == plain.VoidKindOOVProper || c.Kind == plain.VoidKindBrokenCompound {
			t.Fatalf("fully recorded query must not fire blocking certificates, got %+v", cleanCerts)
		}
	}
	cleanReport, ok := clean.Meta["coverage"].(plain.CoverageReport)
	if ok && cleanReport.OOVMass != 0 {
		t.Fatalf("fully recorded query must carry zero OOV mass, got %+v", cleanReport)
	}
}
