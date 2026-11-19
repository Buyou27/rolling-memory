// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package facade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	crystallizationQueueDirSuffix = ".crystallize.d"
	crystallizationQueueMaxFiles  = 1024
	crystallizationMaxAttempts    = 3
)

// CrystallizationRequest is one deferred L3 pulse. The recall surface only
// writes the request; the background worker performs the 200-1200ms ingest.
type CrystallizationRequest struct {
	NoeticID       string   `json:"noetic_id"`
	Query          string   `json:"query"`
	Answer         string   `json:"answer"`
	CitedUnitIDs   []string `json:"cited_unit_ids,omitempty"`
	CitedSentences []string `json:"cited_sentences,omitempty"`
	OccurredAt     int64    `json:"occurred_at"`
	Attempts       int      `json:"attempts,omitempty"`
}

func crystallizationQueueDir(dbPath string) string {
	return dbPath + crystallizationQueueDirSuffix
}

func crystallizationQueueName(noeticID string) string {
	sum := sha256.Sum256([]byte(noeticID))
	return hex.EncodeToString(sum[:16]) + ".json"
}

// EnqueueCrystallization writes one deferred pulse. A read-only recall surface
// uses this instead of running the ingest itself. Duplicate noetic ids collapse
// onto the same file; the worker's write is idempotent.
func (m *Memory) EnqueueCrystallization(req CrystallizationRequest) error {
	if m == nil || m.Config == nil || strings.TrimSpace(m.Config.DB) == "" || m.Config.DB == ":memory:" {
		return nil
	}
	if strings.TrimSpace(req.NoeticID) == "" {
		return errors.New("crystallization request requires a noetic id")
	}
	dir := crystallizationQueueDir(m.Config.DB)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) >= crystallizationQueueMaxFiles {
		return errors.New("crystallization queue is full")
	}
	target := filepath.Join(dir, crystallizationQueueName(req.NoeticID))
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Rename(tempName, target); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}

// ProcessCrystallizationQueue drains up to limit deferred pulses. The worker
// calls it once per tick so a burst of recalls cannot monopolise its slice.
func (m *Memory) ProcessCrystallizationQueue(ctx context.Context, limit int) (int, error) {
	if m == nil || m.Config == nil || strings.TrimSpace(m.Config.DB) == "" || m.Config.DB == ":memory:" {
		return 0, nil
	}
	if limit <= 0 {
		limit = 1
	}
	dir := crystallizationQueueDir(m.Config.DB)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	tc, err := m.LocalVault()
	if err != nil {
		return 0, err
	}
	if tc == nil || tc.Vault == nil {
		return 0, errors.New("crystallization queue requires an open local vault")
	}
	processed := 0
	var failures []error
	for _, entry := range entries {
		if processed >= limit {
			break
		}
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			failures = append(failures, readErr)
			continue
		}
		var req CrystallizationRequest
		if err := json.Unmarshal(raw, &req); err != nil || strings.TrimSpace(req.NoeticID) == "" {
			// Poison entries are dropped; reconcile can regenerate the next pulse.
			_ = os.Remove(path)
			continue
		}
		if err := tc.RememberNoetic(req.NoeticID, req.Query, req.CitedUnitIDs, req.CitedSentences, req.Answer, req.OccurredAt); err != nil {
			failures = append(failures, err)
			req.Attempts++
			if req.Attempts >= crystallizationMaxAttempts {
				_ = os.Remove(path)
				continue
			}
			if encoded, marshalErr := json.Marshal(req); marshalErr == nil {
				_ = os.WriteFile(path, encoded, 0o600)
			}
			continue
		}
		pushNoeticPulse(tc, req.NoeticID, req.Query, req.Answer, req.CitedUnitIDs, req.OccurredAt)
		if err := os.Remove(path); err != nil {
			failures = append(failures, err)
		}
		processed++
	}
	if len(failures) > 0 {
		return processed, fmt.Errorf("crystallization queue: %w", errors.Join(failures...))
	}
	return processed, nil
}
