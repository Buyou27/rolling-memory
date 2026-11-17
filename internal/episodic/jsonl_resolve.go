// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package episodic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

type cachedJSONLTurns struct {
	sum   [32]byte
	bytes int
	turns [][]Segment
}

var (
	jsonlTurnsCache      sync.Map
	jsonlTurnsCacheCount int
	jsonlTurnsCacheBytes int
	jsonlTurnsCacheMu    sync.Mutex
)

const (
	maxJSONLTurnsCacheFiles = 256
	// Cache limits bound retained parsed text and per-segment bookkeeping. Large
	// transcripts still resolve through the streaming path without being cached.
	maxJSONLTurnsCacheEntryBytes = 8 * 1024 * 1024
	maxJSONLTurnsCacheBytes      = 64 * 1024 * 1024
)

func storeJSONLTurnsCache(path string, entry cachedJSONLTurns) {
	jsonlTurnsCacheMu.Lock()
	defer jsonlTurnsCacheMu.Unlock()
	if old, ok := jsonlTurnsCache.Load(path); ok {
		jsonlTurnsCacheBytes -= old.(cachedJSONLTurns).bytes
		jsonlTurnsCacheCount--
		jsonlTurnsCache.Delete(path)
	}
	if jsonlTurnsCacheCount >= maxJSONLTurnsCacheFiles || jsonlTurnsCacheBytes+entry.bytes > maxJSONLTurnsCacheBytes {
		jsonlTurnsCache.Range(func(k, _ any) bool {
			jsonlTurnsCache.Delete(k)
			return true
		})
		jsonlTurnsCacheCount = 0
		jsonlTurnsCacheBytes = 0
	}
	jsonlTurnsCacheCount++
	jsonlTurnsCacheBytes += entry.bytes
	jsonlTurnsCache.Store(path, entry)
}

// jsonlTurnSegments uses the same row parser and turn boundaries as JSONLTurns,
// caching parsed turn segments keyed by file identity so resolving thousands of
// segment pointers in the same transcript does not re-parse the file per pointer.
func jsonlTurnSegments(path string, turnIndex int) ([]Segment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("episodic: empty jsonl path")
	}
	if turnIndex < 0 {
		return nil, fmt.Errorf("episodic: turn %d not found in %s", turnIndex, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	sum, err := hashJSONLSource(context.Background(), path)
	if err != nil {
		return nil, err
	}
	if val, ok := jsonlTurnsCache.Load(path); ok {
		cached := val.(cachedJSONLTurns)
		if cached.sum == sum {
			if turnIndex >= len(cached.turns) || len(cached.turns[turnIndex]) == 0 {
				return nil, fmt.Errorf("episodic: turn %d not found in %s", turnIndex, path)
			}
			out := make([]Segment, len(cached.turns[turnIndex]))
			copy(out, cached.turns[turnIndex])
			return out, nil
		}
	}

	var turns [][]Segment
	var result []Segment
	cacheBytes := 0
	cacheable := true
	err = VisitJSONLTurns(context.Background(), path, func(turn JSONLTurn, _ bool) error {
		if turn.Index == turnIndex {
			result = append([]Segment(nil), turn.Segments...)
		}
		if cacheable {
			// Account for segment and slice overhead as well as text bytes.
			cacheBytes += 24 + len(turn.Segments)*64
			for _, segment := range turn.Segments {
				cacheBytes += len(segment.Text) + len(segment.Role)
			}
			if cacheBytes > maxJSONLTurnsCacheEntryBytes {
				cacheable = false
				turns = nil
			} else {
				turns = append(turns, turn.Segments)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	afterSum, err := hashJSONLSource(context.Background(), path)
	if err != nil {
		return nil, err
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || !os.SameFile(info, after) || afterSum != sum {
		return nil, fmt.Errorf("episodic: transcript changed while resolving: %s", path)
	}
	if cacheable {
		storeJSONLTurnsCache(path, cachedJSONLTurns{sum: sum, bytes: cacheBytes, turns: turns})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("episodic: turn %d not found in %s", turnIndex, path)
	}
	return result, nil
}
