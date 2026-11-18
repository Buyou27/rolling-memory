// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/thinclient"
	_ "modernc.org/sqlite"
)

func openTestVault(t *testing.T) *thinclient.Vault {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "vault_signal.db")
	db, err := sql.Open("sqlite", vaultTestDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	v, err := thinclient.NewVault(db, []byte("user-local-master-key-2026"))
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}
	return v
}

func hasWarningCode(rep thinclient.PulseSignalReport, code string) bool {
	for _, w := range rep.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestEvaluatePulseSignal_HealthyMultilingualAndHarmonic(t *testing.T) {
	v := openTestVault(t)

	// 1. Mixed English + CJK + Compound Identifiers
	text := "Rolling-Memory Engine v2.4 uses WaveCellManifold addressing. 洛灵自知记忆引擎在端侧锁死明文，仅上传盲态整数脉冲。 Zero plaintext leaves the local SQLite vault."
	pkt, _ := v.PackBlindPacket("doc-healthy", text, 1700000000)
	rep := thinclient.EvaluatePulseSignal(pkt)

	if rep.Score < 85 || (rep.Grade != "S" && rep.Grade != "A") {
		t.Fatalf("expected healthy score >= 85 (S/A), got score=%d grade=%s warnings=%+v", rep.Score, rep.Grade, rep.Warnings)
	}
	if rep.NeedsAttention {
		t.Fatalf("expected NeedsAttention=false on healthy packet, got warnings=%+v", rep.Warnings)
	}
	if rep.FoldedBiUniSpans < 1 {
		t.Fatalf("expected CJK 2M-1 dual-track span to be recognized as FoldedBiUniSpans >= 1, got %d", rep.FoldedBiUniSpans)
	}
	if rep.CompoundHarmonicGroups < 2 {
		t.Fatalf("expected compound tokens (Rolling-Memory, WaveCellManifold) to be recognized as CompoundHarmonicGroups >= 2, got %d", rep.CompoundHarmonicGroups)
	}

	// 2. Single Chinese sentence with 2M-1 dual track (>48 raw slots, but <48 effective unfolded units) must NOT false-positive DEGENERATE_SENTENCE_FRAMES
	zhSinglePkt, _ := v.PackBlindPacket("doc-zh-single", "Rolling-Memory 现已全面切换为薄客户端架构，不再维护本地核心算法，100% 走云端 /open/memory/v1 接口，绑定 user_id=10。", 1700000005)
	zhRep := thinclient.EvaluatePulseSignal(zhSinglePkt)
	if zhRep.TotalSlots < 48 {
		t.Fatalf("expected raw slots >= 48 on CJK dual-track sentence, got %d", zhRep.TotalSlots)
	}
	if zhRep.NeedsAttention || len(zhRep.Warnings) > 0 || zhRep.Score < 95 {
		t.Fatalf("expected zero warnings and Score >= 95 on normal single CJK sentence after companion-slot unfolding, got score=%d warnings=%+v",
			zhRep.Score, zhRep.Warnings)
	}

	// 3. Second-order adversarial spoofing: fake compound parent (B=160, FlagHasConn) + 8 single-char dust tokens (B=20)
	// must NOT be accepted as a valid compound harmonic group to bypass OVER_FRAGMENTED_DUST.
	spoofSlots := []thinclient.BlindTokenAtom{
		{H: 0xABCDEF01, RawBitsX10: 160, ClassFlags: thinclient.FlagHasConn},
	}
	for i := 0; i < 8; i++ {
		spoofSlots = append(spoofSlots, thinclient.BlindTokenAtom{
			H:          uint64(0x100 + i),
			RawBitsX10: 20,
			ClassFlags: 0,
		})
	}
	spoofRep := thinclient.EvaluatePulseSignal(thinclient.BlindPulsePacket{
		UnitID:         "doc-spoof-dust",
		Slots:          spoofSlots,
		SentenceFrames: [][]thinclient.BlindTokenAtom{spoofSlots},
	})
	if spoofRep.CompoundHarmonicGroups != 0 || !hasWarningCode(spoofRep, thinclient.WarnOverFragmentedDust) {
		t.Fatalf("expected fake single-char dust compound group to be rejected (CompoundHarmonicGroups=0) and flagged as OVER_FRAGMENTED_DUST, got groups=%d warnings=%+v",
			spoofRep.CompoundHarmonicGroups, spoofRep.Warnings)
	}
}

func TestEvaluatePulseSignal_DetectsPathologicalCustomSegmenters(t *testing.T) {
	v := openTestVault(t)
	sample := "Distributed consensus protocols require deterministic state machine replication across fault-tolerant storage nodes. Leader election timers prevent split-brain partitions."

	// Case 1: Coarse unsegmented segmenter with auto-harmonic explicitly disabled (raw pass-through)
	coarsePkt, _ := v.PackBlindPacketWithSegmenter("doc-coarse", sample, 1700000001, thinclient.SegmenterPackOptions{
		Segmenter: func(sentence string) []string {
			return []string{strings.ReplaceAll(sentence, " ", "")}
		},
		DisableAutoHarmonic: true,
	})
	coarseRep := thinclient.EvaluatePulseSignal(coarsePkt)
	if !coarseRep.NeedsAttention || !hasWarningCode(coarseRep, thinclient.WarnCoarseUnsegmentedCarrier) {
		t.Fatalf("expected COARSE_UNSEGMENTED_CARRIER warning on raw glued segmenter, got score=%d warnings=%+v", coarseRep.Score, coarseRep.Warnings)
	}
	if coarseRep.GluedSlotPermille < 900 {
		t.Fatalf("expected GluedSlotPermille >= 900, got %d", coarseRep.GluedSlotPermille)
	}

	// Case 1b: Exact same coarse unsegmented segmenter with DEFAULT options (Adaptive Harmonic ON by default!)
	// must automatically synthesize V13-conserving sub-carrier harmonics and score >= 90 (S) with 0 warnings!
	autoHealedPkt, _ := v.PackBlindPacketWithSegmenter("doc-coarse-autohealed", sample, 1700000001, thinclient.SegmenterPackOptions{
		Segmenter: func(sentence string) []string {
			return []string{strings.ReplaceAll(sentence, " ", "")}
		},
	})
	autoHealedRep := thinclient.EvaluatePulseSignal(autoHealedPkt)
	if autoHealedRep.Score < 90 || autoHealedRep.NeedsAttention || len(autoHealedRep.Warnings) != 0 || autoHealedRep.CompoundHarmonicGroups < 2 {
		t.Fatalf("expected default Adaptive Harmonic mode to heal glued segmenter to Score>=90 (0 warnings, CompoundHarmonicGroups>=2), got score=%d groups=%d warnings=%+v",
			autoHealedRep.Score, autoHealedRep.CompoundHarmonicGroups, autoHealedRep.Warnings)
	}

	// Case 2: Over-fragmented single-character dust segmenter (raw pass-through)
	dustPkt, _ := v.PackBlindPacketWithSegmenter("doc-dust", strings.ToLower(sample), 1700000002, thinclient.SegmenterPackOptions{
		Segmenter: func(sentence string) []string {
			var chars []string
			for _, r := range sentence {
				if r >= 'a' && r <= 'z' {
					chars = append(chars, string(r))
				}
			}
			return chars
		},
		DisableAutoHarmonic: true,
	})
	dustRep := thinclient.EvaluatePulseSignal(dustPkt)
	if !dustRep.NeedsAttention || !hasWarningCode(dustRep, thinclient.WarnOverFragmentedDust) {
		t.Fatalf("expected OVER_FRAGMENTED_DUST warning on single-char segmenter, got score=%d warnings=%+v", dustRep.Score, dustRep.Warnings)
	}
	if dustRep.Score >= 75 {
		t.Fatalf("expected degraded score < 75 on single-char dust segmenter, got %d", dustRep.Score)
	}

	// Case 3: Degenerate sentence framing (60+ slots jammed into 1 single sentence frame)
	var longWords []string
	for i := 0; i < 60; i++ {
		longWords = append(longWords, fmt.Sprintf("module_%02d", i))
	}
	unframedPkt, _ := v.PackBlindPacket("doc-unframed", strings.Join(longWords, " "), 1700000003)
	unframedRep := thinclient.EvaluatePulseSignal(unframedPkt)
	if !hasWarningCode(unframedRep, thinclient.WarnDegenerateSentenceFrames) {
		t.Fatalf("expected DEGENERATE_SENTENCE_FRAMES warning when 60+ slots have only 1 frame, got %+v", unframedRep.Warnings)
	}

	// Case 4: Tandem repetition noise
	var tandemWords []string
	for i := 0; i < 24; i++ {
		tandemWords = append(tandemWords, "sep", "sep", "sep")
	}
	tandemPkt, _ := v.PackBlindPacket("doc-tandem", strings.Join(tandemWords, " "), 1700000004)
	tandemRep := thinclient.EvaluatePulseSignal(tandemPkt)
	if !hasWarningCode(tandemRep, thinclient.WarnHighTandemRedundancy) {
		t.Fatalf("expected HIGH_TANDEM_REDUNDANCY warning, got %+v", tandemRep.Warnings)
	}

	// Case 5: Co-occurrence horizon truncation (>96 unique tokens in one unit)
	var manySents []string
	for s := 0; s < 12; s++ {
		var w []string
		for k := 0; k < 10; k++ {
			w = append(w, fmt.Sprintf("concept_%02d_%02d", s, k))
		}
		manySents = append(manySents, strings.Join(w, " ")+".")
	}
	overflowPkt, _ := v.PackBlindPacket("doc-overflow", strings.Join(manySents, " "), 1700000005)
	overflowRep := thinclient.EvaluatePulseSignal(overflowPkt)
	if overflowRep.CooccurCoveredUnique != 64 || overflowRep.CooccurTruncatedUnique <= 32 {
		t.Fatalf("expected CooccurCoveredUnique=64 and CooccurTruncatedUnique>32, got covered=%d trunc=%d",
			overflowRep.CooccurCoveredUnique, overflowRep.CooccurTruncatedUnique)
	}
	if !hasWarningCode(overflowRep, thinclient.WarnCooccurHorizonTruncated) {
		t.Fatalf("expected COOCCUR_HORIZON_TRUNCATED warning, got %+v", overflowRep.Warnings)
	}
}

func TestPackBlindPacketWithSegmenter_HarmonicOverlayPreservesRecallAndConservation(t *testing.T) {
	v := openTestVault(t)

	// User custom domain segmenter configured directly on Vault via SetWordSegmenter (Adaptive Harmonic ON by default)
	customSeg := func(sentence string) []string {
		return []string{"zero plaintext vault", "protects", "blind pulse stream"}
	}
	v.SetWordSegmenter(customSeg)
	pkt, _ := v.PackBlindPacket(
		"doc-harmonic",
		"zero plaintext vault protects blind pulse stream.",
		1700000010,
	)
	v.SetWordSegmenter(nil)

	// Also verify default SplitWords path automatically synthesizes sub-carrier harmonics on runaway unspaced Kana/glued runs (>42 bits)
	kanaPkt, _ := v.PackBlindPacket(
		"doc-runaway-kana",
		"ありがとうございますよろしくお願い致します。",
		1700000011,
	)
	kanaRep := thinclient.EvaluatePulseSignal(kanaPkt)
	if kanaRep.GluedSlotCount != 0 || kanaRep.CompoundHarmonicGroups < 1 || kanaRep.Score < 90 {
		t.Fatalf("expected default PackBlindPacket to auto-harmonize long Kana run (GluedSlotCount=0, CompoundHarmonicGroups>=1, Score>=90), got %+v", kanaRep)
	}

	rep := thinclient.EvaluatePulseSignal(pkt)
	if rep.CompoundHarmonicGroups < 2 {
		t.Fatalf("expected >= 2 V13-compatible CompoundHarmonicGroups from HarmonicOverlay, got %d", rep.CompoundHarmonicGroups)
	}
	if rep.Score < 85 || rep.NeedsAttention {
		t.Fatalf("expected high signal score >= 85 with zero warnings on HarmonicOverlay packet, got score=%d warnings=%+v", rep.Score, rep.Warnings)
	}

	// Verify both the custom phrase hash ("zero plaintext vault") AND standard subword hashes ("zero", "plaintext", "vault")
	// exist in the emitted slots so both custom-segmented queries and standard SplitWords queries match!
	slotHashes := make(map[uint64]bool, len(pkt.Slots))
	for _, s := range pkt.Slots {
		slotHashes[s.H] = true
	}
	for _, token := range []string{"zero plaintext vault", "zero", "plaintext", "vault", "blind pulse stream", "blind", "pulse", "stream"} {
		wantH := v.KeyedHash64(token)
		if !slotHashes[wantH] {
			t.Fatalf("expected slotHashes to contain commitment for %q (H=%x)", token, wantH)
		}
	}
}

func TestInspectVaultPulseSignalAndCloudIngestTelemetryWireRoundTrip(t *testing.T) {
	v := openTestVault(t)
	cli := thinclient.NewClient(v, thinclient.NewLocalVaultTransport(v))

	if _, err := cli.Remember(
		context.Background(),
		"DOC-SIG-1",
		"Rolling-Memory Engine computes wave resonance over blind integer pulses. Local SQLite vault retains full plaintext sovereignty.",
		1700000100,
	); err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	vaultRep, err := v.InspectVaultPulseSignal(16, thinclient.SegmenterPackOptions{})
	if err != nil {
		t.Fatalf("InspectVaultPulseSignal failed: %v", err)
	}
	if vaultRep.Score < 85 || vaultRep.TotalSlots == 0 {
		t.Fatalf("expected InspectVaultPulseSignal Score >= 85, got %+v", vaultRep)
	}
}
