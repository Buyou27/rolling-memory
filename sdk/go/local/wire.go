// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package local

import "strings"

// ============================================================================
// Canonical RAME-BlindPulse-v1 Binary Wire Codec ("RMBP" v1 · Mixed-Radix Golomb-Rice)
// ============================================================================

// NormalizeWire expands `AtomsB64` into `Slots` and `SentenceFrames` idempotently.
func (p *BlindPulsePacket) NormalizeWire() error {
	if p == nil || p.AtomsB64 == "" {
		return nil
	}
	slots, frames, err := DecodeWirePayload(p.AtomsB64)
	if err != nil {
		return err
	}
	p.Slots = slots
	p.SentenceFrames = frames
	p.AtomsB64 = ""
	return nil
}

// WirePacket returns a wire-ready copy of p carrying the canonical `atoms_b64` payload
// and omitting the redundant `slots` and `sents` JSON arrays.
func (p BlindPulsePacket) WirePacket() (BlindPulsePacket, error) {
	out := p
	if strings.TrimSpace(out.AtomsB64) == "" {
		slots := out.Slots
		if len(slots) == 0 && len(out.SentenceFrames) > 0 {
			for _, fr := range out.SentenceFrames {
				slots = append(slots, fr...)
			}
		}
		b64, err := EncodeWirePayload(slots, out.SentenceFrames)
		if err != nil {
			return BlindPulsePacket{}, err
		}
		out.AtomsB64 = b64
	}
	out.Slots = nil
	out.SentenceFrames = nil
	return out, nil
}
