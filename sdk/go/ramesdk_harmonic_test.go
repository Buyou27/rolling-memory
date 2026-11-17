// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package ramesdk_test

import (
	"reflect"
	"testing"

	ramesdk "github.com/rocareer/rolling-memory/sdk/go"
)

func TestExtractStructuredDateHarmonics_ZeroLexiconStateMachine(t *testing.T) {
	positives := []struct {
		input string
		want  []string
	}{
		{"Deploy scheduled on 2026/11/8 and verified on 2026.11.09!", []string{"2026-11-08", "2026-11-09"}},
		{"架构定版于2026年11月8日，复盘于2026年12月01日。", []string{"2026-11-08", "2026-12-01"}},
		{"Already canonical 2026-11-08.", []string{"2026-11-08"}},
	}
	for _, tc := range positives {
		got := ramesdk.ExtractStructuredDateHarmonics(tc.input)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("ExtractStructuredDateHarmonics(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}

	negatives := []string{
		"RollingMemory v1.0.1 and v2026.15.1",
		"Host 1920.10.10.1 or 10.0.0.1",
		"Out of range 2026-13-01 or 2026-02-35 or 1899-12-31",
		"Spaced numbers 2026 11 08 are not joined dates",
	}
	for _, neg := range negatives {
		if got := ramesdk.ExtractStructuredDateHarmonics(neg); len(got) != 0 {
			t.Fatalf("negative case %q produced unexpected dates: %v", neg, got)
		}
	}
}

func TestCanonicalHarmonicOverlay_WireRoundtripAndIdempotence(t *testing.T) {
	enc := ramesdk.NewBlindEncoder([]byte("harmonic-test-secret-2026"))
	enc.SetCanonicalHarmonics(ramesdk.CanonicalHarmonicOptions{
		EnableStructuredDates: true,
		Expander: func(tok string) []string {
			switch tok {
			case "kg", "千克", "公斤":
				return []string{"@unit:mass:kg", "@dim:mass"}
			default:
				return nil
			}
		},
	})

	pkt1, _ := enc.PackBlindPacket("U1", "2026/11/8 load 50 kg", 1700000000)
	pkt2, _ := enc.PackBlindPacket("U2", "2026年11月8日 载重 50 千克", 1700000000)

	if pkt1.AtomsB64 == "" || pkt2.AtomsB64 == "" {
		t.Fatal("expected non-empty RMBP atoms_b64 payload")
	}
	decSlots, decFrames, err := ramesdk.DecodeWirePayload(pkt1.AtomsB64)
	if err != nil {
		t.Fatalf("DecodeWirePayload failed: %v", err)
	}
	if !reflect.DeepEqual(decSlots, pkt1.Slots) || !reflect.DeepEqual(decFrames, pkt1.SentenceFrames) {
		t.Fatalf("RMBP wire roundtrip mismatch")
	}

	// Verify that both packets share the exact same canonical ISO date hash and canonical mass unit harmonic hash
	dateH := enc.KeyedHash64("2026-11-08")
	unitH := enc.KeyedHash64("@unit:mass:kg")
	hasHash := func(slots []ramesdk.BlindTokenAtom, target uint64) bool {
		for _, s := range slots {
			if s.H == target {
				return true
			}
		}
		return false
	}
	if !hasHash(pkt1.Slots, dateH) || !hasHash(pkt2.Slots, dateH) {
		t.Fatalf("expected both packets to contain canonical date beacon H(2026-11-08)")
	}
	if !hasHash(pkt1.Slots, unitH) || !hasHash(pkt2.Slots, unitH) {
		t.Fatalf("expected both packets to contain canonical unit harmonic H(@unit:mass:kg)")
	}
}

func TestF19F_CrossLanguageCanonicalHarmonicBitExactParity(t *testing.T) {
	secret := "f19f-cross-lang-harmonic-key"
	docID := "DOC_HARM_01"
	text := "2026/11/8 NYC cargo weight 50 kg. 2026年11月9日 复核 50 千克!"

	goEnc := ramesdk.NewBlindEncoder([]byte(secret))
	goEnc.SetCanonicalHarmonics(ramesdk.CanonicalHarmonicOptions{
		EnableStructuredDates: true,
		Expander: func(tok string) []string {
			switch tok {
			case "kg", "千克":
				return []string{"@unit:mass:kg", "@dim:mass"}
			case "nyc":
				return []string{"@geo:nyc"}
			default:
				return nil
			}
		},
	})
	goPkt, _ := goEnc.PackBlindPacket(docID, text, 1700000000)
	decSlots, decFrames, err := ramesdk.DecodeWirePayload(goPkt.AtomsB64)
	if err != nil || !reflect.DeepEqual(decSlots, goPkt.Slots) || !reflect.DeepEqual(decFrames, goPkt.SentenceFrames) {
		t.Fatalf("harmonic wire round-trip failed: err=%v", err)
	}
}

func TestCrossLanguageInlineSymbolAndSEAsianUnitHarmonicParity(t *testing.T) {
	if got := ramesdk.ExtractInlineSymbolUnits("Paid $480 at 95% efficiency and 25°C."); !reflect.DeepEqual(got, []string{"usd", "pct", "degc"}) {
		t.Fatalf("ExtractInlineSymbolUnits = %v, want [usd pct degc]", got)
	}
	if got := ramesdk.ExtractSEAsianUnitWords("ซื้อของหนัก5กิโลกรัมใช้เวลา30นาทีลดราคา20เปอร์เซ็นต์"); !reflect.DeepEqual(got, []string{"กิโลกรัม", "นาที", "เปอร์เซ็นต์"}) {
		t.Fatalf("ExtractSEAsianUnitWords = %v, want [กิโลกรัม นาที เปอร์เซ็นต์]", got)
	}

	secret := "inline-and-sea-unit-harmonic-key"
	docID := "DOC_UNIT_SCAN_01"
	text := "Paid $480 at 95% and 25°C. ซื้อของหนัก5กิโลกรัมใช้เวลา30นาทีลดราคา20เปอร์เซ็นต์!"

	goEnc := ramesdk.NewBlindEncoder([]byte(secret))
	goEnc.SetCanonicalHarmonics(ramesdk.CanonicalHarmonicOptions{
		EnableStructuredDates: true,
		Expander:              ramesdk.LookupClosedSetHarmonics,
	})
	goPkt, _ := goEnc.PackBlindPacket(docID, text, 1700000000)

	seen := make(map[uint64]bool, len(goPkt.Slots))
	for _, s := range goPkt.Slots {
		seen[s.H] = true
	}
	// 统一打包器（plain 正典）的谐波覆盖：$ / % 符号词项直接经闭集扩展器产出
	// 货币/统计标签，泰语闭集词 span 产出质量/时长标签；旧 SDK 私有的
	// ExtractInlineSymbolUnits / ExtractSEAsianUnitWords 自动注入（裸 "usd"/"pct"/"degc"
	// 伴生词与 °C→degc 链）不属于统一脉冲语义，不再期望出现。
	for _, wantTok := range []string{
		"@unit:curr:usd", "@dim:money",
		"@unit:stat:pct", "@dim:stat",
		"กิโลกรัม", "@unit:mass:kg", "@dim:mass",
		"นาที", "@unit:time:min", "@dim:duration",
		"เปอร์เซ็นต์",
	} {
		if !seen[goEnc.KeyedHash64(wantTok)] {
			t.Fatalf("expected canonical harmonic pulse to contain %q", wantTok)
		}
	}
}
