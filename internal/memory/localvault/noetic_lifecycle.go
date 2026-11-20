// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/core"
	"github.com/rocareer/rolling-memory/internal/ids"
	"github.com/rocareer/rolling-memory/internal/text"
)

// L3 Noetic identity and activation. A Noetic row is the agent's own
// recollection: the evidence set it cites is the trace, and each cue is one
// recall handle of that trace. Recall therefore changes memory by construction:
// a trace that takes part in a delivery gains one activation, and a later cue
// for the same evidence set merges into the same trace instead of creating a
// parallel row. There is no decay model: the state only records use, and the
// existing continuous gain (1 + ln(1 + activations)) carries it into ranking.
const (
	// noeticMaxCueVariants bounds the phrasings one trace keeps as recall
	// handles. The primary cue is always stored separately; this cap bounds the
	// scan and packet cache per trace.
	noeticMaxCueVariants = 4
	// noeticTraceBackfillKey marks the one-time trace-key backfill so routine
	// opens never scan rm_noetic for legacy rows.
	noeticTraceBackfillKey = "noetic_trace_backfill_v1"
)

// NoeticTraceKey returns the stable identity of the evidence set a Noetic
// cites. Order and duplicates never change it: the same evidence means the same
// recollection, regardless of which cue retrieved it.
func NoeticTraceKey(citedUnitIDs []string) string {
	clean := make([]string, 0, len(citedUnitIDs))
	seen := make(map[string]bool, len(citedUnitIDs))
	for _, id := range citedUnitIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		// Chained Noetic ids are citation context, not evidence; they must not
		// change the identity of the evidence trace.
		if strings.HasPrefix(id, "nt_") || strings.HasPrefix(id, "noetic_") {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return ""
	}
	sort.Strings(clean)
	return ids.SHA256Hex("noetic-trace\x00" + strings.Join(clean, "\x00"))[:16]
}

// NoeticTraceID is the row id derived from a trace key.
func NoeticTraceID(traceKey string) string {
	traceKey = strings.TrimSpace(traceKey)
	if traceKey == "" {
		return ""
	}
	return "nt_" + traceKey
}

// ActivationGain is the continuous reinforcement carried by one recollection.
// It reuses the kernel's logarithmic law; there is no time term and no
// dormancy, so a recollection never fades merely because it is old.
func (r LocalNoeticRecord) ActivationGain() float64 {
	activations := r.ActivationCount
	if r.ReuseCount > activations {
		activations = r.ReuseCount
	}
	return core.VerificationGain(activations)
}

// CueTexts returns the recall handles of this trace: the primary cue first,
// then the retained variants.
func (r LocalNoeticRecord) CueTexts() []string {
	return noeticCueSet(r.QueryText, r.QueryVariants)
}

func noeticCueSet(primary string, variants []string) []string {
	out := make([]string, 0, 1+len(variants))
	seen := make(map[string]bool, 1+len(variants))
	for _, cue := range append([]string{primary}, variants...) {
		cue = strings.TrimSpace(cue)
		if cue == "" || seen[cue] {
			continue
		}
		seen[cue] = true
		out = append(out, cue)
	}
	return out
}

// mergeNoeticCueVariants folds a new cue and any explicit variants into the
// handle set of one trace. The newest cue stays primary; the previous primary
// becomes the first variant. The oldest phrasings fall off beyond the cap.
func mergeNoeticCueVariants(existingPrimary string, existingVariants []string, newPrimary string, newVariants []string) []string {
	combined := make([]string, 0, 2+len(existingVariants)+len(newVariants))
	combined = append(combined, newPrimary)
	combined = append(combined, newVariants...)
	combined = append(combined, existingPrimary)
	combined = append(combined, existingVariants...)

	primary := strings.TrimSpace(newPrimary)
	variants := make([]string, 0, noeticMaxCueVariants)
	seen := make(map[string]bool, len(combined))
	for _, cue := range combined {
		cue = strings.TrimSpace(cue)
		if cue == "" || cue == primary || seen[cue] {
			continue
		}
		seen[cue] = true
		variants = append(variants, cue)
		if len(variants) >= noeticMaxCueVariants {
			break
		}
	}
	return variants
}

func noeticCueSetsIntersect(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(y)) {
				return true
			}
		}
	}
	return false
}

// uniqueAtomHashes returns the distinct blind atom hashes of a packet.
func uniqueAtomHashes(slots []BlindTokenAtom) []uint64 {
	seen := make(map[uint64]struct{}, len(slots))
	out := make([]uint64, 0, len(slots))
	for _, atom := range slots {
		if atom.H == 0 {
			continue
		}
		if _, ok := seen[atom.H]; ok {
			continue
		}
		seen[atom.H] = struct{}{}
		out = append(out, atom.H)
	}
	return out
}

// noeticEvidenceCoverageByHashes reports the fraction of unique cue atoms
// covered by the union of the cited units' atoms. Coverage is the physical
// reason a recollection may lift its evidence: the cited set is what jointly
// answered the cue, and a set covering more of the cue deserves more weight.
func (v *Vault) noeticEvidenceCoverageByHashes(citedUnitIDs []string, cueHashes []uint64) float64 {
	if v == nil || len(citedUnitIDs) == 0 || len(cueHashes) == 0 {
		return 0
	}
	want := make(map[uint64]struct{}, len(cueHashes))
	for _, h := range cueHashes {
		if h != 0 {
			want[h] = struct{}{}
		}
	}
	if len(want) == 0 {
		return 0
	}
	covered := make(map[uint64]struct{}, len(want))
	for _, id := range citedUnitIDs {
		id = strings.TrimSpace(id)
		if id == "" || strings.HasPrefix(id, "nt_") || strings.HasPrefix(id, "noetic_") {
			continue
		}
		for _, h := range v.docCoverageHashes(id) {
			if _, ok := want[h]; ok {
				covered[h] = struct{}{}
			}
		}
		if len(covered) == len(want) {
			break
		}
	}
	return float64(len(covered)) / float64(len(want))
}

// docCoverageHashes returns a document's packed atom hashes, preferring the
// slot cache so repeated coverage checks for the same evidence do not query
// SQLite again.
func (v *Vault) docCoverageHashes(docID string) []uint64 {
	if v == nil || docID == "" {
		return nil
	}
	if v.slotCacheMu != nil {
		v.slotCacheMu.RLock()
		entry, ok := v.slotCache[docID]
		v.slotCacheMu.RUnlock()
		if ok && entry.allHashes != nil {
			return entry.allHashes
		}
	}
	raw, _, _, err := v.LoadLocalDocument(docID)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	return v.docSlotHashes(docID, raw)
}

// NoeticEvidenceCoverage is the public wrapper used by the delivery layer to
// apply the same coverage floor the kernel merge uses.
func (v *Vault) NoeticEvidenceCoverage(citedUnitIDs []string, query string) float64 {
	if v == nil || len(citedUnitIDs) == 0 || strings.TrimSpace(query) == "" {
		return 0
	}
	pkt, _ := v.PackBlindPacket("Q-NOETIC-COVERAGE", query, 0)
	return v.noeticEvidenceCoverageByHashes(citedUnitIDs, uniqueAtomHashes(pkt.Slots))
}

// buildNoeticCueIndex materializes the cue inverted index: blind atom hash ->
// trace positions. A query only scores traces sharing at least one cue atom,
// so keeping several handles per trace does not turn every recall into a full
// cross product.
func buildNoeticCueIndex(v *Vault, records []LocalNoeticRecord) map[uint64][]int32 {
	if v == nil || len(records) == 0 {
		return nil
	}
	index := make(map[uint64][]int32, len(records)*4)
	seen := make(map[uint64]struct{}, 32)
	for i := range records {
		clear(seen)
		for _, cue := range records[i].CueTexts() {
			// Keep the packed cue in the slot cache: a noetic reload after every
			// crystallization would otherwise re-run BPE for every handle.
			pkt := v.noeticBlindPacket(records[i].NoeticID, cue, records[i].OccurredAt)
			for _, atom := range pkt.Slots {
				if atom.H == 0 {
					continue
				}
				if _, ok := seen[atom.H]; ok {
					continue
				}
				seen[atom.H] = struct{}{}
				index[atom.H] = append(index[atom.H], int32(i))
			}
		}
	}
	return index
}

// noeticCacheSnapshot returns the cached record set and its cue index from the
// same generation. Callers must treat the slice elements as immutable; writes
// replace the slice (copy-on-write) instead of mutating it.
func (v *Vault) noeticCacheSnapshot() ([]LocalNoeticRecord, map[uint64][]int32, bool) {
	if v == nil || v.slotCacheMu == nil || v.snapRef == nil {
		return nil, nil, false
	}
	v.slotCacheMu.RLock()
	defer v.slotCacheMu.RUnlock()
	if !v.snapRef.noeticReady {
		return nil, nil, false
	}
	return v.snapRef.activeNoetics, v.snapRef.noeticCueIndex, true
}

// noeticCitationSetsKey hashes the active traces' evidence sets. Re-crystallizing
// the same evidence (or merging a cue handle) leaves the key unchanged, so the
// derived co-citation view is only rebuilt when the declared pairs change. The
// per-trace hashes are summed, making the key independent of row order.
func noeticCitationSetsKey(records []LocalNoeticRecord) string {
	var acc uint64
	seen := make(map[string]bool, len(records))
	for _, rec := range records {
		if rec.TraceKey == "" || seen[rec.TraceKey] {
			continue
		}
		seen[rec.TraceKey] = true
		acc += fnv1a64(rec.TraceKey)
	}
	if acc == 0 && len(seen) == 0 {
		return ""
	}
	return strconv.FormatUint(acc, 16)
}

// fnv1a64 is the standard FNV-1a hash, used only to identify one evidence set.
func fnv1a64(value string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(value); i++ {
		h ^= uint64(value[i])
		h *= prime64
	}
	return h
}

// isWarmupProbeCue reports whether a cue is an internal daemon warmup probe.
// Probes are infrastructure, not recollections: they must never share an
// evidence trace with a real recollection, or retiring the probe would retire
// the recollection that cites the same evidence.
func isWarmupProbeCue(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	return strings.HasPrefix(q, "daemon warmup") || strings.HasPrefix(q, "daemon-warmup")
}

// SaveLocalNoeticTrace persists a Noetic recollection and returns the id of the
// row that owns the evidence set. A cue that cites an already-known evidence
// set merges into that trace and adds a handle; a different evidence set for a
// cue the vault already answered supersedes the older trace, so a streaming
// workload cannot keep two competing recollections for the same question.
func (v *Vault) SaveLocalNoeticTrace(rec LocalNoeticRecord) (string, error) {
	if strings.TrimSpace(rec.NoeticID) == "" {
		return "", errors.New("localvault: noetic_id is required")
	}
	if strings.TrimSpace(rec.QueryText) == "" && strings.TrimSpace(rec.AnswerText) == "" {
		return "", errors.New("localvault: query_text or answer_text is required")
	}
	status := strings.TrimSpace(rec.Status)
	if status == "" {
		status = "active"
	}
	// If CitedSentences is empty but CitedUnitIDs are provided, automatically
	// hydrate distilled sentences from rm_units.
	citedSents := rec.CitedSentences
	if len(citedSents) == 0 && len(rec.CitedUnitIDs) > 0 {
		for _, uid := range rec.CitedUnitIDs {
			_, distTxt, _, err := v.LoadLocalDocument(uid)
			if err == nil && strings.TrimSpace(distTxt) != "" {
				citedSents = append(citedSents, distTxt)
			}
		}
	}
	unitIDsJSON, err := json.Marshal(rec.CitedUnitIDs)
	if err != nil {
		return "", err
	}
	sentsJSON, err := json.Marshal(citedSents)
	if err != nil {
		return "", err
	}
	insertAt := rec.InsertedAt
	if insertAt <= 0 {
		insertAt = rec.OccurredAt
	}
	activatedAt := rec.OccurredAt
	if activatedAt <= 0 {
		activatedAt = insertAt
	}
	traceKey := NoeticTraceKey(rec.CitedUnitIDs)
	probe := isWarmupProbeCue(rec.QueryText)
	if probe {
		// A probe keeps its own row so the prune can retire it without touching
		// the recollection trace that cites the same evidence.
		traceKey = "probe:" + rec.NoeticID
	}

	tx, err := v.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	targetID := rec.NoeticID
	variants := mergeNoeticCueVariants("", nil, rec.QueryText, rec.QueryVariants)
	if traceKey != "" && !probe {
		var foundID, foundPrimary, foundVariantsJSON string
		err := tx.QueryRow(`
			SELECT noetic_id, query_text, COALESCE(query_variants_json, '[]')
			  FROM rm_noetic
			 WHERE trace_key = ?
			 ORDER BY CASE WHEN COALESCE(NULLIF(status, ''), 'active') = 'active' THEN 0 ELSE 1 END,
			          occurred_at DESC
			 LIMIT 1`, traceKey).Scan(&foundID, &foundPrimary, &foundVariantsJSON)
		switch {
		case err == nil:
			var foundVariants []string
			_ = json.Unmarshal([]byte(foundVariantsJSON), &foundVariants)
			targetID = foundID
			variants = mergeNoeticCueVariants(foundPrimary, foundVariants, rec.QueryText, rec.QueryVariants)
		case errors.Is(err, sql.ErrNoRows):
		default:
			return "", err
		}
	}
	variantsJSON, err := json.Marshal(variants)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`
				INSERT INTO rm_noetic(
					noetic_id, occurred_at, inserted_at, status, query_text, answer_text, cited_unit_ids_json, cited_sentences_json,
					address_uri, coord64, waveform_bar, assistant_id, incarnation_id,
					activated_at, activation_count, trace_key, query_variants_json, superseded_by
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, '')
				ON CONFLICT(noetic_id) DO UPDATE SET
					occurred_at = CASE
						WHEN excluded.occurred_at > rm_noetic.occurred_at THEN excluded.occurred_at
						WHEN rm_noetic.occurred_at > 0 THEN rm_noetic.occurred_at + 1
						ELSE excluded.occurred_at
					END,
					inserted_at = CASE WHEN rm_noetic.inserted_at > 0 THEN rm_noetic.inserted_at ELSE excluded.inserted_at END,
					status = excluded.status,
					query_text = excluded.query_text,
					answer_text = excluded.answer_text,
					cited_unit_ids_json = excluded.cited_unit_ids_json,
					cited_sentences_json = excluded.cited_sentences_json,
					address_uri = excluded.address_uri,
					coord64 = excluded.coord64,
					waveform_bar = excluded.waveform_bar,
					assistant_id = excluded.assistant_id,
					incarnation_id = excluded.incarnation_id,
					activated_at = MAX(COALESCE(rm_noetic.activated_at, 0), excluded.activated_at),
					activation_count = COALESCE(rm_noetic.activation_count, 0) + 1,
					trace_key = excluded.trace_key,
					query_variants_json = excluded.query_variants_json,
					superseded_by = ''`,
		targetID, rec.OccurredAt, insertAt, status, rec.QueryText, rec.AnswerText, string(unitIDsJSON), string(sentsJSON),
		rec.AddressURI, rec.Coord64, rec.WaveformBar,
		text.FirstTrimmedNonEmpty(rec.AssistantID, v.identity.AssistantID), text.FirstTrimmedNonEmpty(rec.IncarnationID, v.identity.IncarnationID),
		activatedAt, traceKey, string(variantsJSON),
	); err != nil {
		return "", err
	}
	if traceKey != "" && !probe {
		if err := supersedeNoeticPeersTx(tx, targetID, traceKey, noeticCueSet(rec.QueryText, variants)); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.Lock()
		v.snapRef.noeticReady = false
		v.slotCacheMu.Unlock()
	}
	return targetID, nil
}

// SaveLocalNoetic persists an L3 Noetic record into the local plaintext table.
// It is the error-only wrapper around SaveLocalNoeticTrace for callers that do
// not need the resolved trace id.
func (v *Vault) SaveLocalNoetic(rec LocalNoeticRecord) error {
	_, err := v.SaveLocalNoeticTrace(rec)
	return err
}

// supersedeNoeticPeersTx retires older evidence traces that answer the same
// cue. Only traces with a different evidence set are touched; superseded rows
// stay in the vault for audit and can be revived by a later crystallization.
func supersedeNoeticPeersTx(tx *sql.Tx, targetID, traceKey string, cues []string) error {
	if tx == nil || targetID == "" || traceKey == "" || len(cues) == 0 {
		return nil
	}
	rows, err := tx.Query(`
		SELECT noetic_id, query_text, COALESCE(query_variants_json, '[]'), COALESCE(trace_key, '')
		  FROM rm_noetic
		 WHERE COALESCE(NULLIF(status, ''), 'active') = 'active'
		   AND noetic_id <> ?`, targetID)
	if err != nil {
		return err
	}
	var retired []string
	for rows.Next() {
		var id, primary, variantsJSON, rowTraceKey string
		if err := rows.Scan(&id, &primary, &variantsJSON, &rowTraceKey); err != nil {
			_ = rows.Close()
			return err
		}
		if rowTraceKey == "" || rowTraceKey == traceKey {
			continue
		}
		var variants []string
		_ = json.Unmarshal([]byte(variantsJSON), &variants)
		if noeticCueSetsIntersect(cues, noeticCueSet(primary, variants)) {
			retired = append(retired, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, id := range retired {
		if _, err := tx.Exec(`UPDATE rm_noetic SET status = 'superseded', superseded_by = ? WHERE noetic_id = ?`, targetID, id); err != nil {
			return err
		}
	}
	return nil
}

// ActivateNoetics records one real use of a recollection: its handle band or
// one of its cited units survived the delivery caps. Matching alone does not
// count, so repeating an unhelpful question cannot ratchet a recollection.
func (v *Vault) ActivateNoetics(noeticIDs []string, now int64) (int, error) {
	if v == nil || v.db == nil || len(noeticIDs) == 0 {
		return 0, nil
	}
	if now <= 0 {
		now = clock.Unix()
	}
	ids := make([]string, 0, len(noeticIDs))
	seen := make(map[string]bool, len(noeticIDs))
	for _, id := range noeticIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, now)
	for _, id := range ids {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	tx, err := v.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`
		UPDATE rm_noetic
		   SET activated_at = ?, activation_count = COALESCE(activation_count, 0) + 1
		 WHERE noetic_id IN (`+placeholders+`)
		   AND COALESCE(NULLIF(status, ''), 'active') = 'active'`, args...)
	if err != nil {
		return 0, err
	}
	activated, _ := res.RowsAffected()
	if activated == 0 {
		return 0, nil
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if v.slotCacheMu != nil && v.snapRef != nil {
		activatedSet := make(map[string]bool, len(ids))
		for _, id := range ids {
			activatedSet[id] = true
		}
		v.slotCacheMu.Lock()
		if v.snapRef.noeticReady && len(v.snapRef.activeNoetics) > 0 {
			// Copy-on-write: readers keep the slice they already hold, and the
			// cue index stays valid because positions and ids do not change.
			updated := make([]LocalNoeticRecord, len(v.snapRef.activeNoetics))
			copy(updated, v.snapRef.activeNoetics)
			for i := range updated {
				if !activatedSet[updated[i].NoeticID] {
					continue
				}
				if updated[i].ActivatedAt >= now {
					// A reload already picked up this activation; do not count it
					// twice in the cache.
					continue
				}
				updated[i].ActivationCount++
				updated[i].ActivatedAt = now
			}
			v.snapRef.activeNoetics = updated
		}
		v.slotCacheMu.Unlock()
	}
	return int(activated), nil
}

// ensureNoeticTraceColumns idempotently adds the identity/activation columns
// and backfills legacy trace keys once. Column probes are O(1) sqlite_master
// reads, so steady-state opens take no write lock.
func ensureNoeticTraceColumns(db *sql.DB) {
	if db == nil {
		return
	}
	var hasTrace, hasVariants, hasSuperseded, hasActivated, hasCount int
	_ = db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN type = 'table' AND name = 'rm_noetic' AND sql LIKE '%trace_key%' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'table' AND name = 'rm_noetic' AND sql LIKE '%query_variants_json%' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'table' AND name = 'rm_noetic' AND sql LIKE '%superseded_by%' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'table' AND name = 'rm_noetic' AND sql LIKE '%activated_at%' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'table' AND name = 'rm_noetic' AND sql LIKE '%activation_count%' THEN 1 ELSE 0 END), 0)
		FROM sqlite_master
	`).Scan(&hasTrace, &hasVariants, &hasSuperseded, &hasActivated, &hasCount)
	if hasActivated == 0 {
		_, _ = db.Exec(`ALTER TABLE rm_noetic ADD COLUMN activated_at INTEGER NOT NULL DEFAULT 0`)
	}
	if hasCount == 0 {
		_, _ = db.Exec(`ALTER TABLE rm_noetic ADD COLUMN activation_count INTEGER NOT NULL DEFAULT 0`)
	}
	if hasTrace == 0 {
		_, _ = db.Exec(`ALTER TABLE rm_noetic ADD COLUMN trace_key TEXT NOT NULL DEFAULT ''`)
	}
	if hasVariants == 0 {
		_, _ = db.Exec(`ALTER TABLE rm_noetic ADD COLUMN query_variants_json TEXT NOT NULL DEFAULT '[]'`)
	}
	if hasSuperseded == 0 {
		_, _ = db.Exec(`ALTER TABLE rm_noetic ADD COLUMN superseded_by TEXT NOT NULL DEFAULT ''`)
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_rm_noetic_trace ON rm_noetic(trace_key)`)
	// The decay model was retired before release; drop its two bookkeeping keys
	// so no reader mistakes them for live state.
	_, _ = db.Exec(`DELETE FROM rm_meta WHERE k IN ('noetic_activation_interval_max_s', 'noetic_last_crystallization_at')`)
	backfillNoeticTraceKeys(db)
}

func backfillNoeticTraceKeys(db *sql.DB) {
	if db == nil {
		return
	}
	var marker int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rm_meta WHERE k = ?`, noeticTraceBackfillKey).Scan(&marker); err == nil && marker > 0 {
		return
	}
	type pendingTrace struct {
		id       string
		traceKey string
	}
	var pending []pendingTrace
	rows, err := db.Query(`SELECT noetic_id, COALESCE(cited_unit_ids_json, '[]') FROM rm_noetic WHERE COALESCE(trace_key, '') = ''`)
	if err != nil {
		return
	}
	for rows.Next() {
		var id, citedJSON string
		if err := rows.Scan(&id, &citedJSON); err != nil {
			_ = rows.Close()
			return
		}
		var cited []string
		if json.Unmarshal([]byte(citedJSON), &cited) != nil {
			continue
		}
		if key := NoeticTraceKey(cited); key != "" {
			pending = append(pending, pendingTrace{id: id, traceKey: key})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return
	}
	_ = rows.Close()

	tx, err := db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`UPDATE rm_noetic SET trace_key = ? WHERE noetic_id = ? AND COALESCE(trace_key, '') = ''`)
	if err != nil {
		return
	}
	for _, item := range pending {
		_, _ = stmt.Exec(item.traceKey, item.id)
	}
	_ = stmt.Close()
	if _, err := tx.Exec(`INSERT OR REPLACE INTO rm_meta(k, v) VALUES (?, ?)`, noeticTraceBackfillKey, "1"); err != nil {
		return
	}
	_ = tx.Commit()
}
