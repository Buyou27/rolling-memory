// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"math"
	"os"
	"runtime/debug"
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
	if te == nil {
		te = v.newVaultTermExtractor()
	}
	return te.ExtractDoc(docID, occ, rawText)
}

// coreTermsFromPacket remains only for the legacy thin-table codec that the
// canonical store replaced; it returns no terms and is never on a live path.
func coreTermsFromPacket(_ []BlindTokenAtom, _ [][]BlindTokenAtom) (map[uint64]core.Term, map[uint64]int, map[uint64][]core.TermPosition) {
	return map[uint64]core.Term{}, map[uint64]int{}, map[uint64][]core.TermPosition{}
}

// applyColdBuildWindow raises the runtime soft memory limit to the given build
// window and returns the function that restores the previous limit (and returns
// cached pages). A zero window, or one no larger than the current limit, leaves
// the runtime untouched and returns nil.
func applyColdBuildWindow(limit int64) func() {
	if limit <= 0 {
		return nil
	}
	previous := debug.SetMemoryLimit(-1)
	if previous >= limit {
		return nil
	}
	debug.SetMemoryLimit(limit)
	return func() {
		debug.SetMemoryLimit(previous)
		debug.FreeOSMemory()
	}
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
		if v != nil {
			if canonical := v.canonicalSignature(); canonical != "" {
				signature = canonical
			}
		}
		if eng, ok := openSnapshotCoreEngineFromCSR(dbPath, signature); ok {
			s.coreEngine = eng
			return
		}
		// 冷启动全量构建的活集与语料规模同阶，可数倍于守护进程的稳态软上限；在上限内构建
		// 会退化为 GC 空转而无法完成。此处按调用方给的构建窗口临时抬高上限，构建与落盘
		// 结束后恢复稳态上限并归还页缓存。
		if restore := applyColdBuildWindow(v.coldBuildLimit()); restore != nil {
			defer restore()
		}
		if v == nil {
			return
		}
		// 全量构建直接以规范存储为源：Store.LoadIndex 在单事务快照内重建倒排，
		// 词项只来自 rm_terms，不再从打包原子重建或推断角色。
		store, err := v.canonicalStore()
		if err != nil {
			return
		}
		idx, err := store.LoadIndex()
		if err != nil || idx == nil || idx.N == 0 {
			return
		}
		eng := core.NewEngine(idx)
		eng.MaterializeClusterState()
		// 构建后按核心侧生产配方落盘并 mmap 挂载磁盘簇、释放堆过渡 Map：
		// 常驻内存从「堆索引 + 簇状态」降到「mmap 页缓存 + 极小堆壳」，
		// 查询改经 CSR 切片读取（与 RB 服务端同一条路径），下一次进程/守护进程重启
		// 直接零拷贝复用。落盘或挂载失败时保留纯堆形态，行为与历史一致。
		if csrPath, ok := persistSnapshotCoreCSR(dbPath, signature, eng, v, []byte(s.episodicCodecKey)); ok {
			_ = core.AttachDiskClusterCSR(csrPath, eng, true)
			_ = v.persistCanonicalUnitRevisions(dbPath)
			if v.slotCacheMu != nil && v.snapRef != nil {
				if st, sErr := os.Stat(csrPath); sErr == nil && st.Size() > 0 {
					v.slotCacheMu.Lock()
					v.snapRef.csrEngine = eng
					v.snapRef.csrFileMTimeNS = st.ModTime().UnixNano()
					v.snapRef.csrFileSize = st.Size()
					v.slotCacheMu.Unlock()
				}
			}
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
	text := strings.TrimSpace(pkt.RawText)
	if text == "" {
		return nil, nil
	}
	qTerms, _, qPos := v.newVaultTermExtractor().ExtractQueryWithPositions(idx, text)
	return qTerms, qPos
}

// Unused import guards to keep closedset/shaping available when needed.
var (
	_ = strings.TrimSpace
	_ = time.Unix
	_ = closedset.HasToken
	_ = shaping.CanonicalizeApostrophe
)
