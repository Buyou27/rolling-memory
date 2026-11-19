// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package recall

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/db"
	"github.com/rocareer/rolling-memory/internal/memory/delivery"
	"github.com/rocareer/rolling-memory/internal/store"
)

// applyDeliverableMask implements reader-facing entity surface masking:
//   - Modes: "off" (default, zero-op), "shadow" (computes surface dictionary and records dose in meta["mask_delivery"] without modifying text),
//     "on" (replaces non-reader referent labels and aliases with deterministic `[PERSON_<8hex>]` tokens).
//   - Reader exemption: if opts.ReaderSubject matches a referent node (by label or alias), that person's canonical label and all aliases are exempted.
//   - World exemption: spatial/world nodes (origin='world') are never masked.
func applyDeliverableMask(memory *store.Store, cfg *config.Config, scope string, opts Options, bands []map[string]any, rawHits []map[string]any, meta map[string]any) {
	if cfg == nil || memory == nil || memory.DB() == nil || strings.HasPrefix(scope, "mem:golden:") {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Recall.MaskDeliverable))
	switch mode {
	case "shadow", "on":
	default:
		return
	}
	if opts.Reader == "owner" {
		return
	}
	_ = db.EnsureDeliverySchema(memory.DB())

	exemptNodeID := ""
	if rs := strings.TrimSpace(opts.ReaderSubject); rs != "" {
		if n, found, err := memory.NodeByLabel(scope, rs, "referent"); err == nil && found {
			exemptNodeID = n.ID
		}
	}

	rows, err := memory.DB().Query(
		`SELECT n.id, n.label, COALESCE(ne.origin, '') AS origin
		 FROM node n
		 LEFT JOIN node_ext ne ON ne.node_id = n.id AND ne.scope = n.scope
		 WHERE n.scope = ? AND n.kind = 'referent' AND n.status IN ('provisional', 'active')
		 LIMIT 5000`,
		scope,
	)
	if err != nil {
		return
	}
	type maskPair struct {
		surface string
		token   string
	}
	seenSurface := map[string]bool{}
	var pairs []maskPair
	addSurface := func(nodeID, surface string) {
		sf := strings.TrimSpace(surface)
		if sf == "" || delivery.StringShannonBits(sf) < 4.0 {
			return
		}
		lo := strings.ToLower(sf)
		if seenSurface[lo] {
			return
		}
		seenSurface[lo] = true
		sum := sha1.Sum([]byte("rm-deliver|" + scope + "|" + nodeID))
		tok := "[PERSON_" + hex.EncodeToString(sum[:])[:8] + "]"
		pairs = append(pairs, maskPair{surface: sf, token: tok})
	}

	var nodeIDs []string
	for rows.Next() {
		var nid, label, origin string
		if err := rows.Scan(&nid, &label, &origin); err != nil {
			continue
		}
		if nid == "" || nid == exemptNodeID || origin == "world" {
			continue
		}
		// Only mask roster/person or unannotated referent nodes that are not structural paths
		if strings.Contains(label, "/") {
			continue
		}
		addSurface(nid, label)
		nodeIDs = append(nodeIDs, nid)
	}
	_ = rows.Close()

	for _, nid := range nodeIDs {
		if aliases, aErr := memory.Aliases(nid); aErr == nil {
			for _, al := range aliases {
				addSurface(nid, al)
			}
		}
	}

	// Highest information mass surface first to prevent partial prefix corruption
	sort.SliceStable(pairs, func(i, j int) bool {
		bi, bj := delivery.StringShannonBits(pairs[i].surface), delivery.StringShannonBits(pairs[j].surface)
		if bi != bj {
			return bi > bj
		}
		return pairs[i].surface < pairs[j].surface
	})

	replacements := 0
	maskString := func(s string) string {
		if s == "" || len(pairs) == 0 {
			return s
		}
		out := s
		for _, p := range pairs {
			if strings.Contains(out, p.surface) {
				if mode == "on" {
					out = strings.ReplaceAll(out, p.surface, p.token)
				}
				replacements++
			}
		}
		return out
	}

	for _, b := range bands {
		if title, ok := b["title"].(string); ok {
			b["title"] = maskString(title)
		}
		if subj, ok := b["subject"].(string); ok {
			b["subject"] = maskString(subj)
		}
		nodes, _ := b["nodes"].([]map[string]any)
		for _, n := range nodes {
			for _, k := range []string{"label", "content"} {
				if v, ok := n[k].(string); ok {
					n[k] = maskString(v)
				}
			}
		}
		chains, _ := b["chains"].([]any)
		for _, chAny := range chains {
			if ch, ok := chAny.(map[string]any); ok {
				if lb, ok := ch["label"].(string); ok {
					ch["label"] = maskString(lb)
				}
				cNodes, _ := ch["nodes"].([]map[string]any)
				for _, cn := range cNodes {
					for _, k := range []string{"label", "content"} {
						if v, ok := cn[k].(string); ok {
							cn[k] = maskString(v)
						}
					}
				}
			}
		}
	}
	for _, h := range rawHits {
		if txt, ok := h["text"].(string); ok {
			h["text"] = maskString(txt)
		}
	}

	if meta != nil {
		meta["mask_delivery"] = map[string]any{
			"mode":         mode,
			"surfaces":     len(pairs),
			"replacements": replacements,
		}
	}
}

// AssistantNameEvidence holds an explicit assistant name alias and its source basis.
type AssistantNameEvidence struct {
	Alias  string
	Source string
}
