// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// EngineName is the canonical unified engine name across client and evaluation lab.
	EngineName = "Rolling Autonoetic Memory Engine"
	// EngineNameAutonoetic is the canonical full name of the memory engine.
	EngineNameAutonoetic = "Rolling Autonoetic Memory Engine"
	// EngineNameZH is the canonical Chinese engine name.
	EngineNameZH = "洛灵自知记忆引擎"

	// EvalScopeMarker 与评测命名空间正本 internal/evalns.Marker 同源；套件取线名。
	EvalScopeMarker      = "-mem-eval-"
	evalScopeSuiteLoCoMo = "locomo"
	evalScopeSuiteATM    = "atmbench"

	// Canary* constants pin the built-in codec self-test vector shared across all SDKs.
	CanarySecret    = "rame-codec-canary-key-v1"
	CanaryDocID     = "CANARY_V1"
	CanaryText      = "RollingMemory v1.0.1 WaveEngine_256 洛灵记忆. Zero plaintext!"
	CanaryTS        = int64(1700000000)
	CanarySlotCount = 21
	CanarySentCount = 2
	CanaryAtomsB64  = "Uk1CUAEAPA0AFAAAABUAAhQhUFBjZHiLAYwBtAHIAYQCtQJAEATBATAPB0jHISlR96QHZAI40Tt3T0fPAxxcTN+orGsN87QYgKG+M3wAaBUxyxkJx8f8zRbMt024NoH4VZEtimANzWUASZ6GWiX3Z7rb64Eq7inVwwMYS98I2I7VbHWwUFW4WszbtBQnTCgoPkEZy9pjGebXDmmqB9vwbo0DeZvaEa9TH6Ti6fHUlg3MbBKkCYTXBbqHJcBSS+ORCU+K9G0qrLWQNLboQC4oYxzk6gIUARCElBIB"
)

// VerifyCodecLocal executes an in-process round-trip self-test of the thinclient encoder and
// RMBP wire codec against the pinned canary vector without network I/O.
func VerifyCodecLocal() error {
	enc := NewBlindEncoder([]byte(CanarySecret))
	pkt, sents := enc.PackBlindPacket(CanaryDocID, CanaryText, CanaryTS)
	if len(sents) != CanarySentCount || len(pkt.Slots) != CanarySlotCount {
		return fmt.Errorf("thinclient: local codec canary count mismatch (got slots=%d sents=%d, want slots=%d sents=%d)",
			len(pkt.Slots), len(sents), CanarySlotCount, CanarySentCount)
	}
	b64, err := EncodePulsePayload(pkt)
	if err != nil {
		return fmt.Errorf("thinclient: local codec canary encode failed: %w", err)
	}
	if b64 != CanaryAtomsB64 {
		return fmt.Errorf("thinclient: local codec canary atoms_b64 drift (got %q, want %q)", b64, CanaryAtomsB64)
	}
	decAtoms, decOffsets, err := DecodePulsePayloadB64(b64)
	if err != nil {
		return fmt.Errorf("thinclient: local codec canary decode failed: %w", err)
	}
	if len(decAtoms) != len(pkt.Slots) || len(decOffsets) != len(pkt.SentenceFrames) {
		return fmt.Errorf("thinclient: local codec canary round-trip size mismatch")
	}
	for i := range pkt.Slots {
		if decAtoms[i] != pkt.Slots[i] {
			return fmt.Errorf("thinclient: local codec canary atom[%d] mismatch", i)
		}
	}
	return nil
}

// BrainTransport abstracts the retrieval channel of the Ultra-Thin Client: the
// in-process `LocalVaultTransport` over the local SQLite plaintext vault is the
// production implementation (zero network egress).
type BrainTransport interface {
	IngestBlindPulse(ctx context.Context, pkt BlindPulsePacket) (IngestResponse, error)
	QueryBlindManifold(ctx context.Context, queryPkt BlindPulsePacket, topK int) ([]QueryHit, error)
}

// DefaultSiblingExcludeScopeTokens returns the default sibling-resonance exclude list:
// 「标记 + 合法套件名」双令牌，与评测命名空间正本 internal/evalns（Marker + SuiteLoCoMo /
// SuiteATMWire）逐字对齐 —— 覆盖全部现行评测 scope 形态（`<user>-mem-eval-<suite>[-模式][-世代]`，
// 引擎落库后为 `ast:default:inc:...`），同时不误伤含 "eval" 字样的用户空间
// （`u10-mem-eval-team`）。早期裸前缀形态（`mem-eval-<x>`）不在子串令牌语义内。
func DefaultSiblingExcludeScopeTokens() []string {
	return []string{EvalScopeMarker + evalScopeSuiteLoCoMo, EvalScopeMarker + evalScopeSuiteATM}
}

// Client is the top-level Ultra-Thin Client coordinating any user-chosen PlaintextStore
// (SQLite Vault by default, or JSONFileStore, DirTextStore, MySQL, PostgreSQL, etc.)
// with the local plaintext vault retrieval kernel.
type Client struct {
	Vault     *Vault
	Store     PlaintextStore
	Transport BrainTransport
	// AssistantID / IncarnationID / Locus are the persona coordinate stamped onto every
	// pulse and onto each locally saved row. Unlimited parallel incarnations therefore stay
	// attributable without widening the memory envelope; empty values simply inherit the
	// transport-level identity.
	AssistantID   string
	IncarnationID string
	Locus         string

	// recallMu guards lastRecall, the outcome of the most recent Recall call, so a caller
	// can distinguish "memory holds nothing" from "retrieval was unavailable".
	recallMu   sync.Mutex
	lastRecall RecallDiagnostics
}

// SetIdentity records the persona coordinate on the client and its local vault,
// so pulses and local rows agree.
func (c *Client) SetIdentity(assistantID, incarnationID, locus string) {
	if c == nil {
		return
	}
	c.AssistantID = strings.TrimSpace(assistantID)
	c.IncarnationID = strings.TrimSpace(incarnationID)
	c.Locus = strings.TrimSpace(locus)
	if c.Vault != nil {
		c.Vault.SetIdentity(VaultIdentity{
			AssistantID: c.AssistantID, IncarnationID: c.IncarnationID, Locus: c.Locus,
		})
	}
}

// stampIdentity fills the persona coordinate on a pulse that does not carry one yet.
func (c *Client) stampIdentity(pkt *BlindPulsePacket) {
	if strings.TrimSpace(pkt.AssistantID) == "" {
		pkt.AssistantID = c.AssistantID
	}
	if strings.TrimSpace(pkt.IncarnationID) == "" {
		pkt.IncarnationID = c.IncarnationID
	}
	if strings.TrimSpace(pkt.Locus) == "" {
		pkt.Locus = c.Locus
	}
}

// NewClient constructs a production Ultra-Thin Client backed by the default local SQLite Vault and a BrainTransport.
func NewClient(vault *Vault, transport BrainTransport) *Client {
	return &Client{
		Vault:     vault,
		Store:     vault,
		Transport: transport,
	}
}

// NewLocalClient constructs a local_plain client backed by the local SQLite Vault
// and its in-process LocalVaultTransport (zero network egress).
func NewLocalClient(vault *Vault) *Client {
	return NewClient(vault, NewLocalVaultTransport(vault))
}

// NewClientWithStore constructs an Ultra-Thin Client backed by ANY custom PlaintextStore
// (e.g., JSONFileStore, DirTextStore, MySQL, PostgreSQL, Redis, or custom business tables).
func NewClientWithStore(store PlaintextStore, transport BrainTransport) *Client {
	v, _ := store.(*Vault)
	return &Client{
		Vault:     v,
		Store:     store,
		Transport: transport,
	}
}

func (c *Client) activeStore() PlaintextStore {
	if c.Store != nil {
		return c.Store
	}
	return c.Vault
}

// Remember stores a user Harness dialogue turn in `LayerEpisodic` ("episodic", the default conversational stream),
// packs ONLY the blind `{h, b, f}` integer frame into the local vault, and records the
// local retrieval address & dry-sentence indices.
func (c *Client) Remember(ctx context.Context, docID string, plaintext string, occurredAt int64) (IngestResponse, error) {
	return c.RememberWithLayer(ctx, docID, plaintext, occurredAt, LayerEpisodic, nil)
}

// RememberEpisodic explicitly stores a user Harness conversation turn in `LayerEpisodic` ("episodic").
func (c *Client) RememberEpisodic(ctx context.Context, docID string, plaintext string, occurredAt int64) (IngestResponse, error) {
	return c.RememberWithLayer(ctx, docID, plaintext, occurredAt, LayerEpisodic, nil)
}

// localIngestResponse preserves every sentence before any transport write. The
// local plaintext vault is the source of truth; the retrieval address is a
// derived index that may be backfilled on a later reconcile.
func localIngestResponse(docID, layer string, sentenceCount int) IngestResponse {
	kept := make([]int, sentenceCount)
	for i := range kept {
		kept[i] = i
	}
	return IngestResponse{UnitID: docID, Layer: layer, KeptSentenceIndices: kept}
}

// RememberEpisodicByRef ingests the blind `{h, b, f}` pulse of `livePlaintext`
// (`layer="episodic"`) into the local vault and records a verified zero-copy pointer in local SQLite.
// If the pointer cannot be verified, the supplied plaintext is preserved locally instead.
//
// `turnRef` is the provenance of the unit (a content-addressed unit can be said in
// several turns): the vault row keeps one pointer, the lineage table keeps every
// occurrence. Chunked units pass an empty refPointer, so their text is stored locally.
func (c *Client) RememberEpisodicByRef(ctx context.Context, docID, refPointer, turnRef, livePlaintext string, occurredAt int64) (IngestResponse, error) {
	if occurredAt < 0 {
		occurredAt = 0
	}
	st := c.activeStore()
	pkt, sents := st.PackBlindPacket(docID, livePlaintext, occurredAt)
	pkt.Layer = LayerEpisodic
	c.stampIdentity(&pkt)
	usePointer := c.Vault != nil && strings.HasPrefix(strings.TrimSpace(refPointer), "@ref:")
	invalidPointer := false
	saveLocal := func(resp IngestResponse) error {
		if usePointer {
			if err := c.Vault.SaveLocalEpisodicPointerIndexed(docID, occurredAt, refPointer, turnRef, livePlaintext, pkt, resp); err == nil {
				return nil
			} else if errors.Is(err, ErrEpisodicUnitDeleted) || !errors.Is(err, ErrEpisodicIndexItem) {
				return err
			} else if verifyEpisodicPointerSource(refPointer, livePlaintext) == nil {
				var sameInline int
				checkErr := c.Vault.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM thin_vault_docs d WHERE d.doc_id = ? AND d.raw_text = ? AND (d.layer = 'episodic' OR d.layer = '') AND NOT EXISTS (SELECT 1 FROM thin_vault_episodic_index i WHERE i.doc_id = d.doc_id) AND NOT EXISTS (SELECT 1 FROM thin_vault_episodic_lineage l WHERE l.doc_id = d.doc_id AND l.turn_ref = d.raw_text))`, docID, livePlaintext).Scan(&sameInline)
				if checkErr != nil {
					return checkErr
				}
				if sameInline != 0 && livePlaintext != strings.TrimSpace(refPointer) {
					return nil
				}
				return err
			}
			usePointer = false
			invalidPointer = true
			turnRef = ""
		}
		if invalidPointer {
			sentsJSON, err := json.Marshal(sents)
			if err != nil {
				return err
			}
			keptJSON, err := json.Marshal(resp.KeptSentenceIndices)
			if err != nil {
				return err
			}
			tx, err := c.Vault.db.Begin()
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			if err := checkEpisodicTombstone(tx, docID); err != nil {
				return err
			}
			// Never replace a different source, even if another writer claims this ID.
			result, err := tx.Exec(`INSERT INTO thin_vault_docs(doc_id, layer, occurred_at, inserted_at, raw_text, sentences_json, address_uri, coord64, waveform_bar, doc_macro_wave, kept_indices_json, assistant_id, incarnation_id, locus) VALUES (?, 'episodic', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(doc_id) DO UPDATE SET occurred_at=CASE WHEN excluded.occurred_at > 0 THEN excluded.occurred_at ELSE thin_vault_docs.occurred_at END, inserted_at=CASE WHEN thin_vault_docs.inserted_at > 0 THEN thin_vault_docs.inserted_at ELSE excluded.inserted_at END, sentences_json=excluded.sentences_json, address_uri=excluded.address_uri, coord64=excluded.coord64, waveform_bar=excluded.waveform_bar, doc_macro_wave=excluded.doc_macro_wave, kept_indices_json=excluded.kept_indices_json, assistant_id=excluded.assistant_id, incarnation_id=excluded.incarnation_id, locus=excluded.locus WHERE thin_vault_docs.raw_text=excluded.raw_text`, docID, occurredAt, time.Now().Unix(), livePlaintext, string(sentsJSON), resp.AddressURI, int64(resp.AddressCoord64), resp.WaveformBar, resp.DocMacroWaveform, string(keptJSON), c.Vault.identity.AssistantID, c.Vault.identity.IncarnationID, c.Vault.identity.Locus)
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if changed == 0 {
				return fmt.Errorf("thinclient: episodic document already exists with different source")
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			c.Vault.clearSlotCache()
			return nil
		}
		if c.Vault != nil {
			resp.Layer = LayerEpisodic
		}
		if err := st.SaveLocalPlaintext(docID, occurredAt, livePlaintext, sents, resp); err != nil {
			return err
		}
		if c.Vault != nil {
			if err := c.Vault.RecordEpisodicLineage(turnRef, docID, occurredAt); err != nil {
				return err
			}
		}
		return nil
	}
	localResp := localIngestResponse(docID, LayerEpisodic, len(sents))
	if err := saveLocal(localResp); err != nil {
		return IngestResponse{}, err
	}
	if c.Transport == nil {
		return localResp, nil
	}
	resp, err := c.Transport.IngestBlindPulse(ctx, pkt)
	if err != nil {
		return localResp, err
	}
	if strings.TrimSpace(resp.Layer) == "" {
		resp.Layer = LayerEpisodic
	}
	if err := saveLocal(resp); err != nil {
		return IngestResponse{}, err
	}
	return resp, nil
}

// RememberBackground explicitly stores a knowledge-base / codebase / document substrate item in `LayerBackground` ("background").
func (c *Client) RememberBackground(ctx context.Context, docID string, plaintext string, occurredAt int64) (IngestResponse, error) {
	return c.RememberWithLayer(ctx, docID, plaintext, occurredAt, LayerBackground, nil)
}

// RememberWithLayer stores the plaintext in the user's chosen PlaintextStore and packs the blind `{h, b, f}`
// frame into the local vault with an explicit cognitive layer
// (`"background" | "episodic" | "noetic"`).
func (c *Client) RememberWithLayer(ctx context.Context, docID string, plaintext string, occurredAt int64, layer string, citedUnitIDs []string) (IngestResponse, error) {
	st := c.activeStore()
	pkt, sents := st.PackBlindPacket(docID, plaintext, occurredAt)
	if strings.TrimSpace(layer) != "" {
		pkt.Layer = strings.TrimSpace(layer)
	} else {
		pkt.Layer = "episodic"
	}
	if len(citedUnitIDs) > 0 {
		pkt.CitedUnitIDs = citedUnitIDs
	}
	c.stampIdentity(&pkt)
	localResp := localIngestResponse(docID, pkt.Layer, len(sents))
	if err := st.SaveLocalPlaintext(docID, occurredAt, plaintext, sents, localResp); err != nil {
		return IngestResponse{}, err
	}
	if c.Transport == nil {
		return localResp, nil
	}
	resp, err := c.Transport.IngestBlindPulse(ctx, pkt)
	if err != nil {
		return localResp, err
	}
	if strings.TrimSpace(resp.Layer) == "" {
		resp.Layer = pkt.Layer
	}
	if err := st.SaveLocalPlaintext(docID, occurredAt, plaintext, sents, resp); err != nil {
		return IngestResponse{}, err
	}
	return resp, nil
}

// RecallDiagnostics reports how the last Recall on this client was served. Degraded means
// retrieval was unavailable and the answer came from the local lexical fallback,
// so a caller must never read an empty result as "memory holds nothing".
type RecallDiagnostics struct {
	Degraded bool
	Reason   string
}

// RecallDiagnostics returns the outcome of the most recent Recall call.
func (c *Client) RecallDiagnostics() RecallDiagnostics {
	if c == nil {
		return RecallDiagnostics{}
	}
	c.recallMu.Lock()
	defer c.recallMu.Unlock()
	return c.lastRecall
}

func (c *Client) setRecallDiagnostics(d RecallDiagnostics) {
	if c == nil {
		return
	}
	c.recallMu.Lock()
	c.lastRecall = d
	c.recallMu.Unlock()
}

// Recall converts the query into a `{h, b, f}` frame, queries ranked UnitIDs
// and weights, merges the dual-cluster kernel ranking (`SearchClientSDKDefault`),
// and resolves the plaintext locally from the user's PlaintextStore.
func (c *Client) Recall(ctx context.Context, queryPlaintext string, topK int) ([]LocalResolvedHit, error) {
	return c.RecallWithOptions(ctx, queryPlaintext, BlindQueryOptions{TopK: topK})
}

// RecallWithOptions executes the delivery recall: the blind-local candidates are
// hydrated and the dual-cluster kernel ranking is authoritative, with explicit
// temporal horizon (AsOf / TimeRef) and distillation options forwarded.
func (c *Client) RecallWithOptions(ctx context.Context, queryPlaintext string, opts BlindQueryOptions) ([]LocalResolvedHit, error) {
	topK := opts.TopK
	if topK <= 0 {
		topK = 10
	}
	opts.TopK = topK
	st := c.activeStore()
	if c.Transport == nil {
		c.setRecallDiagnostics(RecallDiagnostics{Degraded: true, Reason: "no_transport"})
		return c.RecallDefault(queryPlaintext, topK)
	}
	qPkt, _ := st.PackBlindPacket("QUERY", queryPlaintext, opts.TimeRef)
	var rankedHits []QueryHit
	var err error
	maxProbes := topK * 4
	if maxProbes < 64 {
		maxProbes = 64
	}
	if localTr, ok := c.Transport.(*LocalVaultTransport); ok && c.Vault != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := localTr.Vault.verifyTransportScope(qPkt.Scope); err != nil {
			return nil, err
		}
		c.setRecallDiagnostics(RecallDiagnostics{})
		rankedHits, err = c.Vault.SearchClientSDKDefault(queryPlaintext, maxProbes)
		if err != nil {
			return nil, err
		}
	} else if localTr, ok := c.Transport.(*LocalVaultTransport); ok {
		localQPkt, _ := st.PackBlindPacket("QUERY", queryPlaintext, 0)
		localQPkt.Scope = qPkt.Scope
		rankedHits, err = localTr.QueryBlindManifold(ctx, localQPkt, 0)
	} else {
		rankedHits, err = c.Transport.QueryBlindManifold(ctx, qPkt, topK)
	}
	if err != nil {
		if _, local := c.Transport.(*LocalVaultTransport); local {
			return nil, err
		}
		if c.Vault != nil {
			if fallback, fErr := c.RecallDefault(queryPlaintext, topK); fErr == nil && len(fallback) > 0 {
				c.setRecallDiagnostics(RecallDiagnostics{Degraded: true, Reason: "engine_error_fallback_local"})
				return fallback, nil
			}
		}
		c.setRecallDiagnostics(RecallDiagnostics{Degraded: true, Reason: "engine_error"})
		return nil, err
	}
	c.setRecallDiagnostics(RecallDiagnostics{})
	_, local := c.Transport.(*LocalVaultTransport)
	if local && c.Vault != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(rankedHits) > maxProbes {
			maxProbes = len(rankedHits)
		}
		resolved := make([]LocalResolvedHit, 0, topK+8)
		usableLocal := 0
		seenResolved := make(map[string]bool, len(rankedHits))
		// 交付列表必须仅由金库与查询决定：不设墙钟断流；确定性上界 = maxProbes 与
		// 「已获 topK 条可用命中」两个条件。需要限时的调用方经 ctx 取消，循环按其保已得结果退出。
		for i := 0; i < len(rankedHits) && i < maxProbes; i++ {
			ch := rankedHits[i]
			if seenResolved[ch.UnitID] {
				continue
			}
			seenResolved[ch.UnitID] = true
			if err := ctx.Err(); err != nil {
				if usableLocal > 0 {
					break
				}
				return nil, err
			}
			rawTxt, distTxt, keptSents, err := st.LoadLocalDocument(ch.UnitID)
			if err != nil {
				resolved = append(resolved, LocalResolvedHit{QueryHit: ch, SourceUnavailable: true})
				continue
			}
			usableLocal++
			if len(ch.KeptSentenceIndices) > 0 {
				allSents := SplitSentences(rawTxt)
				if len(allSents) > len(ch.KeptSentenceIndices) {
					distTxt, keptSents = ReconstructDistilled(rawTxt, allSents, ch.KeptSentenceIndices)
				}
			}
			resolved = append(resolved, LocalResolvedHit{
				QueryHit:           ch,
				RawPlaintext:       rawTxt,
				DistilledPlaintext: distTxt,
				KeptSentences:      keptSents,
				Authors:            RoleTaggedAuthors(rawTxt),
			})
			if i+1 >= topK && usableLocal >= topK {
				break
			}
		}
		return limitHydratedLocalHits(resolved, topK), nil
	}

	seen := make(map[string]bool, len(rankedHits)*2)
	resolved := make([]LocalResolvedHit, 0, len(rankedHits)*2)
	rankByID := make(map[string]int, len(rankedHits))
	maxRankedWeight := 1.0
	usableLocal := 0
	// Missing sources remain in the ranked prefix for gap reporting, but do not
	// consume its usable-hit budget. The transport has already sorted all matches.
	for i, ch := range rankedHits {
		if local {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		rawTxt, distTxt, keptSents, err := st.LoadLocalDocument(ch.UnitID)
		if err != nil {
			resolved = append(resolved, LocalResolvedHit{QueryHit: ch, SourceUnavailable: true})
			seen[ch.UnitID] = true
			continue
		}
		if local {
			usableLocal++
		}
		if len(ch.KeptSentenceIndices) > 0 {
			allSents := SplitSentences(rawTxt)
			if len(allSents) > len(ch.KeptSentenceIndices) {
				distTxt, keptSents = ReconstructDistilled(rawTxt, allSents, ch.KeptSentenceIndices)
			}
		}
		seen[ch.UnitID] = true
		rankByID[ch.UnitID] = len(resolved) + 1
		if ch.FinalWeight > maxRankedWeight {
			maxRankedWeight = ch.FinalWeight
		}
		resolved = append(resolved, LocalResolvedHit{
			QueryHit:           ch,
			RawPlaintext:       rawTxt,
			DistilledPlaintext: distTxt,
			KeptSentences:      keptSents,
			Authors:            RoleTaggedAuthors(rawTxt),
		})
		if local && i+1 >= topK && usableLocal >= topK {
			break
		}
	}
	if c.Vault != nil {
		localCap := topK
		if !local && localCap > 6 {
			localCap = 6
		} else if local && localCap < 20 {
			localCap = 20
		}
		if local {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if localHits, lErr := c.Vault.SearchClientSDKDefault(queryPlaintext, localCap); lErr == nil && len(localHits) > 0 {
			if local {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			localWeightByID := make(map[string]float64, len(localHits))
			topLocalScore := localHits[0].FinalWeight
			if topLocalScore <= 0 {
				topLocalScore = 1.0
			}
			addedLocalOnly := 0
			maxLocalAdmit := 2
			if local {
				maxLocalAdmit = topK
			}
			for _, lh := range localHits {
				if local {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				localWeightByID[lh.UnitID] = lh.FinalWeight
				if seen[lh.UnitID] {
					continue
				}
				// When transport hits exist, only admit up to 2 high-confidence local-only L1/L2 units
				// (within 75% of the top local score) so local background chunks supplement rather than flood.
				// In local_plain mode, SearchClientSDKDefault (dual-cluster kernel + L3 crystallization merge)
				// is the authoritative ranker.
				if !local && len(rankByID) > 0 && (addedLocalOnly >= maxLocalAdmit || lh.FinalWeight < topLocalScore*0.75) {
					continue
				}
				if local && addedLocalOnly >= maxLocalAdmit {
					continue
				}
				rawTxt, distTxt, keptSents, err := st.LoadLocalDocument(lh.UnitID)
				if err != nil {
					resolved = append(resolved, LocalResolvedHit{QueryHit: lh, SourceUnavailable: true})
					seen[lh.UnitID] = true
					continue
				}
				seen[lh.UnitID] = true
				addedLocalOnly++
				resolved = append(resolved, LocalResolvedHit{
					QueryHit:           lh,
					RawPlaintext:       rawTxt,
					DistilledPlaintext: distTxt,
					KeptSentences:      keptSents,
					Authors:            RoleTaggedAuthors(rawTxt),
				})
			}
			// Delivery ordering: in local_plain mode (`local == true`) the hits ranked by
			// SearchClientSDKDefault (dual-cluster kernel + L3 crystallization merge) are
			// the authoritative order, carried through a monotone weight transform; hits
			// seen only by the exhaustive candidate pass (`QueryBlindManifold`) rank below
			// the authority set. The non-local transport branch keeps the legacy consensus
			// scaling for a custom transport surface.
			for i := range resolved {
				uid := resolved[i].UnitID
				_, inRanked := rankByID[uid]
				_, inLocal := localWeightByID[uid]
				if local {
					if inLocal {
						sdkNorm := localWeightByID[uid] / topLocalScore
						energyBoost := 1.0 + 0.368*sdkNorm
						resolved[i].FinalWeight = maxRankedWeight * (1.0 + sdkNorm) * energyBoost
					} else {
						resolved[i].FinalWeight *= 0.65
					}
				} else if inRanked && inLocal {
					sdkNorm := localWeightByID[uid] / topLocalScore
					resolved[i].FinalWeight *= (1.0 + 0.368*sdkNorm)
				} else if !inRanked && inLocal {
					resolved[i].FinalWeight = maxRankedWeight * (0.368 * (localWeightByID[uid] / topLocalScore))
				}
			}
			sort.SliceStable(resolved, func(i, j int) bool {
				if resolved[i].FinalWeight != resolved[j].FinalWeight {
					return resolved[i].FinalWeight > resolved[j].FinalWeight
				}
				return resolved[i].UnitID < resolved[j].UnitID
			})
			if local {
				resolved = limitHydratedLocalHits(resolved, topK)
			} else if len(resolved) > topK {
				resolved = resolved[:topK]
			}
		}
	}
	if _, local := c.Transport.(*LocalVaultTransport); local {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

// RecallDefault executes the built-in Client SDK Default Recall (`SearchClientSDKDefault`:
// the dual-cluster kernel ranking over the local plaintext vault plus the L3
// crystallization merge) directly against the local Vault without any transport round trip.
func (c *Client) RecallDefault(queryPlaintext string, topK int) ([]LocalResolvedHit, error) {
	if c.Vault == nil {
		return nil, fmt.Errorf("thinclient: RecallDefault requires a local SQLite Vault")
	}
	if topK <= 0 {
		topK = 10
	}
	hits, err := c.Vault.SearchClientSDKDefault(queryPlaintext, topK)
	if err != nil {
		return nil, err
	}
	st := c.activeStore()
	out := make([]LocalResolvedHit, 0, len(hits))
	for _, h := range hits {
		rawTxt, distTxt, keptSents, lErr := st.LoadLocalDocument(h.UnitID)
		if lErr != nil {
			out = append(out, LocalResolvedHit{QueryHit: h, SourceUnavailable: true})
			continue
		}
		out = append(out, LocalResolvedHit{
			QueryHit:           h,
			RawPlaintext:       rawTxt,
			DistilledPlaintext: distTxt,
			KeptSentences:      keptSents,
			Authors:            RoleTaggedAuthors(rawTxt),
		})
	}
	return out, nil
}

// RememberNoetic stores an L3 Noetic memory record (Query + Retrieved Sentences + Answer)
// directly in the client's own local SQLite table (`thin_vault_noetic`).
func (c *Client) RememberNoetic(noeticID string, queryText string, citedUnitIDs []string, citedSentences []string, answerText string, occurredAt int64) error {
	return c.Vault.SaveLocalNoetic(LocalNoeticRecord{
		NoeticID:       noeticID,
		OccurredAt:     occurredAt,
		Status:         "active",
		QueryText:      queryText,
		AnswerText:     answerText,
		CitedUnitIDs:   citedUnitIDs,
		CitedSentences: citedSentences,
		AssistantID:    c.AssistantID,
		IncarnationID:  c.IncarnationID,
	})
}

// blindPacketDice scores the weighted unique-atom overlap of two blind query
// packets. Only the original retrieval cues participate: a long cited answer
// must never manufacture a match from an incidental token deep in its evidence.
func blindPacketDice(a, b BlindPulsePacket) float64 {
	mass := func(slots []BlindTokenAtom) (map[uint64]uint16, uint64) {
		byHash := make(map[uint64]uint16, len(slots))
		for _, slot := range slots {
			if slot.RawBitsX10 > byHash[slot.H] {
				byHash[slot.H] = slot.RawBitsX10
			}
		}
		var total uint64
		for _, width := range byHash {
			total += uint64(width)
		}
		return byHash, total
	}
	aMass, aTotal := mass(a.Slots)
	bMass, bTotal := mass(b.Slots)
	if aTotal == 0 || bTotal == 0 {
		return 0
	}
	var intersection uint64
	for h, width := range aMass {
		other, ok := bMass[h]
		if !ok {
			continue
		}
		if other < width {
			width = other
		}
		intersection += uint64(width)
	}
	return float64(intersection) / float64(aTotal+bTotal) * 2
}

func (v *Vault) loadActiveNoeticRecords() ([]LocalNoeticRecord, error) {
	if v == nil || v.db == nil {
		return nil, nil
	}
	v.checkExternalDBMutation()
	if v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.RLock()
		ready := v.snapRef.noeticReady
		hasLargeSnap := v.snapRef.snap != nil && len(v.snapRef.snap.docs) >= 2048
		cached := v.snapRef.activeNoetics
		lastTime := v.snapRef.lastNoeticTime
		refreshing := v.snapRef.noeticRefreshing
		v.slotCacheMu.RUnlock()
		if ready && hasLargeSnap {
			if time.Since(lastTime) >= 5*time.Minute && !refreshing {
				v.slotCacheMu.Lock()
				if !v.snapRef.noeticRefreshing {
					v.snapRef.noeticRefreshing = true
					v.snapRef.lastNoeticTime = time.Now()
					go func() {
						defer func() {
							v.slotCacheMu.Lock()
							v.snapRef.noeticRefreshing = false
							v.slotCacheMu.Unlock()
						}()
						_, _ = v.queryAndCacheActiveNoeticRecords()
					}()
				}
				v.slotCacheMu.Unlock()
			}
			return cached, nil
		}
	}
	return v.queryAndCacheActiveNoeticRecords()
}

func (v *Vault) queryAndCacheActiveNoeticRecords() ([]LocalNoeticRecord, error) {
	if v == nil || v.db == nil {
		return nil, nil
	}
	var cnt, maxRowID, occSum int64
	hasFP := false
	if v.slotCacheMu != nil && v.snapRef != nil {
		if v.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(rowid), 0), COALESCE(SUM(occurred_at), 0) FROM thin_vault_noetic WHERE status = 'active'`).Scan(&cnt, &maxRowID, &occSum) == nil {
			hasFP = true
			v.slotCacheMu.RLock()
			if v.snapRef.noeticReady &&
				v.snapRef.noeticCnt == cnt &&
				v.snapRef.noeticMaxRowID == maxRowID &&
				v.snapRef.noeticOccSum == occSum {
				cached := v.snapRef.activeNoetics
				v.slotCacheMu.RUnlock()
				v.slotCacheMu.Lock()
				v.snapRef.lastNoeticTime = time.Now()
				v.slotCacheMu.Unlock()
				return cached, nil
			}
			v.slotCacheMu.RUnlock()
			if cnt == 0 {
				v.slotCacheMu.Lock()
				v.snapRef.noeticReady = true
				v.snapRef.lastNoeticTime = time.Now()
				v.snapRef.noeticCnt = 0
				v.snapRef.noeticMaxRowID = 0
				v.snapRef.noeticOccSum = 0
				v.snapRef.activeNoetics = nil
				v.slotCacheMu.Unlock()
				return nil, nil
			}
		}
	}
	rows, err := v.db.Query(`
				SELECT noetic_id, occurred_at, COALESCE(inserted_at, occurred_at), status, query_text, answer_text, cited_unit_ids_json, cited_sentences_json,
				       COALESCE(address_uri, ''), COALESCE(coord64, 0), COALESCE(waveform_bar, ''),
				       COALESCE(assistant_id, ''), COALESCE(incarnation_id, '')
				FROM thin_vault_noetic
				WHERE status = 'active'
				ORDER BY occurred_at DESC
			`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []LocalNoeticRecord
	idxByID := make(map[string]int, 64)
	inCite := make(map[string]int, 64)
	for rows.Next() {
		var rec LocalNoeticRecord
		var uidsJSON, sentsJSON string
		if err := rows.Scan(&rec.NoeticID, &rec.OccurredAt, &rec.InsertedAt, &rec.Status, &rec.QueryText, &rec.AnswerText,
			&uidsJSON, &sentsJSON, &rec.AddressURI, &rec.Coord64, &rec.WaveformBar, &rec.AssistantID, &rec.IncarnationID); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(uidsJSON), &rec.CitedUnitIDs)
		_ = json.Unmarshal([]byte(sentsJSON), &rec.CitedSentences)
		_ = v.noeticBlindPacket(rec.NoeticID, rec.QueryText, rec.OccurredAt)
		idxByID[rec.NoeticID] = len(list)
		for _, cid := range rec.CitedUnitIDs {
			cid = strings.TrimSpace(cid)
			if cid != "" && cid != rec.NoeticID {
				inCite[cid]++
			}
		}
		list = append(list, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range list {
		rec := &list[i]
		reuse := inCite[rec.NoeticID]
		if rec.InsertedAt > 0 && rec.OccurredAt > rec.InsertedAt {
			reuse++
		}
		rec.ReuseCount = reuse
		if len(rec.CitedUnitIDs) > 0 {
			seen := make(map[string]bool, len(rec.CitedUnitIDs)+4)
			expanded := make([]string, 0, len(rec.CitedUnitIDs)+4)
			for _, cid := range rec.CitedUnitIDs {
				cid = strings.TrimSpace(cid)
				if cid == "" || seen[cid] {
					continue
				}
				seen[cid] = true
				expanded = append(expanded, cid)
				if childIdx, ok := idxByID[cid]; ok && childIdx != i {
					for _, leafID := range list[childIdx].CitedUnitIDs {
						leafID = strings.TrimSpace(leafID)
						if leafID != "" && leafID != rec.NoeticID && !seen[leafID] {
							seen[leafID] = true
							expanded = append(expanded, leafID)
						}
					}
				}
			}
			rec.CitedUnitIDs = expanded
		}
	}
	if hasFP && v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.Lock()
		v.snapRef.noeticReady = true
		v.snapRef.lastNoeticTime = time.Now()
		v.snapRef.noeticCnt = cnt
		v.snapRef.noeticMaxRowID = maxRowID
		v.snapRef.noeticOccSum = occSum
		v.snapRef.activeNoetics = list
		v.slotCacheMu.Unlock()
	}
	return list, nil
}

// RecallNoetic searches the client's local `thin_vault_noetic` table using keyed HMAC root commitments.
func (c *Client) RecallNoetic(queryPlaintext string, topK int) ([]LocalNoeticRecord, error) {
	if c.Vault == nil || c.Vault.db == nil {
		return nil, nil
	}
	qPkt, _ := c.Vault.PackBlindPacket("Q-NOETIC", queryPlaintext, 0)
	if len(qPkt.Slots) == 0 {
		return nil, nil
	}

	records, err := c.Vault.loadActiveNoeticRecords()
	if err != nil || len(records) == 0 {
		return nil, err
	}

	var hits []LocalNoeticRecord
	for i := range records {
		rec := records[i]
		nPkt := c.Vault.noeticBlindPacket(rec.NoeticID, rec.QueryText, rec.OccurredAt)
		rec.MatchScore = blindPacketDice(qPkt, nPkt)
		if rec.MatchScore > 0 {
			hits = append(hits, rec)
		}
	}
	effScore := func(r LocalNoeticRecord) float64 {
		return r.MatchScore * r.VerificationGain()
	}
	sort.SliceStable(hits, func(i, j int) bool {
		ei, ej := effScore(hits[i]), effScore(hits[j])
		if ei != ej {
			return ei > ej
		}
		if hits[i].MatchScore != hits[j].MatchScore {
			return hits[i].MatchScore > hits[j].MatchScore
		}
		return hits[i].NoeticID < hits[j].NoeticID
	})
	// Deliver the strongest handle plateau plus any verified handle whose
	// reinforced score stays within 1/e of the peak.
	if len(hits) > 0 {
		bestEff := effScore(hits[0])
		bestMatch := hits[0].MatchScore
		kept := hits[:1]
		for i := 1; i < len(hits); i++ {
			if hits[i].MatchScore == bestMatch || (hits[i].ReuseCount > 0 && withinNaturalEnergyEnvelope(effScore(hits[i]), bestEff)) {
				kept = append(kept, hits[i])
			}
		}
		hits = kept
	}
	if topK > 0 && len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// LocalVaultTransport provides an offline/local BrainTransport over the local SQLite Vault
// using strictly integer token-commitment matching (zero floating-point transcendental math).
type LocalVaultTransport struct {
	Vault *Vault
}

// NewLocalVaultTransport creates a local fallback transport backed by the SQLite Vault.
func NewLocalVaultTransport(vault *Vault) *LocalVaultTransport {
	return &LocalVaultTransport{Vault: vault}
}

func (v *Vault) verifyTransportScope(scope string) error {
	if v == nil || v.db == nil {
		return nil
	}
	reqScope := strings.TrimSpace(scope)
	if v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.RLock()
		bound := v.snapRef.boundScope
		hasLargeSnap := v.snapRef.snap != nil && len(v.snapRef.snap.docs) >= 2048
		v.slotCacheMu.RUnlock()
		if bound != "" && hasLargeSnap {
			if reqScope != "" && reqScope != bound && reqScope != strings.ReplaceAll(bound, ":", "-") {
				return fmt.Errorf("thinclient: requested scope %q does not match local_plain vault binding %q", reqScope, bound)
			}
			return nil
		}
	}
	var bound, storageType string
	err := v.db.QueryRow(`SELECT v, typeof(v) FROM thin_vault_meta WHERE k = 'local_plain_scope'`).Scan(&bound, &storageType)
	if err == nil {
		bound = strings.TrimSpace(bound)
		if storageType != "text" || bound == "" {
			return fmt.Errorf("thinclient: invalid local_plain vault scope binding")
		}
		if v.slotCacheMu != nil && v.snapRef != nil {
			v.slotCacheMu.Lock()
			v.snapRef.boundScope = bound
			v.slotCacheMu.Unlock()
		}
		if reqScope != "" && reqScope != bound && reqScope != strings.ReplaceAll(bound, ":", "-") {
			return fmt.Errorf("thinclient: requested scope %q does not match local_plain vault binding %q", reqScope, bound)
		}
		return nil
	}
	if reqScope != "" {
		var populated int
		_ = v.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM thin_vault_docs LIMIT 1) OR EXISTS(SELECT 1 FROM thin_vault_noetic LIMIT 1)`).Scan(&populated)
		if populated != 0 {
			return fmt.Errorf("thinclient: local_plain vault scope is unbound; cannot serve explicit scope %q", reqScope)
		}
	}
	return nil
}

func (l *LocalVaultTransport) IngestBlindPulse(_ context.Context, pkt BlindPulsePacket) (IngestResponse, error) {
	if l.Vault != nil {
		if err := l.Vault.verifyTransportScope(pkt.Scope); err != nil {
			return IngestResponse{}, err
		}
	}
	var keptIdx []int
	seenSentSig := make(map[uint64]int, len(pkt.SentenceFrames))
	for sIdx, sf := range pkt.SentenceFrames {
		var sig uint64
		for _, a := range sf {
			sig = (sig * 1099511628211) ^ a.H
		}
		seenSentSig[sig]++
		if seenSentSig[sig] <= 2 {
			keptIdx = append(keptIdx, sIdx)
		}
	}
	return IngestResponse{
		UnitID:              pkt.UnitID,
		Epoch:               1,
		WaveformHex:         "F",
		WaveformBar:         "█",
		ConservedMass:       1.0,
		QualityMult:         1.0,
		AddressURI:          "C6:L1:LOCAL#" + pkt.UnitID,
		AddressCoord64:      0x1000000000000001,
		ContainerID:         0x1100000000000001,
		KeptSentenceIndices: keptIdx,
	}, nil
}

// DeleteUnits is a local no-op because LocalVaultTransport keeps every unit in the local vault.
func (l *LocalVaultTransport) DeleteUnits(_ context.Context, _ []string) error {
	return nil
}

func (l *LocalVaultTransport) QueryBlindManifold(ctx context.Context, queryPkt BlindPulsePacket, topK int) ([]QueryHit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l.Vault == nil || l.Vault.db == nil {
		return nil, nil
	}
	if err := l.Vault.verifyTransportScope(queryPkt.Scope); err != nil {
		return nil, err
	}
	snap, snapErr := l.Vault.loadCorpusSnapshot(ctx)
	if snapErr != nil || snap == nil || len(snap.docs) == 0 {
		return nil, snapErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	zHits := snap.searchSnapshotCoreEngines(l.Vault, "", &queryPkt, topK)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]QueryHit, 0, len(zHits))
	for _, zh := range zHits {
		if zh.Energy <= 0 {
			continue
		}
		if di, ok := snap.docIdxByID[zh.UnitID]; ok && di >= 0 && di < len(snap.docs) {
			d := snap.docs[di]
			out = append(out, QueryHit{
				UnitID:         d.docID,
				Layer:          d.layer,
				OccurredAt:     d.occ,
				AddressURI:     d.addrURI,
				AddressCoord64: d.coord64,
				WaveformBar:    d.waveBar,
				WaveScore:      zh.Energy,
				FinalWeight:    zh.Energy,
			})
		} else {
			out = append(out, QueryHit{
				UnitID:      zh.UnitID,
				Layer:       LayerEpisodic,
				WaveScore:   zh.Energy,
				FinalWeight: zh.Energy,
			})
		}
	}
	return out, nil
}

// ResolveGraph builds the source-agnostic topology from the local Vault (or any transport that
// exposes BuildBlindGraph) and resolves plaintext summaries for every unit and Noetic node on the client.
func (c *Client) ResolveGraph(ctx context.Context, req BlindGraphRequest) (LocalResolvedGraph, error) {
	var graphResp GraphResponse
	if gt, ok := c.Transport.(interface {
		BuildBlindGraph(context.Context, BlindGraphRequest) (GraphResponse, error)
	}); ok {
		var err error
		graphResp, err = gt.BuildBlindGraph(ctx, req)
		if err != nil {
			return LocalResolvedGraph{}, err
		}
	} else if c.Vault != nil {
		localTr := NewLocalVaultTransport(c.Vault)
		graphResp, _ = localTr.BuildBlindGraph(ctx, req)
	}

	layerFilter := make(map[string]bool, len(req.Layers))
	for _, l := range req.Layers {
		trimmed := strings.ToLower(strings.TrimSpace(l))
		if trimmed != "" {
			layerFilter[trimmed] = true
		}
	}
	allowNoetic := len(layerFilter) == 0 || layerFilter["noetic"]
	weights := map[string]float64{"background": 1.0, "episodic": 1.0, "noetic": 1.0}
	for k, v := range graphResp.LayerWeights {
		weights[k] = v
	}
	for k, v := range req.LayerWeights {
		weights[strings.ToLower(strings.TrimSpace(k))] = v
	}

	st := c.activeStore()
	noeticByID := make(map[string]LocalNoeticRecord)
	if c.Vault != nil && c.Vault.db != nil {
		rows, err := c.Vault.db.Query(`
				SELECT noetic_id, occurred_at, status, query_text, answer_text, cited_unit_ids_json,
				       COALESCE(address_uri, ''), COALESCE(coord64, 0), COALESCE(waveform_bar, '')
				FROM thin_vault_noetic WHERE status = 'active' ORDER BY occurred_at DESC
			`)
		if err == nil {
			for rows.Next() {
				var r LocalNoeticRecord
				var uidsJSON string
				if err := rows.Scan(&r.NoeticID, &r.OccurredAt, &r.Status, &r.QueryText, &r.AnswerText, &uidsJSON,
					&r.AddressURI, &r.Coord64, &r.WaveformBar); err == nil {
					_ = json.Unmarshal([]byte(uidsJSON), &r.CitedUnitIDs)
					noeticByID[r.NoeticID] = r
				}
			}
			rows.Close()
		}
	}

	resolvedNodes := make([]LocalResolvedGraphNode, 0, len(graphResp.Nodes)+len(noeticByID))
	seenUnitRaw := make(map[string]bool, len(graphResp.Nodes))
	cellIdxByNodeID := make(map[string]int, len(graphResp.Nodes))
	unitCellByRawID := make(map[string]string, len(graphResp.Nodes))
	edges := append([]GraphEdge(nil), graphResp.Edges...)
	counts := map[string]int{"background": 0, "episodic": 0, "noetic": 0}
	cellCnt, unitCnt := 0, 0

	for _, cn := range graphResp.Nodes {
		rn := LocalResolvedGraphNode{
			GraphNode: cn,
			Summary:   cn.RawID,
		}
		if cn.NodeType == "cell" || cn.NodeType == "super_cell" {
			cellCnt++
			cellIdxByNodeID[cn.ID] = len(resolvedNodes)
			rn.Summary = cn.RawID + " (" + cn.Layer + ")"
		} else {
			unitCnt++
			seenUnitRaw[cn.RawID] = true
			if cn.ParentID != "" {
				unitCellByRawID[cn.RawID] = cn.ParentID
			}
			if nr, isNoetic := noeticByID[cn.RawID]; isNoetic {
				rn.Layer = "noetic"
				rn.RawPlaintext = "Q: " + nr.QueryText + "\nA: " + nr.AnswerText
				rn.DistilledPlaintext = nr.AnswerText
				rn.Summary = nr.QueryText
				if len(rn.CitedUnitIDs) == 0 {
					rn.CitedUnitIDs = nr.CitedUnitIDs
				}
				if rn.AddressURI == "" && nr.AddressURI != "" {
					rn.AddressURI = nr.AddressURI
				}
				if rn.WaveformBar == "" && nr.WaveformBar != "" {
					rn.WaveformBar = nr.WaveformBar
				}
			} else if st != nil {
				if rawTxt, distTxt, _, err := st.LoadLocalDocument(cn.RawID); err == nil {
					rn.RawPlaintext = rawTxt
					rn.DistilledPlaintext = distTxt
					if strings.TrimSpace(distTxt) != "" {
						rn.Summary = distTxt
					} else if strings.TrimSpace(rawTxt) != "" {
						rn.Summary = rawTxt
					}
				}
				if c.Vault != nil && (rn.Layer == "" || rn.Layer == "episodic") {
					if vLayer := c.Vault.LoadLocalDocumentLayer(cn.RawID); vLayer != "" {
						rn.Layer = vLayer
					}
				}
			}
		}
		if rn.Layer == "" {
			rn.Layer = "episodic"
		}
		counts[rn.Layer]++
		resolvedNodes = append(resolvedNodes, rn)
	}

	// Merge any local Noetic records not yet present in graph nodes when Noetic layer is enabled
	if allowNoetic && weights["noetic"] > 0 {
		for nid, nr := range noeticByID {
			if seenUnitRaw[nid] {
				continue
			}
			nodeID := "unit:" + nid
			w := weights["noetic"]
			addrURI := strings.TrimSpace(nr.AddressURI)
			parentCellNodeID := ""
			if addrURI != "" {
				ck := extractLocalCellKey(addrURI)
				if ck != "" {
					parentCellNodeID = "cell:" + ck
					if cIdx, ok := cellIdxByNodeID[parentCellNodeID]; ok {
						resolvedNodes[cIdx].MemberCount++
					} else {
						cellIdxByNodeID[parentCellNodeID] = len(resolvedNodes)
						resolvedNodes = append(resolvedNodes, LocalResolvedGraphNode{
							GraphNode: GraphNode{
								ID:          parentCellNodeID,
								RawID:       ck,
								NodeType:    "cell",
								Layer:       "noetic",
								LayerWeight: w,
								AddressURI:  ck,
								Weight:      w,
								MemberCount: 1,
							},
							Summary: ck + " (noetic)",
						})
						cellCnt++
						counts["noetic"]++
					}
				}
			} else {
				for _, cited := range nr.CitedUnitIDs {
					if pc := unitCellByRawID[strings.TrimSpace(cited)]; pc != "" {
						parentCellNodeID = pc
						if cIdx, ok := cellIdxByNodeID[parentCellNodeID]; ok {
							resolvedNodes[cIdx].MemberCount++
						}
						break
					}
				}
			}
			deg := len(nr.CitedUnitIDs)
			if parentCellNodeID != "" {
				deg++
			}
			resolvedNodes = append(resolvedNodes, LocalResolvedGraphNode{
				GraphNode: GraphNode{
					ID:           nodeID,
					RawID:        nid,
					NodeType:     "unit",
					Layer:        "noetic",
					LayerWeight:  w,
					ParentID:     parentCellNodeID,
					AddressURI:   addrURI,
					WaveformBar:  nr.WaveformBar,
					Weight:       w,
					OccurredAt:   nr.OccurredAt,
					CitedUnitIDs: nr.CitedUnitIDs,
					Degree:       deg,
				},
				Summary:            nr.QueryText,
				RawPlaintext:       "Q: " + nr.QueryText + "\nA: " + nr.AnswerText,
				DistilledPlaintext: nr.AnswerText,
			})
			unitCnt++
			counts["noetic"]++
			if parentCellNodeID != "" {
				edges = append(edges, GraphEdge{
					Source: nodeID,
					Target: parentCellNodeID,
					Kind:   "member_of",
					Layer:  "noetic",
					Weight: w,
				})
			}
			for _, cited := range nr.CitedUnitIDs {
				edges = append(edges, GraphEdge{
					Source: nodeID,
					Target: "unit:" + cited,
					Kind:   "cites",
					Layer:  "noetic",
					Weight: w,
				})
			}
		}
	}

	return LocalResolvedGraph{
		Layers:       graphResp.Layers,
		LayerWeights: weights,
		Nodes:        resolvedNodes,
		Edges:        edges,
		Stats: GraphStats{
			TotalNodes:      len(resolvedNodes),
			TotalEdges:      len(edges),
			CellCount:       cellCnt,
			UnitCount:       unitCnt,
			LayerNodeCounts: counts,
		},
	}, nil
}

func extractLocalCellKey(addrURI string) string {
	trimmed := strings.TrimSpace(addrURI)
	if trimmed == "" {
		return ""
	}
	if parts := strings.Split(trimmed, "/"); len(parts) >= 3 {
		if idx := strings.IndexByte(parts[2], '#'); idx > 0 {
			return parts[2][:idx]
		}
		return parts[2]
	} else if idx := strings.IndexByte(trimmed, '#'); idx > 0 {
		return trimmed[:idx]
	}
	return trimmed
}

// BuildBlindGraph synthesizes a local fallback GraphResponse from `thin_vault_docs` and `thin_vault_noetic`.
func (l *LocalVaultTransport) BuildBlindGraph(_ context.Context, req BlindGraphRequest) (GraphResponse, error) {
	if l.Vault == nil || l.Vault.db == nil {
		return GraphResponse{Success: true, Version: "v1"}, nil
	}
	if err := l.Vault.verifyTransportScope(req.Scope); err != nil {
		return GraphResponse{}, err
	}
	layerFilter := make(map[string]bool, len(req.Layers))
	for _, ly := range req.Layers {
		t := strings.ToLower(strings.TrimSpace(ly))
		if t != "" {
			layerFilter[t] = true
		}
	}
	weights := map[string]float64{"background": 1.0, "episodic": 1.0, "noetic": 1.0}
	for k, v := range req.LayerWeights {
		weights[strings.ToLower(strings.TrimSpace(k))] = v
	}

	rows, err := l.Vault.db.Query(`SELECT doc_id, layer, occurred_at, address_uri, waveform_bar FROM thin_vault_docs ORDER BY occurred_at DESC`)
	if err != nil {
		return GraphResponse{}, err
	}
	defer rows.Close()

	var nodes []GraphNode
	var edges []GraphEdge
	counts := map[string]int{"background": 0, "episodic": 0, "noetic": 0}
	cellSet := make(map[string]int)

	for rows.Next() {
		var docID, layer, addrURI, waveBar string
		var occ int64
		if err := rows.Scan(&docID, &layer, &occ, &addrURI, &waveBar); err != nil {
			continue
		}
		if layer == "" {
			layer = "episodic"
		}
		if len(layerFilter) > 0 && !layerFilter[layer] {
			continue
		}
		w := weights[layer]
		if w <= 0 {
			continue
		}
		cellKey := extractLocalCellKey(addrURI)
		if cellKey == "" {
			cellKey = "C6:L1:LOCAL"
		}
		cellNodeID := "cell:" + cellKey
		if _, exists := cellSet[cellNodeID]; !exists {
			cellSet[cellNodeID] = len(nodes)
			nodes = append(nodes, GraphNode{
				ID:          cellNodeID,
				RawID:       cellKey,
				NodeType:    "cell",
				Layer:       layer,
				LayerWeight: w,
				AddressURI:  cellKey,
				Weight:      w,
				MemberCount: 1,
			})
			counts[layer]++
		} else {
			nodes[cellSet[cellNodeID]].MemberCount++
		}

		unitNodeID := "unit:" + docID
		nodes = append(nodes, GraphNode{
			ID:          unitNodeID,
			RawID:       docID,
			NodeType:    "unit",
			Layer:       layer,
			LayerWeight: w,
			ParentID:    cellNodeID,
			AddressURI:  addrURI,
			WaveformBar: waveBar,
			Weight:      w,
			OccurredAt:  occ,
		})
		counts[layer]++
		edges = append(edges, GraphEdge{
			Source: unitNodeID,
			Target: cellNodeID,
			Kind:   "member_of",
			Layer:  layer,
			Weight: w,
		})
	}

	return GraphResponse{
		Success:      true,
		Version:      "v1",
		Layers:       req.Layers,
		LayerWeights: weights,
		Nodes:        nodes,
		Edges:        edges,
		Stats: GraphStats{
			TotalNodes:      len(nodes),
			TotalEdges:      len(edges),
			CellCount:       len(cellSet),
			UnitCount:       len(nodes) - len(cellSet),
			LayerNodeCounts: counts,
		},
	}, nil
}
