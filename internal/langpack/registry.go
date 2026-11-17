// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package langpack exposes the client-side companion language segmenter and pulse-packer suite.
// By default, the SDK uses the 100% language-agnostic canonical encoder (`plain.SplitWords`);
// callers may explicitly specify a language code (`en`, `zh`, `ja`, `ko`, `sea`/`th`/`lo`/`km`,
// `de`/`nl`/`sv`, `multi`) to activate its companion segmenter and harmonic overlay.
package langpack

import (
	"fmt"
	"strings"

	"github.com/rocareer/rolling-memory/internal/langpack/de"
	"github.com/rocareer/rolling-memory/internal/langpack/en"
	"github.com/rocareer/rolling-memory/internal/langpack/ja"
	"github.com/rocareer/rolling-memory/internal/langpack/ko"
	"github.com/rocareer/rolling-memory/internal/langpack/multi"
	"github.com/rocareer/rolling-memory/internal/langpack/sea"
	"github.com/rocareer/rolling-memory/internal/langpack/zh"
	"github.com/rocareer/rolling-memory/internal/thinclient"
	plain "github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// Descriptor describes one optional language pack in the client language catalog.
type Descriptor struct {
	ID               string
	Aliases          []string
	EnabledByDefault bool
}

// Catalog lists all registered optional language packs. None is enabled by default.
func Catalog() []Descriptor {
	return []Descriptor{
		{ID: "en", EnabledByDefault: false},
		{ID: "zh", Aliases: []string{"zh-cn", "zh-tw"}, EnabledByDefault: false},
		{ID: "ja", EnabledByDefault: false},
		{ID: "ko", EnabledByDefault: false},
		{ID: "sea", Aliases: []string{"th", "lo", "km"}, EnabledByDefault: false},
		{ID: "de", Aliases: []string{"nl", "sv"}, EnabledByDefault: false},
		{ID: "multi", EnabledByDefault: false},
	}
}

func resolveLanguageOptions(language string) (plain.SegmenterPackOptions, bool, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "off", "standard", "default", "universal", "auto":
		return plain.SegmenterPackOptions{}, true, nil
	case "en":
		return plain.SegmenterPackOptions{
			Segmenter:          en.SegmentSentence,
			SentenceAtomPacker: en.PackSentence,
			HarmonicOverlay:    true,
		}, false, nil
	case "zh", "zh-cn", "zh-tw":
		return plain.SegmenterPackOptions{
			Segmenter:          zh.SegmentSentence,
			SentenceAtomPacker: zh.PackSentence,
			HarmonicOverlay:    true,
		}, false, nil
	case "ja":
		return plain.SegmenterPackOptions{
			Segmenter:          ja.SegmentSentence,
			SentenceAtomPacker: ja.PackSentence,
			HarmonicOverlay:    true,
		}, false, nil
	case "ko":
		return plain.SegmenterPackOptions{
			Segmenter:          ko.SegmentSentence,
			SentenceAtomPacker: ko.PackSentence,
			HarmonicOverlay:    true,
		}, false, nil
	case "sea", "th", "lo", "km":
		return plain.SegmenterPackOptions{
			Segmenter:          sea.SegmentSentence,
			SentenceAtomPacker: sea.PackSentence,
			HarmonicOverlay:    true,
		}, false, nil
	case "de", "nl", "sv":
		return plain.SegmenterPackOptions{
			Segmenter:          de.SegmentSentence,
			SentenceAtomPacker: de.PackSentence,
			HarmonicOverlay:    true,
		}, false, nil
	case "multi":
		return plain.SegmenterPackOptions{
			Segmenter:          multi.SegmentSentence,
			SentenceAtomPacker: multi.PackSentence,
			HarmonicOverlay:    true,
		}, false, nil
	default:
		return plain.SegmenterPackOptions{}, false, fmt.Errorf("langpack: unknown language %q", language)
	}
}

// Segmenter returns the companion WordSegmenter for language (or plain.SplitWords when standard/universal).
func Segmenter(language string) (plain.WordSegmenter, error) {
	opts, isStandard, err := resolveLanguageOptions(language)
	if err != nil {
		return nil, err
	}
	if isStandard || opts.Segmenter == nil {
		return plain.SplitWords, nil
	}
	return opts.Segmenter, nil
}

// Configure applies an explicitly selected language pack (segmenter + harmonic packer) to enc.
// Empty, "off", "standard", "default", "universal", and "auto" restore canonical universal behavior.
func Configure(enc *plain.BlindEncoder, language string) error {
	if enc == nil {
		return fmt.Errorf("langpack: nil blind encoder")
	}
	opts, _, err := resolveLanguageOptions(language)
	if err != nil {
		return err
	}
	enc.SetSegmenterOptions(opts)
	return nil
}

// ConfigureVault applies an explicitly selected language pack to a local ThinClient Vault.
func ConfigureVault(v *thinclient.Vault, language string) error {
	if v == nil {
		return fmt.Errorf("langpack: nil vault")
	}
	opts, _, err := resolveLanguageOptions(language)
	if err != nil {
		return err
	}
	v.SetSegmenterOptions(opts)
	return nil
}
