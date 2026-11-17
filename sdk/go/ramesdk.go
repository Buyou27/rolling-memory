// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package ramesdk implements the official Go Client SDK and RAME-BlindPulse-v1
// deterministic blind integer pulse encoder for the Rolling Autonoetic Memory Engine (`/open/memory/v1/*`).
package ramesdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SpecVersion           = "RAME-BlindPulse-v1"
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

// SDKVersion stays on its own line: the SDK version gate extracts the Go
// side with a single-space assignment pattern, so a gofmt-aligned const block would
// silently blank the gate.
const SDKVersion = "1.11.3"

// DefaultAutoTopK (0) selects RB's endogenous adaptive delivery budget (0..96 hits).
// Pass topK > 0 (e.g. 1..96) to override with an explicit cap.
const (
	DefaultAutoTopK     = 0
	MaxAutoDeliveryTopK = 96
)

// RAME Client Operating Modes (100% Symmetric [local | pulse] × [plain | cipher]):
//   - ModeLocalPlain  ("local_plain"):  100% local offline plaintext vault, zero cloud traffic;
//   - ModePulsePlain  ("pulse_plain"):  cloud pulse + plaintext mode (RB directly receives/returns plaintext);
//   - ModePulseCipher ("pulse_cipher"): cloud irreversible cipher pulse mode (0 plaintext leaves client).
const (
	ModeLocalPlain  = "local_plain"
	ModePulsePlain  = "pulse_plain"
	ModePulseCipher = "pulse_cipher"
)

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

type CloudIngestResponse struct {
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

type CloudQueryHit struct {
	UnitID              string   `json:"id"`
	Layer               string   `json:"layer,omitempty"`
	CitedUnitIDs        []string `json:"cited_unit_ids,omitempty"`
	OccurredAt          int64    `json:"occurred_at,omitempty"`
	NarratedAt          int64    `json:"narrated_at,omitempty"`
	IngestedAt          int64    `json:"ingested_at,omitempty"`
	AuthorHash          uint64   `json:"author_h,omitempty"`
	DocHash             uint64   `json:"doc_h,omitempty"`
	BlockHash           uint64   `json:"block_h,omitempty"`
	LineStart           int      `json:"line_start,omitempty"`
	LineEnd             int      `json:"line_end,omitempty"`
	AddressURI          string   `json:"address_uri"`
	AddressCoord64      uint64   `json:"address_coord64"`
	WaveScore           float64  `json:"wave_score"`
	CellWormhole        float64  `json:"cell_wormhole"`
	ManifoldGain        float64  `json:"manifold_gain"`
	FinalWeight         float64  `json:"final_weight"`
	KeptSentenceIndices []int    `json:"kept_sentence_indices,omitempty"`
	RawText             string   `json:"raw_text,omitempty"`
	DistilledText       string   `json:"distilled_text,omitempty"`
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

// FTS5BM25TermMinCarrierX10 是本地 SQLite FTS5/BM25 词项纳入查询的最小机械载体宽度
// （×10）：60 ≈ 3 个拉丁字母 / 2 个汉字，低于它的词项被本地 FTS/BM25 视为噪声丢弃。
const FTS5BM25TermMinCarrierX10 = 60

// FTS5BM25Segmenter 复现本地 SQLite FTS5/BM25 索引的词项流：对一句话取
// SplitWords（表面词 + 复合子词，与 QueryFTS5BM25 完全同源），只保留机械载体宽度
// ≥ FTS5BM25TermMinCarrierX10 的词项，按首次出现顺序返回且**保留重复**
// （重复进入盲态波形的 tf 统计，与 FTS 倒排的词频同口径）。
//
// 用途：把它挂到 `BlindEncoder.SetSegmenterOptions` 后打包的盲态脉冲，其词项集合与
// 本地 FTS5/BM25 检索词表逐项对齐——端侧只负责切词与盲化，排序仍 100% 由云端
// `the memory engine` 完成，绝不与本地 FTS 检索结果做并集或融合。
func FTS5BM25Segmenter(sentence string) []string {
	words := SplitWords(sentence)
	if len(words) == 0 {
		return nil
	}
	out := make([]string, 0, len(words))
	for _, w := range words {
		low := strings.ToLower(strings.TrimSpace(w))
		if low == "" {
			continue
		}
		var carrier uint32
		for _, r := range low {
			carrier += uint32(CarrierUnits(r))
		}
		if carrier < FTS5BM25TermMinCarrierX10 {
			continue
		}
		out = append(out, low)
	}
	return out
}

func resolveSubUnitIDAndText(parentID string, su SubUnit, kindCounts map[string]int) (subID, kind, txt string, idx int, ok bool) {
	txt = strings.TrimSpace(su.Text)
	fp := strings.TrimSpace(su.FileParse)
	if txt == "" {
		txt = fp
	} else if fp != "" && !strings.Contains(txt, fp) {
		txt = txt + "\n" + fp
	}
	if txt == "" {
		return "", "", "", 0, false
	}
	if kind = strings.TrimSpace(su.Kind); kind == "" {
		kind = "sub"
	}
	idx = kindCounts[kind]
	kindCounts[kind] = idx + 1
	if subID = strings.TrimSpace(su.ID); subID == "" {
		subID = ComposeSubUnitID(parentID, kind, idx)
	}
	return subID, kind, txt, idx, true
}

// PlaintextStore 的 SaveLocalPlaintext 必须按 docID 幂等 upsert，不得依赖调用方预先删除；
// 这样远端更新失败时旧原文仍保留，重试同一 docID 即可完成对账。
type PlaintextStore interface {
	PackBlindPacket(docID string, text string, occurredAt int64) (BlindPulsePacket, []string)
	SaveLocalPlaintext(docID string, occurredAt int64, rawText string, sents []string, resp CloudIngestResponse) error
	LoadLocalDocument(docID string) (rawText string, distilledText string, keptSents []string, err error)
}

func distillPlaintext(rawText string, allSents []string, keptIdx []int) (string, string, []string, error) {
	if len(keptIdx) == 0 || len(allSents) == 0 || len(keptIdx) >= len(allSents) {
		return rawText, rawText, allSents, nil
	}
	keptSents := make([]string, 0, len(keptIdx))
	for _, idx := range keptIdx {
		if idx >= 0 && idx < len(allSents) {
			keptSents = append(keptSents, allSents[idx])
		}
	}
	if len(keptSents) == 0 || len(keptSents) >= len(allSents) {
		return rawText, rawText, allSents, nil
	}
	sep := "\n"
	if !strings.Contains(rawText, "\n") {
		sep = ". "
	}
	return rawText, strings.Join(keptSents, sep), keptSents, nil
}

type jsonDocRecord struct {
	DocID       string   `json:"doc_id"`
	OccurredAt  int64    `json:"occurred_at"`
	RawText     string   `json:"raw_text"`
	Sentences   []string `json:"sentences"`
	AddressURI  string   `json:"address_uri"`
	Coord64     uint64   `json:"coord64"`
	KeptIndices []int    `json:"kept_indices"`
}

type JSONFileStore struct {
	*BlindEncoder
	mu          sync.RWMutex
	filePath    string
	docs        map[string]jsonDocRecord
	subToParent map[string]string
}

var _ PlaintextStore = (*JSONFileStore)(nil)

func NewJSONFileStore(path string, secret []byte) (*JSONFileStore, error) {
	filePath := strings.TrimSpace(path)
	if filePath == "" {
		return nil, errors.New("ramesdk: empty json file path")
	}
	s := &JSONFileStore{BlindEncoder: NewBlindEncoder(secret), filePath: filePath, docs: make(map[string]jsonDocRecord), subToParent: make(map[string]string)}
	raw, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	} else if err != nil {
		return nil, fmt.Errorf("ramesdk: read JSON store: %w", err)
	}
	if err := json.Unmarshal(raw, &s.docs); err != nil || s.docs == nil {
		return nil, fmt.Errorf("ramesdk: decode JSON store: %v", err)
	}
	return s, nil
}

func (s *JSONFileStore) SaveSubUnitMapping(subUnitID, parentUnitID string) {
	if subUnitID, parentUnitID = strings.TrimSpace(subUnitID), strings.TrimSpace(parentUnitID); subUnitID == "" || parentUnitID == "" {
		return
	}
	s.mu.Lock()
	if s.subToParent == nil {
		s.subToParent = make(map[string]string)
	}
	s.subToParent[subUnitID] = parentUnitID
	s.mu.Unlock()
}

func (s *JSONFileStore) ResolveCanonicalUnitID(unitID string) string {
	unitID = strings.TrimSpace(unitID)
	s.mu.RLock()
	p := s.subToParent[unitID]
	s.mu.RUnlock()
	if p != "" {
		return p
	}
	return unitID
}

func (s *JSONFileStore) SaveLocalPlaintext(docID string, occurredAt int64, rawText string, sents []string, resp CloudIngestResponse) error {
	s.mu.Lock()
	s.docs[docID] = jsonDocRecord{DocID: docID, OccurredAt: occurredAt, RawText: rawText, Sentences: append([]string(nil), sents...), AddressURI: resp.AddressURI, Coord64: resp.AddressCoord64, KeptIndices: append([]int(nil), resp.KeptSentenceIndices...)}
	s.mu.Unlock()
	return nil
}

func (s *JSONFileStore) DeleteLocalUnits(unitIDs []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for _, id := range unitIDs {
		if _, ok := s.docs[id]; ok {
			delete(s.docs, id)
			deleted++
		}
	}
	return deleted
}

func (s *JSONFileStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s.docs, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(s.filePath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = tmp.Close(); _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.filePath)
}

func (s *JSONFileStore) LoadLocalDocument(docID string) (string, string, []string, error) {
	s.mu.RLock()
	if parentID := s.subToParent[docID]; parentID != "" {
		docID = parentID
	}
	rec, ok := s.docs[docID]
	if !ok {
		s.mu.RUnlock()
		return "", "", nil, errors.New("ramesdk: document not found in JSON store: " + docID)
	}
	allSents, keptIdx := append([]string(nil), rec.Sentences...), append([]int(nil), rec.KeptIndices...)
	s.mu.RUnlock()
	return distillPlaintext(rec.RawText, allSents, keptIdx)
}

type MemoryStore struct {
	*BlindEncoder
	mu          sync.RWMutex
	raw, layer  map[string]string
	occ         map[string]int64
	sents       map[string][]string
	kept        map[string][]int
	noetics     map[string]LocalNoeticRecord
	subToParent map[string]string
}

func NewMemoryStore(secret []byte) *MemoryStore {
	return &MemoryStore{
		BlindEncoder: NewBlindEncoder(secret), raw: make(map[string]string), layer: make(map[string]string),
		occ: make(map[string]int64), sents: make(map[string][]string), kept: make(map[string][]int),
		noetics: make(map[string]LocalNoeticRecord), subToParent: make(map[string]string),
	}
}

func (m *MemoryStore) SaveSubUnitMapping(subUnitID, parentUnitID string) {
	if subUnitID, parentUnitID = strings.TrimSpace(subUnitID), strings.TrimSpace(parentUnitID); subUnitID == "" || parentUnitID == "" {
		return
	}
	m.mu.Lock()
	if m.subToParent == nil {
		m.subToParent = make(map[string]string)
	}
	m.subToParent[subUnitID] = parentUnitID
	m.mu.Unlock()
}

func (m *MemoryStore) ResolveCanonicalUnitID(unitID string) string {
	unitID = strings.TrimSpace(unitID)
	m.mu.RLock()
	p := m.subToParent[unitID]
	m.mu.RUnlock()
	if p != "" {
		return p
	}
	return unitID
}

func (m *MemoryStore) SaveLocalPlaintext(docID string, occurredAt int64, rawText string, sents []string, resp CloudIngestResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.raw[docID] = rawText
	lyr := strings.ToLower(strings.TrimSpace(resp.Layer))
	if lyr != LayerBackground && lyr != LayerNoetic {
		lyr = LayerEpisodic
	}
	m.layer[docID], m.occ[docID] = lyr, occurredAt
	m.sents[docID] = append([]string(nil), sents...)
	m.kept[docID] = append([]int(nil), resp.KeptSentenceIndices...)
	return nil
}

func (m *MemoryStore) SaveLocalNoetic(rec LocalNoeticRecord) error {
	if strings.TrimSpace(rec.NoeticID) == "" {
		return errors.New("ramesdk: noetic_id is required")
	}
	m.mu.Lock()
	m.noetics[rec.NoeticID] = rec
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) GetLocalNoetic(noeticID string) (LocalNoeticRecord, bool) {
	m.mu.RLock()
	rec, ok := m.noetics[noeticID]
	m.mu.RUnlock()
	return rec, ok
}

func (m *MemoryStore) DeleteLocalUnits(unitIDs []string) int {
	target := make(map[string]bool, len(unitIDs))
	for _, id := range unitIDs {
		if id = strings.TrimSpace(id); id != "" {
			target[id] = true
		}
	}
	if len(target) == 0 {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	deleted := 0
	for id := range target {
		if _, ok := m.raw[id]; ok {
			delete(m.raw, id)
			delete(m.layer, id)
			delete(m.occ, id)
			delete(m.sents, id)
			delete(m.kept, id)
			deleted++
		}
		if _, ok := m.noetics[id]; ok {
			delete(m.noetics, id)
			deleted++
		}
	}
	for nid, rec := range m.noetics {
		for _, cid := range rec.CitedUnitIDs {
			if target[cid] {
				delete(m.noetics, nid)
				break
			}
		}
	}
	return deleted
}

func (m *MemoryStore) LoadLocalDocument(docID string) (string, string, []string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if parentID := m.subToParent[docID]; parentID != "" {
		docID = parentID
	}
	raw, ok := m.raw[docID]
	if !ok {
		if rec, nOK := m.noetics[docID]; nOK {
			combined := strings.TrimSpace("Q: " + rec.QueryText + "\nA: " + rec.AnswerText)
			return combined, combined, []string{combined}, nil
		}
		return "", "", nil, errors.New("ramesdk: document not found")
	}
	return distillPlaintext(raw, m.sents[docID], m.kept[docID])
}

type Client struct {
	Endpoint, APIToken, UserID, TenantID, Scope string
	// AssistantID is the answering persona (unlimited value space); IncarnationID is one
	// concrete lifetime instance of that persona. Both default from the environment
	// (ROLLING_MEMORY_ASSISTANT_ID / ROLLING_MEMORY_INCARNATION_ID) and are stamped onto
	// every blind pulse leaving this client, so any number of parallel incarnations can
	// each answer when / where / what without widening the core envelope.
	AssistantID, IncarnationID string
	// CrossIncarnation overrides the engine's sibling-incarnation resonance on recall.
	// nil keeps the engine default (同一 Assistant 跨分身本我共振开启); false locks recall
	// to the current incarnation only — evaluation/isolation callers must set false.
	CrossIncarnation *bool
	// Mode selects the client operating mode: ModeLocalPlain ("local_plain") | ModePulsePlain ("pulse_plain") | ModePulseCipher ("pulse_cipher").
	// Setting PlaintextMode = true is a shorthand for Mode = ModePulsePlain.
	Mode          string
	PlaintextMode bool
	Store         PlaintextStore
	HTTPClient    *http.Client
}

// SetPulseMode switches the client's operating pulse mode ("local_plain" | "pulse_plain" | "pulse_cipher")
// and returns an error if an unsupported mode is supplied.
func (c *Client) SetPulseMode(mode string) error {
	if c == nil {
		return errors.New("ramesdk: nil client")
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", ModePulseCipher:
		c.Mode = ModePulseCipher
		c.PlaintextMode = false
		return nil
	case ModePulsePlain:
		c.Mode = ModePulsePlain
		c.PlaintextMode = true
		return nil
	case ModeLocalPlain:
		c.Mode = ModeLocalPlain
		c.PlaintextMode = false
		return nil
	default:
		return fmt.Errorf("ramesdk: invalid pulse_mode %q (must be %q, %q, or %q)", mode, ModePulseCipher, ModePulsePlain, ModeLocalPlain)
	}
}

// EffectiveCloudPulseMode returns the wire pulse_mode ("pulse_cipher" | "pulse_plain") sent to the cloud engine.
func (c *Client) EffectiveCloudPulseMode() string {
	if c.IsPlaintextMode() {
		return ModePulsePlain
	}
	return ModePulseCipher
}

// IsPlaintextMode reports whether the client is operating in Pulse Plain Mode ("pulse_plain").
func (c *Client) IsPlaintextMode() bool {
	if c == nil {
		return false
	}
	if c.PlaintextMode {
		return true
	}
	m := strings.ToLower(strings.TrimSpace(c.Mode))
	return m == ModePulsePlain
}

func isLocalPulseMode(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), ModeLocalPlain)
}

// envOrDefault returns the trimmed environment value, or fallback when unset.
func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func NewClient(endpoint, apiToken, userID string, store PlaintextStore) *Client {
	cleanUserID := strings.TrimSpace(userID)
	mode := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_PULSE_MODE"))
	if mode == "" {
		// Backward-compatible env name, but only canonical pulse values are accepted;
		// the config-mode values "local"/"cloud" are a different axis and must not
		// leak into the pulse-mode enum.
		if candidate := strings.TrimSpace(os.Getenv("ROLLING_MEMORY_MODE")); candidate == ModeLocalPlain || candidate == ModePulsePlain || candidate == ModePulseCipher {
			mode = candidate
		}
	}
	if mode != "" && mode != ModeLocalPlain && mode != ModePulsePlain && mode != ModePulseCipher {
		mode = ""
	}
	if mode == "" {
		if strings.TrimSpace(endpoint) == "" {
			mode = ModeLocalPlain
		} else {
			mode = ModePulseCipher
		}
	}
	return &Client{
		Endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"), APIToken: strings.TrimSpace(apiToken),
		UserID: cleanUserID, TenantID: cleanUserID, Store: store, HTTPClient: &http.Client{Timeout: 15 * time.Second},
		AssistantID:   envOrDefault("ROLLING_MEMORY_ASSISTANT_ID", "default"),
		IncarnationID: strings.TrimSpace(os.Getenv("ROLLING_MEMORY_INCARNATION_ID")),
		Mode:          mode,
		PlaintextMode: strings.EqualFold(mode, ModePulsePlain),
	}
}

func (c *Client) Remember(ctx context.Context, docID, rawText string, occurredAt int64) (CloudIngestResponse, error) {
	return c.RememberWithLayer(ctx, docID, rawText, occurredAt, LayerEpisodic)
}
func (c *Client) RememberEpisodic(ctx context.Context, docID, rawText string, occurredAt int64) (CloudIngestResponse, error) {
	return c.RememberWithLayer(ctx, docID, rawText, occurredAt, LayerEpisodic)
}
func (c *Client) RememberBackground(ctx context.Context, docID, rawText string, occurredAt int64) (CloudIngestResponse, error) {
	return c.RememberWithLayer(ctx, docID, rawText, occurredAt, LayerBackground)
}

func (c *Client) IngestPacket(ctx context.Context, pkt BlindPulsePacket) (CloudIngestResponse, error) {
	if len(pkt.Slots) == 0 && len(pkt.SentenceFrames) == 0 && strings.TrimSpace(pkt.AtomsB64) == "" && strings.TrimSpace(pkt.RawText) == "" {
		return CloudIngestResponse{}, errors.New("ramesdk: empty pulse packet")
	}
	if strings.TrimSpace(pkt.Layer) == "" {
		pkt.Layer = LayerEpisodic
	}
	if pkt.TenantID == "" {
		pkt.TenantID = c.TenantID
	}
	if pkt.UserID == "" {
		pkt.UserID = c.UserID
	}
	if pkt.Scope == "" {
		pkt.Scope = c.Scope
	}
	if strings.TrimSpace(pkt.AssistantID) == "" {
		pkt.AssistantID = c.AssistantID
	}
	if strings.TrimSpace(pkt.IncarnationID) == "" {
		pkt.IncarnationID = c.IncarnationID
	}
	var wirePkt BlindPulsePacket
	if len(pkt.Slots) == 0 && len(pkt.SentenceFrames) == 0 && strings.TrimSpace(pkt.AtomsB64) == "" && strings.TrimSpace(pkt.RawText) != "" {
		wirePkt = pkt
	} else {
		var err error
		wirePkt, err = pkt.WirePacket()
		if err != nil {
			return CloudIngestResponse{}, err
		}
		if c.IsPlaintextMode() && strings.TrimSpace(pkt.RawText) != "" {
			wirePkt.RawText = pkt.RawText
		}
	}
	if strings.TrimSpace(wirePkt.PulseMode) == "" {
		wirePkt.PulseMode = c.EffectiveCloudPulseMode()
	}
	resp := CloudIngestResponse{UnitID: pkt.UnitID, Layer: pkt.Layer, PulseMode: wirePkt.PulseMode}
	if c.Endpoint != "" && !isLocalPulseMode(c.Mode) {
		if err := c.postJSON(ctx, "/ingest", wirePkt, &resp); err != nil {
			return CloudIngestResponse{}, err
		}
		if err := c.validateCloudSpace(resp.TenantID, resp.Scope); err != nil {
			return CloudIngestResponse{}, fmt.Errorf("ramesdk: ingest space validation: %w", err)
		}
		if resp.PulseMode != "" && !sameCloudPulseMode(resp.PulseMode, wirePkt.PulseMode) {
			return CloudIngestResponse{}, fmt.Errorf("ramesdk: ingest pulse_mode mismatch (server=%q client=%q)", resp.PulseMode, wirePkt.PulseMode)
		}
		if resp.Layer == "" {
			resp.Layer = pkt.Layer
		}
	}
	return resp, nil
}

func sameCloudPulseMode(a, b string) bool {
	na := normalizeWirePulseMode(a)
	nb := normalizeWirePulseMode(b)
	return na != "" && na == nb
}

func normalizeWirePulseMode(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case ModePulseCipher:
		return ModePulseCipher
	case ModePulsePlain:
		return ModePulsePlain
	default:
		return strings.ToLower(strings.TrimSpace(m))
	}
}

type keyedHasher interface {
	KeyedHash64(token string) uint64
}

func (c *Client) RememberWithLayer(ctx context.Context, docID, rawText string, occurredAt int64, layer string) (CloudIngestResponse, error) {
	if c == nil || c.Store == nil {
		return CloudIngestResponse{}, errors.New("ramesdk: nil local store")
	}
	pkt, sents := c.Store.PackBlindPacket(docID, rawText, occurredAt)
	if len(pkt.Slots) == 0 && strings.TrimSpace(pkt.AtomsB64) == "" {
		return CloudIngestResponse{}, errors.New("ramesdk: empty pulse packet")
	}
	if c.IsPlaintextMode() {
		pkt.RawText = rawText
	}
	if lyr := strings.TrimSpace(layer); lyr != "" {
		pkt.Layer = lyr
	} else {
		pkt.Layer = LayerEpisodic
	}
	resp, err := c.IngestPacket(ctx, pkt)
	if err != nil {
		return CloudIngestResponse{}, err
	}
	return resp, c.Store.SaveLocalPlaintext(docID, occurredAt, rawText, sents, resp)
}

func (c *Client) RememberWithMeta(ctx context.Context, docID, rawText, layer string, meta UnitMeta) (CloudIngestResponse, error) {
	if c == nil || c.Store == nil {
		return CloudIngestResponse{}, errors.New("ramesdk: nil local store")
	}
	fullText := FormatPlaintextWithMeta(rawText, meta)
	occ := meta.OccurredAt
	if occ <= 0 && meta.NarratedAt > 0 {
		occ = meta.NarratedAt
	}
	pkt, sents := c.Store.PackBlindPacket(docID, fullText, occ)
	if len(pkt.Slots) == 0 && strings.TrimSpace(pkt.AtomsB64) == "" {
		return CloudIngestResponse{}, errors.New("ramesdk: empty pulse packet")
	}
	if c.IsPlaintextMode() {
		pkt.RawText = fullText
	}
	if meta.NarratedAt > 0 {
		pkt.NarratedAt = meta.NarratedAt
	}
	if meta.IngestedAt > 0 {
		pkt.IngestedAt = meta.IngestedAt
	}
	kh, hasKH := c.Store.(keyedHasher)
	if author := strings.ToLower(strings.TrimSpace(meta.Author)); author != "" && hasKH {
		pkt.AuthorHash = kh.KeyedHash64(author)
	}
	pkt.DocHash = meta.DocHash
	if pkt.DocHash == 0 && hasKH {
		if fileURL := strings.TrimSpace(meta.FileURL); fileURL != "" {
			pkt.DocHash = kh.KeyedHash64(fileURL)
		}
	}
	pkt.BlockHash = meta.BlockHash
	if pkt.BlockHash == 0 && hasKH {
		if blockID := strings.TrimSpace(meta.BlockID); blockID != "" {
			pkt.BlockHash = kh.KeyedHash64(blockID)
		}
	}
	if meta.LineStart > 0 {
		pkt.LineStart = meta.LineStart
	}
	if meta.LineEnd > 0 {
		pkt.LineEnd = meta.LineEnd
	}
	if pkt.LineStart > 0 && pkt.LineEnd < pkt.LineStart {
		pkt.LineEnd = pkt.LineStart
	}
	if lyr := strings.TrimSpace(layer); lyr != "" {
		pkt.Layer = lyr
	} else {
		pkt.Layer = LayerEpisodic
	}
	resp, err := c.IngestPacket(ctx, pkt)
	if err != nil {
		return CloudIngestResponse{}, err
	}
	return resp, c.Store.SaveLocalPlaintext(docID, occ, fullText, sents, resp)
}

type SubUnitMapper interface {
	SaveSubUnitMapping(subUnitID, parentUnitID string)
	ResolveCanonicalUnitID(unitID string) string
}

func (c *Client) RememberComposite(ctx context.Context, docID, rawText string, occurredAt int64, layer string, subUnits []SubUnit) ([]CloudIngestResponse, error) {
	lyr := strings.TrimSpace(layer)
	if lyr == "" {
		lyr = LayerEpisodic
	}
	kh, hasKH := c.Store.(keyedHasher)
	var parentDocHash uint64
	if hasKH && strings.TrimSpace(docID) != "" {
		parentDocHash = kh.KeyedHash64(strings.TrimSpace(docID))
	}
	parentResp, err := c.RememberWithMeta(ctx, docID, rawText, lyr, UnitMeta{
		OccurredAt: occurredAt,
		DocHash:    parentDocHash,
	})
	if err != nil {
		return nil, err
	}
	resps := []CloudIngestResponse{parentResp}
	mapper, _ := c.Store.(SubUnitMapper)
	kindCounts := make(map[string]int, 4)
	for _, su := range subUnits {
		subID, _, txt, _, ok := resolveSubUnitIDAndText(docID, su, kindCounts)
		if !ok {
			continue
		}
		subPkt, _ := c.Store.PackBlindPacket(subID, txt, occurredAt)
		if len(subPkt.Slots) == 0 {
			continue
		}
		subPkt.Layer, subPkt.CitedUnitIDs = lyr, []string{docID}
		if c.Mode == ModePulsePlain {
			subPkt.RawText = txt
		}
		subPkt.DocHash = parentDocHash
		if hasKH {
			if blkID := strings.TrimSpace(su.BlockID); blkID != "" {
				subPkt.BlockHash = kh.KeyedHash64(blkID)
			} else if subID != "" {
				subPkt.BlockHash = kh.KeyedHash64(subID)
			}
		}
		if su.LineStart > 0 {
			subPkt.LineStart = su.LineStart
		}
		if su.LineEnd > 0 {
			subPkt.LineEnd = su.LineEnd
		}
		if subPkt.LineStart > 0 && subPkt.LineEnd < subPkt.LineStart {
			subPkt.LineEnd = subPkt.LineStart
		}
		subResp, sErr := c.IngestPacket(ctx, subPkt)
		if sErr != nil {
			return resps, sErr
		}
		if mapper != nil {
			mapper.SaveSubUnitMapping(subID, docID)
		}
		resps = append(resps, subResp)
	}
	return resps, nil
}

type LocalNoeticSaver interface {
	SaveLocalNoetic(rec LocalNoeticRecord) error
}

func (c *Client) RememberNoetic(noeticID, queryText string, citedUnitIDs []string, answerText string, occurredAt int64) error {
	ns, ok := c.Store.(LocalNoeticSaver)
	if !ok {
		return errors.New("ramesdk: Store does not implement LocalNoeticSaver")
	}
	return ns.SaveLocalNoetic(LocalNoeticRecord{
		NoeticID: noeticID, OccurredAt: occurredAt, QueryText: queryText, AnswerText: answerText,
		CitedUnitIDs: citedUnitIDs, AssistantID: c.AssistantID, IncarnationID: c.IncarnationID,
	})
}

type ResolvedHit struct {
	CloudQueryHit
	RawPlaintext       string   `json:"raw_plaintext"`
	DistilledPlaintext string   `json:"distilled_plaintext"`
	KeptSentences      []string `json:"kept_sentences"`
}

func (c *Client) QueryHits(ctx context.Context, queryText string, topK int) ([]CloudQueryHit, error) {
	if c == nil || c.Store == nil {
		return nil, errors.New("ramesdk: nil local store")
	}
	if topK < 0 {
		topK = 0
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return nil, errors.New("ramesdk: QueryHits requires a cloud Endpoint")
	}
	qPkt, _ := c.Store.PackBlindPacket("QUERY", queryText, 0)
	if len(qPkt.Slots) == 0 && strings.TrimSpace(qPkt.AtomsB64) == "" {
		return nil, errors.New("ramesdk: empty query pulse packet")
	}
	wireQuery, err := qPkt.WirePacket()
	if err != nil {
		return nil, err
	}
	pulseMode := c.EffectiveCloudPulseMode()
	wireQuery.PulseMode = pulseMode
	if c.IsPlaintextMode() {
		wireQuery.RawText = queryText
	}
	payload := map[string]any{
		"tenant_id":  c.TenantID,
		"user_id":    c.UserID,
		"scope":      c.Scope,
		"pulse_mode": pulseMode,
		"query":      wireQuery,
		"top_k":      topK,
	}
	if topK == 0 {
		payload["delivery_budget"] = "auto"
	}
	if c.IsPlaintextMode() {
		payload["query_text"] = queryText
	}
	// CrossIncarnation 由调用方显式设置时才下发（nil = 引擎默认：同 Assistant 跨分身本我共振）。
	// 反馈/评测类需要锁死单一分身空间的调用方设 false（引擎侧 cross_incarnation=false = 仅当前分身）。
	if c.CrossIncarnation != nil {
		payload["cross_incarnation"] = *c.CrossIncarnation
	}
	var rawEnv struct {
		Success   bool            `json:"success"`
		TenantID  string          `json:"tenant_id"`
		Scope     string          `json:"scope"`
		PulseMode string          `json:"pulse_mode"`
		Engine    string          `json:"engine"`
		Hits      []CloudQueryHit `json:"hits"`
		Count     int             `json:"count"`
	}
	if err := c.postJSON(ctx, "/query?envelope=1", payload, &rawEnv); err != nil {
		return nil, err
	}
	if !rawEnv.Success || rawEnv.Engine != "autonoetic" {
		return nil, fmt.Errorf("ramesdk: invalid query envelope (success=%v engine=%q)", rawEnv.Success, rawEnv.Engine)
	}
	if err := c.validateCloudSpace(rawEnv.TenantID, rawEnv.Scope); err != nil {
		return nil, fmt.Errorf("ramesdk: query space validation: %w", err)
	}
	if rawEnv.PulseMode != "" && !sameCloudPulseMode(rawEnv.PulseMode, pulseMode) {
		return nil, fmt.Errorf("ramesdk: query pulse_mode mismatch (server=%q client=%q)", rawEnv.PulseMode, pulseMode)
	}
	if rawEnv.Count != len(rawEnv.Hits) {
		return nil, fmt.Errorf("ramesdk: cloud query count %d does not match %d hits", rawEnv.Count, len(rawEnv.Hits))
	}
	for _, h := range rawEnv.Hits {
		if strings.TrimSpace(h.UnitID) == "" {
			return nil, errors.New("ramesdk: cloud query returned an empty UnitID")
		}
	}
	return rawEnv.Hits, nil
}

func (c *Client) Recall(ctx context.Context, queryText string, topK int) ([]ResolvedHit, error) {
	hits, err := c.QueryHits(ctx, queryText, topK)
	if err != nil {
		return nil, err
	}
	out := make([]ResolvedHit, 0, len(hits))
	seenCanonical := make(map[string]bool, len(hits))
	mapper, _ := c.Store.(SubUnitMapper)
	for _, h := range hits {
		lookupID := h.UnitID
		if mapper != nil {
			lookupID = mapper.ResolveCanonicalUnitID(lookupID)
		}
		if lookupID == h.UnitID && len(h.CitedUnitIDs) == 1 && strings.TrimSpace(h.CitedUnitIDs[0]) != "" && h.Layer != LayerNoetic {
			lookupID = strings.TrimSpace(h.CitedUnitIDs[0])
		}
		if seenCanonical[lookupID] {
			continue
		}
		var raw, dist string
		var kept []string
		var loadErr error
		if c.IsPlaintextMode() && strings.TrimSpace(h.RawText) != "" {
			raw = h.RawText
			dist = h.DistilledText
			if strings.TrimSpace(dist) == "" {
				dist = raw
			}
			kept = SplitSentences(dist)
		} else {
			if c.Store != nil {
				raw, dist, kept, loadErr = c.Store.LoadLocalDocument(lookupID)
			} else {
				loadErr = errors.New("ramesdk: nil local store")
			}
			if loadErr != nil {
				if strings.TrimSpace(h.RawText) != "" {
					raw = h.RawText
					dist = h.DistilledText
					if strings.TrimSpace(dist) == "" {
						dist = raw
					}
					kept = SplitSentences(dist)
					loadErr = nil
				} else {
					return nil, fmt.Errorf("ramesdk: resolve cloud query UnitID %q: %w", h.UnitID, loadErr)
				}
			}
		}
		seenCanonical[lookupID] = true
		h.UnitID = lookupID
		out = append(out, ResolvedHit{CloudQueryHit: h, RawPlaintext: raw, DistilledPlaintext: dist, KeptSentences: kept})
	}
	return out, nil
}

func (c *Client) validateCloudSpace(tenantID, scope string) error {
	if strings.TrimSpace(tenantID) == "" || tenantID != c.TenantID || !c.matchesCloudScope(scope) {
		return fmt.Errorf("space mismatch (got tenant=%q scope=%q, want tenant=%q scope=%q)", tenantID, scope, c.TenantID, c.Scope)
	}
	return nil
}

func isSharedUserScope(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "@user" || s == "shared" || s == "user_shared"
}

func composeClientCanonicalScope(tenantID, baseScope, assistantID, incarnationID string) string {
	ast := strings.TrimSpace(assistantID)
	inc := strings.TrimSpace(incarnationID)
	base := strings.TrimSpace(baseScope)
	if isSharedUserScope(ast) || (ast == "" && inc == "" && isSharedUserScope(base)) {
		return "@user"
	}
	if strings.HasPrefix(base, "ast:") {
		rest := strings.TrimPrefix(base, "ast:")
		var pAst, pInc string
		if idx := strings.Index(rest, ":inc:"); idx >= 0 {
			pAst, pInc = rest[:idx], rest[idx+len(":inc:"):]
		} else {
			pAst = rest
		}
		if ast == "" {
			ast = pAst
		}
		if inc == "" {
			inc = pInc
		}
	}
	if ast == "" && inc == "" {
		return base
	}
	if ast == "" {
		ast = "default"
	}
	if inc == "" && base != "" && base != "default" && base != strings.TrimSpace(tenantID) && !strings.HasPrefix(base, "ast:") {
		inc = base
	}
	if inc == "" {
		return "ast:" + ast
	}
	return "ast:" + ast + ":inc:" + inc
}

// matchesCloudScope 校验引擎回显的 scope。引擎按 ComposeAssistantIncarnationScope 将请求
// (tenant_id, scope, assistant_id, incarnation_id) 归一为分身坐标规范形回显。
func (c *Client) matchesCloudScope(scope string) bool {
	if scope == "" || scope != strings.TrimSpace(scope) {
		return false
	}
	want := strings.TrimSpace(c.Scope)
	if want != "" {
		if scope == want {
			return true
		}
		if scope == composeClientCanonicalScope(c.TenantID, want, c.AssistantID, c.IncarnationID) {
			return true
		}
		assistant := strings.TrimSpace(c.AssistantID)
		if assistant == "" {
			assistant = "default"
		}
		return strings.TrimSpace(c.IncarnationID) == "" && scope == "ast:"+assistant+":inc:"+want
	}
	// 当 c.Scope 为空时，服务端 NormalizeTenantAndScope 将 baseScope 归一为 UserID（或 TenantID / "default"）后再合成。
	candidates := []string{strings.TrimSpace(c.UserID), strings.TrimSpace(c.TenantID), "default"}
	for _, base := range candidates {
		if base == "" {
			continue
		}
		if scope == base || scope == composeClientCanonicalScope(c.TenantID, base, c.AssistantID, c.IncarnationID) {
			return true
		}
	}
	return false
}

// CodecHealthReport holds the local + remote codec verification result from `/health`.
type CodecHealthReport struct {
	LocalVerified  bool   `json:"local_verified"`
	RemoteCanaryOK bool   `json:"remote_canary_ok"`
	RemoteProbeOK  bool   `json:"remote_probe_ok"`
	SpecVersion    string `json:"spec_version"`
	SlotCount      int    `json:"slot_count"`
	SentCount      int    `json:"sent_count"`
	AtomsB64       string `json:"atoms_b64"`
}

// VerifyCodecLocal executes an in-process round-trip self-test of all 5 RAME-BlindPulse-v1 stages
// (KeyedHash64 -> SplitSentences -> SplitWords -> CarrierUnits/Flags -> RMBP AtomsB64 encode & decode)
// against the pinned canary vector without any network I/O.
func VerifyCodecLocal() error {
	enc := NewBlindEncoder([]byte(CanarySecret))
	pkt, sents := enc.PackBlindPacket(CanaryDocID, CanaryText, CanaryTS)
	if len(sents) != CanarySentCount || len(pkt.Slots) != CanarySlotCount {
		return fmt.Errorf("ramesdk: local codec canary count mismatch (got slots=%d sents=%d, want slots=%d sents=%d)",
			len(pkt.Slots), len(sents), CanarySlotCount, CanarySentCount)
	}
	if pkt.AtomsB64 != CanaryAtomsB64 {
		return fmt.Errorf("ramesdk: local codec canary atoms_b64 drift (got %q, want %q)", pkt.AtomsB64, CanaryAtomsB64)
	}
	decSlots, decFrames, err := DecodeWirePayload(pkt.AtomsB64)
	if err != nil {
		return fmt.Errorf("ramesdk: local codec canary decode failed: %w", err)
	}
	if len(decSlots) != len(pkt.Slots) || len(decFrames) != len(pkt.SentenceFrames) {
		return fmt.Errorf("ramesdk: local codec canary round-trip size mismatch")
	}
	for i := range pkt.Slots {
		if decSlots[i] != pkt.Slots[i] {
			return fmt.Errorf("ramesdk: local codec canary slot[%d] mismatch: got %+v want %+v", i, decSlots[i], pkt.Slots[i])
		}
	}
	return nil
}

type healthWireResponse struct {
	Status      string `json:"status"`
	Engine      string `json:"engine"`
	Version     string `json:"version"`
	SpecVersion string `json:"spec_version"`
	CodecCanary *struct {
		Spec      string `json:"spec"`
		SlotCount int    `json:"slot_count"`
		SentCount int    `json:"sent_count"`
		AtomsB64  string `json:"atoms_b64"`
	} `json:"codec_canary"`
	CodecProbe *struct {
		Verified  bool   `json:"verified"`
		SlotCount int    `json:"slot_count"`
		SentCount int    `json:"sent_count"`
		AtomsB64  string `json:"atoms_b64"`
	} `json:"codec_probe"`
}

func (c *Client) Health(ctx context.Context) error {
	if err := VerifyCodecLocal(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return errors.New("ramesdk: Health requires a cloud Endpoint")
	}
	var resp healthWireResponse
	if err := c.getJSON(ctx, "/health", &resp); err != nil {
		return err
	}
	if resp.Status != "ok" || resp.Engine != "autonoetic" || resp.Version != "v1" {
		return fmt.Errorf("ramesdk: unhealthy response status=%q engine=%q version=%q", resp.Status, resp.Engine, resp.Version)
	}
	if resp.CodecCanary != nil && resp.CodecCanary.AtomsB64 != "" {
		if resp.CodecCanary.AtomsB64 != CanaryAtomsB64 || resp.CodecCanary.SlotCount != CanarySlotCount {
			return fmt.Errorf("ramesdk: remote codec canary mismatch (server_b64=%q slots=%d, local_b64=%q slots=%d)",
				resp.CodecCanary.AtomsB64, resp.CodecCanary.SlotCount, CanaryAtomsB64, CanarySlotCount)
		}
	}
	return nil
}

// VerifyCodec runs a full bidirectional codec self-test: first verifying local encode/decode
// against the built-in canary, then sending the encoded `atoms_b64` probe to `/health` so the
// remote engine decodes and re-encodes it live.
func (c *Client) VerifyCodec(ctx context.Context) (CodecHealthReport, error) {
	if err := VerifyCodecLocal(); err != nil {
		return CodecHealthReport{}, err
	}
	report := CodecHealthReport{
		LocalVerified: true,
		SpecVersion:   SpecVersion,
		SlotCount:     CanarySlotCount,
		SentCount:     CanarySentCount,
		AtomsB64:      CanaryAtomsB64,
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return report, nil
	}
	var resp healthWireResponse
	err := c.postJSON(ctx, "/health", map[string]any{"atoms_b64": CanaryAtomsB64}, &resp)
	if err != nil {
		// Fallback to GET /health?atoms_b64=... for GET-only servers
		err = c.getJSON(ctx, "/health?atoms_b64="+url.QueryEscape(CanaryAtomsB64), &resp)
		if err != nil {
			return report, err
		}
	}
	if resp.Status != "ok" || resp.Engine != "autonoetic" || resp.Version != "v1" {
		return report, fmt.Errorf("ramesdk: unhealthy response status=%q engine=%q version=%q", resp.Status, resp.Engine, resp.Version)
	}
	if resp.CodecCanary != nil && resp.CodecCanary.AtomsB64 != "" {
		if resp.CodecCanary.AtomsB64 != CanaryAtomsB64 || resp.CodecCanary.SlotCount != CanarySlotCount {
			return report, fmt.Errorf("ramesdk: remote codec canary mismatch (server=%q, local=%q)", resp.CodecCanary.AtomsB64, CanaryAtomsB64)
		}
		report.RemoteCanaryOK = true
	}
	if resp.CodecProbe != nil {
		if !resp.CodecProbe.Verified || resp.CodecProbe.AtomsB64 != CanaryAtomsB64 || resp.CodecProbe.SlotCount != CanarySlotCount {
			return report, fmt.Errorf("ramesdk: remote codec probe verification failed: %+v", resp.CodecProbe)
		}
		report.RemoteProbeOK = true
	}
	return report, nil
}

type AuthIdentity struct {
	Success        bool     `json:"success"`
	Valid          bool     `json:"valid"`
	Engine         string   `json:"engine"`
	TenantID       string   `json:"tenant_id"`
	UserIDPrefixes []string `json:"user_id_prefixes"`
	ExactNamespace bool     `json:"exact_namespace"`
	LockedUserID   string   `json:"locked_user_id"`
	IsManager      bool     `json:"is_manager"`
}

func (c *Client) VerifyAuth(ctx context.Context) (AuthIdentity, error) {
	if strings.TrimSpace(c.Endpoint) == "" {
		return AuthIdentity{}, errors.New("ramesdk: VerifyAuth requires a cloud Endpoint")
	}
	var identity AuthIdentity
	if err := c.getJSON(ctx, "/auth/verify", &identity); err != nil {
		return AuthIdentity{}, err
	}
	if !identity.Success || !identity.Valid || identity.Engine != "autonoetic" {
		return AuthIdentity{}, fmt.Errorf("ramesdk: invalid auth verify response (success=%v valid=%v engine=%q)", identity.Success, identity.Valid, identity.Engine)
	}
	return identity, nil
}

func (c *Client) ListCloudUnitIDs(ctx context.Context) ([]string, error) {
	if strings.TrimSpace(c.Endpoint) == "" {
		return nil, errors.New("ramesdk: ListCloudUnitIDs requires a cloud Endpoint")
	}
	payload := map[string]any{"tenant_id": c.TenantID, "user_id": c.UserID, "scope": c.Scope}
	var list struct {
		Success  bool     `json:"success"`
		TenantID string   `json:"tenant_id"`
		Scope    string   `json:"scope"`
		Count    int      `json:"count"`
		UnitIDs  []string `json:"unit_ids"`
	}
	if err := c.postJSON(ctx, "/units", payload, &list); err != nil {
		return nil, err
	}
	if !list.Success {
		return nil, errors.New("ramesdk: cloud unit list returned success=false")
	}
	if err := c.validateCloudSpace(list.TenantID, list.Scope); err != nil {
		return nil, fmt.Errorf("ramesdk: unit list space validation: %w", err)
	}
	if list.Count != len(list.UnitIDs) {
		return nil, fmt.Errorf("ramesdk: cloud unit list count %d does not match %d IDs", list.Count, len(list.UnitIDs))
	}
	ids, seen := make([]string, 0, len(list.UnitIDs)), make(map[string]bool, len(list.UnitIDs))
	for _, id := range list.UnitIDs {
		if id = strings.TrimSpace(id); id == "" || seen[id] {
			return nil, fmt.Errorf("ramesdk: cloud unit list returned an empty or duplicate unit ID %q", id)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

type CloudPurgeResponse struct {
	Success        bool   `json:"success"`
	TenantID       string `json:"tenant_id,omitempty"`
	Scope          string `json:"scope,omitempty"`
	Mode           string `json:"mode"`
	DeletedUnits   int64  `json:"deleted_units"`
	RemainingUnits int64  `json:"remaining_units"`
}

type LocalUnitDeleter interface{ DeleteLocalUnits(unitIDs []string) int }
type localStoreFlusher interface{ Flush() error }

func deleteAndFlushLocal(store PlaintextStore, unitIDs []string) (int, error) {
	deleted := 0
	if ud, ok := store.(LocalUnitDeleter); ok {
		deleted = ud.DeleteLocalUnits(unitIDs)
	}
	if deleted > 0 {
		if flusher, ok := store.(localStoreFlusher); ok {
			if err := flusher.Flush(); err != nil {
				return deleted, err
			}
		}
	}
	return deleted, nil
}

// UpdateUnit 按 upsert 语义更新单元：先经 RememberWithLayer 完成远端 ingest（服务端按
// (tenant_id, scope, unit_id) 冲突覆盖，重试幂等），成功后由 SaveLocalPlaintext 按 docID
// 覆盖本地记录。更新前不预删本地原文，远端失败时旧原文保持可读，引用该单元的 noetic
// 记录也不受更新影响；本地保存失败时错误与 resp 一并返回，重试同一 docID 即可对账。
func (c *Client) UpdateUnit(ctx context.Context, docID, newText string, occurredAt int64, layer string) (CloudIngestResponse, error) {
	return c.RememberWithLayer(ctx, docID, newText, occurredAt, layer)
}

func (c *Client) DeleteUnits(ctx context.Context, unitIDs []string) (CloudPurgeResponse, error) {
	var clean []string
	for _, id := range unitIDs {
		if id = strings.TrimSpace(id); id != "" {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return CloudPurgeResponse{}, errors.New("ramesdk: unit_ids must not be empty")
	}
	if c.Endpoint == "" {
		localDeleted, err := deleteAndFlushLocal(c.Store, clean)
		if err != nil {
			return CloudPurgeResponse{}, fmt.Errorf("ramesdk: flush local purge: %w", err)
		}
		return CloudPurgeResponse{Success: true, Mode: "units", DeletedUnits: int64(localDeleted)}, nil
	}
	payload := map[string]any{"tenant_id": c.TenantID, "user_id": c.UserID, "scope": c.Scope, "mode": "units", "unit_ids": clean}
	var resp CloudPurgeResponse
	if err := c.postJSON(ctx, "/purge", payload, &resp); err != nil {
		return CloudPurgeResponse{}, err
	}
	if !resp.Success {
		return CloudPurgeResponse{}, errors.New("ramesdk: cloud purge returned success=false")
	}
	if err := c.validateCloudSpace(resp.TenantID, resp.Scope); err != nil {
		return CloudPurgeResponse{}, fmt.Errorf("ramesdk: purge space validation: %w", err)
	}
	if _, err := deleteAndFlushLocal(c.Store, clean); err != nil {
		return resp, fmt.Errorf("ramesdk: flush local purge after cloud success: %w", err)
	}
	return resp, nil
}

func (c *Client) postJSON(ctx context.Context, subPath string, reqBody any, respOut any) error {
	return c.doJSON(ctx, http.MethodPost, subPath, reqBody, respOut)
}
func (c *Client) getJSON(ctx context.Context, subPath string, respOut any) error {
	return c.doJSON(ctx, http.MethodGet, subPath, nil, respOut)
}

func (c *Client) doJSON(ctx context.Context, method, subPath string, reqBody any, respOut any) error {
	if isLocalPulseMode(c.Mode) {
		return errors.New("ramesdk: HTTP requests are unavailable in local_plain mode")
	}
	var reqReader io.Reader
	if reqBody != nil {
		rawReq, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		reqReader = bytes.NewReader(rawReq)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+subPath, reqReader)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIToken)
		req.Header.Set("X-Api-Key", c.APIToken)
	}
	if c.UserID != "" {
		req.Header.Set("X-Rolling-Memory-User", c.UserID)
	}
	if c.TenantID != "" {
		req.Header.Set("X-Rolling-Memory-Tenant", c.TenantID)
	}
	if c.Scope != "" {
		req.Header.Set("X-Rolling-Memory-Scope", c.Scope)
	}
	if c.AssistantID != "" {
		req.Header.Set("X-Rolling-Memory-Assistant", c.AssistantID)
	}
	if c.IncarnationID != "" {
		req.Header.Set("X-Rolling-Memory-Incarnation", c.IncarnationID)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			// 限流/暂不可用：把服务端的 Retry-After 带进错误，客户端据此精确退避而不是猜速率
			// （限流裁决与预算都在服务端，客户端只做反应式刹车）。
			return &ThrottleError{
				StatusCode: resp.StatusCode, Path: subPath,
				Body: strings.TrimSpace(string(bodyBytes)), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}
		return fmt.Errorf("ramesdk: HTTP %d from %s: %s", resp.StatusCode, subPath, strings.TrimSpace(string(bodyBytes)))
	}
	if respOut != nil && len(bodyBytes) > 0 {
		return json.Unmarshal(bodyBytes, respOut)
	}
	return nil
}

// ThrottleError 是服务端限流（429）或暂不可用（503）的响应错误。
// RetryAfter 为服务端声明的等待时长（0 = 未声明）；错误文本与历史格式保持一致，便于既有断言与日志解析。
type ThrottleError struct {
	StatusCode int
	Path       string
	Body       string
	RetryAfter time.Duration
}

func (e *ThrottleError) Error() string {
	message := fmt.Sprintf("ramesdk: HTTP %d from %s: %s", e.StatusCode, e.Path, e.Body)
	if e.RetryAfter > 0 {
		message += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return message
}

// parseRetryAfter 解析 Retry-After（秒；HTTP-date 形式不支持时返回 0，由调用方退避兜底）。
func parseRetryAfter(raw string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || seconds <= 0 {
		return 0
	}
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

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
