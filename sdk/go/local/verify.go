// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package local

import (
	"fmt"
)

// VerifyCodecLocal executes an in-process round-trip self-test of all 5 RAME-BlindPulse-v1 stages
// (KeyedHash64 -> SplitSentences -> SplitWords -> CarrierUnits/Flags -> RMBP AtomsB64 encode & decode)
// against the pinned canary vector without any network I/O.
func VerifyCodecLocal() error {
	enc := NewBlindEncoder([]byte(CanarySecret))
	pkt, sents := enc.PackBlindPacket(CanaryDocID, CanaryText, CanaryTS)
	if len(sents) != CanarySentCount || len(pkt.Slots) != CanarySlotCount {
		return fmt.Errorf("local: local codec canary count mismatch (got slots=%d sents=%d, want slots=%d sents=%d)",
			len(pkt.Slots), len(sents), CanarySlotCount, CanarySentCount)
	}
	if pkt.AtomsB64 != CanaryAtomsB64 {
		return fmt.Errorf("local: local codec canary atoms_b64 drift (got %q, want %q)", pkt.AtomsB64, CanaryAtomsB64)
	}
	decSlots, decFrames, err := DecodeWirePayload(pkt.AtomsB64)
	if err != nil {
		return fmt.Errorf("local: local codec canary decode failed: %w", err)
	}
	if len(decSlots) != len(pkt.Slots) || len(decFrames) != len(pkt.SentenceFrames) {
		return fmt.Errorf("local: local codec canary round-trip size mismatch")
	}
	for i := range pkt.Slots {
		if decSlots[i] != pkt.Slots[i] {
			return fmt.Errorf("local: local codec canary slot[%d] mismatch: got %+v want %+v", i, decSlots[i], pkt.Slots[i])
		}
	}
	for i := range pkt.SentenceFrames {
		if len(decFrames[i]) != len(pkt.SentenceFrames[i]) {
			return fmt.Errorf("local: local codec canary frame[%d] size mismatch", i)
		}
		for j := range pkt.SentenceFrames[i] {
			if decFrames[i][j] != pkt.SentenceFrames[i][j] {
				return fmt.Errorf("local: local codec canary frame[%d][%d] mismatch: got %+v want %+v", i, j, decFrames[i][j], pkt.SentenceFrames[i][j])
			}
		}
	}
	return nil
}
