// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rocareer/rolling-memory/internal/episodic"
	"github.com/rocareer/rolling-memory/internal/episodicunit"
)

// episodicRescanMaxTurns bounds one same-source digest rescan. The first digest match
// wins, so the bound only keeps a corrupted enumeration from making a sweep unbounded.
const episodicRescanMaxTurns = 1024

// ReanchorSameSourceByDigest searches a row's OWN source for its sealed content digest
// and, when the content is found at another coordinate of the same file with an agreeing
// content-derived identity, re-points and re-seals the row there. It exists because a
// transcript re-enumeration (a removed or prepended turn) can move a unit's ordinal
// inside the same file: the stored coordinate then addresses different speech, the row
// classifies as absent or unreadable, and — without this search — the alive content
// either stays behind a permanent source gap or becomes a prune candidate.
//
// Same-source only: the candidate coordinate must live in the same jsonl file, so no
// cross-session substitution ever happens here (delivery keeps rejecting unattributed
// alternates; this API only repairs the record to the coordinate its own content now
// occupies). zcode rows carry stable message identities and no ordinal drift, so they
// are answered with no candidate. This API never deletes. apply=false probes without
// writing; an empty returned pointer means no candidate (or the row moved underneath
// the caller).
func (v *Vault) ReanchorSameSourceByDigest(scope, docID, refPointer string, occurredAt int64, apply bool) (string, error) {
	if v == nil || v.db == nil {
		return "", errors.New("thinclient: nil Vault db")
	}
	scope = strings.TrimSpace(scope)
	docID = strings.TrimSpace(docID)
	refPointer = strings.TrimSpace(refPointer)
	if docID == "" || !strings.HasPrefix(refPointer, "@ref:jsonl:") {
		return "", nil
	}
	key, err := v.episodicCodecKey()
	if err != nil {
		return "", err
	}
	var indexedRef string
	var digest, payload, mac, storedKey []byte
	err = v.db.QueryRow(`SELECT ref_pointer, content_digest, payload, payload_mac, codec_key FROM rm_episodic_index WHERE doc_id = ? ORDER BY (codec_key = ?) DESC, rowid DESC LIMIT 1`, docID, key).Scan(&indexedRef, &digest, &payload, &mac, &storedKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Legacy rows without an index carry no sealed digest to search for.
			return "", nil
		}
		return "", err
	}
	if indexedRef != refPointer || len(digest) != sha256.Size || !v.validEpisodicPayloadMAC(docID, indexedRef, storedKey, payload, digest, mac) {
		// Only a sealed, verified row's own content identity is a search key.
		return "", nil
	}
	rest := strings.TrimPrefix(refPointer, "@ref:jsonl:")
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 {
		return "", nil
	}
	path := rest[:lastColon]
	// A source that no longer exists cannot hold the content; one stat answers that far
	// cheaper than enumerating turns (the bulk of unreadable rows are deleted sources).
	if _, statErr := os.Stat(path); statErr != nil {
		return "", nil
	}
	// Parse the source once via SourceSnapshot so we do not re-decompress and
	// re-parse the JSONL file from scratch on every turn (which otherwise turns a
	// 200-turn compressed transcript into 200 full-file decompressions).
	snap, snapErr := episodic.NewSourceSnapshot(path)
	if snapErr == nil && snap != nil {
		defer snap.Close()
	}
	maxTurns := episodicRescanMaxTurns
	if snap != nil {
		if n := snap.Turns(); n >= 0 && n < maxTurns {
			maxTurns = n
		}
	}
	for turn := 0; turn < maxTurns; turn++ {
		var segments []episodic.Segment
		if snap != nil {
			var ok bool
			segments, ok = snap.Segments(turn)
			if !ok {
				break
			}
		} else {
			var sErr error
			segments, sErr = episodic.JSONLTurnSegments(path, turn)
			if sErr != nil {
				// Out-of-range turns end the enumeration; an unreadable file ends it too.
				break
			}
		}
		stem := fmt.Sprintf("@ref:jsonl:%s:%d", path, turn)
		for _, segment := range segments {
			rendered := episodicunit.Text(segment.Role, segment.Text)
			chunks := episodicunit.Chunk(rendered, episodicunit.TokenBudget)
			for index, chunk := range chunks {
				if !hmac.Equal(digest, v.episodicContentDigest(chunk)) {
					continue
				}
				if episodicunit.DocID(scope, chunk) != docID &&
					episodicunit.DocID("default", chunk) != docID &&
					episodicunit.DocID("mem:self", chunk) != docID {
					// A digest match whose identity disagrees is not this row's speech.
					return "", nil
				}
				candidate := fmt.Sprintf("%s#%d", stem, segment.Index)
				if len(chunks) > 1 {
					candidate = fmt.Sprintf("%s#%d.%d", stem, segment.Index, index)
				}
				if candidate == refPointer {
					return "", nil
				}
				if !apply {
					return candidate, nil
				}
				if err := v.healEpisodicPointerUnit(docID, occurredAt, refPointer, candidate, stem, chunk); err != nil {
					return "", err
				}
				return candidate, nil
			}
		}
	}
	return "", nil
}
