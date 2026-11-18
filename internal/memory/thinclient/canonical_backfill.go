// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"context"
	"fmt"
	"strings"
)

// CanonicalBackfillReport summarizes one one-way migration of a vault from the
// legacy thin tables into the canonical rm_docs/rm_terms store.
type CanonicalBackfillReport struct {
	Inline      int
	Background  int
	Episodic    int
	Unavailable int
	Failed      int
}

// BackfillCanonicalCorpus derives the canonical retrieval documents from the
// vault's own sources. Inline documents use their stored raw text; background
// chunks are re-read from their source files; @ref units are resolved from the
// source snapshot. Nothing is decoded from packed atoms and no frame is
// synthesised: a unit whose source is gone is counted unavailable and skipped.
//
// Rows are materialised before any canonical write: the vault's connection pool
// is capped at one connection, so writing while a result set is open would
// deadlock.
//
// A vault written before the canonical store existed still carries its corpus in
// the thin tables and their source files. The first open derives those sources
// into rm_docs once; the marker in rm_migration keeps later opens free of the
// scan.
func (v *Vault) BackfillCanonicalCorpus(ctx context.Context, report *CanonicalBackfillReport, onlyMissing bool) error {
	if report == nil {
		report = &CanonicalBackfillReport{}
	}
	if v == nil || v.db == nil {
		return fmt.Errorf("thinclient: nil Vault db")
	}
	// User deletions are tombstones: deriving the canonical store must not bring a
	// unit the user removed back into retrieval.
	deleted := v.DeletedUnitSet("")
	if err := v.backfillInlineDocs(ctx, report, onlyMissing, deleted); err != nil {
		return err
	}
	if err := v.backfillBackgroundChunks(ctx, report, onlyMissing, deleted); err != nil {
		return err
	}
	if err := v.backfillEpisodicPointerDocs(ctx, report, onlyMissing, deleted); err != nil {
		return err
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type canonicalBackfillInline struct {
	docID       string
	layer       string
	occ         int64
	rawText     string
	sentences   string
	kept        string
	addr        string
	coord       int64
	wave        string
	macro       string
	assistant   string
	incarnation string
	locus       string
	inserted    int64
}

func (v *Vault) backfillInlineDocs(ctx context.Context, report *CanonicalBackfillReport, onlyMissing bool, deleted map[string]bool) error {
	rows, err := v.db.QueryContext(ctx, `
		SELECT doc_id, layer, occurred_at, raw_text, COALESCE(sentences_json, '[]'), COALESCE(kept_indices_json, '[]'),
		       COALESCE(address_uri, ''), COALESCE(coord64, 0), COALESCE(waveform_bar, ''), COALESCE(doc_macro_wave, ''),
		       COALESCE(assistant_id, ''), COALESCE(incarnation_id, ''), COALESCE(locus, ''), COALESCE(inserted_at, 0)
		FROM thin_vault_docs
		WHERE substr(trim(raw_text), 1, 5) != '@ref:'
		  AND (? = 0 OR NOT EXISTS (SELECT 1 FROM rm_docs r WHERE r.unit_id = thin_vault_docs.doc_id))`,
		boolInt(onlyMissing))
	if err != nil {
		return err
	}
	var items []canonicalBackfillInline
	for rows.Next() {
		var item canonicalBackfillInline
		if err := rows.Scan(&item.docID, &item.layer, &item.occ, &item.rawText, &item.sentences, &item.kept,
			&item.addr, &item.coord, &item.wave, &item.macro, &item.assistant, &item.incarnation, &item.locus, &item.inserted); err != nil {
			report.Failed++
			continue
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if deleted[item.docID] {
			continue
		}
		if strings.TrimSpace(item.rawText) == "" {
			report.Unavailable++
			continue
		}
		if err := v.ingestCanonicalText(item.docID, item.occ, item.rawText, canonicalUnitMeta{
			Layer:           item.layer,
			SentencesJSON:   item.sentences,
			KeptIndicesJSON: item.kept,
			AddressURI:      item.addr,
			Coord64:         item.coord,
			WaveformBar:     item.wave,
			DocMacroWave:    item.macro,
			AssistantID:     item.assistant,
			IncarnationID:   item.incarnation,
			Locus:           item.locus,
			InsertedAt:      item.inserted,
			StoreRawText:    true,
		}); err != nil {
			report.Failed++
			continue
		}
		report.Inline++
	}
	return nil
}

type canonicalBackfillChunk struct {
	unitID   string
	chunkIdx int64
	occ      int64
	inserted int64
	addr     string
	coord    int64
	wave     string
	kept     string
	anchor   string
}

func (v *Vault) backfillBackgroundChunks(ctx context.Context, report *CanonicalBackfillReport, onlyMissing bool, deleted map[string]bool) error {
	type fileRow struct {
		absPath string
		relPath string
	}
	fileRows, err := v.db.QueryContext(ctx, `SELECT abs_path, rel_path FROM thin_vault_bg_files ORDER BY abs_path`)
	if err != nil {
		return nil // background tables may not exist before the first mount
	}
	var files []fileRow
	for fileRows.Next() {
		var f fileRow
		if err := fileRows.Scan(&f.absPath, &f.relPath); err != nil {
			report.Failed++
			continue
		}
		files = append(files, f)
	}
	fileRows.Close()
	if err := fileRows.Err(); err != nil {
		return err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := v.db.QueryContext(ctx, `
			SELECT unit_id, chunk_index, occurred_at, COALESCE(inserted_at, 0), COALESCE(address_uri, ''),
			       COALESCE(coord64, 0), COALESCE(waveform_bar, ''), COALESCE(kept_indices_json, '[]'),
			       COALESCE(anchor, '')
			FROM thin_vault_bg_chunks WHERE abs_path = ?
			  AND (? = 0 OR NOT EXISTS (SELECT 1 FROM rm_docs r WHERE r.unit_id = thin_vault_bg_chunks.unit_id))
			ORDER BY chunk_index`, file.absPath, boolInt(onlyMissing))
		if err != nil {
			report.Failed++
			continue
		}
		var chunks []canonicalBackfillChunk
		for rows.Next() {
			var chunk canonicalBackfillChunk
			if err := rows.Scan(&chunk.unitID, &chunk.chunkIdx, &chunk.occ, &chunk.inserted, &chunk.addr,
				&chunk.coord, &chunk.wave, &chunk.kept, &chunk.anchor); err != nil {
				report.Failed++
				continue
			}
			chunks = append(chunks, chunk)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(chunks) == 0 {
			continue
		}
		parsed, err := ReadAndChunkLocalFile(file.absPath, file.relPath)
		if err != nil || len(parsed) == 0 {
			report.Unavailable += len(chunks)
			continue
		}
		byIndex := make(map[int]string, len(parsed))
		for _, chunk := range parsed {
			byIndex[chunk.ChunkIndex] = chunk.Text
		}
		for _, chunk := range chunks {
			if deleted[chunk.unitID] {
				continue
			}
			text, ok := byIndex[int(chunk.chunkIdx)]
			if !ok || strings.TrimSpace(text) == "" {
				report.Unavailable++
				continue
			}
			lineStart, lineEnd := parseBackgroundAnchorLines(chunk.anchor)
			fileKey := strings.TrimSpace(file.relPath)
			if fileKey == "" {
				fileKey = strings.TrimSpace(file.absPath)
			}
			docHash, blockHash := uint64(0), uint64(0)
			if fileKey != "" {
				docHash = v.KeyedHash64(fileKey)
			}
			if strings.TrimSpace(chunk.anchor) != "" && fileKey != "" {
				blockHash = v.KeyedHash64(fileKey + "#" + chunk.anchor)
			} else if chunk.unitID != "" {
				blockHash = v.KeyedHash64(chunk.unitID)
			}
			if err := v.ingestCanonicalText(chunk.unitID, chunk.occ, text, canonicalUnitMeta{
				Layer:           LayerBackground,
				KeptIndicesJSON: chunk.kept,
				AddressURI:      chunk.addr,
				Coord64:         chunk.coord,
				WaveformBar:     chunk.wave,
				InsertedAt:      chunk.inserted,
				DocHash:         docHash,
				BlockHash:       blockHash,
				LineStart:       lineStart,
				LineEnd:         lineEnd,
				StoreRawText:    false,
			}); err != nil {
				report.Failed++
				continue
			}
			report.Background++
		}
	}
	return nil
}

type canonicalBackfillEpisodic struct {
	docID       string
	layer       string
	ref         string
	occ         int64
	inserted    int64
	addr        string
	coord       int64
	wave        string
	kept        string
	assistant   string
	incarnation string
	locus       string
}

func (v *Vault) backfillEpisodicPointerDocs(ctx context.Context, report *CanonicalBackfillReport, onlyMissing bool, deleted map[string]bool) error {
	rows, err := v.db.QueryContext(ctx, `
		SELECT doc_id, layer, occurred_at, raw_text, COALESCE(inserted_at, 0), COALESCE(address_uri, ''),
		       COALESCE(coord64, 0), COALESCE(waveform_bar, ''), COALESCE(kept_indices_json, '[]'),
		       COALESCE(assistant_id, ''), COALESCE(incarnation_id, ''), COALESCE(locus, '')
		FROM thin_vault_docs WHERE substr(trim(raw_text), 1, 5) = '@ref:'
		  AND (? = 0 OR NOT EXISTS (SELECT 1 FROM rm_docs r WHERE r.unit_id = thin_vault_docs.doc_id))`,
		boolInt(onlyMissing))
	if err != nil {
		return err
	}
	var items []canonicalBackfillEpisodic
	for rows.Next() {
		var item canonicalBackfillEpisodic
		if err := rows.Scan(&item.docID, &item.layer, &item.occ, &item.ref, &item.inserted, &item.addr,
			&item.coord, &item.wave, &item.kept, &item.assistant, &item.incarnation, &item.locus); err != nil {
			report.Failed++
			continue
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if deleted[item.docID] {
			continue
		}
		text, err := ResolveEpisodicPointerLive(item.ref)
		if err != nil || strings.TrimSpace(text) == "" {
			report.Unavailable++
			continue
		}
		if err := v.ingestCanonicalText(item.docID, item.occ, text, canonicalUnitMeta{
			Layer:           item.layer,
			RefPointer:      item.ref,
			KeptIndicesJSON: item.kept,
			AddressURI:      item.addr,
			Coord64:         item.coord,
			WaveformBar:     item.wave,
			AssistantID:     item.assistant,
			IncarnationID:   item.incarnation,
			Locus:           item.locus,
			InsertedAt:      item.inserted,
			StoreRawText:    false,
		}); err != nil {
			report.Failed++
			continue
		}
		report.Episodic++
	}
	return nil
}
