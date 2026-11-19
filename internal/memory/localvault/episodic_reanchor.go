// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/rocareer/rolling-memory/internal/episodicunit"
)

// ReanchorRelocatedEpisodicUnit re-points one episodic row to the coordinate where
// its sealed content currently resolves and re-seals the index row under the current
// codec. A row's pointer is a coordinate into the source transcript, not its identity:
// when the harness enumeration drifts between imports (segmentation or turn numbering
// changes), a row keeps its recorded content digest while its stored coordinate starts
// addressing different speech, and every display path then reports the source as
// unavailable. The sealed digest is the tie: the alternate coordinate must resolve to
// text matching the row's recorded digest, and the content-derived identity (DocID
// under the rendering scope, with the same legacy scope fallbacks the prune
// classifier accepts) must agree — so a re-anchored row still addresses exactly the
// unit it was sealed from, only at the coordinate its content lives at now.
// apply=false probes re-anchorability without writing. The returned pointer is the
// verified coordinate: an empty pointer means the row already resolves at its own
// coordinate or the row moved underneath the caller, and nothing was written.
func (v *Vault) ReanchorRelocatedEpisodicUnit(scope, docID, refPointer string, occurredAt int64, apply bool) (string, error) {
	if v == nil || v.db == nil {
		return "", errors.New("localvault: nil Vault db")
	}
	scope = strings.TrimSpace(scope)
	docID = strings.TrimSpace(docID)
	refPointer = strings.TrimSpace(refPointer)
	if scope == "" || docID == "" || !strings.HasPrefix(refPointer, "@ref:") {
		return "", errors.New("localvault: episodic re-anchor requires a scope, document and pointer")
	}
	var stored string
	if err := v.db.QueryRow(`SELECT raw_text FROM rm_units WHERE doc_id = ?`, docID).Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("localvault: episodic re-anchor document is gone")
		}
		return "", err
	}
	if strings.TrimSpace(stored) != refPointer {
		// The row moved underneath the caller (a concurrent heal or re-point);
		// there is nothing to re-anchor under the coordinate the caller holds.
		return "", nil
	}
	text, actualRef, err := v.LoadVerifiedEpisodicPointerSource(docID, refPointer)
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	actualRef = strings.TrimSpace(actualRef)
	if actualRef == "" || text == "" {
		return "", errors.New("localvault: episodic re-anchor found no verified alternate source")
	}
	if actualRef == refPointer {
		// The row already resolves at its own coordinate; nothing to move.
		return "", nil
	}
	if episodicunit.DocID(scope, text) != docID &&
		episodicunit.DocID("default", text) != docID &&
		episodicunit.DocID("mem:self", text) != docID {
		return "", errors.New("localvault: episodic re-anchor content identity mismatch")
	}
	if !apply {
		return actualRef, nil
	}
	if err := v.healEpisodicPointerUnit(docID, occurredAt, refPointer, actualRef, pointerStem(actualRef), text); err != nil {
		return "", err
	}
	return actualRef, nil
}
