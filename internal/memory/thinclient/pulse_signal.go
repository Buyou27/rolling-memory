// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"strings"

	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// PulseSignalWarningCode constants and types are canonical aliases to `sdk/go/pulse/plain`.
const (
	WarnCoarseUnsegmentedCarrier = plain.WarnCoarseUnsegmentedCarrier
	WarnOverFragmentedDust       = plain.WarnOverFragmentedDust
	WarnFlatCarrierModulation    = plain.WarnFlatCarrierModulation
	WarnDegenerateSentenceFrames = plain.WarnDegenerateSentenceFrames
	WarnHighTandemRedundancy     = plain.WarnHighTandemRedundancy
	WarnCooccurHorizonTruncated  = plain.WarnCooccurHorizonTruncated
)

type WordSegmenter = plain.WordSegmenter
type SegmenterPackOptions = plain.SegmenterPackOptions
type PulseSignalWarning = plain.PulseSignalWarning
type PulseSignalReport = plain.PulseSignalReport

// SetWordSegmenter configures a custom WordSegmenter on the Vault with endogenous adaptive harmonic
// emission enabled by default.
func (v *Vault) SetWordSegmenter(seg WordSegmenter) {
	if v == nil {
		return
	}
	v.segOpts = SegmenterPackOptions{
		Segmenter:       seg,
		HarmonicOverlay: true,
	}
	v.clearSlotCache()
}

// SetSegmenterOptions configures custom segmentation and harmonic options on the Vault.
func (v *Vault) SetSegmenterOptions(opts SegmenterPackOptions) {
	if v == nil {
		return
	}
	v.segOpts = opts
	v.clearSlotCache()
}

// SetCanonicalHarmonics configures opt-in structured date beacons and canonical harmonic expansion on the Vault.
// Passing an empty CanonicalHarmonicOptions{} explicitly disables automatic default harmonics for raw/baseline comparison.
func (v *Vault) SetCanonicalHarmonics(opts plain.CanonicalHarmonicOptions) {
	if v == nil {
		return
	}
	v.harmOpts = opts
	v.harmOptsExplicit = true
	v.clearSlotCache()
}

// PackBlindPacketWithSegmenter packs plaintext into a BlindPulsePacket using pluggable SegmenterPackOptions via sdk/go/pulse/plain.
func (v *Vault) PackBlindPacketWithSegmenter(docID string, text string, occurredAt int64, opts SegmenterPackOptions) (BlindPulsePacket, []string) {
	enc := plain.NewBlindEncoderFromRawKey(v.secretKey)
	if v != nil {
		enc.SetCanonicalHarmonics(v.harmOpts)
	}
	var rmPkt plain.BlindPulsePacket
	var sents []string
	if opts.Segmenter == nil && opts.SentenceAtomPacker == nil {
		rmPkt, sents = enc.PackBlindPacket(docID, text, occurredAt)
	} else {
		rmPkt, sents = enc.PackBlindPacketWithSegmenter(docID, text, occurredAt, opts)
	}
	return BlindPulsePacket{
		UnitID:         rmPkt.UnitID,
		OccurredAt:     rmPkt.OccurredAt,
		Slots:          rmPkt.Slots,
		SentenceFrames: rmPkt.SentenceFrames,
	}, sents
}

// EvaluatePulseSignal computes a 100% plaintext-free segmentation quality report from a BlindPulsePacket via sdk/go/pulse/plain.
func EvaluatePulseSignal(pkt BlindPulsePacket) PulseSignalReport {
	return plain.EvaluatePulseSignal(plain.BlindPulsePacket{
		UnitID:         pkt.UnitID,
		OccurredAt:     pkt.OccurredAt,
		Slots:          pkt.Slots,
		SentenceFrames: pkt.SentenceFrames,
	})
}

// InspectVaultPulseSignal samples up to sampleLimit documents from the local SQLite Vault,
// packs each with the provided SegmenterPackOptions, and returns the aggregate PulseSignalReport.
func (v *Vault) InspectVaultPulseSignal(sampleLimit int, opts SegmenterPackOptions) (PulseSignalReport, error) {
	if v == nil || v.db == nil {
		return PulseSignalReport{Score: 100, Grade: "EMPTY"}, nil
	}
	if sampleLimit <= 0 {
		sampleLimit = 64
	}
	rows, err := v.db.Query(`SELECT doc_id, occurred_at, raw_text FROM thin_vault_docs ORDER BY occurred_at DESC LIMIT ?`, sampleLimit)
	if err != nil {
		return PulseSignalReport{}, err
	}
	defer rows.Close()

	var merged BlindPulsePacket
	for rows.Next() {
		var docID, rawText string
		var occ int64
		if err := rows.Scan(&docID, &occ, &rawText); err != nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(rawText), "@ref:") {
			if live, rErr := ResolveEpisodicPointerLive(strings.TrimSpace(rawText)); rErr == nil && strings.TrimSpace(live) != "" {
				rawText = live
			}
		}
		pkt, _ := v.PackBlindPacketWithSegmenter(docID, rawText, occ, opts)
		if len(pkt.Slots) == 0 {
			continue
		}
		merged.Slots = append(merged.Slots, pkt.Slots...)
		merged.SentenceFrames = append(merged.SentenceFrames, pkt.SentenceFrames...)
	}
	rep := EvaluatePulseSignal(merged)
	return rep, nil
}
