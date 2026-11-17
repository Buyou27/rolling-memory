// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	SpecVersion            = "RAME-BlindPulse-v1"
	CodecVersion           = "1.11.3"
	LayerBackground        = "background"
	LayerEpisodic          = "episodic"
	LayerNoetic            = "noetic"
	ModeLocalPlain         = "local_plain"
	ModePulsePlain         = "pulse_plain"
	CalendarBeaconWidthX10 = calBeaconCarrierBitsX10
)

type WordSegmenter func(sentence string) []string

type SentenceAtomPacker func(enc *BlindEncoder, sentence string, sentIdx int) []BlindTokenTerm

type SegmenterPackOptions struct {
	Segmenter           WordSegmenter
	SentenceAtomPacker  SentenceAtomPacker
	HarmonicOverlay     bool
	DisableAutoHarmonic bool
}

// PulseSlot is the fixed-width binary carrier slot record {H, RawBitsX10, Flags}.
type PulseSlot struct {
	H          uint64
	RawBitsX10 uint16
	Flags      uint8
}

// ExtractPulsePrimaryDyads extracts adjacent bi-term phrases strictly over
// primary term slots (slots without FlagCompanionModifier), preventing
// companion modifiers of a single word from pairing with their own host word.
func ExtractPulsePrimaryDyads(frames [][]PulseSlot) []PhraseDyad {
	primaryFrames := make([][]TermSample, 0, len(frames))
	for _, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		primaries := make([]TermSample, 0, len(frame))
		for _, slot := range frame {
			if slot.Flags&FlagCompanionModifier != 0 {
				continue
			}
			primaries = append(primaries, TermSample{
				H: slot.H,
				W: float64(slot.RawBitsX10) / 10,
			})
		}
		if len(primaries) >= 2 {
			primaryFrames = append(primaryFrames, primaries)
		}
	}
	return ExtractAdjacentPhrases(primaryFrames)
}

type BlindPulsePacket struct {
	TenantID         string             `json:"tenant_id,omitempty"`
	Scope            string             `json:"scope,omitempty"`
	UserID           string             `json:"user_id,omitempty"`
	AssistantID      string             `json:"assistant_id,omitempty"`
	IncarnationID    string             `json:"incarnation_id,omitempty"`
	Locus            string             `json:"locus,omitempty"`
	UnitID           string             `json:"id"`
	Layer            string             `json:"layer,omitempty"`
	CitedUnitIDs     []string           `json:"cited_unit_ids,omitempty"`
	OccurredAt       int64              `json:"ts,omitempty"`
	NarratedAt       int64              `json:"narrated_at,omitempty"`
	IngestedAt       int64              `json:"ingested_at,omitempty"`
	AuthorHash       uint64             `json:"author_h,omitempty"`
	DocHash          uint64             `json:"doc_h,omitempty"`
	BlockHash        uint64             `json:"block_h,omitempty"`
	LineStart        int                `json:"line_start,omitempty"`
	LineEnd          int                `json:"line_end,omitempty"`
	Slots            []BlindTokenTerm   `json:"slots,omitempty"`
	SentenceFrames   [][]BlindTokenTerm `json:"sents,omitempty"`
	DenoiseRatio     *float64           `json:"denoise_ratio,omitempty"`
	MinKeepSentences int                `json:"min_keep_sentences,omitempty"`
	WaveRadix        int                `json:"wave_radix,omitempty"`
	AtomsB64         string             `json:"atoms_b64,omitempty"`
	RawText          string             `json:"raw_text,omitempty"`
	PulseMode        string             `json:"pulse_mode,omitempty"`
}

func (e *BlindEncoder) buildSingleWordTerm(w string, wordIdx int, extraFlags uint8) (BlindTokenTerm, bool) {
	low := strings.ToLower(strings.TrimSpace(w))
	if low == "" {
		return BlindTokenTerm{}, false
	}
	var rawWidth uint32
	flags := extraFlags
	var prevR rune
	runLen, hasLower := 0, false
	for idx, r := range w {
		if unicode.IsSpace(r) {
			continue
		}
		if lr := unicode.ToLower(r); lr == prevR {
			runLen++
		} else {
			prevR, runLen = lr, 1
		}
		rw := uint32(CarrierUnits(r))
		if runLen >= 3 {
			rw /= 4
		}
		rawWidth += rw
		var fl uint8
		fl, hasLower = classifyRuneFlag(r, wordIdx > 0 || idx > 0, idx > 0 && hasLower)
		flags |= fl
	}
	return BlindTokenTerm{H: e.KeyedHash64(low), RawBitsX10: clampBitsX10(rawWidth), ClassFlags: flags}, true
}

func adaptiveSubcarrierSlices(tok string) []string {
	var runeOffsets, runeWidths []int
	totalW := 0
	for idx, r := range tok {
		if unicode.IsSpace(r) {
			continue
		}
		runeOffsets = append(runeOffsets, idx)
		rw := int(CarrierUnits(r))
		runeWidths = append(runeWidths, rw)
		totalW += rw
	}
	runeCount := len(runeOffsets)
	if runeCount < 4 || totalW <= 160 {
		return nil
	}
	k := (totalW + 119) / 120
	if k < 2 {
		k = 2
	}
	if k > 8 {
		k = 8
	}
	if k > runeCount/2 {
		k = runeCount / 2
	}
	if k < 2 {
		return nil
	}
	targetPerSub := totalW / k
	if targetPerSub < 40 {
		targetPerSub = 40
	}
	var out []string
	startPos, accW := 0, 0
	for i := 0; i < runeCount; i++ {
		accW += runeWidths[i]
		remainingRunes := runeCount - 1 - i
		remainingChunks := k - len(out) - 1
		if remainingChunks > 0 && accW >= targetPerSub && remainingRunes >= remainingChunks*2 {
			out = append(out, tok[runeOffsets[startPos]:runeOffsets[i+1]])
			startPos, accW = i+1, 0
		}
	}
	if startPos < runeCount {
		out = append(out, tok[runeOffsets[startPos]:])
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func (e *BlindEncoder) appendAdaptiveHarmonicTerms(sAtoms []BlindTokenTerm, cwTrim string, wordIdx int, gluedThresholdX10 uint16) []BlindTokenTerm {
	parentAtom, ok := e.buildSingleWordTerm(cwTrim, wordIdx, 0)
	if !ok {
		return sAtoms
	}
	rawSubs := SplitWords(cwTrim)
	var distinctSubs []string
	for _, sub := range rawSubs {
		if st := strings.TrimSpace(sub); st != "" && !strings.EqualFold(st, cwTrim) {
			distinctSubs = append(distinctSubs, st)
		}
	}
	if len(distinctSubs) < 2 && parentAtom.RawBitsX10 > gluedThresholdX10 {
		distinctSubs = adaptiveSubcarrierSlices(cwTrim)
	}
	if len(distinctSubs) < 2 {
		return append(sAtoms, parentAtom)
	}
	subAtoms := make([]BlindTokenTerm, 0, len(distinctSubs))
	subStrings := make([]string, 0, len(distinctSubs))
	for subIdx, sub := range distinctSubs {
		if sa, sOK := e.buildSingleWordTerm(sub, wordIdx+subIdx, 0); sOK && sa.H != parentAtom.H {
			subAtoms = append(subAtoms, sa)
			subStrings = append(subStrings, sub)
		}
	}
	if len(subAtoms) < 2 {
		return append(sAtoms, parentAtom)
	}
	if len(subAtoms) <= 8 {
		var sumSubB uint32
		maxSubB := uint16(0)
		for _, sa := range subAtoms {
			sumSubB += uint32(sa.RawBitsX10)
			if sa.RawBitsX10 > maxSubB {
				maxSubB = sa.RawBitsX10
			}
		}
		if maxSubB >= 33 && sumSubB >= 60 {
			parentAtom.ClassFlags |= FlagHasConn
			parentAtom.RawBitsX10 = clampBitsX10(sumSubB)
			sAtoms = append(sAtoms, parentAtom)
			return append(sAtoms, subAtoms...)
		}
		return append(sAtoms, parentAtom)
	}
	for start := 0; start < len(subAtoms); start += 8 {
		end := start + 8
		if end > len(subAtoms) {
			end = len(subAtoms)
		}
		chunk := subAtoms[start:end]
		if len(chunk) == 1 {
			sAtoms = append(sAtoms, chunk[0])
			continue
		}
		var sumChunkB uint32
		for _, sa := range chunk {
			sumChunkB += uint32(sa.RawBitsX10)
		}
		frameParent := parentAtom
		if start > 0 {
			if fp, fpOK := e.buildSingleWordTerm(strings.Join(subStrings[start:end], " "), wordIdx+start, FlagHasConn); fpOK {
				frameParent = fp
			}
		}
		frameParent.ClassFlags |= FlagHasConn
		if sumChunkB > 65535 {
			frameParent.RawBitsX10 = 65535
		} else if sumChunkB >= 60 {
			frameParent.RawBitsX10 = uint16(sumChunkB)
		}
		sAtoms = append(sAtoms, frameParent)
		sAtoms = append(sAtoms, chunk...)
	}
	return sAtoms
}

func (e *BlindEncoder) PackBlindPacketWithSegmenter(docID string, text string, occurredAt int64, opts SegmenterPackOptions) (BlindPulsePacket, []string) {
	if opts.Segmenter == nil && opts.SentenceAtomPacker == nil {
		return e.packBlindPacketDefault(docID, text, occurredAt)
	}
	autoHarmonic := !opts.DisableAutoHarmonic
	trimmed := strings.TrimSpace(text)
	sents := SplitSentences(text)
	if len(sents) == 0 && trimmed != "" {
		sents = []string{trimmed}
	}
	var allSlots []BlindTokenTerm
	var sentFrames [][]BlindTokenTerm
	var validSents []string
	for sentIdx, s := range sents {
		var sAtoms []BlindTokenTerm
		if opts.SentenceAtomPacker != nil {
			sAtoms = opts.SentenceAtomPacker(e, s, sentIdx)
		} else {
			customWords := opts.Segmenter(s)
			for wordIdx, cw := range customWords {
				cwTrim := strings.TrimSpace(cw)
				if cwTrim == "" {
					continue
				}
				if !autoHarmonic {
					if atom, ok := e.buildSingleWordTerm(cwTrim, wordIdx, 0); ok {
						sAtoms = append(sAtoms, atom)
					}
					continue
				}
				sAtoms = e.appendAdaptiveHarmonicTerms(sAtoms, cwTrim, wordIdx, 320)
			}
		}
		if len(sAtoms) == 0 {
			continue
		}
		validSents = append(validSents, s)
		sentFrames = append(sentFrames, sAtoms)
		allSlots = append(allSlots, sAtoms...)
	}
	return BlindPulsePacket{UnitID: docID, OccurredAt: occurredAt, Slots: allSlots, SentenceFrames: sentFrames}, validSents
}

func (e *BlindEncoder) packBlindPacketDefault(docID string, text string, occurredAt int64) (BlindPulsePacket, []string) {
	trimmed := strings.TrimSpace(text)
	var sents []string
	if trimmed != "" && len(SplitWords(trimmed)) == 0 {
		sents = []string{trimmed}
	} else if sents = SplitSentences(text); len(sents) == 0 && trimmed != "" {
		sents = []string{trimmed}
	}
	var allSlots []BlindTokenTerm
	var sentFrames [][]BlindTokenTerm
	var validSents []string
	for _, s := range sents {
		if sTerms := e.PackSentenceTerms(s); len(sTerms) > 0 {
			validSents = append(validSents, s)
			sentFrames = append(sentFrames, sTerms)
			allSlots = append(allSlots, sTerms...)
		}
	}
	return BlindPulsePacket{UnitID: docID, OccurredAt: occurredAt, Slots: allSlots, SentenceFrames: sentFrames}, validSents
}

func (e *BlindEncoder) PackBlindPacket(docID string, text string, occurredAt int64) (BlindPulsePacket, []string) {
	if e != nil && (e.segOpts.Segmenter != nil || e.segOpts.SentenceAtomPacker != nil) {
		return e.PackBlindPacketWithSegmenter(docID, text, occurredAt, e.segOpts)
	}
	return e.packBlindPacketDefault(docID, text, occurredAt)
}

func (e *BlindEncoder) PackBlindPacketWithMeta(docID string, text string, meta UnitMeta) (BlindPulsePacket, []string) {
	occ := meta.OccurredAt
	if occ <= 0 && meta.NarratedAt > 0 {
		occ = meta.NarratedAt
	}
	pkt, validSents := e.PackBlindPacket(docID, FormatPlaintextWithMeta(text, meta), occ)
	pkt.NarratedAt, pkt.IngestedAt = meta.NarratedAt, meta.IngestedAt
	if author := strings.ToLower(strings.TrimSpace(meta.Author)); author != "" {
		pkt.AuthorHash = e.KeyedHash64(author)
	}
	pkt.DocHash = meta.DocHash
	if pkt.DocHash == 0 {
		if fileURL := strings.TrimSpace(meta.FileURL); fileURL != "" {
			pkt.DocHash = e.KeyedHash64(fileURL)
		}
	}
	pkt.BlockHash = meta.BlockHash
	if pkt.BlockHash == 0 {
		if blockID := strings.TrimSpace(meta.BlockID); blockID != "" {
			pkt.BlockHash = e.KeyedHash64(blockID)
		}
	}
	if meta.LineStart > 0 {
		pkt.LineStart = meta.LineStart
	}
	if meta.LineEnd > 0 {
		pkt.LineEnd = meta.LineEnd
	}
	if pkt.LineStart > 0 && pkt.LineEnd < pkt.LineStart {
		pkt.LineEnd = pkt.LineStart
	}
	return pkt, validSents
}

type SubUnit struct {
	Kind      string `json:"kind"`
	ID        string `json:"id,omitempty"`
	Text      string `json:"text"`
	BlockID   string `json:"block_id,omitempty"`
	FileParse string `json:"file_parse,omitempty"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
}

func ComposeSubUnitID(parentID, kind string, index int) string {
	p, k := strings.TrimSpace(parentID), strings.TrimSpace(kind)
	if k == "" {
		k = "sub"
	}
	if index <= 0 {
		return p + "#" + k
	}
	return fmt.Sprintf("%s#%s:%d", p, k, index)
}

func (e *BlindEncoder) PackCompositePackets(parentID string, parentText string, occurredAt int64, subUnits []SubUnit) ([]BlindPulsePacket, map[string]string) {
	parentID = strings.TrimSpace(parentID)
	var parentDocHash uint64
	if parentID != "" && e != nil {
		parentDocHash = e.KeyedHash64(parentID)
	}
	subToParent := make(map[string]string, len(subUnits)*2)
	var pkts []BlindPulsePacket
	if parentID != "" && strings.TrimSpace(parentText) != "" {
		if pkt, _ := e.PackBlindPacket(parentID, parentText, occurredAt); len(pkt.Slots) > 0 {
			pkt.DocHash = parentDocHash
			pkts = append(pkts, pkt)
		}
	}
	kindCounts := make(map[string]int, 4)
	for _, su := range subUnits {
		txt := strings.TrimSpace(su.Text)
		fp := strings.TrimSpace(su.FileParse)
		if txt == "" {
			txt = fp
		} else if fp != "" && !strings.Contains(txt, fp) {
			txt = txt + "\n" + fp
		}
		if txt == "" {
			continue
		}
		kind := strings.TrimSpace(su.Kind)
		if kind == "" {
			kind = "sub"
		}
		idx := kindCounts[kind]
		kindCounts[kind] = idx + 1
		subID := strings.TrimSpace(su.ID)
		if subID == "" {
			subID = ComposeSubUnitID(parentID, kind, idx)
		}
		subPkt, _ := e.PackBlindPacket(subID, txt, occurredAt)
		if len(subPkt.Slots) == 0 {
			continue
		}
		subPkt.DocHash = parentDocHash
		if e != nil {
			if blkID := strings.TrimSpace(su.BlockID); blkID != "" {
				subPkt.BlockHash = e.KeyedHash64(blkID)
			} else if subID != "" {
				subPkt.BlockHash = e.KeyedHash64(subID)
			}
		}
		if su.LineStart > 0 {
			subPkt.LineStart = su.LineStart
		}
		if su.LineEnd > 0 {
			subPkt.LineEnd = su.LineEnd
		}
		if subPkt.LineStart > 0 && subPkt.LineEnd < subPkt.LineStart {
			subPkt.LineEnd = subPkt.LineStart
		}
		if parentID != "" {
			subPkt.CitedUnitIDs = []string{parentID}
			subToParent[subID] = parentID
			if idx == 0 {
				subToParent[fmt.Sprintf("%s#%s:0", parentID, kind)] = parentID
			}
		}
		pkts = append(pkts, subPkt)
	}
	return pkts, subToParent
}

// FTS5TrigramSegmenter emits overlapping 3-character lowercase trigrams per word.
func FTS5TrigramSegmenter(sentence string) []string {
	words := SplitWords(sentence)
	if len(words) == 0 {
		return nil
	}
	out := make([]string, 0, len(words)*3)
	for _, w := range words {
		low := strings.ToLower(strings.TrimSpace(w))
		var offsets []int
		for idx := range low {
			offsets = append(offsets, idx)
		}
		offsets = append(offsets, len(low))
		if len(offsets)-1 < 3 {
			continue
		}
		for i := 0; i+3 < len(offsets); i++ {
			out = append(out, low[offsets[i]:offsets[i+3]])
		}
	}
	return out
}

// ExtractInlineSymbolUnits scans a sentence for currency/percentage/temperature symbols attached
// to digits (such as "$480", "€9.10", "£655.59", "¥100", "95%", "25°C") that standard alphanumeric
// word splitters skip, returning their canonical common-core symbols in order of appearance.
func ExtractInlineSymbolUnits(sentence string) []string {
	if len(sentence) < 2 {
		return nil
	}
	var out []string
	seen := make(map[string]bool, 2)
	add := func(tok string) {
		if !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}
	var prevR rune
	hasPrev := false
	for i := 0; i < len(sentence); {
		r, w := utf8.DecodeRuneInString(sentence[i:])
		var nextR rune
		hasNext := false
		if i+w < len(sentence) {
			nextR, _ = utf8.DecodeRuneInString(sentence[i+w:])
			hasNext = true
		}
		prevDigit := hasPrev && unicode.IsDigit(prevR)
		nextDigit := hasNext && unicode.IsDigit(nextR)
		if prevDigit || nextDigit {
			switch r {
			case '$':
				add("usd")
			case '€':
				add("eur")
			case '£':
				add("gbp")
			case '¥', '￥':
				add("cny")
			case '₩':
				add("krw")
			case '₹':
				add("inr")
			case '₽':
				add("rub")
			case '₪':
				add("ils")
			case '₺':
				add("try")
			case '฿':
				add("thb")
			case '₱':
				add("php")
			case '₫':
				add("vnd")
			case '%', '％':
				add("pct")
			case '‰':
				add("ppm")
			case '℃':
				add("degc")
			case '°':
				if hasNext && (nextR == 'C' || nextR == 'c' || nextR == 'F' || nextR == 'f') {
					add("degc")
				}
			}
		}
		prevR = r
		hasPrev = true
		i += w
	}
	return out
}

var seAsianUnitWords = []string{
	"กิโลกรัม", "กรัม", "กิโลเมตร", "เซนติเมตร", "เมตร", "มิลลิลิตร", "ลิตร",
	"ชั่วโมง", "สัปดาห์", "วินาที", "นาที", "เดือน", "วัน", "ปี", "บาท", "องศา", "เปอร์เซ็นต์", "กิโลวัตต์",
	"ກິໂລກຣາມ", "ກຣາມ", "ກິໂລແມັດ", "ແມັດ", "ລິດ", "ຊົ່ວໂມງ", "ອາທິດ", "ວິນາທີ", "ນາທີ", "ເດືອນ", "ມື້", "ປີ", "ກີບ", "ເປີເຊັນ",
	"គីឡូក្រាម", "ក្រាម", "គីឡូម៉ែត្រ", "ម៉ែត្រ", "លីត្រ", "ម៉ោង", "សប្តាហ៍", "វិនាទី", "នាទី", "ខែ", "ថ្ងៃ", "ឆ្នាំ", "ភាគរយ",
}

// ExtractSEAsianUnitWords scans unspaced SEA text for companion unit words (longest-first).
func ExtractSEAsianUnitWords(sentence string) []string {
	if strings.TrimSpace(sentence) == "" {
		return nil
	}
	var out []string
	for _, w := range seAsianUnitWords {
		if !strings.Contains(sentence, w) {
			continue
		}
		absorbed := false
		for _, prev := range out {
			if strings.Contains(prev, w) {
				absorbed = true
				break
			}
		}
		if !absorbed {
			out = append(out, w)
		}
	}
	return out
}

const (
	WarnCoarseUnsegmentedCarrier = "COARSE_UNSEGMENTED_CARRIER"
	WarnOverFragmentedDust       = "OVER_FRAGMENTED_DUST"
	WarnFlatCarrierModulation    = "FLAT_CARRIER_MODULATION"
	WarnDegenerateSentenceFrames = "DEGENERATE_SENTENCE_FRAMES"
	WarnHighTandemRedundancy     = "HIGH_TANDEM_REDUNDANCY"
	WarnCooccurHorizonTruncated  = "COOCCUR_HORIZON_TRUNCATED"

	cooccurHorizonCap = 64
)

type PulseSignalWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PulseSignalReport struct {
	Score                    int                  `json:"score"`
	Grade                    string               `json:"grade"`
	NeedsAttention           bool                 `json:"needs_attention"`
	TotalSlots               int                  `json:"total_slots"`
	UniqueSlotCount          int                  `json:"unique_slot_count"`
	SentenceFrameCount       int                  `json:"sentence_frame_count"`
	MeanCarrierBitsX10       int                  `json:"mean_carrier_bits_x10"`
	GluedSlotCount           int                  `json:"glued_slot_count"`
	GluedSlotPermille        int                  `json:"glued_slot_permille"`
	DustSlotCount            int                  `json:"dust_slot_count"`
	DustSlotPermille         int                  `json:"dust_slot_permille"`
	TandemRepeatCount        int                  `json:"tandem_repeat_count"`
	TandemRepeatPermille     int                  `json:"tandem_repeat_permille"`
	SingleSlotFrameCount     int                  `json:"single_slot_frame_count"`
	SingleSlotFramePermille  int                  `json:"single_slot_frame_permille"`
	WidthSpreadPermille      int                  `json:"width_spread_permille"`
	CooccurCoveredUnique     int                  `json:"cooccur_covered_unique"`
	CooccurTruncatedUnique   int                  `json:"cooccur_truncated_unique"`
	CooccurCoveragePermille  int                  `json:"cooccur_coverage_permille"`
	FoldedBiUniSpans         int                  `json:"folded_bi_uni_spans"`
	CompoundHarmonicGroups   int                  `json:"compound_harmonic_groups"`
	CarrierHealthSubscore    int                  `json:"carrier_health_subscore"`
	ModulationHealthSubscore int                  `json:"modulation_health_subscore"`
	FramingHealthSubscore    int                  `json:"framing_health_subscore"`
	DiversityHealthSubscore  int                  `json:"diversity_health_subscore"`
	CooccurHealthSubscore    int                  `json:"cooccur_health_subscore"`
	FTS5BM25Enabled          bool                 `json:"fts5_bm25_enabled"`
	Warnings                 []PulseSignalWarning `json:"warnings,omitempty"`
}

// EvaluatePulseSignal computes a 100% plaintext-free 5-dimension segmentation quality report.
func EvaluatePulseSignal(pkt BlindPulsePacket) PulseSignalReport {
	slots := pkt.Slots
	frames := pkt.SentenceFrames
	if len(slots) == 0 && len(frames) > 0 {
		for _, fr := range frames {
			slots = append(slots, fr...)
		}
	}
	n := len(slots)
	if n == 0 {
		return PulseSignalReport{Score: 100, Grade: "EMPTY"}
	}
	if len(frames) == 0 {
		frames = [][]BlindTokenTerm{slots}
	}
	m := len(frames)

	inHarmonicSpan := make([]bool, n)
	foldedSpans, compoundGroups, companionSlots := markHarmonicConservationSpans(slots, inHarmonicSpan)
	effectiveUnits := n - companionSlots
	if effectiveUnits < 1 {
		effectiveUnits = 1
	}

	uniqueMap := make(map[uint64]int, n)
	var sumB int
	minB, maxB := int(slots[0].RawBitsX10), int(slots[0].RawBitsX10)
	gluedCnt, dustCnt := 0, 0
	for i, s := range slots {
		uniqueMap[s.H]++
		b := int(s.RawBitsX10)
		sumB += b
		if b < minB {
			minB = b
		}
		if b > maxB {
			maxB = b
		}
		if b > 320 && !inHarmonicSpan[i] {
			gluedCnt++
		}
		if b <= 20 && s.ClassFlags == 0 && !inHarmonicSpan[i] {
			dustCnt++
		}
	}

	isTandem := make([]bool, n)
	for p := 1; p <= 3; p++ {
		repRun := 1
		for i := p; i < n; i++ {
			if slots[i].H != 0 && slots[i].H == slots[i-p].H {
				repRun++
				if repRun >= 2 {
					isTandem[i] = true
				}
			} else {
				repRun = 1
			}
		}
	}
	tandemCnt := 0
	for _, td := range isTandem {
		if td {
			tandemCnt++
		}
	}

	singleSlotFrames := 0
	for _, fr := range frames {
		if len(fr) == 1 {
			singleSlotFrames++
		}
	}

	u := len(uniqueMap)
	meanB := sumB / n
	gluedPermille := (gluedCnt * 1000) / n
	dustPermille := (dustCnt * 1000) / n
	tandemPermille := (tandemCnt * 1000) / n
	singleFramePermille := (singleSlotFrames * 1000) / m

	spreadPermille := 0
	if maxB+minB > 0 {
		spreadPermille = ((maxB - minB) * 1000) / (maxB + minB)
	}

	coCovered, coTrunc, coCoveragePermille := u, 0, 1000
	if u > cooccurHorizonCap {
		coCovered = cooccurHorizonCap
		coTrunc = u - cooccurHorizonCap
		coCoveragePermille = (cooccurHorizonCap * 1000) / u
	}

	carrierScore := 100
	if gluedPermille > 50 {
		penalty := (gluedPermille - 50) / 8
		if meanB > 400 {
			penalty += (meanB - 400) / 20
		}
		carrierScore -= penalty
	}
	if dustPermille > 350 {
		carrierScore -= (dustPermille - 350) / 7
	}
	carrierScore = clampScore100(carrierScore)

	modScore := 100
	if n >= 4 {
		if spreadPermille < 120 && u <= 2 {
			modScore -= (120 - spreadPermille) / 2
		} else if spreadPermille == 0 && n >= 6 && dustPermille > 500 {
			modScore -= 45
		}
	}
	if n == 1 && meanB > 320 {
		modScore = 35
	}
	modScore = clampScore100(modScore)

	framingScore := 100
	if effectiveUnits >= 48 && m == 1 {
		framingScore -= minInt(65, (effectiveUnits-32)/2)
	}
	if m >= 6 && singleFramePermille > 600 {
		framingScore -= (singleFramePermille - 500) / 8
	}
	framingScore = clampScore100(framingScore)

	divScore := 100
	if tandemPermille > 120 {
		divScore -= (tandemPermille - 120) / 8
	}
	uniquePermille := (u * 1000) / n
	if n >= 12 && uniquePermille < 200 {
		divScore -= (200 - uniquePermille) / 3
	}
	divScore = clampScore100(divScore)

	coScore := 100
	if coCoveragePermille < 1000 {
		coScore = 40 + (coCoveragePermille*60)/1000
	}
	coScore = clampScore100(coScore)

	composite := clampScore100((carrierScore*25 + modScore*20 + framingScore*20 + divScore*20 + coScore*15) / 100)

	var warnings []PulseSignalWarning
	if gluedPermille >= 150 || (n == 1 && meanB > 320) {
		warnings = append(warnings, PulseSignalWarning{
			Code:    WarnCoarseUnsegmentedCarrier,
			Message: "Coarse segmentation detected: high share of glued slots (>32 bits) triggers rate-distortion soft saturation and degrades partial-query recall; enable 2-gram sliding windows or subword splitting.",
		})
	}
	if dustPermille >= 500 {
		warnings = append(warnings, PulseSignalWarning{
			Code:    WarnOverFragmentedDust,
			Message: "Over-fragmented segmentation detected: excessive single-character dust slots dilute document frequency (DF) and collapse waveform modulation contrast; emit word-level or harmonic parent atoms.",
		})
	}
	if modScore < 65 {
		warnings = append(warnings, PulseSignalWarning{
			Code:    WarnFlatCarrierModulation,
			Message: "Flat carrier modulation detected: uniform or single-slot pulse width reduces waveform peak-to-floor contrast and weakens cell addressing.",
		})
	}
	if (effectiveUnits >= 48 && m == 1) || (m >= 6 && singleFramePermille > 600) {
		warnings = append(warnings, PulseSignalWarning{
			Code:    WarnDegenerateSentenceFrames,
			Message: "Degenerate sentence framing detected: missing sentence boundaries or single-word frames impair cloud fractal dry-sentence distillation and evidence span delivery.",
		})
	}
	if tandemPermille >= 200 {
		warnings = append(warnings, PulseSignalWarning{
			Code:    WarnHighTandemRedundancy,
			Message: "High tandem pulse repetition detected: consecutive repeated token hashes trigger periodic damping; strip formatting separators before packing.",
		})
	}
	if coTrunc > 32 {
		warnings = append(warnings, PulseSignalWarning{
			Code:    WarnCooccurHorizonTruncated,
			Message: "Co-occurrence horizon truncation detected: unique atoms per unit substantially exceed the 64-atom co-occurrence window; split long documents into paragraph units or use harmonic parent grouping.",
		})
	}

	grade := "F"
	switch {
	case composite >= 90:
		grade = "S"
	case composite >= 80:
		grade = "A"
	case composite >= 70:
		grade = "B"
	case composite >= 55:
		grade = "C"
	}

	return PulseSignalReport{
		Score:                    composite,
		Grade:                    grade,
		NeedsAttention:           composite < 70 || len(warnings) > 0,
		TotalSlots:               n,
		UniqueSlotCount:          u,
		SentenceFrameCount:       m,
		MeanCarrierBitsX10:       meanB,
		GluedSlotCount:           gluedCnt,
		GluedSlotPermille:        gluedPermille,
		DustSlotCount:            dustCnt,
		DustSlotPermille:         dustPermille,
		TandemRepeatCount:        tandemCnt,
		TandemRepeatPermille:     tandemPermille,
		SingleSlotFrameCount:     singleSlotFrames,
		SingleSlotFramePermille:  singleFramePermille,
		WidthSpreadPermille:      spreadPermille,
		CooccurCoveredUnique:     coCovered,
		CooccurTruncatedUnique:   coTrunc,
		CooccurCoveragePermille:  coCoveragePermille,
		FoldedBiUniSpans:         foldedSpans,
		CompoundHarmonicGroups:   compoundGroups,
		CarrierHealthSubscore:    carrierScore,
		ModulationHealthSubscore: modScore,
		FramingHealthSubscore:    framingScore,
		DiversityHealthSubscore:  divScore,
		CooccurHealthSubscore:    coScore,
		Warnings:                 warnings,
	}
}

func markHarmonicConservationSpans(slots []BlindTokenTerm, inSpan []bool) (foldedSpans int, compoundGroups int, companionSlots int) {
	n := len(slots)
	for i := 0; i < n; {
		if slots[i].ClassFlags != 0 || slots[i].RawBitsX10 < 70 {
			i++
			continue
		}
		bestM := 0
		maxM := (n - i + 1) / 2
		if maxM > 64 {
			maxM = 64
		}
		for m := maxM; m >= 2; m-- {
			biCnt := m - 1
			uniStart := i + biCnt
			uniBase := int(slots[uniStart].RawBitsX10)
			if uniBase < 35 || uniBase > 150 {
				continue
			}
			valid := true
			for k := 0; k < m; k++ {
				uSlot := slots[uniStart+k]
				if uSlot.ClassFlags != 0 {
					valid = false
					break
				}
				diffU := int(uSlot.RawBitsX10) - uniBase
				if diffU < -20 || diffU > 20 {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			for k := 0; k < biCnt; k++ {
				bSlot := slots[i+k]
				if bSlot.ClassFlags != 0 {
					valid = false
					break
				}
				expectedBi := int(slots[uniStart+k].RawBitsX10) + int(slots[uniStart+k+1].RawBitsX10)
				diffB := int(bSlot.RawBitsX10) - expectedBi
				if diffB < -25 || diffB > 25 {
					valid = false
					break
				}
			}
			if valid {
				bestM = m
				break
			}
		}
		if bestM >= 2 {
			foldedSpans++
			companionSlots += bestM - 1
			spanLen := 2*bestM - 1
			for k := 0; k < spanLen; k++ {
				inSpan[i+k] = true
			}
			i += spanLen
			continue
		}
		i++
	}

	isSub := make([]bool, n)
	for i := 0; i < n; i++ {
		if isSub[i] {
			continue
		}
		harmEnd := i
		for j := i + 1; j < n && j <= i+2; j++ {
			cur := slots[j]
			isParityHarm := (cur.ClassFlags&FlagHasConn) != 0 && (cur.ClassFlags&FlagHasDigit) == 0 &&
				slots[i].RawBitsX10 >= 20 && (slots[i].RawBitsX10&1) == 0 &&
				(cur.RawBitsX10 == slots[i].RawBitsX10-1 || (slots[i].RawBitsX10 >= 22 && cur.RawBitsX10 == slots[i].RawBitsX10-3))
			if isParityHarm || (cur.ClassFlags&FlagIsHarmonicCompanion) != 0 {
				harmEnd = j
			} else {
				break
			}
		}
		if harmEnd > i {
			compoundGroups++
			companionSlots += harmEnd - i
			inSpan[i] = true
			for j := i + 1; j <= harmEnd; j++ {
				isSub[j] = true
				inSpan[j] = true
			}
			i = harmEnd
			continue
		}

		parentB := int(slots[i].RawBitsX10)
		hasCompoundFlag := (slots[i].ClassFlags & (FlagHasConn | FlagHasCamel | FlagHasDigit)) != 0
		if !hasCompoundFlag || parentB < 60 {
			continue
		}
		matchedEnd := -1
		sumSubB, maxSubB := 0, 0
		distinctSubH := false
		firstSubH := uint64(0)
		maxSub := minInt(n-1, i+8)
		for j := i + 1; j <= maxSub; j++ {
			subB := int(slots[j].RawBitsX10)
			if subB <= 0 || subB >= parentB || slots[j].H == slots[i].H {
				break
			}
			if j == i+1 {
				firstSubH = slots[j].H
			} else if slots[j].H != firstSubH {
				distinctSubH = true
			}
			if subB > maxSubB {
				maxSubB = subB
			}
			sumSubB += subB
			subCnt := j - i
			if subCnt >= 2 && maxSubB >= 33 && distinctSubH {
				diff := parentB - sumSubB
				if diff >= -10 && diff <= 15*subCnt {
					matchedEnd = j
					break
				}
			}
			if sumSubB > parentB+10 {
				break
			}
		}
		if matchedEnd > i+1 {
			compoundGroups++
			companionSlots += matchedEnd - i
			inSpan[i] = true
			for j := i + 1; j <= matchedEnd; j++ {
				isSub[j] = true
				inSpan[j] = true
			}
			i = matchedEnd
		}
	}
	return foldedSpans, compoundGroups, companionSlots
}

func clampScore100(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// Compact Binary Frame Serialization ("RMBP" v1 · Mixed-Radix Golomb-Rice)
// ============================================================================

const (
	wireMagic                   = "RMBP"
	wireVersion                 = 1
	wireMaxF                    = 0xFF
	wireHeaderBytes             = 16
	wireMaxDictN                = 65535
	wireMaxSlotN                = 1 << 20
	wireMaxSentN                = 65535
	wireMaxPalN                 = 255
	wireFlagIdentityIndex uint8 = 1 << 0
	wireFlagWideF         uint8 = 1 << 1
	wireMaxFlag                 = wireFlagIdentityIndex | wireFlagWideF
)

type wireBF struct {
	B uint16
	F uint8
}

type wireAtomKey struct {
	H          uint64
	RawBitsX10 uint16
	ClassFlags uint8
}

func wireAtomLess(a, b wireAtomKey) bool {
	if a.H != b.H {
		return a.H < b.H
	}
	if a.RawBitsX10 != b.RawBitsX10 {
		return a.RawBitsX10 < b.RawBitsX10
	}
	return a.ClassFlags < b.ClassFlags
}

func wireGolombKForDict(dictN int) int {
	if dictN <= 0 {
		return 0
	}
	if dictN == 1 {
		return 64
	}
	lg := bits.Len(uint(dictN)) - 1
	k := 64 - lg
	if k < 48 {
		return 48
	}
	if k > 64 {
		return 64
	}
	return k
}

func wireBitsForDict(dictN int) int {
	if dictN <= 1 {
		return 0
	}
	return bits.Len(uint(dictN - 1))
}

// EncodeWirePayload encodes flat slots and sentence frames into the canonical base64 payload.
func EncodeWirePayload(slots []BlindTokenTerm, frames [][]BlindTokenTerm) (string, error) {
	raw, err := EncodeWireBytes(slots, frames)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// DecodeWirePayload decodes a canonical base64 payload back into flat slots and sentence frames.
func DecodeWirePayload(b64 string) ([]BlindTokenTerm, [][]BlindTokenTerm, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, fmt.Errorf("codec: decode wire base64: %w", err)
	}
	return DecodeWireBytes(raw)
}

// EncodeWireBytes encodes flat slots and sentence frames into the compact lossless binary stream.
func EncodeWireBytes(slots []BlindTokenTerm, frames [][]BlindTokenTerm) ([]byte, error) {
	sent := make([]int, 0, len(frames))
	if len(frames) > 0 {
		cum := 0
		for _, fr := range frames {
			cum += len(fr)
			sent = append(sent, cum)
		}
		if len(slots) == 0 {
			slots = make([]BlindTokenTerm, 0, cum)
			for _, fr := range frames {
				slots = append(slots, fr...)
			}
		} else {
			if cum != len(slots) {
				return nil, fmt.Errorf("codec: sentence frames hold %d atoms, slots hold %d", cum, len(slots))
			}
			pos := 0
			for sIdx, fr := range frames {
				for fIdx, fa := range fr {
					if slots[pos] != fa {
						return nil, fmt.Errorf("codec: sentence frame[%d][%d] mismatches slots[%d]", sIdx, fIdx, pos)
					}
					pos++
				}
			}
		}
	}
	if len(slots) > wireMaxSlotN {
		return nil, fmt.Errorf("codec: slotN=%d exceeds capacity", len(slots))
	}
	if len(sent) > wireMaxSentN {
		return nil, fmt.Errorf("codec: sentN=%d exceeds capacity", len(sent))
	}
	for i, a := range slots {
		if a.ClassFlags > wireMaxF {
			return nil, fmt.Errorf("codec: atom[%d] flag %#x out of 8-bit range (0x00..0xFF)", i, a.ClassFlags)
		}
	}
	if err := checkWireOffsets(sent, len(slots)); err != nil {
		return nil, err
	}

	all := make([]wireAtomKey, len(slots))
	for i, s := range slots {
		all[i] = wireAtomKey{H: s.H, RawBitsX10: s.RawBitsX10, ClassFlags: s.ClassFlags}
	}
	sort.Slice(all, func(i, j int) bool { return wireAtomLess(all[i], all[j]) })

	dict := make([]wireAtomKey, 0, len(slots))
	if len(all) > 0 {
		dict = append(dict, all[0])
		for i := 1; i < len(all); i++ {
			if all[i] != all[i-1] {
				dict = append(dict, all[i])
			}
		}
	}
	if len(dict) > wireMaxDictN {
		return nil, fmt.Errorf("codec: dictN exceeds %d", wireMaxDictN)
	}

	dictPos := make(map[wireAtomKey]int, len(dict))
	for i, a := range dict {
		dictPos[a] = i
	}
	index := make([]int, len(slots))
	identity := len(dict) == len(slots)
	for i, s := range slots {
		p := dictPos[wireAtomKey{H: s.H, RawBitsX10: s.RawBitsX10, ClassFlags: s.ClassFlags}]
		index[i] = p
		if p != i {
			identity = false
		}
	}

	grK := wireGolombKForDict(len(dict))

	var palette []wireBF
	palPos := make(map[wireBF]int, 32)
	for _, a := range dict {
		bf := wireBF{B: a.RawBitsX10, F: a.ClassFlags}
		if _, ok := palPos[bf]; !ok {
			palPos[bf] = len(palette)
			palette = append(palette, bf)
		}
	}
	usePalette := len(dict) > 0 && len(palette) <= wireMaxPalN
	palN := 0
	if usePalette {
		palN = len(palette)
		sort.Slice(palette, func(i, j int) bool {
			if palette[i].B != palette[j].B {
				return palette[i].B < palette[j].B
			}
			return palette[i].F < palette[j].F
		})
		for i, bf := range palette {
			palPos[bf] = i
		}
	}

	var flags uint8
	if identity {
		flags |= wireFlagIdentityIndex
	}
	wideF := false
	for _, a := range dict {
		if a.ClassFlags > 0x0F {
			wideF = true
			break
		}
	}
	if wideF {
		flags |= wireFlagWideF
	}

	out := make([]byte, 0, wireHeaderBytes+len(dict)*8)
	out = append(out, wireMagic...)
	out = append(out, wireVersion, flags, uint8(grK), uint8(palN))
	out = binary.BigEndian.AppendUint16(out, uint16(len(dict)))
	out = binary.BigEndian.AppendUint32(out, uint32(len(slots)))
	out = binary.BigEndian.AppendUint16(out, uint16(len(sent)))

	if usePalette {
		for _, bf := range palette {
			out = appendWireVarint(out, bf.B)
		}
		if wideF {
			for _, bf := range palette {
				out = append(out, bf.F)
			}
		} else {
			fBytes := (palN + 1) / 2
			fCol := make([]byte, fBytes)
			for i, bf := range palette {
				if i%2 == 0 {
					fCol[i/2] = bf.F
				} else {
					fCol[i/2] |= bf.F << 4
				}
			}
			out = append(out, fCol...)
		}
	} else if len(dict) > 0 {
		for _, a := range dict {
			out = appendWireVarint(out, a.RawBitsX10)
		}
		if wideF {
			for _, a := range dict {
				out = append(out, a.ClassFlags)
			}
		} else {
			fBytes := (len(dict) + 1) / 2
			fCol := make([]byte, fBytes)
			for i, a := range dict {
				if i%2 == 0 {
					fCol[i/2] = a.ClassFlags
				} else {
					fCol[i/2] |= a.ClassFlags << 4
				}
			}
			out = append(out, fCol...)
		}
	}

	bw := &wireBitWriter{buf: make([]byte, 0, len(dict)*8+len(slots))}
	var prevH uint64
	for i, a := range dict {
		var delta uint64
		if i == 0 {
			delta = a.H
		} else {
			delta = a.H - prevH
		}
		prevH = a.H
		if grK == 64 {
			bw.write(delta, 64)
		} else {
			q := delta >> uint(grK)
			rem := delta & ((uint64(1) << uint(grK)) - 1)
			for u := uint64(0); u < q; u++ {
				bw.write(1, 1)
			}
			bw.write(0, 1)
			bw.write(rem, grK)
		}
	}

	if usePalette {
		palBits := wireBitsForDict(palN)
		if palBits > 0 {
			for _, a := range dict {
				bw.write(uint64(palPos[wireBF{B: a.RawBitsX10, F: a.ClassFlags}]), palBits)
			}
		}
	}

	if !identity && len(dict) > 1 {
		seenPos := make([]int, len(dict))
		for i := range seenPos {
			seenPos[i] = -1
		}
		remaining := make([]int, len(dict))
		for i := range remaining {
			remaining[i] = i
		}
		seenCount := 0
		for _, dIdx := range index {
			if seenCount == 0 {
				rPos := sort.SearchInts(remaining, dIdx)
				remBits := wireBitsForDict(len(remaining))
				bw.write(uint64(rPos), remBits)
				remaining = append(remaining[:rPos], remaining[rPos+1:]...)
				seenPos[dIdx] = seenCount
				seenCount++
				continue
			}
			if sp := seenPos[dIdx]; sp >= 0 {
				if len(remaining) > 0 {
					bw.write(1, 1)
				}
				sBits := wireBitsForDict(seenCount)
				if sBits > 0 {
					bw.write(uint64(sp), sBits)
				}
			} else {
				bw.write(0, 1)
				rPos := sort.SearchInts(remaining, dIdx)
				remBits := wireBitsForDict(len(remaining))
				if remBits > 0 {
					bw.write(uint64(rPos), remBits)
				}
				remaining = append(remaining[:rPos], remaining[rPos+1:]...)
				seenPos[dIdx] = seenCount
				seenCount++
			}
		}
	}
	bw.flush()
	out = append(out, bw.buf...)

	prevOff := 0
	for _, o := range sent {
		deltaLen := uint32(o - prevOff - 1)
		out = appendWireVarint32(out, deltaLen)
		prevOff = o
	}
	return out, nil
}

// DecodeWireBytes decodes a compact lossless binary stream into flat slots and sentence frames.
func DecodeWireBytes(raw []byte) ([]BlindTokenTerm, [][]BlindTokenTerm, error) {
	if len(raw) < wireHeaderBytes {
		return nil, nil, errors.New("codec: wire payload truncated")
	}
	if string(raw[0:4]) != wireMagic {
		return nil, nil, errors.New("codec: wire payload magic mismatch")
	}
	if raw[4] != wireVersion {
		return nil, nil, fmt.Errorf("codec: wire payload unsupported version %d", raw[4])
	}
	flags, grK, palN := raw[5], int(raw[6]), int(raw[7])
	if flags&^wireMaxFlag != 0 {
		return nil, nil, errors.New("codec: wire payload invalid flags")
	}
	dictN := int(binary.BigEndian.Uint16(raw[8:10]))
	slotN := int(binary.BigEndian.Uint32(raw[10:14]))
	sentN := int(binary.BigEndian.Uint16(raw[14:16]))

	if dictN != 0 && grK != wireGolombKForDict(dictN) {
		return nil, nil, fmt.Errorf("codec: wire payload grK=%d want %d", grK, wireGolombKForDict(dictN))
	}
	if dictN == 0 && (grK != 0 || palN != 0 || slotN != 0) {
		return nil, nil, errors.New("codec: wire payload invalid empty header")
	}
	if palN > dictN {
		return nil, nil, errors.New("codec: wire payload palN > dictN")
	}
	identity := flags&wireFlagIdentityIndex != 0
	wideF := flags&wireFlagWideF != 0
	if identity && dictN != slotN {
		return nil, nil, errors.New("codec: wire payload identity conflicts with dictN/slotN")
	}
	if dictN == 0 && wideF {
		return nil, nil, errors.New("codec: wire payload invalid empty wideF header")
	}
	if slotN > wireMaxSlotN || (dictN > 1 && slotN > len(raw)*8) {
		return nil, nil, fmt.Errorf("codec: wire payload slotN=%d exceeds capacity", slotN)
	}

	off := wireHeaderBytes
	dict := make([]wireAtomKey, dictN)
	var palette []wireBF

	if palN > 0 {
		palette = make([]wireBF, palN)
		for i := range palette {
			v, n, err := readWireVarint(raw, off)
			if err != nil {
				return nil, nil, err
			}
			palette[i].B = v
			off += n
		}
		if wideF {
			if off+palN > len(raw) {
				return nil, nil, errors.New("codec: wire payload truncated in wide palette")
			}
			hasHighNib := false
			for i := range palette {
				palette[i].F = raw[off+i]
				if palette[i].F > 0x0F {
					hasHighNib = true
				}
			}
			if !hasHighNib {
				return nil, nil, errors.New("codec: wire payload unnecessary wideF flag")
			}
			off += palN
		} else {
			fBytes := (palN + 1) / 2
			if off+fBytes > len(raw) {
				return nil, nil, errors.New("codec: wire payload truncated in palette")
			}
			for i := range palette {
				nib := raw[off+i/2] & 0x0F
				if i%2 == 1 {
					nib = raw[off+i/2] >> 4
				}
				palette[i].F = nib
			}
			if palN%2 == 1 && (raw[off+fBytes-1]&0xF0) != 0 {
				return nil, nil, errors.New("codec: wire payload non-zero palette padding")
			}
			off += fBytes
		}
		for i := 1; i < palN; i++ {
			if palette[i].B < palette[i-1].B || (palette[i].B == palette[i-1].B && palette[i].F <= palette[i-1].F) {
				return nil, nil, errors.New("codec: wire payload palette not strictly sorted")
			}
		}
	} else if dictN > 0 {
		for i := range dict {
			v, n, err := readWireVarint(raw, off)
			if err != nil {
				return nil, nil, err
			}
			dict[i].RawBitsX10 = v
			off += n
		}
		if wideF {
			if off+dictN > len(raw) {
				return nil, nil, errors.New("codec: wire payload truncated in wide f-column")
			}
			hasHighNib := false
			for i := range dict {
				dict[i].ClassFlags = raw[off+i]
				if dict[i].ClassFlags > 0x0F {
					hasHighNib = true
				}
			}
			if !hasHighNib {
				return nil, nil, errors.New("codec: wire payload unnecessary wideF flag")
			}
			off += dictN
		} else {
			fBytes := (dictN + 1) / 2
			if off+fBytes > len(raw) {
				return nil, nil, errors.New("codec: wire payload truncated in f-column")
			}
			for i := range dict {
				nib := raw[off+i/2] & 0x0F
				if i%2 == 1 {
					nib = raw[off+i/2] >> 4
				}
				dict[i].ClassFlags = nib
			}
			if dictN%2 == 1 && (raw[off+fBytes-1]&0xF0) != 0 {
				return nil, nil, errors.New("codec: wire payload non-zero f-column padding")
			}
			off += fBytes
		}
	}

	br := &wireBitReader{raw: raw[off:]}
	var prevH uint64
	maxUnary := (uint64(1) << uint(64-grK)) - 1
	for i := range dict {
		var delta uint64
		if grK == 64 {
			v, err := br.read(64)
			if err != nil {
				return nil, nil, err
			}
			delta = v
		} else {
			var q uint64
			for {
				bit, err := br.read(1)
				if err != nil {
					return nil, nil, err
				}
				if bit == 0 {
					break
				}
				q++
				if q > maxUnary {
					return nil, nil, errors.New("codec: wire payload unary overflow")
				}
			}
			rem, err := br.read(grK)
			if err != nil {
				return nil, nil, err
			}
			delta = (q << uint(grK)) | rem
		}
		if i > 0 && prevH+delta < prevH {
			return nil, nil, errors.New("codec: wire payload delta overflow")
		}
		dict[i].H = prevH + delta
		prevH = dict[i].H
	}

	if palN > 0 {
		palBits := wireBitsForDict(palN)
		for i := range dict {
			pIdx := uint64(0)
			if palBits > 0 {
				var err error
				pIdx, err = br.read(palBits)
				if err != nil {
					return nil, nil, err
				}
				if pIdx >= uint64(palN) {
					return nil, nil, errors.New("codec: wire payload palette index out of range")
				}
			}
			dict[i].RawBitsX10 = palette[pIdx].B
			dict[i].ClassFlags = palette[pIdx].F
		}
	}

	for i := 1; i < dictN; i++ {
		if !wireAtomLess(dict[i-1], dict[i]) {
			return nil, nil, errors.New("codec: wire payload dictionary not sorted")
		}
	}

	index := make([]int, slotN)
	if identity {
		for i := range index {
			index[i] = i
		}
	} else if dictN == 1 {
		for i := range index {
			index[i] = 0
		}
	} else if dictN > 1 {
		remaining := make([]int, dictN)
		for i := range remaining {
			remaining[i] = i
		}
		seenList := make([]int, 0, dictN)
		for i := 0; i < slotN; i++ {
			if len(seenList) == 0 {
				remBits := wireBitsForDict(len(remaining))
				rPos, err := br.read(remBits)
				if err != nil {
					return nil, nil, err
				}
				if int(rPos) >= len(remaining) {
					return nil, nil, errors.New("codec: wire payload slot index out of range")
				}
				dIdx := remaining[rPos]
				remaining = append(remaining[:rPos], remaining[rPos+1:]...)
				seenList = append(seenList, dIdx)
				index[i] = dIdx
				continue
			}
			isRepeat := uint64(1)
			if len(remaining) > 0 {
				var err error
				isRepeat, err = br.read(1)
				if err != nil {
					return nil, nil, err
				}
			}
			if isRepeat == 1 {
				sBits := wireBitsForDict(len(seenList))
				var sp uint64
				if sBits > 0 {
					var err error
					sp, err = br.read(sBits)
					if err != nil {
						return nil, nil, err
					}
					if int(sp) >= len(seenList) {
						return nil, nil, errors.New("codec: wire payload seen index out of range")
					}
				}
				index[i] = seenList[sp]
			} else {
				remBits := wireBitsForDict(len(remaining))
				var rPos uint64
				if remBits > 0 {
					var err error
					rPos, err = br.read(remBits)
					if err != nil {
						return nil, nil, err
					}
					if int(rPos) >= len(remaining) {
						return nil, nil, errors.New("codec: wire payload remaining index out of range")
					}
				}
				dIdx := remaining[rPos]
				remaining = append(remaining[:rPos], remaining[rPos+1:]...)
				seenList = append(seenList, dIdx)
				index[i] = dIdx
			}
		}
		if len(remaining) != 0 {
			return nil, nil, errors.New("codec: wire payload unused dictionary entry")
		}
	}

	if err := br.verifyZeroPadding(); err != nil {
		return nil, nil, err
	}
	off += br.bytesRead()

	var sent []int
	if sentN > 0 {
		sent = make([]int, sentN)
		prevOff := 0
		for i := 0; i < sentN; i++ {
			deltaLen, n, err := readWireVarint32(raw, off)
			if err != nil {
				return nil, nil, err
			}
			off += n
			nextOff := prevOff + int(deltaLen) + 1
			if nextOff <= prevOff || nextOff > slotN {
				return nil, nil, errors.New("codec: wire payload invalid sentence offset")
			}
			sent[i] = nextOff
			prevOff = nextOff
		}
	}
	if off != len(raw) {
		return nil, nil, errors.New("codec: wire payload trailing bytes")
	}
	if err := checkWireOffsets(sent, slotN); err != nil {
		return nil, nil, err
	}

	atoms := make([]BlindTokenTerm, slotN)
	for i, p := range index {
		d := dict[p]
		atoms[i] = BlindTokenTerm{H: d.H, RawBitsX10: d.RawBitsX10, ClassFlags: d.ClassFlags}
	}
	var frames [][]BlindTokenTerm
	if len(sent) > 0 {
		frames = make([][]BlindTokenTerm, 0, len(sent))
		prev := 0
		for _, o := range sent {
			frames = append(frames, atoms[prev:o])
			prev = o
		}
	}
	return atoms, frames, nil
}

func checkWireOffsets(sent []int, slotN int) error {
	prev := 0
	for i, o := range sent {
		if o <= prev || o > slotN {
			return fmt.Errorf("codec: invalid sentence offset[%d]=%d (prev=%d slots=%d)", i, o, prev, slotN)
		}
		prev = o
	}
	if len(sent) > 0 && prev != slotN {
		return fmt.Errorf("codec: last sentence offset %d != slots %d", prev, slotN)
	}
	return nil
}

func wireVarintLen(v uint16) int {
	switch {
	case v < 0x80:
		return 1
	case v < 0x4000:
		return 2
	default:
		return 3
	}
}

func appendWireVarint(dst []byte, v uint16) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func readWireVarint(raw []byte, off int) (uint16, int, error) {
	var out uint32
	shift := uint(0)
	for n := 1; ; n++ {
		if off+n-1 >= len(raw) {
			return 0, 0, errors.New("codec: wire varint truncated")
		}
		if n > 3 {
			return 0, 0, errors.New("codec: wire varint overflow")
		}
		c := raw[off+n-1]
		out |= uint32(c&0x7F) << shift
		if c&0x80 == 0 {
			if out > 0xFFFF {
				return 0, 0, errors.New("codec: wire varint u16 overflow")
			}
			v := uint16(out)
			if wireVarintLen(v) != n {
				return 0, 0, errors.New("codec: wire varint non-canonical")
			}
			return v, n, nil
		}
		shift += 7
	}
}

func wireVarint32Len(v uint32) int {
	switch {
	case v < (1 << 7):
		return 1
	case v < (1 << 14):
		return 2
	case v < (1 << 21):
		return 3
	case v < (1 << 28):
		return 4
	default:
		return 5
	}
}

func appendWireVarint32(dst []byte, v uint32) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func readWireVarint32(raw []byte, off int) (uint32, int, error) {
	var out uint64
	shift := uint(0)
	for n := 1; ; n++ {
		if off+n-1 >= len(raw) {
			return 0, 0, errors.New("codec: wire varint32 truncated")
		}
		if n > 5 {
			return 0, 0, errors.New("codec: wire varint32 overflow")
		}
		c := raw[off+n-1]
		out |= uint64(c&0x7F) << shift
		if c&0x80 == 0 {
			if out > 0xFFFFFFFF {
				return 0, 0, errors.New("codec: wire varint32 u32 overflow")
			}
			v := uint32(out)
			if wireVarint32Len(v) != n {
				return 0, 0, errors.New("codec: wire varint32 non-canonical")
			}
			return v, n, nil
		}
		shift += 7
	}
}

type wireBitWriter struct {
	buf []byte
	cur byte
	n   uint
}

func (w *wireBitWriter) write(v uint64, width int) {
	for i := width - 1; i >= 0; i-- {
		w.cur = w.cur<<1 | byte((v>>uint(i))&1)
		w.n++
		if w.n == 8 {
			w.buf = append(w.buf, w.cur)
			w.cur, w.n = 0, 0
		}
	}
}

func (w *wireBitWriter) flush() {
	if w.n > 0 {
		w.buf = append(w.buf, w.cur<<(8-w.n))
		w.cur, w.n = 0, 0
	}
}

type wireBitReader struct {
	raw []byte
	bit int
}

func (r *wireBitReader) read(width int) (uint64, error) {
	var out uint64
	for i := 0; i < width; i++ {
		if r.bit >= len(r.raw)*8 {
			return 0, errors.New("codec: wire bitstream truncated")
		}
		byteIdx := r.bit / 8
		bitIdx := 7 - uint(r.bit%8)
		out = out<<1 | uint64((r.raw[byteIdx]>>bitIdx)&1)
		r.bit++
	}
	return out, nil
}

func (r *wireBitReader) bytesRead() int {
	return (r.bit + 7) / 8
}

func (r *wireBitReader) verifyZeroPadding() error {
	rem := r.bit % 8
	if rem == 0 {
		return nil
	}
	byteIdx := r.bit / 8
	mask := byte((1 << uint(8-rem)) - 1)
	if (r.raw[byteIdx] & mask) != 0 {
		return errors.New("codec: wire bitstream non-zero padding")
	}
	return nil
}
