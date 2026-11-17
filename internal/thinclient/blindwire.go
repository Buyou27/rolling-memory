// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"fmt"

	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// Payload codec — delegated 100% to the canonical kernel mirror (`sdk/go/pulse/plain`).
// No duplicate wire codec implementation is maintained in `internal/thinclient`.

// EncodePulsePayload flattens the packet's atoms and sentence frames into the canonical `RMBP` base64 payload string via sdk/go/pulse/plain.
func EncodePulsePayload(pkt BlindPulsePacket) (string, error) {
	return plain.EncodeWirePayload(pkt.Slots, pkt.SentenceFrames)
}

// DecodePulsePayloadB64 is the inverse of EncodePulsePayload, delegating to plain.DecodeWirePayload.
func DecodePulsePayloadB64(s string) ([]BlindTokenAtom, []int, error) {
	atoms, frames, err := plain.DecodeWirePayload(s)
	if err != nil {
		return nil, nil, fmt.Errorf("thinclient: %w", err)
	}
	var sent []int
	if len(frames) > 0 {
		sent = make([]int, 0, len(frames))
		cum := 0
		for _, fr := range frames {
			cum += len(fr)
			sent = append(sent, cum)
		}
	}
	return atoms, sent, nil
}
