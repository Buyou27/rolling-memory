// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

import (
	"strings"
)

// Multimodal part line-start protocol markers synthesized by ComposeTurnText or supplied fixtures.
var multimodalLinePrefixes = []string{
	"【图片】",
	"【音频】",
	"【视频】",
	"【文件】",
	"[image:",
	"[audio:",
	"[video:",
	"[file:",
}

var multimodalInlineBridges = []string{
	" + [image:",
	" + [audio:",
	" + [video:",
	" + [file:",
}

// CanonicalMultimodalProtocolTag reports whether slotLabel is a bare system-synthesized multimodal
// protocol bracket tag ("【图片】", "【音频】", "【视频】", "【文件】") and returns the canonical protocol tag.
// Natural-language nouns (such as "photo", "screenshot", "document") are never matched here.
func CanonicalMultimodalProtocolTag(slotLabel string) string {
	switch strings.TrimSpace(slotLabel) {
	case "【图片】", "【音频】", "【视频】", "【文件】":
		return strings.TrimSpace(slotLabel)
	default:
		return ""
	}
}

// StripModalBracketSlotPrefix strips system-synthesized multimodal bracket tags from an entity slot label
// so that entity nodes in the memory graph carry pure referent names rather than text-tag prefixes:
//   - "【图片】风景油画" -> "风景油画"
//   - "【文件：Q3_roadmap.pdf】" -> "Q3_roadmap.pdf"
//   - "【音频: shift_09.mp3】" -> "shift_09.mp3"
//
// If stripping would leave an empty string (i.e. bare tag "【图片】"), returns the original string unchanged
// so the caller can first resolve the bare tag against the structural multimodal segment.
func StripModalBracketSlotPrefix(slotLabel string) string {
	s := strings.TrimSpace(slotLabel)
	if s == "" {
		return slotLabel
	}
	for _, tag := range []string{"【图片】", "【音频】", "【视频】", "【文件】"} {
		if strings.HasPrefix(s, tag) {
			rest := strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(s, tag), "：:—- "))
			if rest != "" {
				return rest
			}
			return slotLabel
		}
	}
	for _, openPfx := range []string{"【图片：", "【图片:", "【音频：", "【音频:", "【视频：", "【视频:", "【文件：", "【文件:"} {
		if strings.HasPrefix(s, openPfx) && strings.HasSuffix(s, "】") {
			inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, openPfx), "】"))
			if inner != "" {
				return inner
			}
		}
	}
	lo := strings.ToLower(s)
	for _, asciiPfx := range []string{"[image]", "[audio]", "[video]", "[file]"} {
		if strings.HasPrefix(lo, asciiPfx) {
			rest := strings.TrimSpace(strings.TrimLeft(s[len(asciiPfx):], "：:—- "))
			if rest != "" {
				return rest
			}
		}
	}
	for _, asciiOpen := range []string{"[image:", "[audio:", "[video:", "[file:"} {
		if strings.HasPrefix(lo, asciiOpen) && strings.HasSuffix(s, "]") {
			inner := strings.TrimSpace(strings.TrimSuffix(s[len(asciiOpen):], "]"))
			if inner != "" {
				return inner
			}
		}
	}
	return slotLabel
}

// ScriptBitMass measures the Shannon self-information bit mass across orthogonal Unicode script families,
// replacing brittle byte-level `isASCII` checks so that typographic punctuation (“ ” ’ — …), mathematical/Greek
// symbols (μ α β), and accented Latin letters (é ü ñ) never falsely disable script conservation gates.
type ScriptBitMass struct {
	LatinBits         float64
	HanBits           float64
	KanaBits          float64
	HangulBits        float64
	CyrillicBits      float64
	OtherNonLatinBits float64
}
