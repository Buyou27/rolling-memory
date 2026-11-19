// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	core "github.com/rocareer/rolling-memory/internal/core"
)

// canonicalCorpusState is the O(1) freshness identity of the canonical store.
// docs_rev/terms_rev/meta_rev move with every trigger-backed write, so a
// signature match proves the mmap baseline still describes this corpus.
type canonicalCorpusState struct {
	docsRev   int64
	termsRev  int64
	metaRev   int64
	docCnt    int64
	termCnt   int64
	codecKey  string
	signature string
}

// canonicalCorpusState reads the revision counters and corpus counts. The
// revision row is maintained by triggers on the canonical core tables.
func (v *Vault) canonicalCorpusState(ctx context.Context) (canonicalCorpusState, bool) {
	var state canonicalCorpusState
	if v == nil || v.db == nil {
		return state, false
	}
	if err := v.db.QueryRowContext(ctx, `SELECT docs_rev, terms_rev, meta_rev FROM rm_revision WHERE id = 1`).
		Scan(&state.docsRev, &state.termsRev, &state.metaRev); err != nil {
		return state, false
	}
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rm_docs`).Scan(&state.docCnt); err != nil {
		return state, false
	}
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rm_terms`).Scan(&state.termCnt); err != nil {
		return state, false
	}
	key, err := v.episodicCodecKey()
	if err != nil {
		return state, false
	}
	state.codecKey = hex.EncodeToString(key)
	state.signature = fmt.Sprintf("v5|%d|%d|%d|%d|%d|%s",
		state.docsRev, state.termsRev, state.metaRev, state.docCnt, state.termCnt, state.codecKey)
	return state, true
}

// canonicalSnapshotDocs builds the lightweight metadata view of the canonical
// corpus. Terms are NOT loaded here: the engine opens them through the .zcsr
// baseline or Store.LoadIndex; per-unit deltas load only the changed units.
func (v *Vault) canonicalSnapshotDocs(ctx context.Context) ([]vaultDocEntry, map[string]int64, error) {
	rows, err := v.db.QueryContext(ctx, `
		SELECT d.unit_id, d.layer, d.occurred_at, d.doc_hash, d.block_hash, d.line_start, d.line_end,
		       COALESCE(m.address_uri, ''), COALESCE(m.coord64, 0), COALESCE(m.waveform_bar, ''),
		       COALESCE(m.locus, ''), COALESCE(m.ref_pointer, ''), COALESCE(m.meta_rev, 0)
		FROM rm_docs d
		LEFT JOIN rm_unit_meta m ON m.unit_id = d.unit_id
		ORDER BY d.unit_id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	docs := make([]vaultDocEntry, 0, 4096)
	revs := make(map[string]int64, 4096)
	for rows.Next() {
		var entry vaultDocEntry
		var coord64, metaRev int64
		if err := rows.Scan(&entry.docID, &entry.layer, &entry.occ, &entry.docHash, &entry.blockHash,
			&entry.lineStart, &entry.lineEnd, &entry.addrURI, &coord64, &entry.waveBar,
			&entry.locus, &entry.refPointer, &metaRev); err != nil {
			return nil, nil, err
		}
		entry.coord64 = uint64(coord64)
		entry.docLen = 1
		docs = append(docs, entry)
		revs[entry.docID] = metaRev
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return docs, revs, nil
}

// canonicalSnapshotFromDocs assembles the snapshot around a ready engine.
func canonicalSnapshotFromDocs(state canonicalCorpusState, docs []vaultDocEntry, eng *core.Engine) *vaultCorpusSnapshot {
	snap := &vaultCorpusSnapshot{
		docCnt:             state.docCnt,
		docsRev:            state.docsRev,
		episodicCodecKey:   state.codecKey,
		freshnessSignature: state.signature,
		docs:               docs,
		validEpisodicIDs:   make(map[string]struct{}, 1024),
		docIdxByID:         make(map[string]int, len(docs)),
		coreEngine:         eng,
	}
	for i := range docs {
		snap.docIdxByID[docs[i].docID] = i
		if docs[i].layer == LayerEpisodic {
			snap.validEpisodicIDs[docs[i].docID] = struct{}{}
			snap.episodicDocs = append(snap.episodicDocs, docs[i])
		}
	}
	snap.epiCnt = int64(len(snap.episodicDocs))
	snap.coreOnce.Do(func() {})
	return snap
}

// buildCanonicalSnapshotFromCSR reuses the mmap baseline when the canonical
// revision signature matches, or serves baseline + a bounded delta when only a
// few units changed. Any other drift returns false and the caller rebuilds from
// Store.LoadIndex.
func (v *Vault) buildCanonicalSnapshotFromCSR(ctx context.Context) (*vaultCorpusSnapshot, bool) {
	state, ok := v.canonicalCorpusState(ctx)
	if !ok {
		return nil, false
	}
	if state.docCnt == 0 {
		return canonicalSnapshotFromDocs(state, nil, nil), true
	}
	if v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.RLock()
		cached := v.snapRef.snap
		if cached != nil && cached.freshnessSignature == state.signature {
			v.slotCacheMu.RUnlock()
			return cached, true
		}
		v.slotCacheMu.RUnlock()
	}
	docs, revs, err := v.canonicalSnapshotDocs(ctx)
	if err != nil || len(docs) == 0 {
		return nil, false
	}
	dbPath := v.mainDBPath()
	if strings.TrimSpace(dbPath) == "" {
		eng := v.loadCanonicalEngine(ctx)
		if eng == nil {
			return nil, false
		}
		return canonicalSnapshotFromDocs(state, docs, eng), true
	}
	eng, storedLine, ok := v.acquireCachedCSREngine(dbPath)
	if !ok || eng == nil || eng.Index == nil {
		// No reusable baseline: build from the canonical store and persist it so
		// the next start opens the mmap path directly.
		built := v.loadCanonicalEngine(ctx)
		if built == nil {
			return nil, false
		}
		if csrPath, okPersist := persistSnapshotCoreCSR(dbPath, state.signature, built, v, []byte(state.codecKey)); okPersist {
			_ = core.AttachDiskClusterCSR(csrPath, built, true)
			_ = v.persistCanonicalUnitRevisions(dbPath)
			v.cacheCSREngineIdentity(csrPath, built)
		}
		return canonicalSnapshotFromDocs(state, docs, built), true
	}
	// Materialize the mmap baseline's cluster state once. OverlayDelta shares the
	// base engine's state pointer, so every later write produces a delta snapshot
	// that reuses this state instead of rebuilding the whole corpus cluster state
	// on each recall. Without this, continuous background ingestion makes every
	// snapshot pay the full build while the previous build is still in flight.
	eng.MaterializeClusterState()
	isExact := strings.TrimSpace(storedLine) == state.signature
	if isExact {
		if int64(len(docs)) != int64(eng.Index.N) {
			return nil, false
		}
		return canonicalSnapshotFromDocs(state, docs, eng), true
	}
	storedCodec := signatureCodecKey(storedLine)
	if storedCodec == "" || storedCodec != state.codecKey {
		return nil, false
	}
	base, ok := v.loadCanonicalUnitRevisions(dbPath)
	if !ok {
		return nil, false
	}
	deltaIDs := make([]string, 0, 64)
	masked := make([]string, 0, 16)
	for _, doc := range docs {
		prev, seen := base[doc.docID]
		if !seen || prev != revs[doc.docID] {
			deltaIDs = append(deltaIDs, doc.docID)
			if len(deltaIDs) > maxCSRDeltaDocs {
				return nil, false
			}
		}
	}
	for id := range base {
		if _, alive := revs[id]; !alive {
			masked = append(masked, id)
			if len(deltaIDs)+len(masked) > maxCSRDeltaDocs {
				return nil, false
			}
		}
	}
	var snapEng *core.Engine
	if len(deltaIDs) > 0 || len(masked) > 0 {
		deltaDocs, ok := v.loadCanonicalDocs(ctx, deltaIDs)
		if !ok {
			return nil, false
		}
		snapEng = eng.OverlayDelta(deltaDocs, masked)
	} else {
		snapEng = eng
	}
	return canonicalSnapshotFromDocs(state, docs, snapEng), true
}

// canonicalSignature returns the current v5 freshness line when the canonical
// store is populated; empty means the legacy thin fallback is still in charge.
func (v *Vault) canonicalSignature() string {
	state, ok := v.canonicalCorpusState(context.Background())
	if !ok || state.docCnt == 0 {
		return ""
	}
	return state.signature
}

// loadCanonicalEngine builds the engine from the canonical store without any
// disk-cluster persistence (used for in-memory vaults and cold builds).
func (v *Vault) loadCanonicalEngine(ctx context.Context) *core.Engine {
	if v == nil || v.db == nil {
		return nil
	}
	if restore := applyColdBuildWindow(v.coldBuildLimit()); restore != nil {
		defer restore()
	}
	store, err := v.canonicalStore()
	if err != nil {
		return nil
	}
	idx, err := store.LoadIndex()
	if err != nil || idx == nil || idx.N == 0 {
		return nil
	}
	eng := core.NewEngine(idx)
	eng.MaterializeClusterState()
	return eng
}

// loadCanonicalDocs reconstructs core.Doc values from the canonical tables for
// the incremental overlay. It reads terms as stored by core.Store.Ingest; no
// packed atoms and no role inference are involved.
func (v *Vault) loadCanonicalDocs(ctx context.Context, unitIDs []string) ([]core.Doc, bool) {
	if v == nil || v.db == nil || len(unitIDs) == 0 {
		return nil, true
	}
	ids := append([]string(nil), unitIDs...)
	sort.Strings(ids)
	out := make([]core.Doc, 0, len(ids))
	const chunk = 400
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := toAnySlice(batch)

		docRows, err := v.db.QueryContext(ctx, `
			SELECT unit_id, occurred_at, narrated_at, ingested_at, author_hash, doc_hash, block_hash,
			       line_start, line_end, layer, raw_text
			FROM rm_docs WHERE unit_id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, false
		}
		byID := make(map[string]int, len(batch))
		for docRows.Next() {
			var doc core.Doc
			var author, docHash, blockHash int64
			var lineStart, lineEnd int64
			if err := docRows.Scan(&doc.UnitID, &doc.OccurredAt, &doc.NarratedAt, &doc.IngestedAt,
				&author, &docHash, &blockHash, &lineStart, &lineEnd, &doc.Layer, &doc.RawText); err != nil {
				docRows.Close()
				return nil, false
			}
			doc.AuthorHash = uint64(author)
			doc.DocHash = uint64(docHash)
			doc.BlockHash = uint64(blockHash)
			doc.LineStart = int(lineStart)
			doc.LineEnd = int(lineEnd)
			doc.Terms = make(map[uint64]core.Term, 64)
			doc.Counts = make(map[uint64]int, 64)
			doc.Positions = make(map[uint64][]core.TermPosition, 64)
			out = append(out, doc)
			byID[doc.UnitID] = len(out) - 1
		}
		if err := docRows.Err(); err != nil {
			docRows.Close()
			return nil, false
		}
		docRows.Close()

		termRows, err := v.db.QueryContext(ctx, `
			SELECT unit_id, term_hash, bits_x10, class_flags, group_hash, occurrences, pos_blob
			FROM rm_terms WHERE unit_id IN (`+placeholders+`) ORDER BY unit_id, term_hash`, args...)
		if err != nil {
			return nil, false
		}
		for termRows.Next() {
			var unitID string
			var hash, bitsX10, classFlags, groupHash, occ int64
			var posBlob []byte
			if err := termRows.Scan(&unitID, &hash, &bitsX10, &classFlags, &groupHash, &occ, &posBlob); err != nil {
				termRows.Close()
				return nil, false
			}
			doc := byID[unitID]
			if doc < 0 {
				continue
			}
			target := &out[doc]
			var positions []core.TermPosition
			if len(posBlob) > 0 {
				decoded, _, decErr := core.DecodePositionsIntoArena(posBlob, nil)
				if decErr != nil {
					termRows.Close()
					return nil, false
				}
				positions = decoded
			}
			h := uint64(hash)
			role, lowExt := core.UnpackTermRoleExt(uint8((classFlags >> 8) & 0xff))
			ext := core.TermPersistExtFlags(classFlags)
			if ext == 0 {
				ext = lowExt
			}
			term := core.Term{
				Hash:     h,
				Bits:     uint16(bitsX10),
				Flags:    uint8(classFlags),
				Role:     role,
				ExtFlags: ext,
				Group:    uint64(groupHash),
			}
			if len(positions) > 0 {
				term.Sent = clampPosU16(positions[0].Sentence + 1)
				term.Slot = clampPosU16(positions[0].Slot + 1)
			}
			target.Terms[h] = term
			if occ < 1 {
				occ = 1
			}
			target.Counts[h] = int(occ)
			if len(positions) > 0 {
				target.Positions[h] = positions
			}
		}
		if err := termRows.Err(); err != nil {
			termRows.Close()
			return nil, false
		}
		termRows.Close()

		citeRows, err := v.db.QueryContext(ctx, `
			SELECT unit_id, cited_id FROM rm_citations WHERE unit_id IN (`+placeholders+`) ORDER BY unit_id, ord`, args...)
		if err != nil {
			return nil, false
		}
		for citeRows.Next() {
			var unitID, citedID string
			if err := citeRows.Scan(&unitID, &citedID); err != nil {
				citeRows.Close()
				return nil, false
			}
			if idx, ok := byID[unitID]; ok {
				out[idx].CitedIDs = append(out[idx].CitedIDs, citedID)
			}
		}
		if err := citeRows.Err(); err != nil {
			citeRows.Close()
			return nil, false
		}
		citeRows.Close()
	}
	// Preserve the caller's order so OverlayDelta sees a deterministic batch.
	ordered := make([]core.Doc, 0, len(out))
	byID := make(map[string]core.Doc, len(out))
	for _, doc := range out {
		byID[doc.UnitID] = doc
	}
	for _, id := range ids {
		if doc, ok := byID[id]; ok {
			ordered = append(ordered, doc)
		}
	}
	return ordered, true
}

// cacheCSREngineIdentity records the freshly persisted baseline on the shared
// snapshot ref so later calls reuse the same mmap handle.
func (v *Vault) cacheCSREngineIdentity(csrPath string, eng *core.Engine) {
	if v == nil || v.slotCacheMu == nil || v.snapRef == nil {
		return
	}
	st, err := os.Stat(csrPath)
	if err != nil || st.Size() == 0 {
		return
	}
	v.slotCacheMu.Lock()
	v.snapRef.csrEngine = eng
	v.snapRef.csrFileMTimeNS = st.ModTime().UnixNano()
	v.snapRef.csrFileSize = st.Size()
	v.slotCacheMu.Unlock()
}

func clampPosU16(value int) uint16 {
	if value <= 0 {
		return 0
	}
	if value > 65535 {
		return 65535
	}
	return uint16(value)
}

// signatureCodecKey returns the codec-key field of a v5 freshness signature.
func signatureCodecKey(line string) string {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) != 7 || parts[0] != "v5" {
		return ""
	}
	return parts[6]
}

// persistCanonicalUnitRevisions writes the unit -> meta_rev sidecar used to
// bound the delta set on the next start.
func (v *Vault) persistCanonicalUnitRevisions(dbPath string) error {
	if v == nil || v.db == nil || strings.TrimSpace(dbPath) == "" {
		return nil
	}
	rows, err := v.db.Query(`SELECT unit_id, meta_rev FROM rm_unit_meta ORDER BY unit_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var unitID string
		var rev int64
		if err := rows.Scan(&unitID, &rev); err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s\t%d\n", unitID, rev)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	path := vaultCSRUnitPaths(dbPath)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadCanonicalUnitRevisions reads the unit -> meta_rev sidecar.
func (v *Vault) loadCanonicalUnitRevisions(dbPath string) (map[string]int64, bool) {
	raw, err := os.ReadFile(vaultCSRUnitPaths(dbPath))
	if err != nil {
		return nil, false
	}
	out := make(map[string]int64)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			return nil, false
		}
		var rev int64
		if _, err := fmt.Sscanf(parts[1], "%d", &rev); err != nil {
			return nil, false
		}
		out[parts[0]] = rev
	}
	return out, len(out) > 0
}
