// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"strings"
)

// PlaintextStore is the universal pluggable storage interface for user plaintext.
type PlaintextStore interface {
	// PackBlindPacket mechanically splits raw plaintext into sentences and encodes
	// each word into an irreversible blind integer pulse `{h, b, f}` using the user's secret key.
	PackBlindPacket(docID string, text string, occurredAt int64) (BlindPulsePacket, []string)

	// SaveLocalPlaintext persists the user's raw plaintext alongside the engine-assigned
	// cell address, waveform bar, and kept dry-sentence indices.
	SaveLocalPlaintext(docID string, occurredAt int64, rawText string, sents []string, resp IngestResponse) error

	// LoadLocalDocument loads the user's raw plaintext and reconstructs the distilled
	// dry text from `kept_sentence_indices` when the engine returns a hit for `docID`.
	LoadLocalDocument(docID string) (rawText string, distilledText string, keptSents []string, err error)
}

// Compile-time check that the default SQLite `*Vault` implements `PlaintextStore`.
var _ PlaintextStore = (*Vault)(nil)

// ReconstructDistilled reconstructs distilled dry text from `allSents` and `keptIdx`.
func ReconstructDistilled(rawText string, allSents []string, keptIdx []int) (distilledText string, keptSents []string) {
	if len(keptIdx) == 0 || len(allSents) == 0 || len(keptIdx) >= len(allSents) {
		return rawText, allSents
	}
	for _, idx := range keptIdx {
		if idx >= 0 && idx < len(allSents) {
			keptSents = append(keptSents, allSents[idx])
		}
	}
	if len(keptSents) == 0 || len(keptSents) >= len(allSents) {
		return rawText, allSents
	}
	sep := "\n"
	if !strings.Contains(rawText, "\n") {
		sep = ". "
	}
	return strings.Join(keptSents, sep), keptSents
}
