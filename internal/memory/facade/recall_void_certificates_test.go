// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade_test

import (
	"testing"

	"github.com/rocareer/rolling-memory/internal/ids"
	"github.com/rocareer/rolling-memory/internal/memory/facade"
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
		if _, err := memory.Remember(facade.RememberRequest{Content: &body, Session: "s1"}); err != nil {
			t.Fatalf("remember %d: %v", i, err)
		}
	}

	result, err := memory.Recall(facade.RecallRequest{
		Cue: "How much did I pay for the boat tour during my trip to Aspen?", NoCrystallize: true,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	certs, ok := result.Meta["void_certificates"].([]map[string]any)
	if !ok || len(certs) == 0 {
		t.Fatalf("meta must carry void certificates, got %T %v", result.Meta["void_certificates"], result.Meta["void_certificates"])
	}
	found := false
	for _, c := range certs {
		if c["kind"] == plain.VoidKindOOVProper {
			found = true
		}
	}
	if !found {
		t.Fatalf("absent proper noun must fire C1, got %+v", certs)
	}
	report, ok := result.Meta["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("meta must carry coverage report, got %T", result.Meta["coverage"])
	}
	fullMass, _ := report["full_mass"].(float64)
	oovMass, _ := report["oov_mass"].(float64)
	if fullMass <= 0 || oovMass <= 0 || oovMass >= fullMass {
		t.Fatalf("partial void must show OOV mass strictly inside full mass, got %+v", report)
	}
	// 探针收据由 Python 兼容 JSON 编码器渲染，meta 内任何结构体值都会让整次探针调用失败。
	if _, err := ids.PyJSONDumpIndent(result.ToProbe(), false, true, 2); err != nil {
		t.Fatalf("probe receipt must stay encodable: %v", err)
	}

	clean, err := memory.Recall(facade.RecallRequest{
		Cue: "Where did I stay in the hotel for the weekend?", NoCrystallize: true,
	})
	if err != nil {
		t.Fatalf("clean recall: %v", err)
	}
	cleanCerts, _ := clean.Meta["void_certificates"].([]map[string]any)
	for _, c := range cleanCerts {
		if c["kind"] == plain.VoidKindOOVProper || c["kind"] == plain.VoidKindBrokenCompound {
			t.Fatalf("fully recorded query must not fire blocking certificates, got %+v", cleanCerts)
		}
	}
	cleanReport, ok := clean.Meta["coverage"].(map[string]any)
	if ok {
		if oovMass, _ := cleanReport["oov_mass"].(float64); oovMass != 0 {
			t.Fatalf("fully recorded query must carry zero OOV mass, got %+v", cleanReport)
		}
	}
}
