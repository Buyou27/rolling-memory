// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"

	core "github.com/rocareer/rolling-memory/internal/core"
	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// parseBackgroundAnchorLines extracts 1-based (LineStart, LineEnd) from a line-range
// anchor such as "L12-40" or "L12"; non-line anchors (e.g. "p1", "page2") return (0, 0).
func parseBackgroundAnchorLines(anchor string) (int, int) {
	s := strings.TrimSpace(anchor)
	if len(s) < 2 || (s[0] != 'L' && s[0] != 'l') {
		return 0, 0
	}
	rest := s[1:]
	if dash := strings.IndexByte(rest, '-'); dash > 0 {
		start, err1 := strconv.Atoi(rest[:dash])
		end, err2 := strconv.Atoi(rest[dash+1:])
		if err1 == nil && err2 == nil && start > 0 && end >= start {
			return start, end
		}
		return 0, 0
	}
	if line, err := strconv.Atoi(rest); err == nil && line > 0 {
		return line, line
	}
	return 0, 0
}

// ============================================================================
// Compact blind-slot storage (`thin_vault_bg_chunks.slots_json`)
//
// One background chunk's pre-packed atoms are a materialised scoring index: local
// recall sums hash frequencies per chunk without touching the source file. Storing
// them as a JSON array of `{"h":…,"b":…,"f":…}` objects costs ~26 bytes per atom
// (≈8.6 KiB per chunk); the packed form is 11 bytes per atom (≈3.6 KiB), with the
// exact same values — no field is dropped.
//
// The column name is kept (`slots_json`) so builds of the previous shape keep
// working during a rolling restart: they still write JSON (accepted below) and
// simply score zero for packed rows until they restart. New writes are always the
// packed form; `migrateBackgroundSlotBlobs` rewrites legacy JSON rows once.
// ============================================================================

// atomSlotsMagic marks the packed form so a reader can tell it from legacy JSON.
const atomSlotsMagic = "RMA1"

// atomSlotStride is the packed width of one atom: H(8) + RawBitsX10(2) + ClassFlags(1).
const atomSlotStride = 11

// encodeAtomSlots packs atoms into `RMA1` + count(uint32 LE) + fixed-width records.
func encodeAtomSlots(atoms []BlindTokenAtom) []byte {
	buf := make([]byte, 0, len(atomSlotsMagic)+4+len(atoms)*atomSlotStride)
	buf = append(buf, atomSlotsMagic...)
	var count [4]byte
	binary.LittleEndian.PutUint32(count[:], uint32(len(atoms)))
	buf = append(buf, count[:]...)
	var record [atomSlotStride]byte
	for _, atom := range atoms {
		binary.LittleEndian.PutUint64(record[0:8], atom.H)
		binary.LittleEndian.PutUint16(record[8:10], atom.RawBitsX10)
		record[10] = atom.ClassFlags
		buf = append(buf, record[:]...)
	}
	return buf
}

// encodePacketWireBlob encodes a BlindPulsePacket (both flat Slots and SentenceFrames)
// into the canonical RMBP wire stream (`plain.EncodeWireBytes`), falling back to
// `encodeAtomSlots` only if the wire encoder rejects the frame shape.
func encodePacketWireBlob(pkt BlindPulsePacket) []byte {
	if len(pkt.SentenceFrames) > 0 || len(pkt.Slots) > 0 {
		if wire, err := plain.EncodeWireBytes(pkt.Slots, pkt.SentenceFrames); err == nil && len(wire) > 0 {
			return wire
		}
	}
	return encodeAtomSlots(pkt.Slots)
}

// decodeAtomSlots accepts the canonical RMBP wire stream (`RMBP`), the packed `RMA1`
// form, and legacy JSON rows. ok is false only for data that is none of them.
func decodeAtomSlots(raw []byte) (atoms []BlindTokenAtom, ok bool) {
	if len(raw) == 0 {
		return nil, true
	}
	if len(raw) >= 4 && string(raw[:4]) == "RMBP" {
		slots, _, err := plain.DecodeWireBytes(raw)
		if err != nil {
			return nil, false
		}
		return slots, true
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, true
	}
	if trimmed[0] == '[' {
		var legacy []BlindTokenAtom
		if err := json.Unmarshal(trimmed, &legacy); err != nil {
			return nil, false
		}
		return legacy, true
	}
	if len(raw) < len(atomSlotsMagic)+4 || !bytes.HasPrefix(raw, []byte(atomSlotsMagic)) {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint32(raw[4:8]))
	if count < 0 || len(raw) != len(atomSlotsMagic)+4+count*atomSlotStride {
		return nil, false
	}
	atoms = make([]BlindTokenAtom, count)
	for i := 0; i < count; i++ {
		offset := len(atomSlotsMagic) + 4 + i*atomSlotStride
		atoms[i] = BlindTokenAtom{
			H:          binary.LittleEndian.Uint64(raw[offset : offset+8]),
			RawBitsX10: binary.LittleEndian.Uint16(raw[offset+8 : offset+10]),
			ClassFlags: raw[offset+10],
		}
	}
	return atoms, true
}

// partitionFlatAtomsIntoFrames splits a flat `RMA1` / JSON atom slice that lost its
// sentence boundaries into synthetic sentence frames (~24 primary words per frame),
// preventing a 1024-token background chunk from collapsing into one 900-word sentence
// and generating O(W^2) false intra-sentence dyads before the mount re-syncs to RMBP.
func partitionFlatAtomsIntoFrames(atoms []BlindTokenAtom) [][]BlindTokenAtom {
	const primaryPerFrame = 24
	if len(atoms) == 0 {
		return nil
	}
	frames := make([][]BlindTokenAtom, 0, len(atoms)/32+1)
	start := 0
	primCount := 0
	for i, a := range atoms {
		isPrim := a.ClassFlags&plain.FlagIsHarmonicCompanion == 0
		if isPrim {
			if primCount >= primaryPerFrame && i > start {
				frames = append(frames, atoms[start:i])
				start = i
				primCount = 0
			}
			primCount++
		}
	}
	if start < len(atoms) {
		frames = append(frames, atoms[start:])
	}
	return frames
}

// decodeAtomSlotCoreTerms extracts canonical core.Term, counts, positions, and
// total slot length from an `RMBP` wire blob, a packed `RMA1` blob, or a legacy JSON row.
func decodeAtomSlotCoreTerms(raw []byte) (tMeta map[uint64]core.Term, tCounts map[uint64]int, tPos map[uint64][]core.TermPosition, docLen int, ok bool) {
	if len(raw) >= 4 && string(raw[:4]) == "RMBP" {
		slots, frames, err := plain.DecodeWireBytes(raw)
		if err != nil {
			return nil, nil, nil, 0, false
		}
		tMeta, tCounts, tPos = coreTermsFromPacket(slots, frames)
		docLen = len(slots)
		if docLen < 1 {
			docLen = 1
		}
		return tMeta, tCounts, tPos, docLen, true
	}
	atoms, ok := decodeAtomSlots(raw)
	if !ok {
		return nil, nil, nil, 0, false
	}
	tMeta, tCounts, tPos = coreTermsFromPacket(atoms, partitionFlatAtomsIntoFrames(atoms))
	docLen = len(atoms)
	if docLen < 1 {
		docLen = 1
	}
	return tMeta, tCounts, tPos, docLen, true
}

// migrateBackgroundSlotBlobs rewrites legacy JSON slot rows into the packed form.
// It is idempotent (a packed row never starts with '[') and tolerates a vault that
// has no background tables yet.
func migrateBackgroundSlotBlobs(db *sql.DB) error {
	if db == nil {
		return nil
	}
	// Fast O(1) boundary probe: check the earliest and latest rows by rowid before
	// scanning hundreds of megabytes of already-packed binary slot blobs.
	probeRows, err := db.Query(`
		SELECT substr(CAST(slots_json AS BLOB), 1, 4)
		FROM thin_vault_bg_chunks
		WHERE rowid IN (
			(SELECT MIN(rowid) FROM thin_vault_bg_chunks),
			(SELECT MAX(rowid) FROM thin_vault_bg_chunks)
		)`)
	if err != nil {
		return nil // background tables are created on first mount
	}
	needsScan := false
	seenProbe := 0
	for probeRows.Next() {
		var head []byte
		if err := probeRows.Scan(&head); err == nil {
			seenProbe++
			if len(head) > 0 && (head[0] == '[' || (string(head) != atomSlotsMagic && string(head) != "RMBP")) {
				needsScan = true
			}
		}
	}
	probeRows.Close()
	if seenProbe == 0 || !needsScan {
		return nil
	}
	rows, err := db.Query(`SELECT unit_id, slots_json FROM thin_vault_bg_chunks WHERE length(slots_json) > 0 AND substr(CAST(slots_json AS TEXT), 1, 1) = '['`)
	if err != nil {
		return nil
	}
	type legacyRow struct {
		unitID string
		blob   []byte
	}
	pending := make([]legacyRow, 0, 64)
	for rows.Next() {
		var unitID string
		var raw []byte
		if err := rows.Scan(&unitID, &raw); err != nil {
			continue
		}
		atoms, ok := decodeAtomSlots(raw)
		if !ok {
			continue
		}
		pending = append(pending, legacyRow{unitID: unitID, blob: encodeAtomSlots(atoms)})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, row := range pending {
		if _, err := tx.Exec(`UPDATE thin_vault_bg_chunks SET slots_json = ? WHERE unit_id = ?`, row.blob, row.unitID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
