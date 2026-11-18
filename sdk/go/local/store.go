// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PlaintextStore 的 SaveLocalPlaintext 必须按 docID 幂等 upsert，不得依赖调用方预先删除；
// 这样远端更新失败时旧原文仍保留，重试同一 docID 即可完成对账。
type PlaintextStore interface {
	PackBlindPacket(docID string, text string, occurredAt int64) (BlindPulsePacket, []string)
	SaveLocalPlaintext(docID string, occurredAt int64, rawText string, sents []string, resp IngestResponse) error
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
	// SubUnitIDs 把「子单元 → 本父单元」映射随父记录一并落盘：Flush 后随载入重建
	// subToParent（旧文件缺该字段即无持久化映射，纯附加式兼容；映射在父记录存在时
	// 才持久化——复合写入流程恒先落父文档）。
	SubUnitIDs []string `json:"sub_unit_ids,omitempty"`
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
		return nil, errors.New("local store: empty json file path")
	}
	s := &JSONFileStore{BlindEncoder: NewBlindEncoder(secret), filePath: filePath, docs: make(map[string]jsonDocRecord), subToParent: make(map[string]string)}
	raw, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	} else if err != nil {
		return nil, fmt.Errorf("local store: read JSON store: %w", err)
	}
	if err := json.Unmarshal(raw, &s.docs); err != nil || s.docs == nil {
		return nil, fmt.Errorf("local store: decode JSON store: %v", err)
	}
	// 从父记录内持久化的子表重建 sub→parent 映射（旧文件无子表即空起点，兼容）。
	for parentID, rec := range s.docs {
		for _, subID := range rec.SubUnitIDs {
			if subID = strings.TrimSpace(subID); subID != "" {
				s.subToParent[subID] = parentID
			}
		}
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
	if rec, ok := s.docs[parentUnitID]; ok {
		dup := false
		for _, id := range rec.SubUnitIDs {
			if id == subUnitID {
				dup = true
				break
			}
		}
		if !dup {
			rec.SubUnitIDs = append(append([]string(nil), rec.SubUnitIDs...), subUnitID)
			s.docs[parentUnitID] = rec
		}
	}
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

func (s *JSONFileStore) SaveLocalPlaintext(docID string, occurredAt int64, rawText string, sents []string, resp IngestResponse) error {
	s.mu.Lock()
	s.docs[docID] = jsonDocRecord{DocID: docID, OccurredAt: occurredAt, RawText: rawText, Sentences: append([]string(nil), sents...), AddressURI: resp.AddressURI, Coord64: resp.AddressCoord64, KeptIndices: append([]int(nil), resp.KeptSentenceIndices...)}
	s.mu.Unlock()
	return nil
}

func (s *JSONFileStore) DeleteLocalUnits(unitIDs []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	target := make(map[string]bool, len(unitIDs))
	for _, id := range unitIDs {
		if id = strings.TrimSpace(id); id != "" {
			target[id] = true
		}
	}
	deleted := 0
	for id := range target {
		if _, ok := s.docs[id]; ok {
			delete(s.docs, id)
			deleted++
		}
	}
	// 子映射与父记录内持久化子表同步清理（被删 id 作为子或被作为父的双向清理）。
	for subID, parentID := range s.subToParent {
		if target[subID] || target[parentID] {
			delete(s.subToParent, subID)
		}
	}
	for parentID, rec := range s.docs {
		if len(rec.SubUnitIDs) == 0 {
			continue
		}
		kept := make([]string, 0, len(rec.SubUnitIDs))
		for _, subID := range rec.SubUnitIDs {
			if !target[subID] {
				kept = append(kept, subID)
			}
		}
		if len(kept) != len(rec.SubUnitIDs) {
			rec.SubUnitIDs = kept
			s.docs[parentID] = rec
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
		return "", "", nil, errors.New("local store: document not found in JSON store: " + docID)
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

func (m *MemoryStore) SaveLocalPlaintext(docID string, occurredAt int64, rawText string, sents []string, resp IngestResponse) error {
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
		return errors.New("local store: noetic_id is required")
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
		return "", "", nil, errors.New("local store: document not found")
	}
	return distillPlaintext(raw, m.sents[docID], m.kept[docID])
}
