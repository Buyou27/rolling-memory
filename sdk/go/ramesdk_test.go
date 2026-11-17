// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package ramesdk_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	ramesdk "github.com/rocareer/rolling-memory/sdk/go"
)

type goldenVectorCase struct {
	Name      string                   `json:"name"`
	Secret    string                   `json:"secret"`
	DocID     string                   `json:"doc_id"`
	Text      string                   `json:"text"`
	Sentences []string                 `json:"sentences"`
	Packet    ramesdk.BlindPulsePacket `json:"packet"`
}

type goldenVectorSuite struct {
	Spec  string             `json:"spec"`
	Cases []goldenVectorCase `json:"cases"`
}

// TestCrossLanguageSDKGoldenParity verifies 100% bit-for-bit parity of
// `sdk/go` (`ramesdk.BlindEncoder`) against frozen `sdk/go/spec/golden_vectors.json`.
func TestCrossLanguageSDKGoldenParity(t *testing.T) {
	specPath := filepath.Join("spec", "golden_vectors.json")
	rawGolden, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	var frozenSuite goldenVectorSuite
	if err := json.Unmarshal(rawGolden, &frozenSuite); err != nil {
		t.Fatalf("unmarshal %s: %v", specPath, err)
	}
	if frozenSuite.Spec != ramesdk.SpecVersion {
		t.Fatalf("spec version = %q, want %q", frozenSuite.Spec, ramesdk.SpecVersion)
	}
	if len(frozenSuite.Cases) < 3 {
		t.Fatalf("expected at least 3 golden cases, got %d", len(frozenSuite.Cases))
	}

	for i, tc := range frozenSuite.Cases {
		goEnc := ramesdk.NewBlindEncoder([]byte(tc.Secret))
		goPkt, goSents := goEnc.PackBlindPacket(tc.DocID, tc.Text, 1700000000)

		if os.Getenv("UPDATE_GOLDEN") == "1" {
			frozenSuite.Cases[i].Sentences = goSents
			frozenSuite.Cases[i].Packet = goPkt
			tc = frozenSuite.Cases[i]
		}

		// Verify against frozen golden_vectors.json (strict read-only drift gate)
		if !reflect.DeepEqual(goSents, tc.Sentences) || !reflect.DeepEqual(goPkt, tc.Packet) {
			t.Fatalf("[%s] Go SDK drifted from frozen golden_vectors.json!\n  got:  %+v\n  want: %+v", tc.Name, goPkt, tc.Packet)
		}
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		outRaw, err := json.MarshalIndent(frozenSuite, "", "  ")
		if err != nil {
			t.Fatalf("marshal golden vectors: %v", err)
		}
		if err := os.WriteFile(specPath, append(outRaw, '\n'), 0o644); err != nil {
			t.Fatalf("write %s: %v", specPath, err)
		}
	}
}

// TestCrossLanguageAdversarialUnicodeAndWireRejects attacks:
//  1. Special Unicode case-folding (Turkish 'İ' U+0130, Greek final 'Σ' U+03A3), U+3000 full-width space trim,
//     non-BMP Emoji/CJK-ExtB, and `mem2.*` signed-key pepper derivation;
//  2. The `palN > 255` direct B/F fallback branch (260 distinct (B,F) pairs -> palN=0);
//  3. Canonical wire rejection of corrupted/non-canonical RMBP payloads.
func TestCrossLanguageAdversarialUnicodeAndWireRejects(t *testing.T) {
	// 1. Unicode & mem2 pepper adversarial corpus
	secret := "mem2.eyJ0aWQiOiJ1c2VyIiwidWlkIjoiMTAifQ.sig"
	advTexts := []string{
		"İstanbul ΛΟΓΟΣ Σωκράτης \u3000全角空格\u3000",
		"Emoji 💡✨🚀 CJK-ExtB 𠀀𠀁𠀂 combining e\u0301 a-b_c.d .net v1.2.3",
		"aaaaAAa 000000 ---___...",
		"   \u00a0\u3000  !?;  \u3000 ",
	}
	goEnc := ramesdk.NewBlindEncoder([]byte(secret))
	for idx, txt := range advTexts {
		docID := "ADV_UNI"
		goPkt, _ := goEnc.PackBlindPacket(docID, txt, 1700000000)
		if goPkt.AtomsB64 != "" {
			decSlots, decFrames, err := ramesdk.DecodeWirePayload(goPkt.AtomsB64)
			if err != nil || !reflect.DeepEqual(decSlots, goPkt.Slots) || !reflect.DeepEqual(decFrames, goPkt.SentenceFrames) {
				t.Fatalf("ADV_%d wire round-trip failed: err=%v", idx, err)
			}
		}
	}

	// 2. palN > 255 direct B/F fallback + corrupted wire rejections
	slots := make([]ramesdk.BlindTokenAtom, 260)
	for i := 0; i < 260; i++ {
		slots[i] = ramesdk.BlindTokenAtom{
			H:          uint64(i+1) * 0x0080000000000001,
			RawBitsX10: uint16(10 + (i/16)*10),
			ClassFlags: uint8(i % 16),
		}
	}
	frames := [][]ramesdk.BlindTokenAtom{slots[:130], slots[130:]}
	goB64, err := ramesdk.EncodeWirePayload(slots, frames)
	if err != nil {
		t.Fatalf("EncodeWirePayload(palN>255): %v", err)
	}
	decSlots, decFrames, err := ramesdk.DecodeWirePayload(goB64)
	if err != nil || !reflect.DeepEqual(decSlots, slots) || !reflect.DeepEqual(decFrames, frames) {
		t.Fatalf("Go palN>255 round-trip failed: err=%v", err)
	}

	// 3. Verify Go rejects corrupted payloads (trailing byte, bad flags, bad magic, non-minimal varint)
	raw, err := base64.StdEncoding.DecodeString(ramesdk.CanaryAtomsB64)
	if err != nil {
		t.Fatalf("decode CanaryAtomsB64: %v", err)
	}
	cloneMutate := func(fn func([]byte) []byte) []byte {
		cp := make([]byte, len(raw))
		copy(cp, raw)
		return fn(cp)
	}
	mutations := [][]byte{
		append(cloneMutate(func(b []byte) []byte { return b }), 0),
		cloneMutate(func(b []byte) []byte { b[0] = 88; return b }),
		cloneMutate(func(b []byte) []byte { b[4] = 99; return b }),
		cloneMutate(func(b []byte) []byte { b[5] |= 0x80; return b }),
		cloneMutate(func(b []byte) []byte { b[6] = 9; return b }),
		cloneMutate(func(b []byte) []byte { b[7] = 99; return b }),
		cloneMutate(func(b []byte) []byte { b[16] |= 0x80; return b }),
		cloneMutate(func(b []byte) []byte { b[len(b)-1] = 99; return b }),
	}
	for i, mut := range mutations {
		if _, _, err := ramesdk.DecodeWireBytes(mut); err == nil {
			t.Fatalf("Go failed to reject mutation #%d", i)
		}
	}
}
