// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package local is the local-only subset of the SDK: the pluggable plaintext
// store, the client-side encoder facade, and the shared public DTOs and
// constants that both the local store and the cloud client build on. This
// package performs no network I/O; the cloud client (package ramesdk)
// re-exports its surface so existing callers keep compiling unchanged.
package local

const (
	FlagHasUpper    uint8 = 1 << 0
	FlagHasCamel    uint8 = 1 << 1
	FlagHasDigit    uint8 = 1 << 2
	FlagHasConn     uint8 = 1 << 3
	LayerBackground       = "background"
	LayerEpisodic         = "episodic"
	LayerNoetic           = "noetic"

	// Canary* constants define the built-in self-test vector shared by all 4 SDKs and `/open/memory/v1/health`.
	CanarySecret    = "rame-codec-canary-key-v1"
	CanaryDocID     = "CANARY_V1"
	CanaryText      = "RollingMemory v1.0.1 WaveEngine_256 洛灵记忆. Zero plaintext!"
	CanaryTS        = int64(1700000000)
	CanarySlotCount = 21
	CanarySentCount = 2
	CanaryAtomsB64  = "Uk1CUAEAPA0AFAAAABUAAhQhUFBjZHiLAYwBtAHIAYQCtQJAEATBATAPB0jHISlR96QHZAI40Tt3T0fPAxxcTN+orGsN87QYgKG+M3wAaBUxyxkJx8f8zRbMt024NoH4VZEtimANzWUASZ6GWiX3Z7rb64Eq7inVwwMYS98I2I7VbHWwUFW4WszbtBQnTCgoPkEZy9pjGebXDmmqB9vwbo0DeZvaEa9TH6Ti6fHUlg3MbBKkCYTXBbqHJcBSS+ORCU+K9G0qrLWQNLboQC4oYxzk6gIUARCElBIB"
)

// ModeLocalPlain identifies the local offline plaintext vault (zero cloud traffic).
// Cloud pulse modes live in the cloud client package, not in this local subset.
const ModeLocalPlain = "local_plain"

// UnitMeta carries optional speaker, tri-temporal metadata (narrated_at, occurred_at, ingested_at),
// and 4D spatial coordinates (file_url/doc_h, block_id/block_h, line_start, line_end, file_parse).
// All fields are strictly opt-in: unset fields stay empty/zero and produce zero prefix or wire overhead.
type BlindTokenAtom struct {
	H          uint64 `json:"h"`
	RawBitsX10 uint16 `json:"b"`
	ClassFlags uint8  `json:"f"`
}

type BlindPulsePacket struct {
	TenantID         string             `json:"tenant_id,omitempty"`
	Scope            string             `json:"scope,omitempty"`
	UserID           string             `json:"user_id,omitempty"`
	AssistantID      string             `json:"assistant_id,omitempty"`
	IncarnationID    string             `json:"incarnation_id,omitempty"`
	UnitID           string             `json:"id"`
	Layer            string             `json:"layer,omitempty"`
	CitedUnitIDs     []string           `json:"cited_unit_ids,omitempty"`
	OccurredAt       int64              `json:"ts,omitempty"`
	NarratedAt       int64              `json:"narrated_at,omitempty"`
	IngestedAt       int64              `json:"ingested_at,omitempty"`
	AuthorHash       uint64             `json:"author_h,omitempty"`
	DocHash          uint64             `json:"doc_h,omitempty"`
	BlockHash        uint64             `json:"block_h,omitempty"`
	LineStart        int                `json:"line_start,omitempty"`
	LineEnd          int                `json:"line_end,omitempty"`
	Slots            []BlindTokenAtom   `json:"slots,omitempty"`
	SentenceFrames   [][]BlindTokenAtom `json:"sents,omitempty"`
	DenoiseRatio     *float64           `json:"denoise_ratio,omitempty"`
	MinKeepSentences int                `json:"min_keep_sentences,omitempty"`
	WaveRadix        int                `json:"wave_radix,omitempty"`
	AtomsB64         string             `json:"atoms_b64,omitempty"`
	RawText          string             `json:"raw_text,omitempty"`
	PulseMode        string             `json:"pulse_mode,omitempty"`
}

type IngestResponse struct {
	TenantID            string               `json:"tenant_id,omitempty"`
	Scope               string               `json:"scope,omitempty"`
	PulseMode           string               `json:"pulse_mode,omitempty"`
	UnitID              string               `json:"id"`
	Layer               string               `json:"layer,omitempty"`
	CitedUnitIDs        []string             `json:"cited_unit_ids,omitempty"`
	Epoch               int                  `json:"epoch"`
	ConservedMass       float64              `json:"conserved_mass"`
	QualityMult         float64              `json:"quality_mult"`
	AddressURI          string               `json:"address_uri"`
	AddressCoord64      uint64               `json:"address_coord64"`
	ContainerID         uint64               `json:"container_id"`
	ParentID            uint64               `json:"parent_id"`
	ShiftScore          float64              `json:"shift_score"`
	KeptSentenceIndices []int                `json:"kept_sentence_indices,omitempty"`
	CompressionRatio    float64              `json:"compression_ratio,omitempty"`
	PulseSignalScore    int                  `json:"pulse_signal_score,omitempty"`
	PulseSignalGrade    string               `json:"pulse_signal_grade,omitempty"`
	PulseSignalWarnings []PulseSignalWarning `json:"pulse_signal_warnings,omitempty"`
}

type SentenceAtomPacker func(enc *BlindEncoder, sentence string, sentIdx int) []BlindTokenAtom

// CanonicalHarmonicOptions configures opt-in client-side canonical harmonic overlay
// (structured ISO date canonicalization + optional domain/ontology harmonic expander).
type CanonicalHarmonicOptions struct {
	EnableStructuredDates bool
	Expander              HarmonicExpander
}

type SegmenterPackOptions struct {
	Segmenter           WordSegmenter
	SentenceAtomPacker  SentenceAtomPacker
	HarmonicOverlay     bool
	DisableAutoHarmonic bool
}

type QueryHit struct {
	UnitID         string   `json:"id"`
	Layer          string   `json:"layer,omitempty"`
	CitedUnitIDs   []string `json:"cited_unit_ids,omitempty"`
	OccurredAt     int64    `json:"occurred_at,omitempty"`
	NarratedAt     int64    `json:"narrated_at,omitempty"`
	IngestedAt     int64    `json:"ingested_at,omitempty"`
	AuthorHash     uint64   `json:"author_h,omitempty"`
	DocHash        uint64   `json:"doc_h,omitempty"`
	BlockHash      uint64   `json:"block_h,omitempty"`
	LineStart      int      `json:"line_start,omitempty"`
	LineEnd        int      `json:"line_end,omitempty"`
	AddressURI     string   `json:"address_uri"`
	AddressCoord64 uint64   `json:"address_coord64"`
	WaveScore      float64  `json:"wave_score"`
	ManifoldGain   float64  `json:"manifold_gain"`
	FinalWeight    float64  `json:"final_weight"`
	// DirectWeight is the candidate's own query-collision evidence (Nats): direct
	// term collisions plus within-record structural/proximity terms, excluding the
	// Hop1/Hop2 energy borrowed from same-cluster neighbors. Delivery stopping uses
	// it as the reference scale so borrowed cluster mass cannot raise the envelope
	// against an isolated-date record that carries complementary evidence.
	DirectWeight        float64 `json:"direct_weight,omitempty"`
	KeptSentenceIndices []int   `json:"kept_sentence_indices,omitempty"`
	RawText             string  `json:"raw_text,omitempty"`
	DistilledText       string  `json:"distilled_text,omitempty"`
}

type LocalNoeticRecord struct {
	NoeticID     string   `json:"noetic_id"`
	OccurredAt   int64    `json:"occurred_at"`
	QueryText    string   `json:"query_text"`
	AnswerText   string   `json:"answer_text"`
	CitedUnitIDs []string `json:"cited_unit_ids"`
	// AssistantID / IncarnationID pin the crystallizing persona and its concrete
	// lifetime instance, so parallel incarnations never overwrite each other's answer.
	AssistantID   string `json:"assistant_id,omitempty"`
	IncarnationID string `json:"incarnation_id,omitempty"`
}

type ResolvedHit struct {
	QueryHit
	RawPlaintext       string   `json:"raw_plaintext"`
	DistilledPlaintext string   `json:"distilled_plaintext"`
	KeptSentences      []string `json:"kept_sentences"`
}
