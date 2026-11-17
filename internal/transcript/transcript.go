// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package transcript adapters for harness hooks and “import“.
//
// Supported shapes (auto-detected):
//
//   - JSONL with {type: "user"|"assistant", message: {role, content}}
//     (Claude Code / ZCode style), including content-block lists.
//   - JSONL with flat {role, content} rows.
//   - Plain text: one user message per non-empty line.
//
// Only user/assistant text blocks are imported. Tool calls, tool results,
// thinking blocks, meta rows and previously injected memory blocks are skipped.
package transcript

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/episodic"
	"github.com/rocareer/rolling-memory/internal/ids"
	"github.com/rocareer/rolling-memory/internal/speech"
	"github.com/rocareer/rolling-memory/internal/store"
)

// MaxTranscriptBytes caps one transcript file; larger files are refused.
const MaxTranscriptBytes = 64 * 1024 * 1024

// DefaultImportLines is import_transcript's max_lines default.
const DefaultImportLines = 500

// Message is one importable transcript turn.
type Message struct {
	Session    string `json:"session"`
	Turn       int64  `json:"turn"`
	Author     string `json:"author"`
	Text       string `json:"text"`
	OccurredAt int64  `json:"occurred_at"`
	Kind       string `json:"kind"`
}

// ReadMessages reads at most maxLines messages starting at line offset.
// It returns the messages plus (nextOffset, totalLines, size, mtime, mtimeNanoseconds).
func ReadMessages(path string, session *string, offset, maxLines int) ([]Message, int, int, int64, float64, error) {
	target := expandUser(path)
	info, err := os.Stat(target)
	if err != nil {
		// A missing transcript path returns the target path itself as the error
		// message; the CLI prints that message ("import <path>"; golden 208/400).
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, 0, 0, 0, errors.New(target)
		}
		return nil, 0, 0, 0, 0, err
	}
	size := info.Size()
	if size > MaxTranscriptBytes {
		return nil, 0, 0, 0, 0, fmt.Errorf("transcript is larger than %d MB", MaxTranscriptBytes/(1024*1024))
	}
	mtime := float64(info.ModTime().UnixNano()) / 1e9
	defaultSession := pyStem(target)
	if session != nil && *session != "" {
		defaultSession = *session
	}

	file, err := episodic.OpenJSONL(target)
	if err != nil {
		return nil, 0, 0, 0, 0, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, MaxTranscriptBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, 0, 0, 0, 0, readErr
	}
	if closeErr != nil {
		return nil, 0, 0, 0, 0, closeErr
	}
	if len(raw) > MaxTranscriptBytes {
		return nil, 0, 0, 0, 0, fmt.Errorf("transcript is larger than %d MB", MaxTranscriptBytes/(1024*1024))
	}
	lines := pyLines(pyDecodeUTF8(raw))
	jsonl := looksLikeJSONL(target, lines)
	if jsonl && len(raw) > 0 && raw[len(raw)-1] != '\n' && raw[len(raw)-1] != '\r' && !json.Valid([]byte(lines[len(lines)-1])) {
		lines = lines[:len(lines)-1]
	}

	messages := []Message{}
	nextOffset := offset
	totalLines := len(lines)
	for index, line := range lines {
		if index < offset {
			continue
		}
		if len(messages) >= maxLines {
			break
		}
		nextOffset = index + 1
		var message *Message
		if jsonl {
			message = parseJSONLLine(line, index, defaultSession)
		} else {
			message = parseTextLine(line, index, defaultSession)
		}
		if message != nil {
			messages = append(messages, *message)
		}
	}
	if totalLines < offset {
		nextOffset = 0
	}
	return messages, nextOffset, totalLines, size, mtime, nil
}

// ImportTranscript inserts the next transcript slice and stores the cursor in
// the standard working-memory state.
func ImportTranscript(st *store.Store, path, scope string, session *string, audience string, offset, maxLines int) (map[string]any, error) {
	target := expandUser(path)
	effectiveSession := pyStem(target)
	if session != nil && *session != "" {
		effectiveSession = *session
	}
	if audience == "" {
		audience = "shared"
	}
	messages, nextOffset, totalLines, size, mtime, err := ReadMessages(
		target, &effectiveSession, offset, maxLines)
	if err != nil {
		return nil, err
	}
	inserted := 0
	for _, message := range messages {
		messageSession := message.Session
		if messageSession == "" {
			messageSession = effectiveSession
		}
		occurred := message.OccurredAt
		_, created, err := st.AddTrace(scope, message.Text, store.TraceOptions{
			Session:    messageSession,
			Turn:       message.Turn,
			Kind:       message.Kind,
			Author:     message.Author,
			Audience:   audience,
			OccurredAt: &occurred,
			Meta:       map[string]any{"source": target},
		})
		if err != nil {
			return nil, err
		}
		if created {
			inserted++
		}
	}
	if err := saveCursor(st, target, effectiveSession, nextOffset, size, mtime); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":          true,
		"path":        target,
		"session":     effectiveSession,
		"messages":    len(messages),
		"inserted":    inserted,
		"offset":      nextOffset,
		"total_lines": totalLines,
		"size":        size,
	}, nil
}

// CursorFor returns the stored cursor row, if any.
func CursorFor(st *store.Store, path, session string) (map[string]any, bool, error) {
	row := st.DB().QueryRow(
		"SELECT path, session, offset, size, mtime, updated_at FROM transcript_cursor WHERE path = ? AND session = ?",
		expandUser(path), session,
	)
	var (
		cursorPath string
		cursorSess string
		offset     int64
		size       int64
		mtime      float64
		updatedAt  int64
	)
	if err := row.Scan(&cursorPath, &cursorSess, &offset, &size, &mtime, &updatedAt); err != nil {
		if isNoRows(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return map[string]any{
		"path":       cursorPath,
		"session":    cursorSess,
		"offset":     offset,
		"size":       size,
		"mtime":      mtime,
		"updated_at": updatedAt,
	}, true, nil
}

// CurrentOffset returns the cursor offset, or 0 when the file shrank or the
// cursor is missing.
func CurrentOffset(st *store.Store, path, session string) (int, error) {
	cursor, found, err := CursorFor(st, path, session)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	info, err := os.Stat(expandUser(path))
	if err != nil {
		return 0, nil
	}
	if info.Size() < asInt64(cursor["size"]) {
		return 0, nil
	}
	return int(asInt64(cursor["offset"])), nil
}

// ------------------------------------------------------------------ internals

func saveCursor(st *store.Store, path, session string, offset int, size int64, mtime float64) error {
	_, err := st.DB().Exec(
		`INSERT OR REPLACE INTO transcript_cursor(path, session, offset, size, mtime, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		path, session, offset, size, mtime, clock.Unix(),
	)
	return err
}

func looksLikeJSONL(path string, lines []string) bool {
	path = strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(path), ".zstd"), ".zst")
	switch filepath.Ext(path) {
	case ".jsonl", ".ndjson":
		return true
	case ".txt", ".text", ".md":
		return false
	}
	for _, line := range lines {
		stripped := pyStrip(line)
		if stripped != "" {
			return strings.HasPrefix(stripped, "{")
		}
	}
	return false
}

func parseJSONLLine(line string, index int, defaultSession string) *Message {
	stripped := pyStrip(line)
	if stripped == "" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(stripped), &obj); err != nil {
		return nil
	}
	if obj == nil {
		return nil
	}
	roleText, parts, occurred := episodic.ParseJSONLSpeech(line)
	if roleText == "" || len(parts) == 0 {
		return nil
	}
	text := speech.StripConversationNoise(strings.Join(parts, "\n"))
	if text == "" {
		return nil
	}
	message, _ := obj["message"].(map[string]any)
	if message == nil {
		message = map[string]any{}
	}
	var sessionValue any
	for _, candidate := range []any{obj["sessionId"], obj["session_id"], message["sessionId"]} {
		if truthy(candidate) {
			sessionValue = candidate
			break
		}
	}
	session := defaultSession
	if sessionValue != nil {
		session = pyStr(sessionValue)
	}
	return &Message{
		Session:    session,
		Turn:       int64(index + 1),
		Author:     roleText,
		Text:       text,
		OccurredAt: occurred,
		Kind:       "message",
	}
}

func parseTextLine(line string, index int, defaultSession string) *Message {
	text := ids.StripInjected(line)
	if text == "" {
		return nil
	}
	return &Message{
		Session:    defaultSession,
		Turn:       int64(index + 1),
		Author:     "user",
		Text:       text,
		OccurredAt: 0,
		Kind:       "message",
	}
}

// pyDecodeUTF8 mirrors bytes.decode("utf-8", "replace"): one U+FFFD per
// invalid (maximal valid-prefix) subsequence, then resynchronise.
func pyDecodeUTF8(raw []byte) string {
	var out strings.Builder
	for index := 0; index < len(raw); {
		lead := raw[index]
		if lead < 0x80 {
			out.WriteByte(lead)
			index++
			continue
		}
		length := 0
		low := byte(0x80)
		high := byte(0xBF)
		switch {
		case lead >= 0xC2 && lead <= 0xDF:
			length = 2
		case lead == 0xE0:
			length, low = 3, 0xA0
		case lead >= 0xE1 && lead <= 0xEC:
			length = 3
		case lead == 0xED:
			length, high = 3, 0x9F
		case lead >= 0xEE && lead <= 0xEF:
			length = 3
		case lead == 0xF0:
			length, low = 4, 0x90
		case lead >= 0xF1 && lead <= 0xF3:
			length = 4
		case lead == 0xF4:
			length, high = 4, 0x8F
		default:
			out.WriteRune(utf8.RuneError)
			index++
			continue
		}
		consumed := 1
		complete := true
		for offset := 1; offset < length; offset++ {
			if index+offset >= len(raw) {
				complete = false
				consumed = offset
				break
			}
			current := raw[index+offset]
			if offset == 1 {
				if current < low || current > high {
					complete = false
					consumed = 1
					break
				}
			} else if current < 0x80 || current > 0xBF {
				complete = false
				consumed = offset
				break
			}
			consumed = offset + 1
		}
		if !complete || consumed < length {
			out.WriteRune(utf8.RuneError)
			index += consumed
			continue
		}
		out.Write(raw[index : index+length])
		index += length
	}
	return out.String()
}

// pyLines splits text on universal newlines (\r\n, \r, \n) without yielding a
// trailing empty element when text ends with a newline.
func pyLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// pyStem mirrors Path(path).stem (last suffix dropped, dotfiles keep their
// name).
func pyStem(path string) string {
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		base = path
	}
	index := strings.LastIndex(base, ".")
	if index <= 0 {
		return base
	}
	return base[:index]
}

func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return filepath.Clean(path)
}

func pyStrip(value string) string {
	return strings.TrimFunc(value, isPySpace)
}

func isPySpace(char rune) bool {
	switch char {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u0085', '\u00a0', '\u1680',
		'\u2028', '\u2029', '\u202f', '\u205f', '\u3000':
		return true
	}
	if char >= '\u001c' && char <= '\u001f' {
		return true
	}
	if char >= '\u2000' && char <= '\u200a' {
		return true
	}
	return false
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	}
	return true
}

func pyStr(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	}
	return fmt.Sprint(value)
}

func asInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	}
	return 0
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
