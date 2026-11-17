// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package episodic

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/rocareer/rolling-memory/internal/speech"
)

// MaxJSONLLineBytes is an engineering safety limit for a single transcript row.
const MaxJSONLLineBytes = 64 * 1024 * 1024

// MaxJSONLDecoderMemory bounds a compressed frame's window, not transcript size.
const MaxJSONLDecoderMemory = 128 * 1024 * 1024

type jsonlReader struct {
	file    *os.File
	decoder *zstd.Decoder
	digest  hash.Hash
}

func (r *jsonlReader) Read(p []byte) (int, error) {
	if r.decoder != nil {
		return r.decoder.Read(p)
	}
	n, err := r.file.Read(p)
	if n > 0 {
		_, _ = r.digest.Write(p[:n])
	}
	return n, err
}

func (r *jsonlReader) Close() error {
	if r.decoder != nil {
		r.decoder.Close()
	}
	return r.file.Close()
}

// OpenJSONL opens a plain or zstd-compressed transcript as a streaming reader.
func OpenJSONL(path string) (io.ReadCloser, error) {
	return openJSONL(path)
}

func openJSONL(path string) (*jsonlReader, error) {
	file, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	reader := &jsonlReader{file: file, digest: sha256.New()}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".zst") || strings.HasSuffix(lower, ".zstd") {
		reader.decoder, err = zstd.NewReader(io.TeeReader(file, reader.digest), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(MaxJSONLDecoderMemory))
		if err != nil {
			file.Close()
			return nil, err
		}
	}
	return reader, nil
}

// VisitJSONLTurns streams turns without materializing the transcript. The
// callback's complete flag requires an explicit successful completion event.
func VisitJSONLTurns(ctx context.Context, path string, visit func(JSONLTurn, bool) error) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	completed, dshCompleted, sourceDigest, err := jsonlCompletionEvidence(ctx, path)
	if err != nil {
		return err
	}
	currentDigest, err := hashJSONLSource(ctx, path)
	if err != nil {
		return err
	}
	if currentDigest != sourceDigest {
		return fmt.Errorf("episodic: transcript changed during scan: %s", path)
	}
	after, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || !os.SameFile(info, after) {
		return fmt.Errorf("episodic: transcript changed during scan: %s", path)
	}
	if err := visitJSONLTurns(ctx, path, completed, dshCompleted, visit); err != nil {
		return err
	}
	after, err = os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || !os.SameFile(info, after) {
		return fmt.Errorf("episodic: transcript changed during scan: %s", path)
	}
	return nil
}

func visitJSONLTurns(ctx context.Context, path string, codexCompleted map[string]bool, dshCompleted map[int64]bool, visit func(JSONLTurn, bool) error) error {
	file, err := OpenJSONL(path)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var current *JSONLTurn
	var pendingAssist []Segment
	complete := false
	codexTurnID := ""
	codexTurn := false
	var dshActive, dshTurn int64
	dshActiveSet, dshTurnSet, dshMessage := false, false, false
	nextIndex := 0
	isComplete := func() bool {
		if codexTurn {
			return codexTurnID != "" && codexCompleted[codexTurnID]
		}
		if dshMessage {
			return dshTurnSet && dshCompleted[dshTurn]
		}
		return complete
	}
	flushPending := func() {
		if current == nil || len(pendingAssist) == 0 {
			pendingAssist = pendingAssist[:0]
			return
		}
		for _, seg := range pendingAssist {
			seg.Index = len(current.Segments)
			current.Segments = append(current.Segments, seg)
			if seg.OccurredAt > current.OccurredAt {
				current.OccurredAt = seg.OccurredAt
			}
		}
		pendingAssist = pendingAssist[:0]
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := readJSONLLine(reader)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		unterminated := errors.Is(readErr, io.EOF)
		if unterminated && !json.Valid([]byte(strings.TrimSpace(line))) {
			break
		}
		if event, turn, ok := dshJSONLTurnEvent(line); !unterminated && event == "turn/start" {
			dshActive, dshActiveSet = turn, ok
		} else if !unterminated && event == "turn/end" {
			dshActiveSet = false
		}
		if isJSONLToolCallLine(line) {
			pendingAssist = pendingAssist[:0]
			complete = false
		} else {
			if current != nil && isJSONLActivityLine(line) {
				complete = false
			}
			role, texts, occurredAt := parseJSONLLine(line)
			if role != "" && len(texts) != 0 {
				if role == RoleUser {
					flushPending()
					if current != nil && len(current.Segments) > 0 {
						if err := visit(*current, isComplete()); err != nil {
							return err
						}
					}
					current = &JSONLTurn{Index: nextIndex}
					codexTurnID, codexTurn = codexJSONLMessageTurnID(line)
					dshMessage = isDSHJSONLUser(line)
					dshTurn, dshTurnSet = dshActive, dshActiveSet
					nextIndex++
				}
				if current != nil {
					complete = false
					if role == RoleAssistant {
						for _, text := range texts {
							pendingAssist = append(pendingAssist, Segment{Role: role, Text: text, OccurredAt: occurredAt})
						}
					} else {
						for _, text := range texts {
							current.Segments = append(current.Segments, Segment{Role: role, Text: text, OccurredAt: occurredAt, Index: len(current.Segments)})
						}
						if occurredAt > current.OccurredAt {
							current.OccurredAt = occurredAt
						}
					}
				}
			}
			if current != nil && !unterminated && !codexTurn && !dshMessage && isJSONLCompletionLine(line) {
				complete = true
			}
		}
		if unterminated {
			break
		}
	}
	flushPending()
	if current != nil && len(current.Segments) > 0 {
		return visit(*current, isComplete())
	}
	return nil
}

// maxCodexCompletionRecords limits the memory used by the per-file completion
// index. Large or malformed files fail visibly rather than allocating without bound.
const maxCodexCompletionRecords = 100000

type codexCompletionState struct {
	started   bool
	ended     bool
	valid     bool
	completed bool
}

func jsonlCompletionEvidence(ctx context.Context, path string) (map[string]bool, map[int64]bool, [32]byte, error) {
	file, err := openJSONL(path)
	if err != nil {
		return nil, nil, [32]byte{}, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	states := make(map[string]codexCompletionState)
	dshStates := make(map[int64]codexCompletionState)
	var dshActive, dshLastEnded int64
	dshActiveSet, dshLastEndedSet := false, false
	active, lastEnded := "", ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, [32]byte{}, err
		}
		line, readErr := readJSONLLine(reader)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, nil, [32]byte{}, readErr
		}
		if event, turn, ok := dshJSONLTurnEvent(line); event != "" {
			if event == "turn/start" {
				dshLastEndedSet = false
				if dshActiveSet {
					old := dshStates[dshActive]
					old.valid = false
					dshStates[dshActive] = old
				}
				dshActive, dshActiveSet = turn, ok
				if ok {
					state, exists := dshStates[turn]
					if !exists && len(dshStates) >= maxCodexCompletionRecords {
						return nil, nil, [32]byte{}, fmt.Errorf("episodic: DSH completion index exceeds %d turns", maxCodexCompletionRecords)
					}
					if exists {
						state.valid = false
					} else {
						state = codexCompletionState{started: true, valid: true}
					}
					dshStates[turn] = state
				}
			} else if ok {
				state, exists := dshStates[turn]
				if !exists && len(dshStates) >= maxCodexCompletionRecords {
					return nil, nil, [32]byte{}, fmt.Errorf("episodic: DSH completion index exceeds %d turns", maxCodexCompletionRecords)
				}
				if !exists || state.ended || !dshActiveSet || dshActive != turn {
					state.valid = false
				}
				state.ended = true
				state.completed = isJSONLCompletionLine(line)
				dshStates[turn] = state
				dshActiveSet = false
				dshLastEnded, dshLastEndedSet = turn, true
			}
			continue
		}
		if dshLastEndedSet && isDSHJSONLActivity(line) {
			state := dshStates[dshLastEnded]
			state.valid = false
			dshStates[dshLastEnded] = state
		}
		if lastEnded != "" && isCodexJSONLActivity(line) {
			state := states[lastEnded]
			state.valid = false
			states[lastEnded] = state
		}
		if id, ok := codexJSONLMessageTurnID(line); ok {
			if id != "" && id != active {
				state, exists := states[id]
				if !exists && len(states) >= maxCodexCompletionRecords {
					return nil, nil, [32]byte{}, fmt.Errorf("episodic: Codex completion index exceeds %d turns", maxCodexCompletionRecords)
				}
				state.valid = false
				states[id] = state
			}
			continue
		}
		if !strings.Contains(line, `"event_msg"`) {
			continue
		}
		var obj struct {
			Type    string `json:"type"`
			Payload struct {
				Type   string `json:"type"`
				TurnID string `json:"turn_id"`
				Error  any    `json:"error"`
			} `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil || obj.Type != "event_msg" {
			continue
		}
		id := strings.TrimSpace(obj.Payload.TurnID)
		switch obj.Payload.Type {
		case "task_started":
			lastEnded = ""
			if active != "" {
				old := states[active]
				old.valid = false
				states[active] = old
			}
			active = id
			if id == "" {
				continue
			}
			state, exists := states[id]
			if !exists && len(states) >= maxCodexCompletionRecords {
				return nil, nil, [32]byte{}, fmt.Errorf("episodic: Codex completion index exceeds %d turns", maxCodexCompletionRecords)
			}
			if exists {
				state.valid = false
			} else {
				state = codexCompletionState{started: true, valid: true}
			}
			states[id] = state
		case "task_complete", "turn_aborted":
			if id == "" {
				continue
			}
			state, exists := states[id]
			if !exists && len(states) >= maxCodexCompletionRecords {
				return nil, nil, [32]byte{}, fmt.Errorf("episodic: Codex completion index exceeds %d turns", maxCodexCompletionRecords)
			}
			if !exists || state.ended || active != id {
				state.valid = false
			}
			state.ended = true
			state.completed = obj.Payload.Type == "task_complete" && !truthy(obj.Payload.Error)
			states[id] = state
			lastEnded = id
			if active == id {
				active = ""
			}
		}
	}
	completed := make(map[string]bool, len(states))
	for id, state := range states {
		if state.started && state.ended && state.valid && state.completed {
			completed[id] = true
		}
	}
	dshCompleted := make(map[int64]bool, len(dshStates))
	for turn, state := range dshStates {
		if state.started && state.ended && state.valid && state.completed {
			dshCompleted[turn] = true
		}
	}
	var digest [32]byte
	copy(digest[:], file.digest.Sum(nil))
	return completed, dshCompleted, digest, nil
}

func hashJSONLSource(ctx context.Context, path string) ([32]byte, error) {
	var digest [32]byte
	file, err := os.Open(path)
	if err != nil {
		return digest, err
	}
	defer file.Close()
	h := sha256.New()
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return digest, err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			_, _ = h.Write(buffer[:n])
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return digest, readErr
			}
			break
		}
	}
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

func dshJSONLTurnEvent(line string) (string, int64, bool) {
	if !strings.Contains(line, `"turn/start"`) && !strings.Contains(line, `"turn/end"`) {
		return "", 0, false
	}
	var obj struct {
		Type string `json:"type"`
		Data struct {
			Turn *int64 `json:"turn"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(line), &obj) != nil || (obj.Type != "turn/start" && obj.Type != "turn/end") {
		return "", 0, false
	}
	if obj.Data.Turn == nil {
		return obj.Type, 0, false
	}
	return obj.Type, *obj.Data.Turn, true
}

func isDSHJSONLActivity(line string) bool {
	if !strings.Contains(line, `"assistant/`) && !strings.Contains(line, `"tool/`) {
		return false
	}
	var obj struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(line), &obj) != nil {
		return false
	}
	switch obj.Type {
	case "assistant/message", "assistant/chunk", "tool/call", "tool/result":
		return true
	}
	return false
}

func isCodexJSONLActivity(line string) bool {
	if !strings.Contains(line, `"response_item"`) {
		return false
	}
	var obj struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			Role string `json:"role"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &obj) != nil || obj.Type != "response_item" {
		return false
	}
	switch obj.Payload.Type {
	case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "web_search_call", "local_shell_call":
		return true
	case "message":
		return obj.Payload.Role == "assistant"
	}
	return false
}

func isDSHJSONLUser(line string) bool {
	if !strings.Contains(line, `"user/message"`) {
		return false
	}
	var obj struct {
		Type string `json:"type"`
	}
	return json.Unmarshal([]byte(line), &obj) == nil && obj.Type == "user/message"
}

func codexJSONLMessageTurnID(line string) (string, bool) {
	if !strings.Contains(line, `"response_item"`) {
		return "", false
	}
	var obj struct {
		Type    string `json:"type"`
		Payload struct {
			Type     string `json:"type"`
			Role     string `json:"role"`
			Metadata struct {
				TurnID string `json:"turn_id"`
			} `json:"internal_chat_message_metadata_passthrough"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &obj) != nil || obj.Type != "response_item" || obj.Payload.Type != "message" || obj.Payload.Role != "user" {
		return "", false
	}
	return strings.TrimSpace(obj.Payload.Metadata.TurnID), true
}

func readJSONLLine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > MaxJSONLLineBytes {
			return "", fmt.Errorf("episodic: JSONL row exceeds %d bytes", MaxJSONLLineBytes)
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return string(line), err
	}
}

func isJSONLActivityLine(line string) bool {
	var obj map[string]any
	if json.Unmarshal([]byte(line), &obj) != nil {
		return false
	}
	switch obj["type"] {
	case "assistant/message", "assistant/chunk", "tool/result":
		return true
	case "event_msg":
		payload, _ := obj["payload"].(map[string]any)
		switch payload["type"] {
		case "task_started", "turn_aborted":
			return true
		case "task_complete":
			return truthy(payload["error"])
		}
	case "turn/end":
		data, _ := obj["data"].(map[string]any)
		reason, _ := data["reason"].(map[string]any)
		return reason["kind"] != "completed"
	}
	return false
}

func isJSONLCompletionLine(line string) bool {
	var obj map[string]any
	if json.Unmarshal([]byte(line), &obj) != nil {
		return false
	}
	switch obj["type"] {
	case "turn/end":
		data, _ := obj["data"].(map[string]any)
		reason, _ := data["reason"].(map[string]any)
		kind, _ := reason["kind"].(string)
		return kind == "completed"
	case "event_msg":
		payload, _ := obj["payload"].(map[string]any)
		typeName, _ := payload["type"].(string)
		return typeName == "task_complete" && !truthy(payload["error"])
	case "assistant":
		message, _ := obj["message"].(map[string]any)
		return message["stop_reason"] == "end_turn"
	}
	return false
}

// VisitZCodeTurns streams one session from a SQLite query snapshot. The callback
// receives closed=false only for the tail, and may stop iteration with an error.
func VisitZCodeTurns(ctx context.Context, db *sql.DB, sessionID string, visit func(Turn, bool) error) error {
	return VisitZCodeTurnsFrom(ctx, db, sessionID, "", visit)
}

// ZCodeQueryer is implemented by both a database and a transaction.
type ZCodeQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// VisitZCodeTurnsFrom streams one session starting at startUserMsgID when present
// in the session's sequence order, falling back to the start of the session otherwise.
func VisitZCodeTurnsFrom(ctx context.Context, db ZCodeQueryer, sessionID, startUserMsgID string, visit func(Turn, bool) error) error {
	if db == nil || sessionID == "" {
		return errors.New("episodic: zcode session requires a database and session id")
	}
	var mRows *sql.Rows
	var err error
	var startSeq sql.NullInt64
	if strings.TrimSpace(startUserMsgID) != "" {
		_ = db.QueryRowContext(ctx, `SELECT sequence FROM message WHERE session_id = ? AND id = ?`, sessionID, strings.TrimSpace(startUserMsgID)).Scan(&startSeq)
	}
	if startSeq.Valid {
		mRows, err = db.QueryContext(ctx, `
			SELECT m.id, COALESCE(m.time_created, 0), json_extract(m.data, '$.role')
			FROM message m
			WHERE m.session_id = ? AND m.sequence >= ?
			ORDER BY m.sequence ASC`, sessionID, startSeq.Int64)
	} else {
		mRows, err = db.QueryContext(ctx, `
			SELECT m.id, COALESCE(m.time_created, 0), json_extract(m.data, '$.role')
			FROM message m
			WHERE m.session_id = ?
			ORDER BY m.sequence ASC`, sessionID)
	}
	if err != nil {
		return err
	}
	type msgHeader struct {
		id      string
		ts      int64
		speaker string
	}
	var headers []msgHeader
	for mRows.Next() {
		var msgID, role sql.NullString
		var timestamp int64
		if err := mRows.Scan(&msgID, &timestamp, &role); err != nil {
			mRows.Close()
			return err
		}
		speaker := normalizeRole(role.String)
		if speaker == "" {
			continue
		}
		headers = append(headers, msgHeader{id: msgID.String, ts: timestamp, speaker: speaker})
	}
	if err := mRows.Err(); err != nil {
		mRows.Close()
		return err
	}
	if err := mRows.Close(); err != nil {
		return err
	}

	var current *Turn
	const batchSize = 256
	for start := 0; start < len(headers); start += batchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + batchSize
		if end > len(headers) {
			end = len(headers)
		}
		chunk := headers[start:end]
		marks := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, h := range chunk {
			marks[i] = "?"
			args[i] = h.id
		}
		pRows, err := db.QueryContext(ctx, `
			SELECT p.message_id, json_extract(p.data, '$.text')
			FROM part p
			WHERE p.message_id IN (`+strings.Join(marks, ",")+`)
			  AND `+ZCodeTextPartFilter+`
			ORDER BY p.message_id, p.sequence ASC`, args...)
		if err != nil {
			return err
		}
		partsByMsg := make(map[string][]string, len(chunk))
		for pRows.Next() {
			var msgID, text sql.NullString
			if err := pRows.Scan(&msgID, &text); err != nil {
				pRows.Close()
				return err
			}
			if cleaned := speech.StripConversationNoise(text.String); cleaned != "" {
				partsByMsg[msgID.String] = append(partsByMsg[msgID.String], cleaned)
			}
		}
		pErr := pRows.Err()
		_ = pRows.Close()
		if pErr != nil {
			return pErr
		}
		for _, h := range chunk {
			if err := ctx.Err(); err != nil {
				return err
			}
			cleanedParts := partsByMsg[h.id]
			if len(cleanedParts) == 0 {
				continue
			}
			if h.speaker == RoleUser && (current == nil || h.id != current.UserMsgID) {
				if current != nil {
					if err := visit(*current, true); err != nil {
						return err
					}
				}
				current = &Turn{UserMsgID: h.id}
			}
			if current != nil {
				for _, cleaned := range cleanedParts {
					current.Segments = append(current.Segments, Segment{Role: h.speaker, Text: cleaned, OccurredAt: h.ts, Index: len(current.Segments)})
				}
				if h.ts > current.OccurredAt {
					current.OccurredAt = h.ts
				}
			}
		}
	}
	if current != nil {
		return visit(*current, false)
	}
	return nil
}
