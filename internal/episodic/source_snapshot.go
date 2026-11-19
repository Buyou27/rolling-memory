// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package episodic

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// maxSourceSnapshotBytes bounds how much decompressed transcript one snapshot may
// materialize. A snapshot keeps every enumerated turn in memory for the life of one
// ingest pass — the whole source is the memory unit — so the ceiling reuses the
// 256 MiB envelope the live pointer hydration path already applies to one evidence
// read. Above it the constructor returns ErrSourceSnapshotTooLarge and the caller
// falls back to the per-pointer streaming path instead of allocating without bound.
const maxSourceSnapshotBytes int64 = 256 << 20

// ErrSourceSnapshotTooLarge reports that the decompressed source exceeds
// maxSourceSnapshotBytes: the caller falls back to resolving pointers against the
// streaming scan.
var ErrSourceSnapshotTooLarge = errors.New("episodic: transcript exceeds the source snapshot size limit")

// jsonlRefPrefix is the pointer scheme the ingest writes for JSONL transcripts
// (`@ref:jsonl:<absolute path>:<turn index>` plus `#<k>` / `#<k>.<c>`).
const jsonlRefPrefix = "@ref:jsonl:"

// SourceSnapshot is one frozen parse of one JSONL transcript source: a single
// streaming pass over the file (plain, `.zst` and `.zstd` through OpenJSONL) that
// enumerates every closed turn under the same completion evidence
// `VisitJSONLTurns` applies, computes the raw-byte SHA-256, and keeps the parsed
// turns so every later `@ref:jsonl:` pointer of this source resolves without
// touching the source again.
//
// The snapshot is the source's state at construction time: a rewrite on disk never
// changes what it answers, it only makes Stale report the change. Stale is a hint —
// the authoritative submit-time proof is the caller's re-verification of Digest and
// of the source MAC.
//
// A zero value — or any snapshot after Close — answers nothing and never panics:
// Resolve and Segments report false, Turns reports -1, Completed reports false,
// Digest and SessionID report false, Stale reports true. Queries are safe for
// concurrent use and Close may run concurrently with them.
type SourceSnapshot struct {
	mu    sync.RWMutex
	state *sourceSnapshotState
	path  string
	info  os.FileInfo
}

type sourceSnapshotState struct {
	digest    [32]byte
	sessionID string
	turns     []sourceSnapshotTurn
}

type sourceSnapshotTurn struct {
	occurredAt int64
	completed  bool
	segments   []Segment
}

// sourceSnapshotDraft is one enumerated turn while the pass is still running: the
// completion verdict can only be resolved once the evidence index has seen the
// whole file.
type sourceSnapshotDraft struct {
	occurredAt int64
	segments   []Segment
	completion jsonlTurnCompletion
}

// NewSourceSnapshot parses one JSONL transcript source once under the default
// resource bound. The path may be plain, `.zst` or `.zstd`; the digest is always
// the SHA-256 of the bytes as stored on disk.
func NewSourceSnapshot(path string) (*SourceSnapshot, error) {
	return newSourceSnapshot(context.Background(), strings.TrimSpace(path), maxSourceSnapshotBytes)
}

// NewSourceSnapshotContext is NewSourceSnapshot with a caller-supplied cancellation
// context: a cancelled context fails construction.
func NewSourceSnapshotContext(ctx context.Context, path string) (*SourceSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return newSourceSnapshot(ctx, strings.TrimSpace(path), maxSourceSnapshotBytes)
}

func newSourceSnapshot(ctx context.Context, path string, byteLimit int64) (*SourceSnapshot, error) {
	if path == "" {
		return nil, errors.New("episodic: empty jsonl path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("episodic: source snapshot requires a regular file: %s", path)
	}
	file, err := openJSONL(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	index := newJSONLCompletionIndex()
	draft := make([]sourceSnapshotDraft, 0, 16)
	decompressed := int64(0)
	scan := &jsonlTurnScan{visit: func(turn JSONLTurn, completion jsonlTurnCompletion) error {
		// The scan drops its reference to each turn when the next user message opens,
		// so the slice it hands over is exclusively the snapshot's from here on.
		draft = append(draft, sourceSnapshotDraft{occurredAt: turn.OccurredAt, segments: turn.Segments, completion: completion})
		return nil
	}}
	err = walkJSONLLines(ctx, bufio.NewReader(file), func(line string, unterminated bool) error {
		decompressed += int64(len(line))
		if decompressed > byteLimit {
			return fmt.Errorf("%w: %s exceeds %d decompressed bytes", ErrSourceSnapshotTooLarge, path, byteLimit)
		}
		if err := index.observe(line); err != nil {
			return err
		}
		return scan.feed(line, unterminated)
	})
	if err != nil {
		return nil, err
	}
	if err := scan.finish(); err != nil {
		return nil, err
	}
	// One pass cannot re-verify the raw digest, so the before/after stat is the
	// append, truncate and rotation detector; a rewrite that preserved size and
	// mtime is caught later by Stale and by the caller's submit-time verification.
	after, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || !os.SameFile(info, after) {
		return nil, fmt.Errorf("episodic: transcript changed during scan: %s", path)
	}
	// The evidence index has now seen the whole file, so every verdict is exactly
	// the one the streaming scan resolves against its own evidence pass.
	codexCompleted := index.completedTurns()
	dshCompleted := index.completedDSHTurns()
	turns := make([]sourceSnapshotTurn, 0, len(draft))
	for _, entry := range draft {
		turns = append(turns, sourceSnapshotTurn{
			occurredAt: entry.occurredAt,
			completed:  entry.completion.completed(codexCompleted, dshCompleted),
			segments:   entry.segments,
		})
	}
	var digest [32]byte
	copy(digest[:], file.digest.Sum(nil))
	return &SourceSnapshot{
		path: path,
		info: info,
		state: &sourceSnapshotState{
			digest:    digest,
			sessionID: transcriptSessionStem(path),
			turns:     turns,
		},
	}, nil
}

// Close releases the retained parse and makes every query reject. It is idempotent
// and safe on a nil receiver; a closed snapshot answers nothing (see the type doc).
func (s *SourceSnapshot) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.state = nil
	s.mu.Unlock()
	return nil
}

// load returns the frozen state, or nil for a zero value or a closed snapshot.
func (s *SourceSnapshot) load() *sourceSnapshotState {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Turns reports the number of enumerated turns. A zero value or a closed snapshot
// reports -1 — never a silent 0, which is a valid answer for an empty transcript.
func (s *SourceSnapshot) Turns() int {
	state := s.load()
	if state == nil {
		return -1
	}
	return len(state.turns)
}

// Segments returns a copy of turn idx's speech segments in conversation order, so
// the caller cannot mutate the frozen parse.
func (s *SourceSnapshot) Segments(idx int) ([]Segment, bool) {
	state := s.load()
	if state == nil || idx < 0 || idx >= len(state.turns) {
		return nil, false
	}
	segments := state.turns[idx].segments
	out := make([]Segment, len(segments))
	copy(out, segments)
	return out, true
}

// OccurredAt reports the newest known timestamp (Unix seconds, 0 = unknown) of turn
// idx, the turn time the ingest stamps its units with.
func (s *SourceSnapshot) OccurredAt(idx int) (int64, bool) {
	state := s.load()
	if state == nil || idx < 0 || idx >= len(state.turns) {
		return 0, false
	}
	return state.turns[idx].occurredAt, true
}

// Completed reports whether turn idx carries an explicit successful completion
// event that no later activity contradicted — the closed verdict the streaming scan
// reports. Completion is never assumed: a zero value, a closed snapshot or an
// out-of-range index reports false.
func (s *SourceSnapshot) Completed(idx int) bool {
	state := s.load()
	if state == nil || idx < 0 || idx >= len(state.turns) {
		return false
	}
	return state.turns[idx].completed
}

// Digest returns the SHA-256 of the source's raw bytes as read for this snapshot —
// the compressed bytes for `.zst` / `.zstd`, the same value the streaming scan
// compares against its own hash of the file.
func (s *SourceSnapshot) Digest() ([32]byte, bool) {
	state := s.load()
	if state == nil {
		return [32]byte{}, false
	}
	return state.digest, true
}

// SessionID returns the transcript's session identifier: the file name stem, the
// rule `internal/harness.TranscriptSessionID` applies to every host whose transcript
// is named after the session (compression suffixes are not part of the id). The
// harness package is a host-adapter layer above this one, so the one-line stem rule
// is repeated here rather than imported; keep the two in lockstep.
func (s *SourceSnapshot) SessionID() (string, bool) {
	state := s.load()
	if state == nil {
		return "", false
	}
	return state.sessionID, true
}

// Stale reports whether the source no longer matches the frozen snapshot: the path
// is unreachable, is not a regular file any more, or changed its size,
// modification time, file identity or raw bytes. The stat answer is used first; an
// unchanged stat falls through to re-hashing the raw bytes (no decompression), so
// an in-place rewrite that restored size and mtime is still reported stale.
//
// This is a hint that answers "is the frozen view still valid", not a proof: the
// authoritative submit-time check stays with the caller, which re-verifies Digest
// and the source MAC of every pointer it submits.
func (s *SourceSnapshot) Stale() bool {
	if s == nil || s.path == "" || s.info == nil {
		return true
	}
	state := s.load()
	if state == nil {
		return true
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return true
	}
	if !info.Mode().IsRegular() || info.Size() != s.info.Size() || !info.ModTime().Equal(s.info.ModTime()) || !os.SameFile(info, s.info) {
		return true
	}
	digest, err := hashJSONLSource(context.Background(), s.path)
	if err != nil {
		return true
	}
	return digest != state.digest
}

// Resolve hydrates one segment pointer of this source:
// `@ref:jsonl:<path>:<turn>#<k>` names segment k of turn `<turn>`, and
// `@ref:jsonl:<path>:<turn>#<k>.<c>` names chunk c of that segment, re-split from
// the frozen parse under the same deterministic tokenz-r rung the ingest used.
//
// The pointer must name this snapshot's own path (exact match) and an existing
// turn, segment and chunk; anything else — another path, another pointer scheme, a
// whole-turn pointer, a malformed or out-of-range index — reports false, so the
// caller can fall back to the live resolver.
func (s *SourceSnapshot) Resolve(refPointer string) (text string, ok bool) {
	state := s.load()
	if state == nil {
		return "", false
	}
	refPath, turnIndex, segmentIndex, chunkIndex, hasChunk, ok := parseJSONLSegmentRef(refPointer)
	if !ok || refPath != s.path {
		return "", false
	}
	if turnIndex >= len(state.turns) {
		return "", false
	}
	segments := state.turns[turnIndex].segments
	if segmentIndex >= len(segments) {
		return "", false
	}
	rendered := Text(segments[segmentIndex].Role, segments[segmentIndex].Text)
	if !hasChunk {
		return rendered, true
	}
	chunks := Chunk(rendered, TokenBudget)
	if chunkIndex >= len(chunks) {
		return "", false
	}
	return chunks[chunkIndex], true
}

// parseJSONLSegmentRef parses one segment pointer of the jsonl form. The suffix
// grammar is the one the ingest writes (`SegmentUnits`) and the live resolver parses
// (`localvault.splitPointerSegment`): `#<k>` names the whole segment, `#<k>.<c>`
// chunk c of it, and a tail without a `#` suffix is a whole-turn pointer, which is
// not a segment and is rejected here.
func parseJSONLSegmentRef(refPointer string) (path string, turnIndex, segmentIndex, chunkIndex int, hasChunk, ok bool) {
	trimmed := strings.TrimSpace(refPointer)
	if !strings.HasPrefix(trimmed, jsonlRefPrefix) {
		return
	}
	rest := strings.TrimPrefix(trimmed, jsonlRefPrefix)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 {
		return
	}
	path = rest[:lastColon]
	tail := rest[lastColon+1:]
	hash := strings.LastIndex(tail, "#")
	if hash <= 0 || hash == len(tail)-1 {
		return
	}
	suffix := tail[hash+1:]
	segmentPart, chunkPart := suffix, ""
	if dot := strings.Index(suffix, "."); dot >= 0 {
		segmentPart, chunkPart = suffix[:dot], suffix[dot+1:]
	}
	segment, err := strconv.Atoi(segmentPart)
	if err != nil || segment < 0 {
		return
	}
	turn, err := strconv.Atoi(strings.TrimSpace(tail[:hash]))
	if err != nil || turn < 0 {
		return
	}
	if chunkPart != "" {
		chunk, err := strconv.Atoi(chunkPart)
		if err != nil || chunk < 0 {
			return
		}
		chunkIndex, hasChunk = chunk, true
	}
	return path, turn, segment, chunkIndex, hasChunk, true
}

// transcriptSessionStem derives the session id from one transcript's file name:
// the base name without its compression and format suffix. It mirrors the stem rule
// of `internal/harness.TranscriptSessionID` for hosts that name the transcript after
// the session; DSH (which carries the id in the enclosing directory) is not
// addressable from the path alone and has no snapshot form.
func transcriptSessionStem(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".zstd")
	stem = strings.TrimSuffix(stem, ".zst")
	return strings.TrimSuffix(stem, filepath.Ext(stem))
}
