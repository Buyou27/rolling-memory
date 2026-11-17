// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package common provides the language-agnostic Common Core harmonic table (L1)
// for the opt-in client-side companion harmonic plugin (`internal/langpack`):
// international SI/IEC measurement symbols, ISO 4217 currency codes/symbols,
// universal statistical quantiles/symbols (%, ‰, p50..p99, avg, std), and inline symbol extraction.
//
// Architectural Boundary:
//   - The server engine and the default `ramesdk` encoder are 100% zero-lexicon.
//   - This table lives exclusively in the client-side opt-in `langpack` plugin and is activated
//     only when the caller explicitly configures a language pack (`en`, `zh`, `ja`, `ko`, `sea`, `de`, `multi`).
//
// Delegates to the canonical client SDK (`plain.LookupUnitDimensionHarmonicPair`) so that
// all 4 language SDKs and the Go `langpack` companion suite share a single source of truth.
package common

import (
	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// LookupCommonUnitAndStatHarmonics resolves universal SI/ISO symbols and statistical notations
// into a 1st-order canonical unit tag (2k-1) and a 2nd-order dimension category tag (2k-3).
//
// The unit/dimension axis view comes from the canonical SDK table
// (`plain.LookupUnitDimensionHarmonicPair`); non-unit tokens fall back to the geographic axis
// (`LookupISO3166GeoHarmonics`), preserving the original Common Core fallback semantics.
func LookupCommonUnitAndStatHarmonics(low string) (canonicalUnit string, dimensionTag string) {
	if unit, dim := plain.LookupUnitDimensionHarmonicPair(low); unit != "" || dim != "" {
		return unit, dim
	}
	if geo1, geo2 := LookupISO3166GeoHarmonics(low); geo1 != "" || geo2 != "" {
		return geo1, geo2
	}
	tags := plain.LookupClosedSetHarmonics(low)
	if len(tags) >= 2 {
		return tags[0], tags[1]
	}
	if len(tags) == 1 {
		return tags[0], ""
	}
	return "", ""
}

// ExtractInlineSymbolUnits scans a sentence for currency/percentage/temperature symbols attached
// to digits (such as "$480", "€9.10", "£655.59", "¥100", "95%", "25°C") that standard alphanumeric
// word splitters skip, returning their canonical common-core symbols in order of appearance.
func ExtractInlineSymbolUnits(sentence string) []string {
	return plain.ExtractInlineSymbolUnits(sentence)
}
