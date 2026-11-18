// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rocareer/rolling-memory/internal/episodic"
	"github.com/rocareer/rolling-memory/internal/episodicunit"
	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

const episodicCodecVersion = "episodic-rmbp-v1"

const episodicIndexDDL = `CREATE TABLE IF NOT EXISTS thin_vault_episodic_index (
 doc_id TEXT NOT NULL, ref_pointer TEXT NOT NULL, codec_key BLOB NOT NULL, payload BLOB NOT NULL, content_digest BLOB, payload_mac BLOB,
 PRIMARY KEY (doc_id, codec_key)
);
CREATE TABLE IF NOT EXISTS thin_vault_episodic_revision (id INTEGER PRIMARY KEY CHECK (id = 1), revision INTEGER NOT NULL);
INSERT OR IGNORE INTO thin_vault_episodic_revision(id, revision) VALUES (1, 0);
CREATE TRIGGER IF NOT EXISTS thin_episodic_index_insert AFTER INSERT ON thin_vault_episodic_index BEGIN UPDATE thin_vault_episodic_revision SET revision = revision + 1 WHERE id = 1; END;
CREATE TRIGGER IF NOT EXISTS thin_episodic_index_update AFTER UPDATE ON thin_vault_episodic_index BEGIN UPDATE thin_vault_episodic_revision SET revision = revision + 1 WHERE id = 1; END;
CREATE TRIGGER IF NOT EXISTS thin_episodic_index_delete AFTER DELETE ON thin_vault_episodic_index BEGIN UPDATE thin_vault_episodic_revision SET revision = revision + 1 WHERE id = 1; END;
CREATE TRIGGER IF NOT EXISTS thin_episodic_source_delete AFTER DELETE ON thin_vault_docs BEGIN DELETE FROM thin_vault_episodic_index WHERE doc_id = OLD.doc_id; END;`

// episodicCodecKey binds the secret, SDK version and explicit encoder options.
// Only named project language-pack functions are stable across process restarts;
// arbitrary closures cannot be identified safely and are rejected.
func (v *Vault) episodicCodecKey() ([]byte, error) {
	functionName := func(fn any) (string, error) {
		value := reflect.ValueOf(fn)
		if !value.IsValid() || value.IsNil() {
			return "", nil
		}
		f := runtime.FuncForPC(value.Pointer())
		if f == nil {
			return "", errors.New("thinclient: unidentifiable episodic encoder function")
		}
		name := f.Name()
		plainSub := "github.com/rocareer/rolling-memory/sdk/go/pulse/plain/"
		plainPkg := "github.com/rocareer/rolling-memory/sdk/go/pulse/plain."
		supported := strings.HasPrefix(name, plainSub) || strings.HasPrefix(name, plainPkg)
		if !supported || strings.Contains(name, ".func") || strings.HasSuffix(name, "-fm") {
			return "", errors.New("thinclient: unsupported episodic encoder function")
		}
		return name, nil
	}
	segmenter, err := functionName(v.segOpts.Segmenter)
	if err != nil {
		return nil, err
	}
	packer, err := functionName(v.segOpts.SentenceAtomPacker)
	if err != nil {
		return nil, err
	}
	expander, err := functionName(v.harmOpts.Expander)
	if err != nil {
		return nil, err
	}
	spec := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00%t", episodicCodecVersion, plain.CodecVersion, segmenter, packer, expander, v.segOpts.HarmonicOverlay, v.segOpts.DisableAutoHarmonic, v.harmOpts.EnableStructuredDates)
	return episodicunit.HMACSHA256(v.secretKey[:], []byte(spec)), nil
}

// episodicContentDigest binds an index to exact source bytes without storing text.
func (v *Vault) episodicContentDigest(text string) []byte {
	return episodicunit.HMACSHA256(v.secretKey[:], []byte("episodic-content-v1\x00"), []byte(text))
}

// episodicPayloadMAC authenticates the complete index row with unambiguous framing.
func (v *Vault) episodicPayloadMAC(docID, refPointer string, codecKey, payload, digest []byte) []byte {
	parts := make([][]byte, 0, 11)
	parts = append(parts, []byte("episodic-index-row-v1\x00"))
	for _, field := range [][]byte{[]byte(docID), []byte(refPointer), codecKey, payload, digest} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		sizeCopy := size
		parts = append(parts, sizeCopy[:], field)
	}
	return episodicunit.HMACSHA256(v.secretKey[:], parts...)
}

func (v *Vault) validEpisodicPayloadMAC(docID, refPointer string, codecKey, payload, digest, mac []byte) bool {
	return len(digest) == sha256.Size && len(mac) == sha256.Size && hmac.Equal(mac, v.episodicPayloadMAC(docID, refPointer, codecKey, payload, digest))
}

func (v *Vault) episodicSourceMAC(docID, turnRef, refPointer string, digest []byte) []byte {
	parts := make([][]byte, 0, 9)
	parts = append(parts, []byte("episodic-source-v1\x00"))
	for _, field := range [][]byte{[]byte(docID), []byte(turnRef), []byte(refPointer), digest} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		sizeCopy := size
		parts = append(parts, sizeCopy[:], field)
	}
	return episodicunit.HMACSHA256(v.secretKey[:], parts...)
}

func (v *Vault) validEpisodicSourceMAC(docID, turnRef, refPointer string, digest, mac []byte) bool {
	return pointerStem(refPointer) == turnRef && len(digest) == sha256.Size && len(mac) == sha256.Size &&
		hmac.Equal(mac, v.episodicSourceMAC(docID, turnRef, refPointer, digest))
}

func (v *Vault) recordEpisodicSource(tx *sql.Tx, docID, turnRef, refPointer string, digest []byte, occurredAt int64) error {
	if turnRef == "" || pointerStem(refPointer) != turnRef || len(digest) != sha256.Size {
		return errors.New("thinclient: invalid episodic source identity")
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO thin_vault_episodic_lineage(turn_ref, doc_id, occurred_at) VALUES (?, ?, ?)`, turnRef, docID, occurredAt); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO thin_vault_episodic_sources(turn_ref, doc_id, ref_pointer, content_digest, source_mac) VALUES (?, ?, ?, ?, ?) ON CONFLICT(turn_ref, doc_id, ref_pointer) DO NOTHING`, turnRef, docID, refPointer, digest, v.episodicSourceMAC(docID, turnRef, refPointer, digest))
	if err != nil {
		return err
	}
	var storedDigest, mac []byte
	if err := tx.QueryRow(`SELECT content_digest, source_mac FROM thin_vault_episodic_sources WHERE turn_ref = ? AND doc_id = ? AND ref_pointer = ?`, turnRef, docID, refPointer).Scan(&storedDigest, &mac); err != nil {
		return err
	}
	if !hmac.Equal(storedDigest, digest) || !v.validEpisodicSourceMAC(docID, turnRef, refPointer, storedDigest, mac) {
		return fmt.Errorf("%w: episodic source integrity mismatch", ErrEpisodicIndexItem)
	}
	return nil
}

type episodicSource struct {
	turnRef    string
	refPointer string
	digest     []byte
	mac        []byte
}

func (v *Vault) episodicSources(docID, turnRef string) ([]episodicSource, error) {
	query := `SELECT turn_ref, ref_pointer, content_digest, source_mac FROM thin_vault_episodic_sources WHERE doc_id = ?`
	args := []any{docID}
	if turnRef != "" {
		query += ` AND turn_ref = ?`
		args = append(args, turnRef)
	}
	query += ` ORDER BY turn_ref, ref_pointer`
	rows, err := v.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []episodicSource
	for rows.Next() {
		var source episodicSource
		if err := rows.Scan(&source.turnRef, &source.refPointer, &source.digest, &source.mac); err != nil {
			return nil, err
		}
		if v.validEpisodicSourceMAC(docID, source.turnRef, source.refPointer, source.digest, source.mac) {
			sources = append(sources, source)
		}
	}
	return sources, rows.Err()
}

// ErrEpisodicUnitDeleted reports an episodic unit blocked by a deletion tombstone.
var ErrEpisodicUnitDeleted = errors.New("thinclient: episodic unit was deleted")

func checkEpisodicTombstone(tx *sql.Tx, docID string) error {
	var deleted int
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM thin_vault_deleted_units WHERE unit_id = ? AND (layer = 'episodic' OR layer = ''))`, strings.TrimSpace(docID)).Scan(&deleted); err != nil {
		return fmt.Errorf("thinclient: check episodic tombstone: %w", err)
	}
	if deleted != 0 {
		return fmt.Errorf("%w: %s", ErrEpisodicUnitDeleted, docID)
	}
	return nil
}

// ErrEpisodicPointerSourceConflict marks a stored document whose source cannot be
// reconciled with a verified episodic pointer.
var ErrEpisodicPointerSourceConflict = errors.New("thinclient: episodic document already exists with different source")

// resolveEpisodicTurnByDigest re-locates an indexed unit inside its own turn by
// content identity: the docID and the recorded digest both name the exact speech,
// while the stored `#<k>` index is positional and shifts whenever the segmentation
// filter evolves (or the harness appends a step). The search re-runs the canonical
// unit derivation (one unit per speech segment, sentence-boundary chunks under the
// tokenz-r budget) over the pointer's turn, so only the identical text can match.
func (v *Vault) resolveEpisodicTurnByDigest(refPointer string, digest []byte) (string, string, bool) {
	if len(digest) != sha256.Size {
		return "", "", false
	}
	segments, err := episodicPointerTurnSegments(refPointer)
	if err != nil {
		return "", "", false
	}
	stem := pointerStem(refPointer)
	for _, segment := range segments {
		chunks := episodicunit.Chunk(episodicunit.Text(segment.Role, segment.Text), episodicunit.TokenBudget)
		for index, chunk := range chunks {
			if !hmac.Equal(digest, v.episodicContentDigest(chunk)) {
				continue
			}
			if len(chunks) == 1 {
				return chunk, fmt.Sprintf("%s#%d", stem, segment.Index), true
			}
			return chunk, fmt.Sprintf("%s#%d.%d", stem, segment.Index, index), true
		}
	}
	return "", "", false
}

// episodicTurnDigestMatch looks the recorded content digest up in the canonical unit
// derivation of one enumerated turn (segment text, sentence-boundary chunks).
func (v *Vault) episodicTurnDigestMatch(segments []episodic.Segment, digest []byte) (string, bool) {
	for _, segment := range segments {
		rendered := episodicunit.Text(segment.Role, segment.Text)
		for _, chunk := range episodicunit.Chunk(rendered, episodicunit.TokenBudget) {
			if hmac.Equal(digest, v.episodicContentDigest(chunk)) {
				return chunk, true
			}
		}
	}
	return "", false
}

// EpisodicContentStatus classifies one indexed unit against its live source turn.
type EpisodicContentStatus int

const (
	// EpisodicContentUnreadable: the source turn cannot be enumerated (transcript
	// gone or store unavailable) — the row is kept (fail-safe, never pruned).
	EpisodicContentUnreadable EpisodicContentStatus = iota
	// EpisodicContentVerified: the stored pointer still resolves to the identical text.
	EpisodicContentVerified
	// EpisodicContentRelocatable: the turn still yields the identical text, at a
	// different `#<k>` (a re-scan re-points it; hydration relocates it meanwhile).
	EpisodicContentRelocatable
	// EpisodicContentAbsent: the turn enumerates successfully and does not contain
	// this content at all — an artifact of an older segmentation filter (the agent's
	// tool-step narration). Only this class is a prune candidate.
	EpisodicContentAbsent
)

// EpisodicPointerContentStatus classifies an indexed unit: resolvable at its stored
// index (Verified), present elsewhere in the same turn (Relocatable), provably
// absent from a successfully enumerated turn (Absent), or unclassifiable because
// the source is unavailable (Unreadable).
func (v *Vault) EpisodicPointerContentStatus(docID, refPointer string) EpisodicContentStatus {
	if v == nil || v.db == nil {
		return EpisodicContentUnreadable
	}
	refPointer = strings.TrimSpace(refPointer)
	key, err := v.episodicCodecKey()
	if err != nil {
		return EpisodicContentUnreadable
	}
	var digest, payload, mac, storedKey []byte
	if err := v.db.QueryRow(`SELECT content_digest, payload, payload_mac, codec_key FROM thin_vault_episodic_index WHERE doc_id = ? AND ref_pointer = ? ORDER BY (codec_key = ?) DESC, rowid DESC LIMIT 1`, docID, refPointer, key).Scan(&digest, &payload, &mac, &storedKey); err != nil || !v.validEpisodicPayloadMAC(docID, refPointer, storedKey, payload, digest, mac) {
		return EpisodicContentUnreadable
	}
	if text, err := ResolveEpisodicPointerLive(refPointer); err == nil && strings.TrimSpace(text) != "" && hmac.Equal(digest, v.episodicContentDigest(text)) {
		return EpisodicContentVerified
	}
	if sources, err := v.episodicSources(docID, ""); err != nil {
		return EpisodicContentUnreadable
	} else {
		for _, source := range sources {
			if source.turnRef != pointerStem(refPointer) && hmac.Equal(source.digest, digest) {
				return EpisodicContentRelocatable
			}
		}
	}
	segments, err := episodicPointerTurnSegments(refPointer)
	if err != nil {
		if strings.Contains(err.Error(), "empty zcode turn") {
			return EpisodicContentAbsent
		}
		return EpisodicContentUnreadable
	}
	if _, ok := v.episodicTurnDigestMatch(segments, digest); ok {
		return EpisodicContentRelocatable
	}
	return EpisodicContentAbsent
}

// isPollutedInlineEpisodicText reports whether a legacy non-pointer episodic row
// contains unstripped tool-call wire traces or internal thinking scratchpads.
// Line-anchored checks ensure genuine inline memories that merely mention tags
// like `[thinking]` or `[tool_result]` inside a sentence are never misclassified.
func isPollutedInlineEpisodicText(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	for _, line := range strings.Split(trimmed, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		for _, prefix := range []string{
			"call:default_api:",
			"[assistant] call:default_api:",
			"[thinking]",
			"<thinking>",
			"[tool_use",
			"[tool_result]",
			"[assistant] [thinking]",
			"[assistant] <thinking>",
			"[assistant] [tool_use",
			"[assistant] [tool_result]",
			"[assistant] Thinking Process:",
			"Thinking Process:",
			"[assistant] [{\"content\":",
		} {
			if strings.HasPrefix(l, prefix) {
				return true
			}
		}
	}
	return false
}

// ClassifyAndHealEpisodicUnit classifies any episodic vault row (indexed pointer,
// unindexed pointer, legacy whole-turn pointer, or inline text) and, when heal is
// true, repairs relocatable or unindexed pointers in-place once their content
// identity is verified against the live turn.
//
// This sweep resolves each unit's source live: it repairs one row at a time from a
// caller that holds no page-scoped parse, so the frozen-source seam of the ingest
// chains (SaveLocalEpisodicPointerIndexedVerified, EnsureLocalEpisodicIndexVerified)
// has nothing to share here.
func (v *Vault) ClassifyAndHealEpisodicUnit(scope, docID, rawText string, occurredAt int64, heal bool) EpisodicContentStatus {
	if v == nil || v.db == nil {
		return EpisodicContentUnreadable
	}
	rawText = strings.TrimSpace(rawText)
	if !strings.HasPrefix(rawText, "@ref:") {
		if isPollutedInlineEpisodicText(rawText) {
			return EpisodicContentAbsent
		}
		return EpisodicContentVerified
	}
	stem := pointerStem(rawText)
	if stem == "" {
		// Legacy whole-turn pointer without `#<k>` has no segment authorization.
		return EpisodicContentAbsent
	}
	key, err := v.episodicCodecKey()
	if err != nil {
		return EpisodicContentUnreadable
	}
	var indexedRef string
	var digest, payload, mac, storedKey []byte
	err = v.db.QueryRow(`SELECT ref_pointer, content_digest, payload, payload_mac, codec_key FROM thin_vault_episodic_index WHERE doc_id = ? ORDER BY (codec_key = ?) DESC, rowid DESC LIMIT 1`, docID, key).Scan(&indexedRef, &digest, &payload, &mac, &storedKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return EpisodicContentUnreadable
	}
	if err == nil && (indexedRef != rawText || !v.validEpisodicPayloadMAC(docID, indexedRef, storedKey, payload, digest, mac)) {
		return EpisodicContentUnreadable
	}
	wasIndexed := err == nil
	if len(digest) == sha256.Size {
		if text, err := ResolveEpisodicPointerLive(rawText); err == nil && strings.TrimSpace(text) != "" && hmac.Equal(digest, v.episodicContentDigest(text)) {
			if heal && !wasIndexed {
				_ = v.healEpisodicPointerUnit(docID, occurredAt, rawText, rawText, stem, text)
			}
			if !wasIndexed {
				return EpisodicContentRelocatable
			}
			return EpisodicContentVerified
		}
	}
	if wasIndexed {
		if _, source, err := v.loadVerifiedEpisodicPointer(docID, rawText, true); err == nil && pointerStem(source) != stem {
			return EpisodicContentRelocatable
		}
	}
	segments, err := episodicPointerTurnSegments(rawText)
	if err != nil {
		if strings.Contains(err.Error(), "empty zcode turn") {
			if wasIndexed {
				sources, sourceErr := v.episodicSources(docID, "")
				if sourceErr != nil {
					return EpisodicContentUnreadable
				}
				for _, source := range sources {
					if source.turnRef != stem && hmac.Equal(source.digest, digest) {
						return EpisodicContentRelocatable
					}
				}
			}
			return EpisodicContentAbsent
		}
		return EpisodicContentUnreadable
	}
	for _, seg := range segments {
		for _, u := range episodicunit.SegmentUnits(scope, stem, seg, occurredAt, true) {
			matched := false
			if len(digest) == sha256.Size {
				matched = hmac.Equal(digest, v.episodicContentDigest(u.Text))
			} else {
				matched = u.DocID == docID || episodicunit.DocID("default", u.Text) == docID || episodicunit.DocID("mem:self", u.Text) == docID
			}
			if !matched {
				continue
			}
			if heal {
				_ = v.healEpisodicPointerUnit(docID, occurredAt, rawText, u.RefPointer, stem, u.Text)
			}
			if u.RefPointer == rawText && wasIndexed {
				return EpisodicContentVerified
			}
			return EpisodicContentRelocatable
		}
	}
	if wasIndexed {
		sources, sourceErr := v.episodicSources(docID, "")
		if sourceErr != nil {
			return EpisodicContentUnreadable
		}
		for _, source := range sources {
			if source.turnRef == stem || !hmac.Equal(source.digest, digest) {
				continue
			}
			return EpisodicContentRelocatable
		}
	}
	return EpisodicContentAbsent
}

func (v *Vault) repointEpisodicIndexes(tx *sql.Tx, docID, oldRef, newRef string, digest []byte) error {
	rows, err := tx.Query(`SELECT codec_key, payload, content_digest, payload_mac FROM thin_vault_episodic_index WHERE doc_id = ? AND ref_pointer = ?`, docID, oldRef)
	if err != nil {
		return err
	}
	type row struct{ key, payload, digest, mac []byte }
	var indexes []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.key, &r.payload, &r.digest, &r.mac); err != nil {
			rows.Close()
			return err
		}
		if !v.validEpisodicPayloadMAC(docID, oldRef, r.key, r.payload, r.digest, r.mac) || !hmac.Equal(r.digest, digest) {
			rows.Close()
			return fmt.Errorf("%w: episodic codec generation integrity mismatch", ErrEpisodicIndexItem)
		}
		indexes = append(indexes, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, r := range indexes {
		if _, err := tx.Exec(`UPDATE thin_vault_episodic_index SET ref_pointer = ?, payload_mac = ? WHERE doc_id = ? AND codec_key = ? AND ref_pointer = ?`, newRef, v.episodicPayloadMAC(docID, newRef, r.key, r.payload, r.digest), docID, r.key, oldRef); err != nil {
			return err
		}
	}
	return nil
}

// healEpisodicPointerUnit updates a verified episodic row's pointer to targetRef
// and ensures a MAC-authenticated index row exists under the current codec key.
func (v *Vault) healEpisodicPointerUnit(docID string, occurredAt int64, oldRaw, targetRef, turnRef, text string) error {
	key, err := v.episodicCodecKey()
	if err != nil {
		return err
	}
	pkt, _ := v.PackBlindPacket(docID, text, occurredAt)
	payload, err := plain.EncodeWireBytes(pkt.Slots, pkt.SentenceFrames)
	if err != nil {
		return err
	}
	digest := v.episodicContentDigest(text)
	mac := v.episodicPayloadMAC(docID, targetRef, key, payload, digest)
	tx, err := v.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkEpisodicTombstone(tx, docID); err != nil {
		return err
	}
	var curRaw string
	if err := tx.QueryRow(`SELECT raw_text FROM thin_vault_docs WHERE doc_id = ?`, docID).Scan(&curRaw); err != nil {
		return err
	}
	if curRaw != oldRaw && curRaw != targetRef {
		return errors.New("thinclient: episodic pointer changed concurrently")
	}
	var existingRef string
	var existingDigest, existingPayload, existingMAC, existingKey []byte
	err = tx.QueryRow(`SELECT ref_pointer, content_digest, payload, payload_mac, codec_key FROM thin_vault_episodic_index WHERE doc_id = ? ORDER BY (codec_key = ?) DESC, rowid DESC LIMIT 1`, docID, key).Scan(&existingRef, &existingDigest, &existingPayload, &existingMAC, &existingKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if existingRef != curRaw || !v.validEpisodicPayloadMAC(docID, existingRef, existingKey, existingPayload, existingDigest, existingMAC) {
			return errors.New("thinclient: existing episodic index integrity unavailable")
		}
		if !hmac.Equal(existingDigest, digest) {
			return errors.New("thinclient: existing episodic content identity mismatch")
		}
	}
	if curRaw != targetRef {
		if err := v.repointEpisodicIndexes(tx, docID, curRaw, targetRef, digest); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE thin_vault_docs SET raw_text = ? WHERE doc_id = ? AND raw_text = ?`, targetRef, docID, curRaw); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO thin_vault_episodic_index(doc_id, ref_pointer, codec_key, payload, content_digest, payload_mac) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(doc_id, codec_key) DO UPDATE SET ref_pointer = excluded.ref_pointer, payload = excluded.payload, content_digest = excluded.content_digest, payload_mac = excluded.payload_mac`, docID, targetRef, key, payload, digest, mac); err != nil {
		return err
	}
	if turnRef = strings.TrimSpace(turnRef); turnRef != "" {
		if pointerStem(targetRef) != turnRef {
			return errors.New("thinclient: episodic source turn mismatch")
		}
		if err := v.recordEpisodicSource(tx, docID, turnRef, targetRef, digest, occurredAt); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	v.InvalidateRuntimeCaches()
	return nil
}

// DeleteLocalEpisodicUnit removes one pointer-indexed episodic unit (document row,
// index row via the delete trigger, lineage references and the FTS mirror) when its
// stored pointer still matches expectPointer: a concurrent re-point (the scan
// healing a drifted row) keeps the row instead of deleting verified speech.
func (v *Vault) DeleteLocalEpisodicUnit(docID, expectPointer string) (bool, error) {
	if v == nil || v.db == nil {
		return false, errors.New("thinclient: nil Vault db")
	}
	docID = strings.TrimSpace(docID)
	expectPointer = strings.TrimSpace(expectPointer)
	if docID == "" || expectPointer == "" {
		return false, errors.New("thinclient: episodic delete requires a document and pointer")
	}
	tx, err := v.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	// Any separately recorded source can change after classification. Keep the
	// document when one exists, including malformed rows whose identity is unknown.
	result, err := tx.Exec(`DELETE FROM thin_vault_docs WHERE doc_id = ? AND raw_text = ? AND (layer = 'episodic' OR layer = '') AND NOT EXISTS (SELECT 1 FROM thin_vault_episodic_sources s WHERE s.doc_id = thin_vault_docs.doc_id AND s.turn_ref <> ?)`, docID, expectPointer, pointerStem(expectPointer))
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed == 0 {
		return false, nil
	}
	if _, err = tx.Exec(`DELETE FROM thin_vault_episodic_lineage WHERE doc_id = ?`, docID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(`DELETE FROM thin_vault_episodic_sources WHERE doc_id = ?`, docID); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	v.InvalidateRuntimeCaches()
	return true, nil
}

// episodicPointerTurnSegments enumerates the speech segments of the turn a segment
// pointer names, ignoring the positional `#<k>` / `#<k>.<c>` suffix.
func episodicPointerTurnSegments(refPointer string) ([]episodic.Segment, error) {
	trimmed := strings.TrimSpace(refPointer)
	if strings.HasPrefix(trimmed, "@ref:zcode:") {
		rest := strings.TrimPrefix(trimmed, "@ref:zcode:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("thinclient: invalid zcode pointer %q", refPointer)
		}
		msgID := parts[1]
		if idx := strings.LastIndex(msgID, "#"); idx > 0 {
			msgID = msgID[:idx]
		}
		zdb, err := openZCodeReadOnly()
		if err != nil {
			return nil, err
		}
		segments, _, err := episodic.ZCodeTurnSegmentsUpTo(zdb, parts[0], msgID, int(^uint(0)>>1))
		return segments, err
	}
	if strings.HasPrefix(trimmed, "@ref:jsonl:") {
		rest := strings.TrimPrefix(trimmed, "@ref:jsonl:")
		lastColon := strings.LastIndex(rest, ":")
		if lastColon <= 0 {
			return nil, fmt.Errorf("thinclient: invalid jsonl pointer %q", refPointer)
		}
		turnStr := rest[lastColon+1:]
		if idx := strings.LastIndex(turnStr, "#"); idx > 0 {
			turnStr = turnStr[:idx]
		}
		turnIndex, err := strconv.Atoi(strings.TrimSpace(turnStr))
		if err != nil || turnIndex < 0 {
			return nil, fmt.Errorf("thinclient: invalid jsonl turn index in %q", refPointer)
		}
		return episodic.JSONLTurnSegments(rest[:lastColon], turnIndex)
	}
	return nil, fmt.Errorf("thinclient: unsupported episodic pointer %q", refPointer)
}

// pointerStem returns the turn-level pointer a segment or chunk pointer belongs to
// (everything before the final `#<k>` / `#<k>.<c>` suffix); empty when the pointer
// carries no segment suffix. A stored pointer may only be re-pointed onto another
// index of the same stem, so a re-enumerated `#<k>` can never cross turns.
func pointerStem(refPointer string) string {
	trimmed := strings.TrimSpace(refPointer)
	if idx := strings.LastIndex(trimmed, "#"); idx > 0 {
		return trimmed[:idx]
	}
	return ""
}

func verifyEpisodicPointerSource(refPointer, text string) error {
	resolved, err := ResolveEpisodicPointerLive(refPointer)
	if err != nil {
		return fmt.Errorf("thinclient: resolve episodic pointer: %w", err)
	}
	if strings.TrimSpace(resolved) == "" || resolved != text {
		return fmt.Errorf("%w: episodic pointer content mismatch", ErrEpisodicIndexItem)
	}
	return nil
}

// verifyEpisodicPointerSourceSnapshot is verifyEpisodicPointerSource against a
// frozen source parse: the ref must belong to the snapshot's own source and
// resolve to text verbatim, so one page's saves verify in O(1) per unit instead of
// re-reading and re-decompressing the transcript each time. A ref the snapshot
// cannot address is an explicit error — never a silent fallback to the live read
// the snapshot exists to replace; callers pass a snapshot exactly when the ref
// belongs to its source. A nil snapshot keeps the live resolver.
func verifyEpisodicPointerSourceSnapshot(snapshot *episodic.SourceSnapshot, refPointer, text string) error {
	if snapshot == nil {
		return verifyEpisodicPointerSource(refPointer, text)
	}
	resolved, ok := snapshot.Resolve(refPointer)
	if !ok {
		return fmt.Errorf("%w: episodic pointer %s is not addressable in the source snapshot", ErrEpisodicIndexItem, strings.TrimSpace(refPointer))
	}
	if strings.TrimSpace(resolved) == "" || resolved != text {
		return fmt.Errorf("%w: episodic pointer content mismatch", ErrEpisodicIndexItem)
	}
	return nil
}

// verifyEpisodicPointerSourceEnsure is the pointer-source check of the
// index-creation path (EnsureLocalEpisodicIndex and the re-scan backfill): the
// frozen parse answers first when it can address the ref, and a ref it cannot
// address falls to the live resolver as it always has — the ref then belongs to
// another source (content-addressed units can be said in several transcripts) or
// to a view the frozen parse predates, which only the live read can answer.
func verifyEpisodicPointerSourceEnsure(snapshot *episodic.SourceSnapshot, refPointer, text string) error {
	if snapshot != nil {
		if resolved, ok := snapshot.Resolve(refPointer); ok {
			if strings.TrimSpace(resolved) == "" || resolved != text {
				return fmt.Errorf("%w: episodic pointer content mismatch", ErrEpisodicIndexItem)
			}
			return nil
		}
	}
	return verifyEpisodicPointerSource(refPointer, text)
}

// resolveEpisodicPointerFrozenFirst resolves one pointer against a frozen source
// parse when the snapshot can address it, and falls back to the live resolver
// otherwise: the pointer then belongs to another source, or to a view the frozen
// parse predates, and the live read is the only answer available.
func resolveEpisodicPointerFrozenFirst(snapshot *episodic.SourceSnapshot, refPointer string) (string, error) {
	if snapshot != nil {
		if text, ok := snapshot.Resolve(refPointer); ok {
			return text, nil
		}
	}
	return ResolveEpisodicPointerLive(refPointer)
}

// LoadVerifiedEpisodicPointer resolves an indexed pointer only if its source still
// matches the content used for the index. Unindexed legacy pointers fail closed.
func (v *Vault) LoadVerifiedEpisodicPointer(docID, refPointer string) (string, error) {
	text, _, err := v.loadVerifiedEpisodicPointer(docID, refPointer, false)
	return text, err
}

// LoadVerifiedEpisodicPointerSource also reports the pointer that supplied the
// verified content, which may differ from refPointer when a primary source is gone.
func (v *Vault) LoadVerifiedEpisodicPointerSource(docID, refPointer string) (string, string, error) {
	return v.loadVerifiedEpisodicPointer(docID, refPointer, true)
}

func (v *Vault) loadVerifiedEpisodicPointer(docID, refPointer string, allowAlternate bool) (string, string, error) {
	if v == nil || v.db == nil {
		return "", "", errors.New("thinclient: nil Vault db")
	}
	refPointer = strings.TrimSpace(refPointer)
	tail := refPointer
	if colon := strings.LastIndex(tail, ":"); colon >= 0 {
		tail = tail[colon+1:]
	}
	_, _, _, hasSegment, _ := splitPointerSegment(tail)
	if !hasSegment {
		return "", "", errors.New("thinclient: whole-turn pointer has no independently authorized segment")
	}
	key, err := v.episodicCodecKey()
	if err != nil {
		return "", "", err
	}
	var primary string
	var payload, digest, mac, storedKey []byte
	err = v.db.QueryRow(`SELECT d.raw_text, i.payload, i.content_digest, i.payload_mac, i.codec_key FROM thin_vault_docs d JOIN thin_vault_episodic_index i ON i.doc_id = d.doc_id AND i.ref_pointer = d.raw_text WHERE d.doc_id = ? ORDER BY (i.codec_key = ?) DESC, i.rowid DESC LIMIT 1`, docID, key).Scan(&primary, &payload, &digest, &mac, &storedKey)
	if err != nil {
		return "", "", fmt.Errorf("thinclient: episodic index identity unavailable for %s: %w", docID, err)
	}
	if !v.validEpisodicPayloadMAC(docID, primary, storedKey, payload, digest, mac) {
		return "", "", fmt.Errorf("thinclient: episodic index integrity unavailable for %s", docID)
	}
	var sources []episodicSource
	if refPointer != primary {
		sources, err = v.episodicSources(docID, "")
		if err != nil {
			return "", "", err
		}
	}
	if refPointer != primary {
		authorized := false
		for _, source := range sources {
			if source.refPointer == refPointer && hmac.Equal(source.digest, digest) {
				authorized = true
				break
			}
		}
		if !authorized {
			return "", "", fmt.Errorf("thinclient: episodic source identity unavailable for %s", docID)
		}
	}
	resolve := func(ref string) (string, string, bool) {
		text, err := ResolveEpisodicPointerLive(ref)
		if err == nil && strings.TrimSpace(text) != "" && hmac.Equal(digest, v.episodicContentDigest(text)) {
			return text, ref, true
		}
		return v.resolveEpisodicTurnByDigest(ref, digest)
	}
	if text, source, ok := resolve(refPointer); ok {
		if source != refPointer && !allowAlternate {
			return "", "", fmt.Errorf("thinclient: episodic pointer moved for %s", docID)
		}
		return text, source, nil
	}
	if allowAlternate {
		if sources == nil {
			sources, err = v.episodicSources(docID, "")
			if err != nil {
				return "", "", err
			}
		}
		for _, source := range sources {
			if source.refPointer == refPointer || !hmac.Equal(source.digest, digest) {
				continue
			}
			if text, actualRef, ok := resolve(source.refPointer); ok {
				return text, actualRef, nil
			}
		}
	}
	return "", "", fmt.Errorf("thinclient: episodic pointer content unavailable for %s", docID)
}

// EpisodicTurnDocuments returns only stored identities explicitly linked to a turn.
// Callers must still verify each pointer before delivering its content.
func (v *Vault) EpisodicTurnDocuments(turnRef string) (map[string]string, error) {
	rows, err := v.db.Query(`SELECT d.doc_id, d.raw_text FROM thin_vault_episodic_lineage l JOIN thin_vault_docs d ON d.doc_id = l.doc_id WHERE l.turn_ref = ?`, turnRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	docs := make(map[string]string)
	for rows.Next() {
		var id, pointer string
		if err := rows.Scan(&id, &pointer); err != nil {
			return nil, err
		}
		if pointerStem(pointer) == turnRef {
			docs[id] = pointer
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sourceRows, err := v.db.Query(`SELECT doc_id, ref_pointer, content_digest, source_mac FROM thin_vault_episodic_sources WHERE turn_ref = ? ORDER BY doc_id, ref_pointer`, turnRef)
	if err != nil {
		return nil, err
	}
	defer sourceRows.Close()
	for sourceRows.Next() {
		var id, pointer string
		var digest, mac []byte
		if err := sourceRows.Scan(&id, &pointer, &digest, &mac); err != nil {
			return nil, err
		}
		if v.validEpisodicSourceMAC(id, turnRef, pointer, digest, mac) {
			previous, exists := docs[id]
			_, segment, chunk, hasSegment, hasChunk := splitPointerSegment(pointer)
			_, prevSegment, prevChunk, prevHasSegment, prevHasChunk := splitPointerSegment(previous)
			if !exists || (hasSegment && (!prevHasSegment || segment < prevSegment ||
				(segment == prevSegment && (!hasChunk || (prevHasChunk && chunk < prevChunk))))) {
				docs[id] = pointer
			}
		}
	}
	return docs, sourceRows.Err()
}

// VerifiedTurnFirstSegment resolves the turn's first speech segment (`#0`), which by
// construction is the user message that opened the turn, when a stored unit for it still
// passes content verification. Recall delivery prepends it so the reader always sees what
// the user asked, even when retrieval matched only later assistant segments.
func (v *Vault) VerifiedTurnFirstSegment(turnRef string) (string, bool) {
	turnRef = strings.TrimSpace(turnRef)
	if v == nil || v.db == nil || turnRef == "" {
		return "", false
	}
	docs, err := v.EpisodicTurnDocuments(turnRef)
	if err != nil || len(docs) == 0 {
		return "", false
	}
	ids := make([]string, 0, len(docs))
	for id := range docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	bestID, bestPointer, bestChunk := "", "", -1
	for _, id := range ids {
		pointer := strings.TrimSpace(docs[id])
		colon := strings.LastIndex(pointer, ":")
		if colon <= 0 || colon == len(pointer)-1 {
			continue
		}
		_, segmentIndex, chunkIndex, hasSegment, hasChunk := splitPointerSegment(pointer[colon+1:])
		if !hasSegment || segmentIndex != 0 {
			continue
		}
		if !hasChunk {
			// A whole-segment `#0` unit outranks any chunked variant.
			bestID, bestPointer, bestChunk = id, pointer, -1
			break
		}
		if bestChunk < 0 || chunkIndex < bestChunk {
			bestID, bestPointer, bestChunk = id, pointer, chunkIndex
		}
	}
	if bestID == "" {
		return "", false
	}
	text, _, err := v.loadVerifiedEpisodicPointer(bestID, bestPointer, false)
	if err != nil || strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

// SaveLocalEpisodicPointerIndexed stores a pointer and its canonical, plaintext-free
// scoring packet in one transaction. pkt must be the packet produced by this Vault
// for livePlaintext under its current key and segmentation options.
func (v *Vault) SaveLocalEpisodicPointerIndexed(docID string, occurredAt int64, refPointer, turnRef, livePlaintext string, pkt BlindPulsePacket, resp IngestResponse) error {
	return v.saveLocalEpisodicPointerIndexed(docID, occurredAt, refPointer, turnRef, livePlaintext, pkt, resp, nil)
}

// SaveLocalEpisodicPointerIndexedVerified stores one pointer and its canonical,
// plaintext-free scoring packet in one transaction, verifying the pointer against a
// frozen source parse instead of re-reading the transcript per unit. The ref must
// belong to the snapshot's own source and resolve to text verbatim; a ref the
// snapshot does not own, or cannot resolve, is an explicit error — the live read
// the snapshot exists to replace is never consulted as a silent fallback. A nil
// snapshot behaves exactly like SaveLocalEpisodicPointerIndexed, for callers that
// hold no frozen view.
//
// scope binds the verified write to the content addressing every ingest path
// shares: docID must be episodicunit.DocID(scope, text), so verification and
// document identity can never disagree about which speech was written.
//
// Everything else — the tombstone check inside the write transaction, the payload
// MAC, the source record, the lineage row and the runtime cache invalidation — is
// byte-for-byte the semantics of SaveLocalEpisodicPointerIndexed.
func (v *Vault) SaveLocalEpisodicPointerIndexedVerified(scope string, snapshot *episodic.SourceSnapshot, docID string, occurredAt int64, refPointer, turnRef, text string, resp IngestResponse) error {
	if v == nil || v.db == nil {
		return errors.New("thinclient: nil Vault db")
	}
	if snapshot == nil {
		pkt, _ := v.PackBlindPacket(docID, text, occurredAt)
		return v.SaveLocalEpisodicPointerIndexed(docID, occurredAt, refPointer, turnRef, text, pkt, resp)
	}
	if episodicunit.DocID(scope, text) != docID {
		return fmt.Errorf("%w: episodic document content identity mismatch", ErrEpisodicIndexItem)
	}
	pkt, _ := v.PackBlindPacket(docID, text, occurredAt)
	return v.saveLocalEpisodicPointerIndexed(docID, occurredAt, refPointer, turnRef, text, pkt, resp, snapshot)
}

// saveLocalEpisodicPointerIndexed is the shared write behind the two public saves.
// snapshot selects the pointer-source verification: nil keeps the live transcript
// read, non-nil answers from the frozen parse.
func (v *Vault) saveLocalEpisodicPointerIndexed(docID string, occurredAt int64, refPointer, turnRef, livePlaintext string, pkt BlindPulsePacket, resp IngestResponse, snapshot *episodic.SourceSnapshot) error {
	if v == nil || v.db == nil {
		return errors.New("thinclient: nil Vault db")
	}
	refPointer = strings.TrimSpace(refPointer)
	if !strings.HasPrefix(refPointer, "@ref:") || strings.TrimSpace(docID) == "" || strings.TrimSpace(livePlaintext) == "" {
		return fmt.Errorf("%w: episodic index requires a document, pointer and plaintext", ErrEpisodicIndexItem)
	}
	turnRef = strings.TrimSpace(turnRef)
	stem := pointerStem(refPointer)
	if stem == "" || (turnRef != "" && turnRef != stem) {
		return fmt.Errorf("%w: episodic source turn mismatch", ErrEpisodicIndexItem)
	}
	if turnRef == "" {
		turnRef = stem
	}
	if pkt.UnitID != docID {
		return fmt.Errorf("%w: episodic packet document mismatch", ErrEpisodicIndexItem)
	}
	if err := verifyEpisodicPointerSourceSnapshot(snapshot, refPointer, livePlaintext); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %w", ErrEpisodicIndexItem, err)
		}
		return err
	}
	expected, _ := v.PackBlindPacket(docID, livePlaintext, occurredAt)
	payload, err := plain.EncodeWireBytes(pkt.Slots, pkt.SentenceFrames)
	if err != nil {
		return fmt.Errorf("thinclient: encode episodic packet: %w", err)
	}
	canonical, err := plain.EncodeWireBytes(expected.Slots, expected.SentenceFrames)
	if err != nil {
		return err
	}
	if !bytes.Equal(payload, canonical) {
		return fmt.Errorf("%w: episodic packet key or encoder mismatch", ErrEpisodicIndexItem)
	}
	codecKey, err := v.episodicCodecKey()
	if err != nil {
		return err
	}
	keptJSON, err := json.Marshal(resp.KeptSentenceIndices)
	if err != nil {
		return err
	}
	tx, err := v.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkEpisodicTombstone(tx, docID); err != nil {
		return err
	}
	digest := v.episodicContentDigest(livePlaintext)
	var existingRaw string
	err = tx.QueryRow(`SELECT raw_text FROM thin_vault_docs WHERE doc_id = ?`, docID).Scan(&existingRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var storedPointer string
	var storedDigest, storedPayload, storedMAC []byte
	repoint, alternate := false, false
	if err == nil {
		err = tx.QueryRow(`SELECT ref_pointer, content_digest, payload, payload_mac FROM thin_vault_episodic_index WHERE doc_id = ? AND ref_pointer = ? AND codec_key = ?`, docID, existingRaw, codecKey).Scan(&storedPointer, &storedDigest, &storedPayload, &storedMAC)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: existing episodic document identity is unverified; ensure index first", ErrEpisodicIndexItem)
		}
		if err != nil {
			return err
		}
		if !v.validEpisodicPayloadMAC(docID, storedPointer, codecKey, storedPayload, storedDigest, storedMAC) {
			return fmt.Errorf("%w: existing episodic index integrity unavailable; rebuild required", ErrEpisodicIndexItem)
		}
		if !hmac.Equal(storedDigest, digest) {
			return fmt.Errorf("%w: existing episodic content identity mismatch", ErrEpisodicIndexItem)
		}
		if !bytes.Equal(storedPayload, payload) {
			return fmt.Errorf("%w: existing episodic packet mismatch", ErrEpisodicIndexItem)
		}
		if existingRaw != refPointer {
			if pointerStem(existingRaw) == "" {
				return fmt.Errorf("%w: %w", ErrEpisodicIndexItem, ErrEpisodicPointerSourceConflict)
			}
			if pointerStem(existingRaw) != stem {
				alternate = true
			} else {
				oldText, oldErr := resolveEpisodicPointerFrozenFirst(snapshot, existingRaw)
				if oldErr == nil {
					repoint = !hmac.Equal(digest, v.episodicContentDigest(oldText))
				}
			}
		}
	}
	if err == nil && existingRaw != refPointer && !repoint {
		alternate = true
	}
	if alternate {
		var changesBefore, changesAfter int64
		if err := tx.QueryRow(`SELECT total_changes()`).Scan(&changesBefore); err != nil {
			return err
		}
		if err := v.recordEpisodicSource(tx, docID, turnRef, refPointer, digest, occurredAt); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT total_changes()`).Scan(&changesAfter); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if changesAfter != changesBefore {
			v.InvalidateRuntimeCaches()
		}
		return nil
	}
	_, err = tx.Exec(`INSERT INTO thin_vault_docs(doc_id, layer, occurred_at, inserted_at, raw_text, sentences_json, address_uri, coord64, waveform_bar, doc_macro_wave, kept_indices_json, assistant_id, incarnation_id, locus) VALUES (?, 'episodic', ?, ?, ?, '[]', ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(doc_id) DO UPDATE SET occurred_at=CASE WHEN thin_vault_docs.occurred_at <= 0 AND excluded.occurred_at > 0 THEN excluded.occurred_at ELSE thin_vault_docs.occurred_at END, inserted_at=CASE WHEN thin_vault_docs.inserted_at > 0 THEN thin_vault_docs.inserted_at ELSE excluded.inserted_at END, address_uri=excluded.address_uri, coord64=excluded.coord64, waveform_bar=excluded.waveform_bar, doc_macro_wave=excluded.doc_macro_wave, kept_indices_json=excluded.kept_indices_json`, docID, occurredAt, time.Now().Unix(), refPointer, resp.AddressURI, int64(resp.AddressCoord64), resp.WaveformBar, resp.DocMacroWaveform, string(keptJSON), v.identity.AssistantID, v.identity.IncarnationID, v.identity.Locus)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO thin_vault_episodic_index(doc_id, ref_pointer, codec_key, payload, content_digest, payload_mac) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(doc_id, codec_key) DO NOTHING`, docID, refPointer, codecKey, payload, digest, v.episodicPayloadMAC(docID, refPointer, codecKey, payload, digest)); err != nil {
		return err
	}
	if repoint {
		// Same content, new enumeration index: move the stored pointer to the index the
		// current filter names, and re-authenticate the index row against it (the MAC is
		// bound to the ref pointer), so verification and hydration agree again.
		if err := v.repointEpisodicIndexes(tx, docID, existingRaw, refPointer, digest); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE thin_vault_docs SET raw_text = ? WHERE doc_id = ? AND raw_text = ?`, refPointer, docID, existingRaw); err != nil {
			return err
		}
	}
	if err = v.recordEpisodicSource(tx, docID, turnRef, refPointer, digest, occurredAt); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	v.InvalidateRuntimeCaches()
	return nil
}

// ErrEpisodicIndexItem identifies an individual source or index integrity failure.
var ErrEpisodicIndexItem = errors.New("thinclient: episodic index item")

// EnsureLocalEpisodicIndex indexes a content-addressed live source without replacing
// another encoder's packet or an established content identity.
//
// The pointer-source check reads the live transcript. The ingest chains
// (reconcile, source scan, canonical import) pass their page's frozen parse
// through EnsureLocalEpisodicIndexVerified instead; the per-unit heal sweep
// (ClassifyAndHealEpisodicUnit) keeps this live read — it repairs one unit at a
// time and holds no page-scoped parse to share.
func (v *Vault) EnsureLocalEpisodicIndex(scope, docID, refPointer, text string) (bool, error) {
	return v.ensureLocalEpisodicIndex(scope, docID, refPointer, text, false, nil)
}

// EnsureLocalEpisodicIndexVerified is EnsureLocalEpisodicIndex against a frozen
// source parse: the pointer-source check answers from the snapshot when it can
// address the ref (O(1) per unit), and only a ref the snapshot cannot address —
// another source, or a view the frozen parse predates — falls to the live
// resolver, which has always been this check's answer there. A ref the snapshot
// resolves to different text fails closed like the live mismatch.
func (v *Vault) EnsureLocalEpisodicIndexVerified(scope string, snapshot *episodic.SourceSnapshot, docID, refPointer, text string) (bool, error) {
	if snapshot == nil {
		return v.EnsureLocalEpisodicIndex(scope, docID, refPointer, text)
	}
	return v.ensureLocalEpisodicIndex(scope, docID, refPointer, text, false, snapshot)
}

// HasLocalEpisodicIndex reports whether docID already has a MAC-verified index row
// for refPointer under the current episodic encoder.
func (v *Vault) HasLocalEpisodicIndex(docID, refPointer string) bool {
	if v == nil || v.db == nil {
		return false
	}
	key, err := v.episodicCodecKey()
	if err != nil {
		return false
	}
	var payload, digest, mac []byte
	if err := v.db.QueryRow(`SELECT payload, content_digest, payload_mac FROM thin_vault_episodic_index WHERE doc_id = ? AND ref_pointer = ? AND codec_key = ?`, docID, strings.TrimSpace(refPointer), key).Scan(&payload, &digest, &mac); err != nil {
		return false
	}
	return v.validEpisodicPayloadMAC(docID, strings.TrimSpace(refPointer), key, payload, digest, mac)
}

func (v *Vault) ensureLocalEpisodicIndex(scope, docID, refPointer, text string, sourceAlreadyVerified bool, snapshot *episodic.SourceSnapshot) (bool, error) {
	if v == nil || v.db == nil {
		return false, errors.New("thinclient: nil Vault db")
	}
	refPointer = strings.TrimSpace(refPointer)
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(docID) == "" || !strings.HasPrefix(refPointer, "@ref:") || strings.TrimSpace(text) == "" {
		return false, fmt.Errorf("%w: requires a scope, document, pointer and plaintext", ErrEpisodicIndexItem)
	}
	if episodicunit.DocID(scope, text) != docID {
		return false, fmt.Errorf("%w: document content identity mismatch", ErrEpisodicIndexItem)
	}
	key, err := v.episodicCodecKey()
	if err != nil {
		return false, err
	}
	digest := v.episodicContentDigest(text)

	// Fast read-only check before touching the live transcript or re-packing: when the
	// document and its verified index row already match the current codec, content
	// digest and MAC, return without opening the harness store or taking a write lock.
	var roRaw, roRef string
	var roPayload, roDigest, roMAC []byte
	if v.db.QueryRow(`SELECT raw_text FROM thin_vault_docs WHERE doc_id = ?`, docID).Scan(&roRaw) == nil && roRaw == refPointer {
		if v.db.QueryRow(`SELECT ref_pointer, payload, content_digest, payload_mac FROM thin_vault_episodic_index WHERE doc_id = ? AND codec_key = ?`, docID, key).Scan(&roRef, &roPayload, &roDigest, &roMAC) == nil {
			if roRef == refPointer && len(roDigest) == 32 && hmac.Equal(roDigest, digest) && len(roMAC) == 32 && v.validEpisodicPayloadMAC(docID, roRef, key, roPayload, roDigest, roMAC) {
				if _, _, dErr := plain.DecodeWireBytes(roPayload); dErr == nil {
					return false, nil
				}
			}
		}
	}

	if !sourceAlreadyVerified {
		if err := verifyEpisodicPointerSourceEnsure(snapshot, refPointer, text); err != nil {
			return false, fmt.Errorf("%w: %w", ErrEpisodicIndexItem, err)
		}
	}

	pkt, _ := v.PackBlindPacket(docID, text, 0)
	payload, err := plain.EncodeWireBytes(pkt.Slots, pkt.SentenceFrames)
	if err != nil {
		return false, err
	}
	mac := v.episodicPayloadMAC(docID, refPointer, key, payload, digest)

	tx, err := v.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkEpisodicTombstone(tx, docID); err != nil {
		return false, err
	}
	var raw string
	if err := tx.QueryRow(`SELECT raw_text FROM thin_vault_docs WHERE doc_id = ?`, docID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("%w: document unavailable: %w", ErrEpisodicIndexItem, err)
		}
		return false, err
	}
	if raw != refPointer {
		return false, fmt.Errorf("%w: episodic pointer changed", ErrEpisodicIndexItem)
	}
	var storedRef string
	var storedPayload, storedDigest, storedMAC []byte
	err = tx.QueryRow(`SELECT ref_pointer, payload, content_digest, payload_mac FROM thin_vault_episodic_index WHERE doc_id = ? AND codec_key = ?`, docID, key).Scan(&storedRef, &storedPayload, &storedDigest, &storedMAC)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil {
		if storedRef != refPointer || !bytes.Equal(storedPayload, payload) || (len(storedDigest) != 0 && !hmac.Equal(storedDigest, digest)) {
			return false, fmt.Errorf("%w: existing episodic index identity mismatch", ErrEpisodicIndexItem)
		}
		if len(storedMAC) != 0 {
			if !v.validEpisodicPayloadMAC(docID, storedRef, key, storedPayload, storedDigest, storedMAC) {
				return false, fmt.Errorf("%w: existing episodic index integrity mismatch", ErrEpisodicIndexItem)
			}
			return false, nil
		}
		if _, err = tx.Exec(`UPDATE thin_vault_episodic_index SET content_digest = ?, payload_mac = ? WHERE doc_id = ? AND codec_key = ?`, digest, mac, docID, key); err != nil {
			return false, err
		}
	} else if _, err = tx.Exec(`INSERT INTO thin_vault_episodic_index(doc_id, ref_pointer, codec_key, payload, content_digest, payload_mac) VALUES (?, ?, ?, ?, ?, ?)`, docID, refPointer, key, payload, digest, mac); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	v.InvalidateRuntimeCaches()
	return true, nil
}

// EpisodicIndexDiagnostics reports pointer coverage for the current encoder without
// opening source transcripts. Mismatched entries need caller-supplied reindexing.
func (v *Vault) EpisodicIndexDiagnostics() (compatible, incompatible, missing int, err error) {
	if v == nil || v.db == nil {
		return 0, 0, 0, errors.New("thinclient: nil Vault db")
	}
	key, err := v.episodicCodecKey()
	if err != nil {
		return 0, 0, 0, err
	}
	report, err := v.episodicIndexFaultDiagnostics(key)
	return report.Compatible, report.Incompatible, report.Missing, err
}

func (v *Vault) indexedEpisodicDocs() ([]vaultDocEntry, error) {
	return v.indexedEpisodicDocsContext(context.Background())
}

func (v *Vault) indexedEpisodicDocsContext(ctx context.Context) ([]vaultDocEntry, error) {
	docs, _, err := v.indexedEpisodicDocsSince(ctx, 0)
	return docs, err
}

func (v *Vault) indexedEpisodicDocsSince(ctx context.Context, minIndexRowID int64) ([]vaultDocEntry, int64, error) {
	key, err := v.episodicCodecKey()
	if err != nil {
		return nil, minIndexRowID, nil
	}
	var rows *sql.Rows
	// Multi-generation union read: index rows written by any historical codec
	// generation stay loadable (the wire payload is self-describing and each
	// row's MAC is verified with the row's own stored key), because an SDK or
	// language-pack bump must never orphan previously indexed speech. Rows are
	// ordered so the current generation wins per document; both the full load
	// and the incremental catch-up share the same query shape so rows written
	// by a differently-configured sibling process are never missed.
	selCols := `SELECT i.rowid, d.doc_id, d.layer, d.occurred_at, d.address_uri, d.coord64, d.waveform_bar, COALESCE(d.locus, ''), i.ref_pointer, i.codec_key, i.payload, i.content_digest, i.payload_mac FROM thin_vault_docs d JOIN thin_vault_episodic_index i ON i.doc_id = d.doc_id AND i.ref_pointer = d.raw_text WHERE d.layer = 'episodic'`
	if minIndexRowID > 0 {
		rows, err = v.db.QueryContext(ctx, selCols+` AND i.rowid > ? ORDER BY d.doc_id, (i.codec_key = ?) DESC, i.rowid DESC`, minIndexRowID, key)
	} else {
		rows, err = v.db.QueryContext(ctx, selCols+` ORDER BY d.doc_id, (i.codec_key = ?) DESC, i.rowid DESC`, key)
	}
	if err != nil {
		return nil, minIndexRowID, err
	}
	defer rows.Close()
	type rawEpisodicRow struct {
		rowID      int64
		docID      string
		layer      string
		occ        int64
		addrURI    string
		coord64    uint64
		waveBar    string
		locus      string
		refPointer string
		storedKey  []byte
		payload    []byte
		digest     []byte
		mac        []byte
		cacheKey   string
		ok         bool
		entry      vaultDocEntry
	}
	maxSeenRowID := minIndexRowID
	var rawRows []rawEpisodicRow
	var toDecodeIdx []int
	seenDocIDs := make(map[string]bool, 4096)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, minIndexRowID, err
		}
		var r rawEpisodicRow
		var coord int64
		if err := rows.Scan(&r.rowID, &r.docID, &r.layer, &r.occ, &r.addrURI, &coord, &r.waveBar, &r.locus, &r.refPointer, &r.storedKey, &r.payload, &r.digest, &r.mac); err != nil {
			return nil, minIndexRowID, err
		}
		if r.rowID > maxSeenRowID {
			maxSeenRowID = r.rowID
		}
		// Deduplicate across generations: the SQL orders current-generation rows
		// first, so the first row seen for a document is the one to keep.
		if seenDocIDs[r.docID] {
			continue
		}
		seenDocIDs[r.docID] = true
		r.coord64 = uint64(coord)
		toDecodeIdx = append(toDecodeIdx, len(rawRows))
		rawRows = append(rawRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, minIndexRowID, err
	}
	rows.Close()
	if len(rawRows) == 0 {
		return nil, maxSeenRowID, nil
	}

	decodeOne := func(r *rawEpisodicRow) {
		// Accept any historical codec generation: the payload is verified with
		// the key the row itself was written under, so an SDK or language-pack
		// bump cannot orphan previously indexed speech.
		if len(r.storedKey) == 0 || !v.validEpisodicPayloadMAC(r.docID, r.refPointer, r.storedKey, r.payload, r.digest, r.mac) {
			return
		}
		pkt, err := v.decodeEpisodicIndex(r.docID, r.storedKey, r.payload, r.storedKey)
		if err != nil {
			return
		}
		tMeta, tCounts, tPos := coreTermsFromPacket(pkt.Slots, pkt.SentenceFrames)
		docLen := len(pkt.Slots)
		if docLen < 1 {
			docLen = 1
		}
		doc := vaultDocEntry{
			docID:         r.docID,
			layer:         r.layer,
			occ:           r.occ,
			addrURI:       r.addrURI,
			coord64:       r.coord64,
			waveBar:       r.waveBar,
			locus:         strings.TrimSpace(r.locus),
			refPointer:    r.refPointer,
			contentDigest: r.digest,
			termMeta:      tMeta,
			termCounts:    tCounts,
			termPositions: tPos,
			docLen:        docLen,
		}
		r.payload = nil
		r.entry = doc
		r.ok = true
	}

	if len(toDecodeIdx) >= 32 {
		workers := runtime.NumCPU()
		if workers < 2 {
			workers = 2
		}
		if workers > 12 {
			workers = 12
		}
		var wg sync.WaitGroup
		step := (len(toDecodeIdx) + workers - 1) / workers
		for w := 0; w < workers; w++ {
			start := w * step
			if start >= len(toDecodeIdx) {
				break
			}
			end := start + step
			if end > len(toDecodeIdx) {
				end = len(toDecodeIdx)
			}
			wg.Add(1)
			go func(s, e int) {
				defer wg.Done()
				for i := s; i < e; i++ {
					if ctx.Err() != nil {
						return
					}
					decodeOne(&rawRows[toDecodeIdx[i]])
				}
			}(start, end)
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return nil, minIndexRowID, err
		}
	} else {
		for _, idx := range toDecodeIdx {
			if err := ctx.Err(); err != nil {
				return nil, minIndexRowID, err
			}
			decodeOne(&rawRows[idx])
		}
	}

	docs := make([]vaultDocEntry, 0, len(rawRows))
	for idx := range rawRows {
		if rawRows[idx].ok {
			docs = append(docs, rawRows[idx].entry)
		}
	}
	return docs, maxSeenRowID, nil
}

func (v *Vault) decodeEpisodicIndex(ref string, key, payload, expectedKey []byte) (BlindPulsePacket, error) {
	if !hmac.Equal(key, expectedKey) {
		return BlindPulsePacket{}, fmt.Errorf("thinclient: episodic index encoder mismatch for %s", ref)
	}
	slots, frames, err := plain.DecodeWireBytes(payload)
	if err != nil {
		return BlindPulsePacket{}, fmt.Errorf("thinclient: decode episodic index: %w", err)
	}
	return BlindPulsePacket{Slots: slots, SentenceFrames: frames}, nil
}
