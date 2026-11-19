// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package tokenzr

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// bpePieceBits computes sum_{id in BPE(prefix + piece)} log2(id + 2) without heap-allocating
// intermediate []uint slices or concatenating (" " + piece) strings.
func (c *codec) bpePieceBits(withLeadingSpace bool, piece string) float64 {
	if len(piece) == 0 {
		if withLeadingSpace {
			id := c.byteToID[' ']
			return c.tokenPriorBitsLUT[id]
		}
		return 0
	}
	if withLeadingSpace {
		if id, ok := c.spacedTokenID[piece]; ok && int(id) < len(c.tokenPriorBitsLUT) {
			return c.tokenPriorBitsLUT[id]
		}
	} else {
		if id, ok := c.bareTokenID[piece]; ok && int(id) < len(c.tokenPriorBitsLUT) {
			return c.tokenPriorBitsLUT[id]
		}
	}
	totalLen := len(piece)
	if withLeadingSpace {
		totalLen++
	}
	if totalLen <= 64 {
		var stackBuf [64]uint32
		ids := stackBuf[:totalLen]
		off := 0
		if withLeadingSpace {
			ids[0] = c.byteToID[' ']
			off = 1
		}
		for i := 0; i < len(piece); i++ {
			ids[off+i] = c.byteToID[piece[i]]
		}
		for len(ids) >= 2 {
			bestRank := uint32(0xFFFFFFFF)
			bestIdx := -1
			for i := 0; i < len(ids)-1; i++ {
				key := (uint64(ids[i]) << 32) | uint64(ids[i+1])
				if mergedID, ok := c.merges[key]; ok && mergedID < bestRank {
					bestRank = mergedID
					bestIdx = i
				}
			}
			if bestIdx < 0 {
				break
			}
			ids[bestIdx] = bestRank
			copy(ids[bestIdx+1:], ids[bestIdx+2:])
			ids = ids[:len(ids)-1]
		}
		var b float64
		for _, id := range ids {
			if int(id) < len(c.tokenPriorBitsLUT) {
				b += c.tokenPriorBitsLUT[id]
			} else {
				b += math.Log2(float64(id) + 2.0)
			}
		}
		return b
	}
	str := piece
	if withLeadingSpace {
		str = " " + piece
	}
	ids := c.bpePiece(str)
	var b float64
	for _, id := range ids {
		if int(id) < len(c.tokenPriorBitsLUT) {
			b += c.tokenPriorBitsLUT[id]
		} else {
			b += math.Log2(float64(id) + 2.0)
		}
	}
	return b
}

// precomputeVocabularyPriors populates O(1) lookup tables for all vocabulary tokens at codec load time,
// eliminating runtime string concatenations (" "+w, w+suffix) and map allocations on hot paths.
func (c *codec) precomputeVocabularyPriors() {
	for w := range c.spacedTokenID {
		low := strings.ToLower(w)
		if _, exists := c.spacedValenceLUT[low]; !exists {
			val := c.computeInflectionValenceSlow(low)
			if val > 255 {
				val = 255
			}
			c.spacedValenceLUT[low] = uint8(val)
		}
		if _, exists := c.spacedProperNounLUT[low]; !exists {
			if bpeLexicalCarryBits(low) >= 4.0 {
				r0, w0 := utf8.DecodeRuneInString(low)
				if unicode.IsLetter(r0) && !isCJKIsolated(r0) {
					titleStr := string(unicode.ToUpper(r0)) + low[w0:]
					minGap := 0.0
					if id := c.spacedTokenID[low]; id > 0 && id < 4096 && c.spacedValenceLUT[low] == 0 {
						minGap = 1.0
					}
					if titleStr != low && c.bpePieceBits(true, titleStr)+minGap < c.bpePieceBits(true, low) {
						c.spacedProperNounLUT[low] = true
					}
				}
			}
		}
	}
}

// TokenPriorBits computes the intrinsic Zipfian BPE merge-rank self-information (in bits) of a lexical piece
// under the Pre-Space Word-Boundary Principle:
//
//   - For unspaced ideographic/syllabic scripts (CJK/Japanese/Thai), words are encoded without leading
//     ASCII spaces: I_BPE*(w) = I_BPE(w);
//   - For space-delimited scripts (Latin/Cyrillic/Arabic/Greek), standalone words in running text are
//     encoded with a leading space (" " + w): I_BPE*(w) = I_BPE(" " + w), where I_BPE(x) = sum_{id in BPE(x)} log2(id + 2).
//
// Evaluating I_BPE(" " + w) rather than min(I_BPE(w), I_BPE(" " + w)) prevents short open-class content words
// (e.g. "ice", "art", "red", "end", "able") from being hijacked by their high-frequency word-internal
// suffix/substring pieces (e.g. "not-ice", "p-art", "ti-red").
func TokenPriorBits(piece string) float64 {
	s := strings.TrimSpace(piece)
	if s == "" {
		return 0
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		// 降级路径（内嵌词表加载失败，实际不可达）：逐 rune 载体先验，与主线标尺同表。
		return bpeLexicalCarryBits(s)
	}
	r0, _ := utf8.DecodeRuneInString(s)
	if isUnspacedScriptRune(r0) {
		return ctr.codec.bpePieceBits(false, s)
	}
	return ctr.codec.bpePieceBits(true, s)
}

func isUnspacedScriptRune(r rune) bool {
	if isCJKIsolated(r) || unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
		return true
	}
	return unicode.Is(unicode.Thai, r) || unicode.Is(unicode.Lao, r) || unicode.Is(unicode.Khmer, r) ||
		unicode.Is(unicode.Myanmar, r) || unicode.Is(unicode.Tibetan, r)
}

func bpeRuneCarryBits(r rune) float64 {
	if unicode.IsLetter(r) {
		if isCJKIsolated(r) {
			return 10.0
		}
		return 2.0
	}
	if unicode.IsDigit(r) {
		return 3.32
	}
	return 0
}

func bpeLexicalCarryBits(s string) float64 {
	var bits float64
	for _, r := range s {
		bits += bpeRuneCarryBits(r)
	}
	return bits
}

func bpeSliceCarryBits(rs []rune) float64 {
	var bits float64
	for _, r := range rs {
		bits += bpeRuneCarryBits(r)
	}
	return bits
}

// isPureLowerTrimmed reports whether s is already trimmed and contains no uppercase letters,
// avoiding strings.ToLower allocations on the common lowercase hot path.
func toLowerTrimmedFast(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			return strings.ToLower(s)
		}
		if c >= 0x80 {
			for _, r := range s[i:] {
				if unicode.IsUpper(r) {
					return strings.ToLower(s)
				}
			}
			return s
		}
	}
	return s
}

// BPEInflectionValence measures the second-order morphological productivity of a word in the 128k BPE merge tree
// using ONLY the endogenous right-child suffix hub topology (monoHubSuffix, superHubSuffixes, polyHubSuffixes)
// discovered during vocabulary graph loading and bit-bounded stem prefixes (zero hardcoded suffix strings or character counts).
func BPEInflectionValence(word string) int {
	w := toLowerTrimmedFast(word)
	if w == "" {
		return 0
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return 0
	}
	c := ctr.codec
	// O(1) zero-allocation lookup for precomputed vocabulary tokens
	if v, ok := c.spacedValenceLUT[w]; ok {
		return int(v)
	}
	wBits := bpeLexicalCarryBits(w)
	if wBits < 4.0 {
		return 0
	}
	var vsh *countCacheShard
	if len(w) <= 64 {
		vsh = &c.valenceShards[shardIdx(w)]
		vsh.mu.RLock()
		if cached, ok := vsh.m[w]; ok {
			vsh.mu.RUnlock()
			return cached
		}
		vsh.mu.RUnlock()
	}
	valence := c.computeInflectionValenceSlow(w)
	if vsh != nil {
		vsh.mu.Lock()
		if len(vsh.m) >= maxCountPerShard {
			vsh.m = make(map[string]int, 128)
		}
		vsh.m[w] = valence
		vsh.mu.Unlock()
	}
	return valence
}

func (c *codec) computeInflectionValenceSlow(w string) int {
	wBits := bpeLexicalCarryBits(w)
	if wBits < 4.0 {
		return 0
	}
	isSingleToken := func(s string) bool {
		id := c.spacedTokenID[s]
		return id > 0 && id < 60000
	}
	isFullVocabSingle := func(s string) bool {
		return c.spacedTokenID[s] > 0
	}
	var seenBuf [32]string
	seenN := 1
	seenBuf[0] = w
	hasSeen := func(cand string) bool {
		for i := 0; i < seenN; i++ {
			if seenBuf[i] == cand {
				return true
			}
		}
		return false
	}
	valence := 0
	addIfSingle := func(cand string) bool {
		if hasSeen(cand) || !isSingleToken(cand) {
			return false
		}
		if seenN < len(seenBuf) {
			seenBuf[seenN] = cand
			seenN++
		}
		valence++
		return true
	}
	addIfFullSingle := func(cand string) bool {
		if hasSeen(cand) || !isFullVocabSingle(cand) {
			return false
		}
		if seenN < len(seenBuf) {
			seenBuf[seenN] = cand
			seenN++
		}
		valence++
		return true
	}
	lastR, lastW := utf8.DecodeLastRuneInString(w)
	lastRuneStr := ""
	if lastW > 0 {
		lastRuneStr = string(lastR)
	}
	if wBits <= 4.0 {
		hasTop := c.topPolyHubSuffix != "" && isSingleToken(w+c.topPolyHubSuffix)
		if !hasTop && len(c.superHubSuffixes) >= 2 {
			allGem := true
			for _, sh := range c.superHubSuffixes {
				if !isSingleToken(w + lastRuneStr + sh) {
					allGem = false
					break
				}
			}
			hasTop = allGem
		}
		if !hasTop {
			return 0
		}
	}
	if c.monoHubSuffix != "" {
		addIfSingle(w + c.monoHubSuffix)
	}
	for _, sh := range c.superHubSuffixes {
		addIfSingle(w + sh)
	}
	// Co-occurring full-vocabulary inflectional paradigm (e.g. "mentor" -> "mentors"(68054) + "mentoring"(66729)):
	// when a stem >= 8.0b has both a superHubSuffix (-ed/-ing) and another inflectional hub in the 128k vocabulary
	// whose BPE leftChild shares the stem prefix, admit the full-vocab paradigm without false-firing on isolated slang ("whats"/"heres").
	if valence == 0 && wBits >= 8.0 {
		fullSuperHits := 0
		for _, sh := range c.superHubSuffixes {
			cand := w + sh
			if id := c.spacedTokenID[cand]; id > 0 {
				lid := c.leftChild[id]
				if lid > 0 && int(lid) < len(c.idToValidUTF8Str) {
					lTrim := strings.TrimSpace(c.idToValidUTF8Str[lid])
					if len(lTrim) >= 3 && strings.HasPrefix(w, lTrim) {
						fullSuperHits++
					}
				}
			}
		}
		if fullSuperHits >= 1 && ((c.monoHubSuffix != "" && isFullVocabSingle(w+c.monoHubSuffix)) || fullSuperHits >= 2) {
			if c.monoHubSuffix != "" {
				addIfFullSingle(w + c.monoHubSuffix)
			}
			for _, sh := range c.superHubSuffixes {
				addIfFullSingle(w + sh)
			}
		}
	}
	if wBits >= 4.0 && len(c.superHubSuffixes) >= 2 {
		allSuperGem := true
		for _, sh := range c.superHubSuffixes {
			if !isSingleToken(w + lastRuneStr + sh) {
				allSuperGem = false
				break
			}
		}
		if allSuperGem {
			for _, sh := range c.superHubSuffixes {
				addIfSingle(w + lastRuneStr + sh)
			}
		}
	}
	if valence > 0 {
		for _, ph := range c.polyHubSuffixes {
			addIfSingle(w + ph)
		}
	}
	if wBits >= 6.0 && lastRuneStr != "" && lastRuneStr != w {
		stemMinus2 := strings.TrimSuffix(w, lastRuneStr)
		if stemMinus2 != "" && stemMinus2 != w {
			superHits := 0
			for _, sh := range c.superHubSuffixes {
				if isSingleToken(stemMinus2 + sh) {
					superHits++
				}
			}
			monoID := uint32(0)
			if c.monoHubSuffix != "" {
				monoID = c.spacedTokenID[w+c.monoHubSuffix]
			}
			canUseFullMono := false
			if monoID > 0 {
				if monoID < 60000 {
					canUseFullMono = true
				} else if int(monoID) < len(c.rightChild) {
					rid := c.rightChild[monoID]
					if rid > 0 && int(rid) < len(c.idToValidUTF8Str) {
						rStr := c.idToValidUTF8Str[rid]
						_, w0 := utf8.DecodeRuneInString(w)
						if rStr == c.monoHubSuffix || (w0 > 0 && rStr == w[w0:]+c.monoHubSuffix) {
							canUseFullMono = true
						}
					}
				}
			}
			if superHits >= 2 || (superHits >= 1 && canUseFullMono) {
				if canUseFullMono {
					addIfFullSingle(w + c.monoHubSuffix)
				}
				for _, sh := range c.superHubSuffixes {
					addIfSingle(stemMinus2 + sh)
				}
				for _, ph := range c.polyHubSuffixes {
					addIfSingle(stemMinus2 + ph)
				}
			} else if wBits >= 10.0 && c.monoHubSuffix != "" && strings.HasSuffix(w, c.monoHubSuffix) && isSingleToken(stemMinus2) && superHits >= 1 {
				addIfSingle(stemMinus2)
				for _, sh := range c.superHubSuffixes {
					addIfSingle(stemMinus2 + sh)
				}
			}
			// Endogenous syllabic nucleus alternation (e.g. "study" -> stemMinus2="stud" + nuc="i" -> "studied", "studies"):
			if c.nucleusRunes[lastR] && bpeLexicalCarryBits(stemMinus2) >= 6.0 {
				for nuc := range c.nucleusRunes {
					if nuc == lastR {
						continue
					}
					altStem := stemMinus2 + string(nuc)
					if isSingleToken(altStem) && c.spacedTokenID[altStem] < c.spacedTokenID[w] {
						continue
					}
					altHits := 0
					for _, sh := range c.superHubSuffixes {
						if isSingleToken(altStem + sh) {
							altHits++
						}
					}
					for _, ph := range c.polyHubSuffixes {
						if isSingleToken(altStem + ph) {
							altHits++
						}
					}
					if altHits >= 2 {
						for _, sh := range c.superHubSuffixes {
							addIfSingle(altStem + sh)
						}
						for _, ph := range c.polyHubSuffixes {
							addIfSingle(altStem + ph)
						}
					}
				}
			}
		}
	}
	return valence
}

// AtomInitialEnergyBits computes the intrinsic initial prior self-information E_0(w) (in bits)
// of a lexical atom by conjugating its Unicode script Shannon carrier capacity bpeLexicalCarryBits(w)
// with its 128k BPE Pre-Space Zipfian prior TokenPriorBits(w) and morphological valence:
//
//	E_0(w) = (1 - 1/e) * I_BPE_eff(w) + (1/e) * H_carrier(w)
//
// In N=1 cold-start states where ensemble statistics (df, cooccurrence) are flat, E_0(w) provides
// zero-shot discrimination between common functional glue and rare content/proper-noun atoms.
func AtomInitialEnergyBits(word string) float64 {
	s := strings.TrimSpace(word)
	if s == "" {
		return 0
	}
	carry := bpeLexicalCarryBits(s)
	effBPE := EffectiveLexicalBits(s)
	if effBPE <= 0 {
		return carry
	}
	energy := (1.0-transmissionPerHop)*effBPE + transmissionPerHop*carry
	if IsBPEProperNounPrior(s) {
		energy += eulerNumber * math.Ln2
	}
	return energy
}

// BPETreeCohesionBits returns the BPE internal binding surprisal cohesion bits (Ψ_tree)
// derived from the endogenous binary merge tree topology:
//
//	Ψ_tree(id) = max(0, I_0(id) - 0.5*(I_0(leftChild[id]) + I_0(rightChild[id]))) / (1 + ln(1 + mergeDeg[id]))
func BPETreeCohesionBits(word string) float64 {
	s := toLowerTrimmedFast(word)
	if s == "" {
		return 0
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return 0
	}
	c := ctr.codec
	id := c.spacedTokenID[s]
	if id == 0 {
		id = c.bareTokenID[s]
	}
	if id == 0 || int(id) >= len(c.treeCohesionBitsLUT) {
		return 0
	}
	return c.treeCohesionBitsLUT[id]
}

// EffectiveLexicalBits combines 1st-order Pre-Space MDL Zipfian self-information TokenPriorBits(w)
// with 2nd-order BPE morphological neighborhood valence BPEInflectionValence(w) for space-delimited scripts,
// or damped BPE binary-tree cohesion (1/e)*BPETreeCohesionBits(w) for unspaced ideographic scripts.
func EffectiveLexicalBits(word string) float64 {
	s := strings.TrimSpace(word)
	if s == "" {
		return 0
	}
	prior := TokenPriorBits(s)
	if prior == 0 {
		return 0
	}
	val := BPEInflectionValence(s)
	if val > 0 {
		return prior + 1.2*float64(val)
	}
	r0, _ := utf8.DecodeRuneInString(s)
	if isUnspacedScriptRune(r0) {
		return prior + (1.0/eulerNumber)*BPETreeCohesionBits(s)
	}
	return prior
}

// IsBPEProperNounPrior reports whether a word exhibits proper-noun case asymmetry in the 128k BPE vocabulary:
//
//	I_BPE(" " + TitleCase(w)) < I_BPE(" " + lower(w))
func IsBPEProperNounPrior(word string) bool {
	lowStr := toLowerTrimmedFast(word)
	if lowStr == "" {
		return false
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	if _, inVocab := ctr.codec.spacedValenceLUT[lowStr]; inVocab {
		return ctr.codec.spacedProperNounLUT[lowStr]
	}
	if bpeLexicalCarryBits(lowStr) < 4.0 {
		return false
	}
	r0, w0 := utf8.DecodeRuneInString(lowStr)
	if !unicode.IsLetter(r0) || isCJKIsolated(r0) {
		return false
	}
	titleStr := string(unicode.ToUpper(r0)) + lowStr[w0:]
	if titleStr == lowStr {
		return false
	}
	return ctr.codec.bpePieceBits(true, titleStr) < ctr.codec.bpePieceBits(true, lowStr)
}

// IsBPESingleToken reports in O(1) zero-allocation time whether piece exists as a single
// vocabulary token in the embedded 128k BPE merge tree (either bare or pre-spaced).
func IsBPESingleToken(piece string) bool {
	s := strings.TrimSpace(piece)
	if s == "" {
		return false
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	return ctr.codec.bareTokenID[s] > 0 || ctr.codec.spacedTokenID[s] > 0
}

// HasCompetingBPESubroot reports whether the longer word in a diverging pair (qRunes, wRunes)
// forms an independent open-class BPE root at the bit span of the shorter word or via an elided
// syllabic nucleus in the 128k BPE merge tree (zero hardcoded suffix strings or character counts).
func HasCompetingBPESubroot(qRunes, wRunes []rune, lcpLen int) bool {
	// 契约外参数防御：shorter 按比特重量选取、与 rune 长度无关，lcpLen 仅保证 ≤ min(rune 长度)。
	if lcpLen <= 0 {
		return false
	}
	bQ := bpeSliceCarryBits(qRunes)
	bW := bpeSliceCarryBits(wRunes)
	shorter, longer := qRunes, wRunes
	bMin, bMax := bQ, bW
	if bQ > bW {
		shorter, longer = wRunes, qRunes
		bMin, bMax = bW, bQ
	}
	if lcpLen > len(shorter) {
		return false
	}
	bLCP := bpeSliceCarryBits(shorter[:lcpLen])
	if bLCP <= 0 || bMin <= bLCP || bMax <= bMin {
		return false
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	c := ctr.codec
	isFullVocabSingle := func(s string) bool {
		return c.spacedTokenID[s] > 0
	}
	countHubAttachments := func(stem string) int {
		n := 0
		if c.monoHubSuffix != "" && isFullVocabSingle(stem+c.monoHubSuffix) {
			n++
		}
		for _, sh := range c.superHubSuffixes {
			if isFullVocabSingle(stem + sh) {
				n++
			}
		}
		return n
	}
	sh := strings.ToLower(string(shorter))
	lg := strings.ToLower(string(longer))
	lcp := strings.ToLower(string(shorter[:lcpLen]))
	matchedSuperSuffix := false
	for _, shSfx := range c.superHubSuffixes {
		if lg == lcp+shSfx {
			matchedSuperSuffix = true
		}
		if strings.HasSuffix(lg, shSfx) {
			lgStem := strings.TrimSuffix(lg, shSfx)
			if lgStem != sh && lgStem != lcp && strings.HasPrefix(lgStem, lcp) {
				if bpeLexicalCarryBits(lgStem) > bMin || (isFullVocabSingle(lgStem) && countHubAttachments(lgStem) >= 2) {
					return true
				}
			}
		}
	}
	for _, phSfx := range c.polyHubSuffixes {
		if strings.HasSuffix(lg, phSfx) {
			lgStem := strings.TrimSuffix(lg, phSfx)
			if lgStem != sh && lgStem != lcp && strings.HasPrefix(lgStem, lcp) && isFullVocabSingle(lgStem) && countHubAttachments(lgStem) >= 2 {
				return true
			}
		}
	}
	if matchedSuperSuffix {
		for nuc := range c.nucleusRunes {
			compRoot := lcp + string(nuc)
			if compRoot != sh && isFullVocabSingle(compRoot) && BPEInflectionValence(compRoot) >= 2 {
				return true
			}
		}
	}
	return false
}

// IsTerminalInflectedExtension reports whether longer (1.25..1.5*tauAtom) is a terminal inflected form
// of shorter (1.0*tauAtom) in the 128k BPE merge tree (e.g., "play" <-> "plays"/"played", "move" <-> "moved"),
// as opposed to an independent uninflected lexeme sharing a prefix ("part" <-> "party", "plan" <-> "planet").
func IsTerminalInflectedExtension(qRunes, wRunes []rune) bool {
	bQ := bpeSliceCarryBits(qRunes)
	bW := bpeSliceCarryBits(wRunes)
	shorter, longer := qRunes, wRunes
	bMin, bMax := bQ, bW
	if bQ > bW {
		shorter, longer = wRunes, qRunes
		bMin, bMax = bW, bQ
	}
	if bMin <= 0 || bMax <= bMin {
		return false
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	c := ctr.codec
	isSingle := func(s string) bool {
		id := c.spacedTokenID[s]
		return id > 0 && id < 60000
	}
	sh := strings.ToLower(string(shorter))
	lg := strings.ToLower(string(longer))
	if !isSingle(lg) && len(c.bpePiece(" "+lg)) > 2 {
		return false
	}
	if c.monoHubSuffix != "" && isSingle(lg+c.monoHubSuffix) {
		return false
	}
	for _, shSfx := range c.superHubSuffixes {
		if isSingle(lg + shSfx) {
			return false
		}
	}
	tail := strings.TrimPrefix(lg, sh)
	isHubTail := tail != "" && (tail == c.monoHubSuffix)
	if !isHubTail {
		for _, shSfx := range c.superHubSuffixes {
			if tail == shSfx {
				isHubTail = true
				break
			}
		}
	}
	isGeminateHub := false
	lastR, lastW := utf8.DecodeLastRuneInString(sh)
	if !isHubTail && bMin >= 6.0 && lastW > 0 && strings.HasPrefix(lg, sh) {
		if strings.HasPrefix(tail, string(lastR)) {
			gemTail := tail[lastW:]
			for _, shSfx := range c.superHubSuffixes {
				if gemTail == shSfx {
					isHubTail = true
					isGeminateHub = true
					break
				}
			}
		}
	}
	if !isHubTail && bMin >= 6.0 && lastW > 0 {
		shMinus2 := strings.TrimSuffix(sh, string(lastR))
		if shMinus2 != "" && shMinus2 != sh && strings.HasPrefix(lg, shMinus2) {
			elidedTail := strings.TrimPrefix(lg, shMinus2)
			for _, shSfx := range c.superHubSuffixes {
				if elidedTail == shSfx {
					isHubTail = true
					break
				}
			}
		}
	}
	if !isHubTail {
		return false
	}
	if bMax-bMin <= 2.0 || isGeminateHub {
		return BPEInflectionValence(sh) >= 1
	}
	return BPEInflectionValence(sh) >= 2
}

// BPEPrefixStats returns (spacedID, minChildID, childCnt4k) for " " + word.
func BPEPrefixStats(word string) (uint32, uint32, uint16) {
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return 0, 0, 0
	}
	id := ctr.codec.spacedTokenID[strings.ToLower(strings.TrimSpace(word))]
	if id == 0 || int(id) >= len(ctr.codec.minChildID) {
		return 0, 0, 0
	}
	return id, ctr.codec.minChildID[id], ctr.codec.childCnt4k[id]
}

// IsBPEStandaloneGluePrior reports whether a non-CJK word exhibits the mathematical BPE signature
// of a high-frequency standalone closed-class function word across languages without per-language wordlists.
func IsBPEStandaloneGluePrior(word string) bool {
	w := toLowerTrimmedFast(word)
	wBits := bpeLexicalCarryBits(w)
	if wBits < 4.0 || wBits > 6.0 {
		return false
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	hasNucleus := false
	for _, r := range w {
		if !unicode.IsLetter(r) || isUnspacedScriptRune(r) || unicode.Is(unicode.Hangul, r) {
			return false
		}
		if ctr.codec.nucleusRunes[r] || r > 127 {
			hasNucleus = true
		}
	}
	if !hasNucleus || IsBPEProperNounPrior(w) {
		return false
	}
	spBits := ctr.codec.bpePieceBits(true, w)
	if spBits <= 0 || spBits >= 9.90 {
		return false
	}
	rawBits := ctr.codec.bpePieceBits(false, w)
	spID, minChild, cnt4k := BPEPrefixStats(w)
	if spID == 0 {
		return false
	}
	val := BPEInflectionValence(w)
	if spID < 512 && spBits < rawBits && val <= 1 {
		return true
	}
	if wBits <= 4.0 {
		return val == 0 && (spID < 400 || (minChild >= 2000 && (spBits < rawBits || spBits < 9.30)))
	}
	return val == 0 && cnt4k <= 1 && spBits < rawBits
}

// IsAlreadyInflectedVerbToken reports whether word is a finite inflected verb form
// using endogenous BPE suffix hub topology and bit-bounded stem prefixes (zero hardcoded suffixes or character counts).
func IsAlreadyInflectedVerbToken(word string) bool {
	w := strings.ToLower(strings.TrimSpace(word))
	wBits := bpeLexicalCarryBits(w)
	if wBits < 8.0 {
		return false
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	c := ctr.codec
	isSingle := func(s string) bool {
		id := c.spacedTokenID[s]
		return id > 0 && id < 60000
	}
	if isSingle(w) {
		if c.monoHubSuffix != "" && isSingle(w+c.monoHubSuffix) {
			return false
		}
		for _, shSfx := range c.superHubSuffixes {
			if isSingle(w + shSfx) {
				return false
			}
		}
	} else {
		spIDs := c.bpePiece(" " + w)
		if len(spIDs) > 2 {
			return false
		}
		if c.monoHubSuffix != "" && isSingle(w+c.monoHubSuffix) {
			return false
		}
		for _, shSfx := range c.superHubSuffixes {
			if isSingle(w + shSfx) {
				return false
			}
		}
		if len(spIDs) == 2 && spIDs[0] < 60000 && int(spIDs[0]) < len(c.idToBytes) && int(spIDs[1]) < len(c.idToBytes) {
			tailPiece := string(c.idToBytes[spIDs[1]])
			for _, shSfx := range c.superHubSuffixes {
				if tailPiece == shSfx {
					stemPiece := strings.TrimSpace(string(c.idToBytes[spIDs[0]]))
					if bpeLexicalCarryBits(stemPiece) >= 8.0 && !IsBPEProperNounPrior(stemPiece) {
						return true
					}
				}
			}
		}
	}
	checkInflectSuffix := func(sfx string, isSuperOrPoly bool) bool {
		if sfx == "" || !strings.HasSuffix(w, sfx) {
			return false
		}
		cand := strings.TrimSuffix(w, sfx)
		targetBits := bpeLexicalCarryBits(cand)
		if cand == "" || cand == w || targetBits < 6.0 {
			return false
		}
		if !isSuperOrPoly && strings.HasSuffix(cand, sfx) {
			return false
		}
		if BPEInflectionValence(cand) >= 1 || (targetBits >= 8.0 && isSingle(cand) && TokenPriorBits(cand) >= 10.5) {
			return true
		}
		if isSuperOrPoly && targetBits >= 6.0 {
			for nuc := range c.nucleusRunes {
				candNuc := cand + string(nuc)
				if isSingle(candNuc) && BPEInflectionValence(candNuc) >= 1 {
					return true
				}
			}
		}
		return false
	}
	if checkInflectSuffix(c.monoHubSuffix, false) {
		return true
	}
	for _, shSfx := range c.superHubSuffixes {
		if checkInflectSuffix(shSfx, true) {
			return true
		}
	}
	for _, phSfx := range c.polyHubSuffixes {
		isCompoundInflect := c.monoHubSuffix != "" && strings.HasSuffix(phSfx, c.monoHubSuffix) && !strings.HasSuffix(phSfx, c.monoHubSuffix+c.monoHubSuffix)
		if !isCompoundInflect {
			for _, shSfx := range c.superHubSuffixes {
				if strings.HasSuffix(phSfx, shSfx) {
					isCompoundInflect = true
					break
				}
			}
		}
		if isCompoundInflect && checkInflectSuffix(phSfx, true) {
			return true
		}
	}
	return false
}

// BPEContainedSubblocks extracts up to 2 endogenous structural ancestor/constituent sub-blocks
// contained in word along the 128k BPE merge tree:
//   - For space-delimited scripts: unwraps terminal inflected extensions via endogenous BPE right-child
//     hub suffixes (including geminate consonant reduction and elided nucleus restoration) and left-child
//     spaced BPE ancestors, strictly guarded by BPEInflectionValence and HasCompetingBPESubroot so
//     false-positive substrings ("art" in "party", "car" in "carpet", "he" in "there") never fire;
//   - For unspaced ideographic/syllabic scripts: unwraps multi-character BPE binary-tree constituents
//     (phrases and >=1-atom leaves) ordered by information coverage ratio.
func BPEContainedSubblocks(word string) []string {
	w := strings.ToLower(strings.TrimSpace(word))
	if w == "" {
		return nil
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return nil
	}
	c := ctr.codec
	r0, _ := utf8.DecodeRuneInString(w)
	wCarry := bpeLexicalCarryBits(w)

	// 1. Unspaced ideographic/syllabic scripts (CJK / Kana / SE Asian): unwrap BPE binary merge tree constituents
	//    + occluded boundary single-tokens + orthogonal span coverage selection.
	if isUnspacedScriptRune(r0) {
		if wCarry <= 10.0 {
			return nil
		}
		leaves, phrases := SplitSubwordConstituents(w)
		seen := map[string]bool{w: true}
		type cand struct {
			s     string
			carry float64
			bits  float64
		}
		var cands []cand
		add := func(p string) {
			s := strings.ToLower(strings.TrimSpace(p))
			if s == "" || seen[s] || !strings.Contains(w, s) {
				return
			}
			cb := bpeLexicalCarryBits(s)
			if cb < 10.0 || cb >= wCarry {
				return
			}
			seen[s] = true
			cands = append(cands, cand{s: s, carry: cb, bits: TokenPriorBits(s)})
		}
		for _, ph := range phrases {
			add(ph)
		}
		for _, lf := range leaves {
			add(lf)
		}
		for _, sp := range SplitSubwords(w) {
			add(sp)
		}
		// Unwrap occluded BPE single-token prefix/infix/suffix spans (e.g. "東京都" -> greedy BPE splits ["東", "京都"],
		// occluding the legitimate 2-char BPE single-token prefix "東京", or an internal 2~3-char BPE single-token concept).
		var byteBounds []int
		for idx := range w {
			byteBounds = append(byteBounds, idx)
		}
		byteBounds = append(byteBounds, len(w))
		if len(byteBounds) <= 16 {
			for i := 0; i < len(byteBounds)-1; i++ {
				for j := i + 1; j < len(byteBounds); j++ {
					if i == 0 && j == len(byteBounds)-1 {
						continue
					}
					span := w[byteBounds[i]:byteBounds[j]]
					if bpeLexicalCarryBits(span) >= 20.0 && (c.bareTokenID[span] > 0 || c.spacedTokenID[span] > 0) {
						add(span)
					}
				}
			}
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].carry != cands[j].carry {
				return cands[i].carry > cands[j].carry
			}
			if cands[i].bits != cands[j].bits {
				return cands[i].bits > cands[j].bits
			}
			return cands[i].s < cands[j].s
		})
		if len(cands) == 0 {
			return nil
		}
		// Orthogonal span selection: after picking top constituent cands[0] (e.g. "笔记本" in "笔记本电脑"),
		// prioritize a second constituent that is not already a substring of cands[0] (e.g. "电脑" over redundant "笔记").
		out := make([]string, 0, 2)
		out = append(out, cands[0].s)
		for i := 1; i < len(cands) && len(out) < 2; i++ {
			if !strings.Contains(out[0], cands[i].s) && !strings.Contains(cands[i].s, out[0]) {
				if cands[i].carry >= 20.0 || cands[0].carry < 20.0 {
					out = append(out, cands[i].s)
					break
				}
			}
		}
		for i := 1; i < len(cands) && len(out) < 2; i++ {
			if cands[i].s != out[0] {
				out = append(out, cands[i].s)
				break
			}
		}
		return out
	}

	// 2. Space-delimited scripts (Latin / Cyrillic / Hangul / compound identifiers):
	//    endogenous BPE suffix-hub unwrapping + BPE binary-tree leftChild/syllabic-nucleus stem + compound boundary blocks
	isHangulWord := unicode.Is(unicode.Hangul, r0)
	if wCarry < 8.0 && !(isHangulWord && wCarry >= 6.0) {
		return nil
	}
	isSingle := func(s string) bool {
		id := c.spacedTokenID[s]
		return id > 0 && id < 60000
	}
	isFullSingle := func(s string) bool {
		return c.spacedTokenID[s] > 0
	}
	seen := map[string]bool{w: true}
	var out []string
	emit := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] || len(out) >= 2 {
			return
		}
		sCarry := bpeLexicalCarryBits(s)
		minCarry := 6.0
		if isHangulWord && isFullSingle(s) {
			minCarry = 4.0
		}
		// 严格包含不变量（Strict Containment Invariant）：发射的母词必须 100% 真包含于原词 w 内部（绝不脑补或改写原词不存在的字符）
		if !strings.HasPrefix(w, s) || sCarry < minCarry {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	emitContained := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] || len(out) >= 2 {
			return
		}
		if !strings.Contains(w, s) || bpeLexicalCarryBits(s) < 6.0 {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	tryStripHub := func(sfx string, allowGeminate bool) {
		if sfx == "" || !strings.HasSuffix(w, sfx) || len(out) >= 2 {
			return
		}
		base := strings.TrimSuffix(w, sfx)
		bBits := bpeLexicalCarryBits(base)
		if base == "" || base == w || bBits < 6.0 {
			return
		}
		if !allowGeminate && strings.HasSuffix(base, sfx) {
			return
		}
		// Direct contained stem in BPE vocabulary (e.g. "hotels" -> "hotel", "visited" -> "visit")
		if isSingle(base) && (BPEInflectionValence(base) >= 1 || bBits >= 8.0) {
			emit(base)
			// Second-order recursive BPE hub unwrapping (e.g. stacked inflectional/derivational suffixes
			// where base itself is a single token ending in another hub suffix with a valid open-class root >= 8.0b).
			if len(out) < 2 && bBits >= 10.0 {
				for _, sh2 := range c.superHubSuffixes {
					if sh2 != "" && strings.HasSuffix(base, sh2) {
						root2 := strings.TrimSuffix(base, sh2)
						if bpeLexicalCarryBits(root2) >= 8.0 && isSingle(root2) && BPEInflectionValence(root2) >= 2 {
							emit(root2)
							break
						}
					}
				}
			}
		}
		if allowGeminate {
			// Geminate consonant reduction or syllabic nucleus alternation stem (e.g. "running" -> "run", "studies" -> "studi" + "stud")
			lastR, lastW := utf8.DecodeLastRuneInString(base)
			if lastW > 0 {
				trimmed1 := strings.TrimSuffix(base, string(lastR))
				prevR, _ := utf8.DecodeLastRuneInString(trimmed1)
				if prevR == lastR && bpeLexicalCarryBits(trimmed1) >= 6.0 && isSingle(trimmed1) && BPEInflectionValence(trimmed1) >= 1 {
					emit(trimmed1)
				}
				if bBits >= 8.0 && len(out) < 2 {
					emit(base)
				}
				// If base ends with an alternating nucleus (e.g. "studi" in "studies"/"studied") whose BPE leftChild is trimmed1 ("stud"),
				// also emit the shared BPE merge-tree leftChild stem ("stud" >= 8.0b) so "study" <-> "studies" connect cleanly.
				if c.nucleusRunes[lastR] && bpeLexicalCarryBits(trimmed1) >= 8.0 && len(out) < 2 {
					if id := c.spacedTokenID[w]; id > 0 && int(id) < len(c.leftChild) {
						lid := c.leftChild[id]
						if lid > 0 && int(lid) < len(c.idToValidUTF8Str) && strings.TrimSpace(c.idToValidUTF8Str[lid]) == trimmed1 {
							emit(trimmed1)
						}
					}
				}
			}
		}
	}

	tryStripHub(c.monoHubSuffix, false)
	for _, shSfx := range c.superHubSuffixes {
		tryStripHub(shSfx, true)
		// Colloquial g-dropping / terminal-coda elision on >=6.0b superHubSuffix (e.g. "ing" -> "in" in "talkin", "runnin", "campin", "goin"):
		// if w is uninflected in BPE (valence == 0), ends with shSfx minus its final consonant, and restoring shSfx yields an in-vocab
		// single token whose stem has BPEInflectionValence >= 2, unwrap the true contained stem prefix.
		if len(out) < 2 && bpeLexicalCarryBits(shSfx) >= 6.0 {
			_, sfxLastW := utf8.DecodeLastRuneInString(shSfx)
			if sfxLastW > 0 && sfxLastW < len(shSfx) {
				elidedSfx := shSfx[:len(shSfx)-sfxLastW]
				if strings.HasSuffix(w, elidedSfx) && !strings.HasSuffix(w, shSfx) && BPEInflectionValence(w) == 0 {
					fullForm := strings.TrimSuffix(w, elidedSfx) + shSfx
					if isSingle(fullForm) {
						base := strings.TrimSuffix(w, elidedSfx)
						if bpeLexicalCarryBits(base) >= 4.0 && isSingle(base) && BPEInflectionValence(base) >= 2 {
							if bpeLexicalCarryBits(base) >= 6.0 {
								emit(base)
							} else if !seen[base] && strings.HasPrefix(w, base) {
								seen[base] = true
								out = append(out, base)
							}
						} else if lastR, lastW := utf8.DecodeLastRuneInString(base); lastW > 0 {
							trimmed1 := strings.TrimSuffix(base, string(lastR))
							prevR, _ := utf8.DecodeLastRuneInString(trimmed1)
							if prevR == lastR && bpeLexicalCarryBits(trimmed1) >= 6.0 && isSingle(trimmed1) && BPEInflectionValence(trimmed1) >= 2 {
								emit(trimmed1)
							}
						}
					}
				}
			}
		}
	}
	for _, phSfx := range c.polyHubSuffixes {
		isCompoundInflect := c.monoHubSuffix != "" && strings.HasSuffix(phSfx, c.monoHubSuffix) && !strings.HasSuffix(phSfx, c.monoHubSuffix+c.monoHubSuffix)
		if !isCompoundInflect {
			for _, shSfx := range c.superHubSuffixes {
				if strings.HasSuffix(phSfx, shSfx) {
					isCompoundInflect = true
					break
				}
			}
		}
		if isCompoundInflect {
			tryStripHub(phSfx, true)
		}
	}

	// Uninflected open-class word ending in a syllabic nucleus whose BPE inflectional paradigm uses nucleus alternation
	// (e.g. "study" -> "stud" + "ied"/"ies" in BPE merge tree; guarded by !HasCompetingBPESubroot so "party" never emits "part").
	if len(out) == 0 && wCarry >= 10.0 && BPEInflectionValence(w) >= 2 {
		lastR, lastW := utf8.DecodeLastRuneInString(w)
		if lastW > 0 && c.nucleusRunes[lastR] {
			stem := strings.TrimSuffix(w, string(lastR))
			if bpeLexicalCarryBits(stem) >= 8.0 {
				for nuc := range c.nucleusRunes {
					if nuc == lastR {
						continue
					}
					altStem := stem + string(nuc)
					hasAltPlural := false
					for _, phSfx := range c.polyHubSuffixes {
						if isSingle(altStem + phSfx) {
							hasAltPlural = true
							break
						}
					}
					if hasAltPlural {
						wRunes := []rune(w)
						matchedExt := false
						for _, shSfx := range c.superHubSuffixes {
							if isSingle(altStem + shSfx) {
								extRunes := []rune(altStem + shSfx)
								lcp := 0
								for lcp < len(wRunes) && lcp < len(extRunes) && wRunes[lcp] == extRunes[lcp] {
									lcp++
								}
								if lcp > 0 && !HasCompetingBPESubroot(wRunes, extRunes, lcp) {
									matchedExt = true
									break
								}
							}
						}
						if matchedExt {
							emit(stem)
							break
						}
					}
				}
			}
		}
	}

	// 3. Compound/identifier prefix & constituent unwrapping along spaced BPE pieces (e.g. "containers.go" -> "containers", "container"; "한국어" -> "한국")
	if len(out) < 2 {
		spPieces := SplitSubwords(" " + w)
		if len(spPieces) >= 2 {
			first := strings.ToLower(strings.TrimSpace(spPieces[0]))
			minFirstCarry := 6.0
			if isHangulWord && isFullSingle(first) {
				minFirstCarry = 4.0
			}
			if first != "" && first != w && strings.HasPrefix(w, first) && bpeLexicalCarryBits(first) >= minFirstCarry {
				subFirst := BPEContainedSubblocks(first)
				if isSingle(first) || (isHangulWord && isFullSingle(first)) || len(subFirst) > 0 {
					emit(first)
					for _, sub := range subFirst {
						emit(sub)
					}
				}
			}
			// Long multi-piece compound/camelCase identifier tail constituent (e.g. "cosinethreshold" / "endogenousl2mergecosinethreshold" -> "reshold")
			if len(out) < 2 && wCarry >= 24.0 && len(spPieces) >= 3 {
				lastPiece := strings.ToLower(strings.TrimSpace(spPieces[len(spPieces)-1]))
				if lastPiece != "" && lastPiece != w && strings.HasSuffix(w, lastPiece) && bpeLexicalCarryBits(lastPiece) >= 12.0 {
					if len(out) == 0 && bpeLexicalCarryBits(first) >= 6.0 {
						emit(first)
					}
					emitContained(lastPiece)
				}
			}
		}
	}
	return out
}

// CollapseMarkovElongatedWord collapses expressive colloquial character elongation (runs of >=3 identical letters,
// e.g. "coooool"->"cool", "loooove"->"love", "pleeeease"->"please", "sooooo"->"so", "ummmmm"->"um")
// using the 128k BPE single-token vocabulary to arbitrate between geminate (run=2) and singleton (run=1) canonical forms.
// Returns "" if word contains no >=3 identical character runs.
func CollapseMarkovElongatedWord(word string) string {
	s := toLowerTrimmedFast(word)
	if s == "" {
		return ""
	}
	hasRun3 := false
	var prevR rune
	runLen := 0
	for _, r := range s {
		if r == prevR {
			runLen++
			if runLen >= 3 && unicode.IsLetter(r) {
				hasRun3 = true
				break
			}
		} else {
			prevR = r
			runLen = 1
		}
	}
	if !hasRun3 {
		return ""
	}
	var b1, b2 strings.Builder
	prevR = 0
	runLen = 0
	for _, r := range s {
		if r == prevR {
			runLen++
		} else {
			prevR = r
			runLen = 1
		}
		if runLen == 1 {
			b1.WriteRune(r)
			b2.WriteRune(r)
		} else if runLen == 2 {
			b2.WriteRune(r)
		}
	}
	cand1 := b1.String()
	cand2 := b2.String()
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return cand1
	}
	c := ctr.codec
	id1 := c.spacedTokenID[cand1]
	id2 := c.spacedTokenID[cand2]
	if id1 > 0 && id2 > 0 {
		v1 := BPEInflectionValence(cand1)
		v2 := BPEInflectionValence(cand2)
		if v1 >= 1 && v2 >= 1 {
			if id2 < id1 {
				return cand2
			}
			return cand1
		}
		if v2 > v1 && id2 < 32768 {
			return cand2
		}
		if v1 > v2 && id1 < 32768 {
			return cand1
		}
		if id2 < id1 {
			return cand2
		}
		return cand1
	}
	if id2 > 0 && id2 < 60000 {
		return cand2
	}
	if id1 > 0 {
		return cand1
	}
	return cand1
}

// IsBPECliticTail reports whether "'" + tail exists as a high-frequency grammatical clitic token
// in the 128k BPE vocabulary (e.g. "'s", "'t", "'re", "'ve", "'ll", "'d", "'m"), distinguishing
// colloquial contractions ("we've", "they'll", "you're") from proper names ("Xi'an", "O'Neal", "d'Azur").
func IsBPECliticTail(tail string) bool {
	low := toLowerTrimmedFast(tail)
	if low == "" || bpeLexicalCarryBits(low) > 4.0 {
		return false
	}
	for _, r := range low {
		if !unicode.IsLower(r) || isCJKIsolated(r) {
			return false
		}
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	id := ctr.codec.bareTokenID["'"+low]
	return id > 0 && id < 32768
}

// IsBPEContractedAuxStem reports whether word is an uninflected auxiliary negation truncation stem
// produced by splitting before "'t" (e.g. "didn", "wasn", "couldn", "wouldn", "shouldn", "doesn", "haven", "hadn", "aren", "weren", "isn", "don").
func IsBPEContractedAuxStem(word string) bool {
	return isBPEBareNegationAuxStem(word)
}

func isBPEBareNegationAuxStem(word string) bool {
	w := toLowerTrimmedFast(word)
	wBits := bpeLexicalCarryBits(w)
	if wBits < 6.0 || wBits > 14.0 {
		return false
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	c := ctr.codec
	if c.monoHubSuffix != "" && c.spacedTokenID[w+c.monoHubSuffix] > 0 {
		return false
	}
	lastR, lastW := utf8.DecodeLastRuneInString(w)
	if lastW <= 0 || c.nucleusRunes[lastR] || !unicode.IsLower(lastR) {
		return false
	}
	if idDid := c.spacedTokenID["did"+string(lastR)]; idDid == 0 || idDid >= 4096 {
		return false
	}
	base := strings.TrimSuffix(w, string(lastR))
	if bpeLexicalCarryBits(base) < 4.0 {
		return false
	}
	baseID := c.spacedTokenID[base]
	return baseID > 0 && baseID < 2048 && (c.spacedTokenID[w] == 0 || c.spacedTokenID[w] > baseID)
}

// UnwrapApostrophelessContraction detects apostrophe-less colloquial contractions typed in fast IM/chat
// ("didnt"->"didn", "couldnt"->"couldn", "shouldnt"->"shouldn", "wasnt"->"wasn", "doesnt"->"doesn",
// "havent"->"haven", "dont"->"don", "wont"->"won", "youre"->"you", "theyre"->"they", "weve"->"we", "theyll"->"they")
// purely from 128k BPE clitic and closed-class auxiliary topology (zero hardcoded wordlists).
//
// Precision guard against real "-nt" verbs ("went", "sent", "bent", "spent", "meant"):
// In standard corpora, contractions are written with an apostrophe ("didn't", "don't", "won't"), so the pre-apostrophe
// stem (" didn", " don", " won") is far more frequent in BPE than the apostrophe-less whole word (" didnt", " dont", " wont");
// conversely, for real verbs ("went", "sent", "bent"), the whole word is far more frequent than its truncated prefix ("wen", "sen", "ben").
func UnwrapApostrophelessContraction(word string) string {
	w := toLowerTrimmedFast(word)
	wBits := bpeLexicalCarryBits(w)
	if wBits < 8.0 || wBits > 16.0 {
		return ""
	}
	for _, r := range w {
		if !unicode.IsLower(r) || isCJKIsolated(r) {
			return ""
		}
	}
	if BPEInflectionValence(w) >= 1 {
		return ""
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return ""
	}
	c := ctr.codec
	if c.monoHubSuffix != "" && c.spacedTokenID[w+c.monoHubSuffix] > 0 {
		return ""
	}
	lastR, lastW := utf8.DecodeLastRuneInString(w)
	if lastW <= 0 {
		return ""
	}
	tail1 := string(lastR)
	stem1 := strings.TrimSuffix(w, tail1)
	wID := c.spacedTokenID[w]
	if IsBPECliticTail(tail1) && (wID == 0 || wID >= 16384) {
		if isBPEBareNegationAuxStem(stem1) {
			return stem1
		}
		stem1ID := c.spacedTokenID[stem1]
		if bpeLexicalCarryBits(stem1) == 6.0 && stem1ID > 0 && stem1ID < 4096 && (wID == 0 || wID > stem1ID*4) {
			nR, nW := utf8.DecodeLastRuneInString(stem1)
			if nW > 0 && !c.nucleusRunes[nR] && c.spacedTokenID["did"+string(nR)] > 0 {
				auxBase := strings.TrimSuffix(stem1, string(nR))
				if bpeLexicalCarryBits(auxBase) == 4.0 && c.spacedTokenID[auxBase] > 0 && c.spacedTokenID[auxBase] < 2048 {
					return stem1
				}
			}
		}
	}
	if wID == 0 || wID >= 8192 {
		prevR, prevW := utf8.DecodeLastRuneInString(stem1)
		if prevW > 0 {
			tail2 := string(prevR) + tail1
			if bpeLexicalCarryBits(tail2) == 4.0 && IsBPECliticTail(tail2) {
				pronBase := strings.TrimSuffix(stem1, string(prevR))
				if bpeLexicalCarryBits(pronBase) >= 4.0 && bpeLexicalCarryBits(pronBase) <= 8.0 &&
					c.spacedTokenID[pronBase] > 0 && c.spacedTokenID[pronBase] < 1024 && BPEInflectionValence(pronBase) == 0 {
					return pronBase
				}
			}
		}
	}
	return ""
}

// CollapsePeriodicSyllableReduplication collapses >=3-cycle colloquial syllabic laugh/interjection
// reduplications (e.g. "hahaha"->"haha", "hahahaha"->"haha", "hehehe"->"hehe", "hihihi"->"hihi")
// to their 2-cycle tandem base u^2 so multi-cycle chat laughter aligns with "haha"/"hehe" without
// vanishing under tandem entropy damping.
func CollapsePeriodicSyllableReduplication(word string) string {
	s := toLowerTrimmedFast(word)
	if bpeLexicalCarryBits(s) < 10.0 {
		return ""
	}
	r0, w0 := utf8.DecodeRuneInString(s)
	if w0 <= 0 || !unicode.IsLower(r0) || isCJKIsolated(r0) || bpeRuneCarryBits(r0) != 2.0 {
		return ""
	}
	r1, w1 := utf8.DecodeRuneInString(s[w0:])
	if w1 <= 0 || r1 == r0 || !unicode.IsLower(r1) || isCJKIsolated(r1) || bpeRuneCarryBits(r1) != 2.0 {
		return ""
	}
	unit := s[:w0+w1]
	rem := s
	cycles := 0
	for strings.HasPrefix(rem, unit) {
		rem = strings.TrimPrefix(rem, unit)
		cycles++
	}
	if cycles >= 2 && (rem == "" || rem == string(r0)) && s != unit+unit {
		return unit + unit
	}
	return ""
}

// IsBPEUninflectedTandemReduplication reports whether word is an uninflected 2-cycle or multi-cycle
// syllabic tandem reduplication (e.g. "haha", "hehe", "hihi", "hoho", "xoxo", "hahaha", "hehehe")
// that carries zero open-class inflectional valence in the 128k BPE vocabulary, preventing colloquial
// chat laughter from hijacking the Primary Gain Tier while preserving inflectable or noun-family
// reduplications ("mama", "papa", "yoyo", "tutu").
func IsBPEUninflectedTandemReduplication(word string) bool {
	s := toLowerTrimmedFast(word)
	if collapsed := CollapsePeriodicSyllableReduplication(s); collapsed != "" {
		return true
	}
	if bpeLexicalCarryBits(s) != 8.0 {
		return false
	}
	r0, w0 := utf8.DecodeRuneInString(s)
	if w0 <= 0 || !unicode.IsLower(r0) || isCJKIsolated(r0) {
		return false
	}
	r1, w1 := utf8.DecodeRuneInString(s[w0:])
	if w1 <= 0 || r1 == r0 || !unicode.IsLower(r1) || isCJKIsolated(r1) {
		return false
	}
	unit := s[:w0+w1]
	if s != unit+unit {
		return false
	}
	if BPEInflectionValence(s) >= 1 {
		return false
	}
	ctr, err := Default()
	if err != nil || ctr == nil || ctr.codec == nil {
		return false
	}
	c := ctr.codec
	if c.monoHubSuffix != "" && c.spacedTokenID[s+c.monoHubSuffix] > 0 {
		return false
	}
	// A 2-cycle syllable word that exists as a full 128k vocabulary token is an ordinary
	// lexicalized noun ("mama", "papa", "dada"), whereas colloquial laughter/interjections
	// ("haha", "hehe", "hihi", "hoho", "xoxo") remain unmerged 2-piece syllable pairs.
	if c.spacedTokenID[s] > 0 || c.bareTokenID[s] > 0 {
		return false
	}
	return true
}
