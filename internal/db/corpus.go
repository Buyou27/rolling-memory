// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package db

import "database/sql"

// CorpusCounts are the physical counters of the canonical memory corpus
// (`rm_docs` plus active `rm_noetic` entries). They are the only counts the
// product reports; the legacy assertion graph is not part of a shipped vault.
type CorpusCounts struct {
	Episodic   int
	Background int
	Noetic     int
}

// MemoryUnits returns the number of stored documents plus active L3 entries.
func (c CorpusCounts) MemoryUnits() int {
	return c.Episodic + c.Background + c.Noetic
}

// Map returns the JSON-facing corpus block.
func (c CorpusCounts) Map() map[string]int {
	return map[string]int{
		"memories":     c.MemoryUnits(),
		"episodic":     c.Episodic,
		"background":   c.Background,
		"crystallized": c.Noetic,
	}
}

// ReadCorpusCounts reads the canonical corpus counters. A database without the
// canonical tables (a legacy assertion-graph vault) reports zero counts.
func ReadCorpusCounts(connection *sql.DB) (CorpusCounts, error) {
	var counts CorpusCounts
	if connection == nil {
		return counts, nil
	}
	var present int
	if err := connection.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'rm_docs'`,
	).Scan(&present); err != nil {
		return counts, err
	}
	if present == 0 {
		return counts, nil
	}
	rows, err := connection.Query(`SELECT layer, COUNT(*) FROM rm_docs GROUP BY layer`)
	if err != nil {
		return counts, err
	}
	defer rows.Close()
	for rows.Next() {
		var layer string
		var count int
		if err := rows.Scan(&layer, &count); err != nil {
			return counts, err
		}
		switch layer {
		case "background":
			counts.Background += count
		case "noetic":
			// L3 entries live in rm_noetic; a stray rm_docs row is not a
			// retrievable memory unit.
		default:
			counts.Episodic += count
		}
	}
	if err := rows.Err(); err != nil {
		return counts, err
	}
	if err := connection.QueryRow(
		`SELECT COUNT(*) FROM rm_noetic WHERE status = 'active'`,
	).Scan(&counts.Noetic); err != nil && err != sql.ErrNoRows {
		return counts, err
	}
	return counts, nil
}

// CountCorpusTerms returns the number of stored term postings (rm_terms). It is
// a doctor-only size probe, not a retrieval input.
func CountCorpusTerms(connection *sql.DB) (int, error) {
	var present, count int
	if err := connection.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'rm_terms'`,
	).Scan(&present); err != nil {
		return 0, err
	}
	if present == 0 {
		return 0, nil
	}
	if err := connection.QueryRow(`SELECT COUNT(*) FROM rm_terms`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
