// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"math"
	"os"
	"strings"
	"time"

	core "github.com/rocareer/rolling-memory/internal/core"
	"github.com/rocareer/rolling-memory/sdk/go/public/closedset"
	"github.com/rocareer/rolling-memory/sdk/go/public/shaping"
	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// CoreClusterEmbedded reports whether the full 4-step dual-cluster engine
// (`sdk/go/pulse/plain`) is compiled into this binary.
const CoreClusterEmbedded = true

// NewCoreRuntimeEngine constructs an in-memory 4-step dual-cluster engine
// from the embedded `sdk/go/pulse/plain` kernel.
func NewCoreRuntimeEngine(docs []core.Doc) *core.Engine {
	return core.NewEngine(core.BuildIndex(docs))
}

// withinNaturalEnergyEnvelope reports whether score stays within the natural 1/e
// energy envelope of peak (score >= peak / e).
func withinNaturalEnergyEnvelope(score, peak float64) bool {
	return score*math.E >= peak
}

// noeticFusionDoc projects a matched Layer-3 consolidation hit into the
// index as a pure citation carrier: identity, occurrence clock, layer tag
// and the declarative CitedIDs edge list, with zero fabricated terms. The
// kernel registers term-less units in UnitMeta, so the citation provenance
// edges (subject-cos floor + complementary anchor bridge) stay active while
// the carrier itself never competes in the lexical channel.
func noeticFusionDoc(hit QueryHit) (core.Doc, bool) {
	if hit.UnitID == "" || len(hit.CitedUnitIDs) == 0 {
		return core.Doc{}, false
	}
	return core.Doc{
		UnitID:     hit.UnitID,
		OccurredAt: hit.OccurredAt,
		Layer:      LayerNoetic,
		CitedIDs:   append([]string(nil), hit.CitedUnitIDs...),
	}, true
}

// effectiveVaultHarmonics returns the Vault's canonical harmonic configuration
// for the full-corpus core.Engine. Unless the caller explicitly disabled
// harmonics via SetCanonicalHarmonics({}) or DisableAutoHarmonic, core.Engine
// defaults to the full RB canonical companion tags and structured date beacons.
func (v *Vault) effectiveVaultHarmonics() plain.CanonicalHarmonicOptions {
	if v == nil {
		return plain.CanonicalHarmonicOptions{
			EnableStructuredDates: true,
			Expander:              plain.CanonicalCompanionTags,
		}
	}
	if v.harmOptsExplicit {
		if v.harmOpts.EnableStructuredDates && v.harmOpts.Expander == nil && !v.segOpts.DisableAutoHarmonic {
			opts := v.harmOpts
			opts.Expander = plain.CanonicalCompanionTags
			return opts
		}
		return v.harmOpts
	}
	if v.segOpts.DisableAutoHarmonic || v.segOpts.Segmenter != nil {
		return v.harmOpts
	}
	return plain.CanonicalHarmonicOptions{
		EnableStructuredDates: true,
		Expander:              plain.CanonicalCompanionTags,
	}
}

// newVaultTermExtractor constructs a canonical plain.TermExtractor bound to the
// Vault's raw 256-bit secretKey and effective harmonic options.
func (v *Vault) newVaultTermExtractor() *plain.TermExtractor {
	var te *plain.TermExtractor
	if v != nil && v.segOpts.DisableAutoHarmonic {
		te = plain.NewBaselineTermExtractor(nil)
	} else {
		te = plain.NewTermExtractor(nil)
	}
	if v != nil {
		enc := te.Encoder()
		*enc = *plain.NewBlindEncoderFromRawKey(v.secretKey)
		enc.SetCanonicalHarmonics(v.effectiveVaultHarmonics())
		// Do not forward SentenceAtomPacker to TermExtractor's underlying
		// BlindEncoder: ExtractDoc / ExtractQueryWithPositions calls
		// enc.packSentenceWords directly, only consults harmOpts, and does not
		// use SentenceAtomPacker. Only custom Segmenter is forwarded when set.
		if v.segOpts.Segmenter != nil || v.segOpts.DisableAutoHarmonic {
			enc.SetSegmenterOptions(v.segOpts)
		}
	}
	return te
}

// extractVaultCoreDoc converts a plaintext document into a canonical core.Doc
// using the Vault's TermExtractor (or custom Segmenter when explicitly configured).
func (v *Vault) extractVaultCoreDoc(te *plain.TermExtractor, docID string, occ int64, rawText string) core.Doc {
	if v != nil && v.segOpts.Segmenter != nil {
		pkt, _ := v.PackBlindPacket(docID, rawText, occ)
		terms, counts, positions := coreTermsFromPacket(pkt.Slots, pkt.SentenceFrames)
		return core.Doc{
			UnitID:     docID,
			OccurredAt: occ,
			Terms:      terms,
			Counts:     counts,
			Positions:  positions,
		}
	}
	if te == nil {
		te = v.newVaultTermExtractor()
	}
	return te.ExtractDoc(docID, occ, rawText)
}

// coreTermsFromPacket reconstructs canonical core.Term, counts, and (Sentence, Slot)
// positions from a pre-packed BlindPulsePacket (used by L2 zero-copy episodic
// index rows and L1 pre-packed background chunk blobs).
func coreTermsFromPacket(slots []BlindTokenAtom, frames [][]BlindTokenAtom) (map[uint64]core.Term, map[uint64]int, map[uint64][]core.TermPosition) {
	if len(frames) == 0 && len(slots) > 0 {
		frames = [][]BlindTokenAtom{slots}
	}
	hint := len(slots)/2 + 4
	if hint < 8 {
		hint = 8
	}
	atoms := make(map[uint64]core.Term, hint)
	counts := make(map[uint64]int, hint)
	positions := make(map[uint64][]core.TermPosition, hint)
	var docSlot int
	for sentenceAt, frame := range frames {
		var currentGroup uint64
		var currentHostBits uint16
		var pos core.TermPosition
		for _, raw := range frame {
			if raw.H == 0 {
				continue
			}
			isPrimary := raw.ClassFlags&plain.FlagIsHarmonicCompanion == 0
			if !isPrimary && raw.Role == 0 && (raw.RawBitsX10&1) == 1 &&
				(raw.ClassFlags&FlagHasConn) != 0 && (raw.ClassFlags&FlagHasDigit) == 0 &&
				currentHostBits >= raw.RawBitsX10 && (currentHostBits-raw.RawBitsX10 == 1 || currentHostBits-raw.RawBitsX10 == 3) {
				isPrimary = false
			}
			grp := currentGroup
			slotRole := raw.Role
			if isPrimary {
				slotRole = core.RolePrimary
				currentHostBits = raw.RawBitsX10
				docSlot++
				currentGroup = raw.H
				grp = currentGroup
				pos = core.TermPosition{Sentence: sentenceAt, Slot: docSlot}
			} else if slotRole == 0 {
				if raw.RawBitsX10 == plain.ISODateCarrierBitsX10 && (raw.ClassFlags&FlagHasConn) != 0 {
					slotRole = core.RoleClosedBeacon
				} else if int(currentHostBits) >= int(raw.RawBitsX10)+3 {
					slotRole = core.RoleSubwordPiece
				} else {
					slotRole = core.RoleMorphStem
				}
			}
			if grp == 0 {
				grp = raw.H
			}
			var extBit uint16
			switch {
			case isPrimary:
				extBit = core.ExtFlagSeenPrimary
			case slotRole == core.RoleSubwordPiece:
				extBit = core.ExtFlagSeenSubword
			default:
				extBit = core.ExtFlagSeenMorphOrClos
			}
			atom, seen := atoms[raw.H]
			if !seen {
				atom = core.Term{
					Hash:     raw.H,
					Role:     slotRole,
					ExtFlags: extBit,
					Group:    grp,
					Sent:     plain.ClampPosU16(sentenceAt + 1),
					Slot:     plain.ClampPosU16(pos.Slot),
				}
			} else {
				atom.ExtFlags |= extBit
				if isPrimary {
					atom.Group = raw.H
					atom.Role = core.RolePrimary
				} else if atom.Role != core.RolePrimary && slotRole < atom.Role {
					atom.Role = slotRole
				}
			}
			bits := raw.RawBitsX10
			if bits == 0 {
				bits = 32
			}
			if bits > atom.Bits {
				atom.Bits = bits
			}
			atom.Flags |= raw.ClassFlags
			atoms[raw.H] = atom
			prev := positions[raw.H]
			if len(prev) == 0 || prev[len(prev)-1] != pos {
				positions[raw.H] = append(prev, pos)
				counts[raw.H]++
			}
		}
	}
	return atoms, counts, positions
}

// ensureSnapshotCoreEngine lazily builds (and caches on the immutable snapshot)
// the full-corpus core.Engine over all snapshot documents.
//
// 冷启动路径：优先按语料签名复用同目录的 .zcsr 磁盘簇（mmap 零拷贝，毫秒级）；
// 缺失/失效时全量构建一次并把结果原子写回磁盘簇，下一次（含后续进程/守护进程重启）
// 即走零拷贝路径。dbPath 为空（内存库等）时不做磁盘缓存，行为与历史一致。
func (s *vaultCorpusSnapshot) ensureSnapshotCoreEngine(v *Vault) *core.Engine {
	if s == nil || len(s.docs) == 0 {
		return nil
	}
	var dbPath string
	if v != nil {
		dbPath = v.mainDBPath()
	}
	s.coreOnce.Do(func() {
		signature := s.freshnessLine()
		if eng, ok := openSnapshotCoreEngineFromCSR(dbPath, signature); ok {
			s.coreEngine = eng
			if len(s.docs) >= 2048 {
				s.releaseTransientDocTermMaps(v)
			}
			return
		}
		rmDocs := make([]core.Doc, len(s.docs))
		for i := range s.docs {
			d := &s.docs[i]
			occ := d.occ
			if d.layer == LayerBackground {
				// L1 background chunks are static document slices rather than temporal
				// dialogue events; their file mtime must not enter SpacetimeCluster
				// exact-timestamp buckets (where git checkouts or multi-chunk files
				// collapse thousands of chunks onto one timestamp and trigger O(B^2)
				// neighbor edges). Same-document slices couple via DocHash + LineStart..LineEnd.
				occ = 0
			}
			rmDocs[i] = core.Doc{
				UnitID:     d.docID,
				OccurredAt: occ,
				Layer:      d.layer,
				DocHash:    d.docHash,
				BlockHash:  d.blockHash,
				LineStart:  d.lineStart,
				LineEnd:    d.lineEnd,
				Terms:      d.termMeta,
				Counts:     d.termCounts,
				Positions:  d.termPositions,
			}
		}
		eng := core.NewEngine(core.BuildIndex(rmDocs))
		rmDocs = nil
		eng.MaterializeClusterState()
		// 构建后按核心侧生产配方落盘并 mmap 挂载磁盘簇、释放堆过渡 Map：
		// 常驻内存从「堆索引 + 簇状态」降到「mmap 页缓存 + 极小堆壳」，
		// 查询改经 CSR 切片读取（与 RB 服务端同一条路径），下一次进程/守护进程重启
		// 直接零拷贝复用。落盘或挂载失败时保留纯堆形态，行为与历史一致。
		if csrPath, ok := persistSnapshotCoreCSR(dbPath, signature, eng, v, []byte(s.episodicCodecKey)); ok {
			_ = core.AttachDiskClusterCSR(csrPath, eng, true)
			if v != nil && v.slotCacheMu != nil && v.snapRef != nil {
				if st, sErr := os.Stat(csrPath); sErr == nil && st.Size() > 0 {
					v.slotCacheMu.Lock()
					v.snapRef.csrEngine = eng
					v.snapRef.csrFileMTimeNS = st.ModTime().UnixNano()
					v.snapRef.csrFileSize = st.Size()
					v.slotCacheMu.Unlock()
				}
			}
		}
		if len(s.docs) >= 2048 {
			s.releaseTransientDocTermMaps(v)
		}
		s.coreEngine = eng
	})
	return s.coreEngine
}

// releaseTransientDocTermMaps drops the per-document Go maps (`termMeta`,
// `termPositions`, and `termCounts`) from large snapshots and component caches
// once `core.Engine` has built its flat CSR arrays, eliminating multi-gigabyte
// duplicate heap residency (`DocAtomCoverageWithCueHashes` reads directly from
// `core.Engine` CSR entries via `ForEachDocEntry`).
func (s *vaultCorpusSnapshot) releaseTransientDocTermMaps(v *Vault) {
	if s == nil {
		return
	}
	for i := range s.docs {
		d := &s.docs[i]
		d.termMeta = nil
		d.termPositions = nil
		d.termCounts = nil
		d.allHashes = nil
	}
	for i := range s.episodicDocs {
		ed := &s.episodicDocs[i]
		ed.termMeta = nil
		ed.termPositions = nil
		ed.termCounts = nil
		ed.allHashes = nil
	}
	if v != nil && v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.Lock()
		for k, entry := range v.slotCache {
			if len(entry.termMeta) > 0 || len(entry.termPositions) > 0 {
				entry.termMeta = nil
				entry.termPositions = nil
				entry.termCounts = nil
				v.slotCache[k] = entry
			}
		}
		v.slotCacheMu.Unlock()
	}
}

// extractQueryFromBlindPacket reconstructs query terms and positions when a
// caller invokes LocalVaultTransport.QueryBlindManifold with a pre-packed
// BlindPulsePacket instead of raw query text.
func (v *Vault) extractQueryFromBlindPacket(idx *core.Index, pkt BlindPulsePacket) (map[uint64]core.Term, map[uint64][]core.TermPosition) {
	qTerms, _, qPos := coreTermsFromPacket(pkt.Slots, pkt.SentenceFrames)
	if len(qTerms) == 0 || idx == nil || idx.N == 0 {
		return qTerms, qPos
	}
	// Apply the same clock-header de-rigidification on packet-only queries.
	if clockExcl := localQueryClockHeaderExclusiveHashes(pkt); len(clockExcl) > 0 {
		for h := range clockExcl {
			if tm, ok := qTerms[h]; ok {
				tm.Bits = 18
				tm.Flags &^= FlagHasDigit | FlagHasUpper | FlagHasCamel
				qTerms[h] = tm
			}
		}
	}
	return qTerms, qPos
}

// Unused import guards to keep closedset/shaping available when needed.
var (
	_ = strings.TrimSpace
	_ = time.Unix
	_ = closedset.HasToken
	_ = shaping.CanonicalizeApostrophe
)
