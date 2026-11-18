// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// encoder_facade.go 把本地子集的编码面（词项编码器、分词、RMBP 编解码、谐波与
// 闭集查找、脉冲信号评估）统一委托到唯一实现 pulse/plain；本文件只做公开 DTO
// 适配与传输层补全（atoms_b64），不复制任何编码逻辑。
package local

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rocareer/rolling-memory/sdk/go/pulse/plain"
)

// 与 plain 字段完全一致的公开 DTO 直接别名，字段名/顺序/JSON 标签均不变，
// 既有带字段名与位置字面量均保持源码兼容。
type (
	UnitMeta           = plain.UnitMeta
	SubUnit            = plain.SubUnit
	WordSegmenter      = plain.WordSegmenter
	HarmonicExpander   = plain.HarmonicExpander
	PulseSignalWarning = plain.PulseSignalWarning
	PulseSignalReport  = plain.PulseSignalReport
)

const (
	WarnCoarseUnsegmentedCarrier = plain.WarnCoarseUnsegmentedCarrier
	WarnOverFragmentedDust       = plain.WarnOverFragmentedDust
	WarnFlatCarrierModulation    = plain.WarnFlatCarrierModulation
	WarnDegenerateSentenceFrames = plain.WarnDegenerateSentenceFrames
	WarnHighTandemRedundancy     = plain.WarnHighTandemRedundancy
	WarnCooccurHorizonTruncated  = plain.WarnCooccurHorizonTruncated

	CalendarBeaconWidthX10 = plain.CalendarBeaconWidthX10
)

// 高 4 位结构形态标志位与低位标志位同源：统一引用 plain 的 8-bit ClassFlags 定义。
const (
	FlagIsHarmonicCompanion = plain.FlagIsHarmonicCompanion
	FlagIsFocalHeader       = plain.FlagIsFocalHeader
	FlagHasBoundaryQuote    = plain.FlagHasBoundaryQuote
	FlagIsQuantityOrUnit    = plain.FlagIsQuantityOrUnit
)

// blindFormFlagMarks 保留包内既有测试入口，唯一实现见 plain.BlindFormFlagMarks。
func blindFormFlagMarks(line string, words []string) []uint8 {
	return plain.BlindFormFlagMarks(line, words)
}

// wireFlagWideF 是 RMBP 头 flags 的宽 F 域测试锚点（权威定义见 plain 线编码器，
// 头字节相等性由 codec 差分测试双向钉死）。
const wireFlagWideF uint8 = 1 << 1

// DeriveBlindPepper extracts a rotation-invariant tenant blind pepper from a short-lived
// `mem2.<payload_b64>.<sig>` data-plane signed key, or returns secret unchanged for
// static keys/phrases.
func DeriveBlindPepper(secret []byte) []byte {
	trimmed := strings.TrimSpace(string(secret))
	if strings.HasPrefix(trimmed, "mem2.") {
		if parts := strings.Split(trimmed, "."); len(parts) == 3 {
			if raw, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
				var claims struct {
					Tid  string `json:"tid"`
					UID  string `json:"uid"`
					Bpep string `json:"bpep,omitempty"`
				}
				if json.Unmarshal(raw, &claims) == nil && strings.TrimSpace(claims.Tid) != "" {
					if len(claims.Bpep) == 64 {
						valid := true
						for _, r := range claims.Bpep {
							if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
								valid = false
								break
							}
						}
						if valid {
							return []byte(claims.Bpep)
						}
					}
					return []byte("rame-tenant-vault:" + strings.TrimSpace(claims.Tid) + ":" + strings.TrimSpace(claims.UID))
				}
			}
		}
	}
	return secret
}

func CarrierUnits(r rune) uint16             { return plain.CarrierUnits(r) }
func SplitSentences(text string) []string    { return plain.SplitSentences(text) }
func SplitWords(sent string) []string        { return plain.SplitWords(sent) }
func FTS5TrigramSegmenter(s string) []string { return plain.FTS5TrigramSegmenter(s) }
func ExtractStructuredDateHarmonics(sent string) []string {
	return plain.ExtractStructuredDateHarmonics(sent)
}
func BlindFormFlagMarks(line string, words []string) []uint8 {
	return plain.BlindFormFlagMarks(line, words)
}
func FormatPlaintextWithMeta(rawText string, meta UnitMeta) string {
	return plain.FormatPlaintextWithMeta(rawText, meta)
}
func ComposeSubUnitID(parentID, kind string, index int) string {
	return plain.ComposeSubUnitID(parentID, kind, index)
}
func ResolveSubUnitIDAndText(parentID string, su SubUnit, kindCounts map[string]int) (subID, kind, txt string, idx int, ok bool) {
	return plain.ResolveSubUnitIDAndText(parentID, su, kindCounts)
}
func ExtractInlineSymbolUnits(sentence string) []string {
	return plain.ExtractInlineSymbolUnits(sentence)
}
func ExtractSEAsianUnitWords(sentence string) []string {
	return plain.ExtractSEAsianUnitWords(sentence)
}
func LookupISO3166GeoHarmonicPair(token string) (string, string) {
	return plain.LookupISO3166GeoHarmonicPair(token)
}
func LookupISO3166GeoHarmonics(token string) []string {
	return plain.LookupISO3166GeoHarmonics(token)
}
func LookupUnitDimensionHarmonicPair(token string) (string, string) {
	return plain.LookupUnitDimensionHarmonicPair(token)
}
func LookupClosedSetHarmonics(token string) []string {
	return plain.LookupClosedSetHarmonics(token)
}
func CombineHarmonicExpanders(es ...HarmonicExpander) HarmonicExpander {
	return plain.CombineHarmonicExpanders(es...)
}

// BlindEncoder 是 plain.BlindEncoder 的传输适配包装：编码、分词与打包语义 100%
// 由 plain 提供，本层只把打包结果转换为 本地子集公开 DTO 并补齐 atoms_b64。
// segUserOpts 保存用户原始分段配置，供 CloneWithPepper 用新门面重绑定
// SentenceAtomPacker 闭包（否则闭包会继续引用旧键空间的门面）。
type BlindEncoder struct {
	enc         *plain.BlindEncoder
	segUserOpts *SegmenterPackOptions
}

func NewBlindEncoder(secret []byte) *BlindEncoder {
	return &BlindEncoder{enc: plain.NewBlindEncoder(DeriveBlindPepper(secret))}
}

func NewBlindEncoderFromRawKey(rawKey [32]byte) *BlindEncoder {
	return &BlindEncoder{enc: plain.NewBlindEncoderFromRawKey(rawKey)}
}

func (e *BlindEncoder) KeyedHash64(token string) uint64 { return e.enc.KeyedHash64(token) }

// CloneWithPepper 以新 pepper 派生编码器并保留 segOpts/harmOpts（唯一实现见 plain）。
func (e *BlindEncoder) CloneWithPepper(pepper []byte) *BlindEncoder {
	if e == nil {
		return NewBlindEncoder(pepper)
	}
	clone := &BlindEncoder{enc: e.enc.CloneWithPepper(pepper)}
	if e.segUserOpts != nil {
		opts := *e.segUserOpts
		clone.SetSegmenterOptions(opts)
	}
	return clone
}

func (e *BlindEncoder) SetWordSegmenter(seg WordSegmenter) {
	if e == nil || e.enc == nil {
		return
	}
	e.enc.SetWordSegmenter(seg)
}

func (e *BlindEncoder) SetCanonicalHarmonics(opts CanonicalHarmonicOptions) {
	if e == nil || e.enc == nil {
		return
	}
	e.enc.SetCanonicalHarmonics(plain.CanonicalHarmonicOptions{
		EnableStructuredDates: opts.EnableStructuredDates,
		Expander:              opts.Expander,
	})
}

func (e *BlindEncoder) SetSegmenterOptions(opts SegmenterPackOptions) {
	if e == nil || e.enc == nil {
		return
	}
	e.segUserOpts = &opts
	e.enc.SetSegmenterOptions(e.plainSegmenterOptions(opts))
}

func (e *BlindEncoder) plainSegmenterOptions(opts SegmenterPackOptions) plain.SegmenterPackOptions {
	popts := plain.SegmenterPackOptions{
		Segmenter:           opts.Segmenter,
		HarmonicOverlay:     opts.HarmonicOverlay,
		DisableAutoHarmonic: opts.DisableAutoHarmonic,
	}
	if opts.SentenceAtomPacker != nil {
		popts.SentenceAtomPacker = func(_ *plain.BlindEncoder, sentence string, sentIdx int) []plain.BlindTokenTerm {
			return atomsToPlain(opts.SentenceAtomPacker(e, sentence, sentIdx))
		}
	}
	return popts
}

// atomsToPlain / atomsFromPlain 在 ramesdk 公开三字段 DTO 与 plain 词项间转换。
// RMBP 线格式只承载 {h, b, f}，plain 的 Role（零值即 RolePrimary）不进入线格式，
// 转换仅做字段映射，不改变任何编码语义。
func atomsToPlain(atoms []BlindTokenAtom) []plain.BlindTokenTerm {
	if atoms == nil {
		return nil
	}
	terms := make([]plain.BlindTokenTerm, len(atoms))
	for i, a := range atoms {
		terms[i] = plain.BlindTokenTerm{H: a.H, RawBitsX10: a.RawBitsX10, ClassFlags: a.ClassFlags}
	}
	return terms
}

func atomsFromPlain(terms []plain.BlindTokenTerm) []BlindTokenAtom {
	if terms == nil {
		return nil
	}
	atoms := make([]BlindTokenAtom, len(terms))
	for i, t := range terms {
		atoms[i] = BlindTokenAtom{H: t.H, RawBitsX10: t.RawBitsX10, ClassFlags: t.ClassFlags}
	}
	return atoms
}

func framesToPlain(frames [][]BlindTokenAtom) [][]plain.BlindTokenTerm {
	if frames == nil {
		return nil
	}
	out := make([][]plain.BlindTokenTerm, len(frames))
	for i, fr := range frames {
		out[i] = atomsToPlain(fr)
	}
	return out
}

func framesFromPlain(frames [][]plain.BlindTokenTerm) [][]BlindTokenAtom {
	if frames == nil {
		return nil
	}
	out := make([][]BlindTokenAtom, len(frames))
	for i, fr := range frames {
		out[i] = atomsFromPlain(fr)
	}
	return out
}

// packetToPlain 逐字段映射公开 DTO；本地 DTO 无 Locus 字段（恒零值），
// plain 打包路径也不设置 Locus，转换无信息损失。
func packetToPlain(p BlindPulsePacket) plain.BlindPulsePacket {
	return plain.BlindPulsePacket{
		TenantID: p.TenantID, Scope: p.Scope, UserID: p.UserID,
		AssistantID: p.AssistantID, IncarnationID: p.IncarnationID,
		UnitID: p.UnitID, Layer: p.Layer, CitedUnitIDs: p.CitedUnitIDs,
		OccurredAt: p.OccurredAt, NarratedAt: p.NarratedAt, IngestedAt: p.IngestedAt,
		AuthorHash: p.AuthorHash, DocHash: p.DocHash, BlockHash: p.BlockHash,
		LineStart: p.LineStart, LineEnd: p.LineEnd,
		Slots: atomsToPlain(p.Slots), SentenceFrames: framesToPlain(p.SentenceFrames),
		DenoiseRatio: p.DenoiseRatio, MinKeepSentences: p.MinKeepSentences, WaveRadix: p.WaveRadix,
		AtomsB64: p.AtomsB64, RawText: p.RawText, PulseMode: p.PulseMode,
	}
}

func packetFromPlain(p plain.BlindPulsePacket) BlindPulsePacket {
	return BlindPulsePacket{
		TenantID: p.TenantID, Scope: p.Scope, UserID: p.UserID,
		AssistantID: p.AssistantID, IncarnationID: p.IncarnationID,
		UnitID: p.UnitID, Layer: p.Layer, CitedUnitIDs: p.CitedUnitIDs,
		OccurredAt: p.OccurredAt, NarratedAt: p.NarratedAt, IngestedAt: p.IngestedAt,
		AuthorHash: p.AuthorHash, DocHash: p.DocHash, BlockHash: p.BlockHash,
		LineStart: p.LineStart, LineEnd: p.LineEnd,
		Slots: atomsFromPlain(p.Slots), SentenceFrames: framesFromPlain(p.SentenceFrames),
		DenoiseRatio: p.DenoiseRatio, MinKeepSentences: p.MinKeepSentences, WaveRadix: p.WaveRadix,
		AtomsB64: p.AtomsB64, RawText: p.RawText, PulseMode: p.PulseMode,
	}
}

// wireReadyPacket 保持 本地打包一贯行为：只要有槽位即补齐 atoms_b64。
func wireReadyPacket(p plain.BlindPulsePacket) BlindPulsePacket {
	pkt := packetFromPlain(p)
	if len(pkt.Slots) > 0 && pkt.AtomsB64 == "" {
		pkt.AtomsB64, _ = EncodeWirePayload(pkt.Slots, pkt.SentenceFrames)
	}
	return pkt
}

func (e *BlindEncoder) PackSentenceAtoms(sent string) []BlindTokenAtom {
	return atomsFromPlain(e.enc.PackSentenceTerms(sent))
}

func (e *BlindEncoder) PackBlindPacketWithSegmenter(docID string, text string, occurredAt int64, opts SegmenterPackOptions) (BlindPulsePacket, []string) {
	pkt, sents := e.enc.PackBlindPacketWithSegmenter(docID, text, occurredAt, e.plainSegmenterOptions(opts))
	return wireReadyPacket(pkt), sents
}

func (e *BlindEncoder) PackBlindPacket(docID string, text string, occurredAt int64) (BlindPulsePacket, []string) {
	pkt, sents := e.enc.PackBlindPacket(docID, text, occurredAt)
	return wireReadyPacket(pkt), sents
}

func (e *BlindEncoder) PackBlindPacketWithMeta(docID string, text string, meta UnitMeta) (BlindPulsePacket, []string) {
	pkt, sents := e.enc.PackBlindPacketWithMeta(docID, text, meta)
	return wireReadyPacket(pkt), sents
}

func (e *BlindEncoder) PackCompositePackets(parentID string, parentText string, occurredAt int64, subUnits []SubUnit) ([]BlindPulsePacket, map[string]string) {
	var pe *plain.BlindEncoder
	if e != nil {
		pe = e.enc
	}
	pkts, subToParent := pe.PackCompositePackets(parentID, parentText, occurredAt, subUnits)
	out := make([]BlindPulsePacket, 0, len(pkts))
	for _, p := range pkts {
		out = append(out, wireReadyPacket(p))
	}
	return out, subToParent
}

// EvaluatePulseSignal computes a 100% plaintext-free 5-dimension segmentation quality report.
func EvaluatePulseSignal(pkt BlindPulsePacket) PulseSignalReport {
	return plain.EvaluatePulseSignal(packetToPlain(pkt))
}

// EncodeWirePayload encodes flat slots and sentence frames into the canonical `atoms_b64` payload.
func EncodeWirePayload(slots []BlindTokenAtom, frames [][]BlindTokenAtom) (string, error) {
	raw, err := plain.EncodeWireBytes(atomsToPlain(slots), framesToPlain(frames))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// DecodeWirePayload decodes a canonical `atoms_b64` payload back into flat slots and sentence frames.
func DecodeWirePayload(b64 string) ([]BlindTokenAtom, [][]BlindTokenAtom, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, fmt.Errorf("local: decode wire base64: %w", err)
	}
	return DecodeWireBytes(raw)
}

// EncodeWireBytes encodes flat slots and sentence frames into the compact lossless `RMBP` binary stream.
func EncodeWireBytes(slots []BlindTokenAtom, frames [][]BlindTokenAtom) ([]byte, error) {
	return plain.EncodeWireBytes(atomsToPlain(slots), framesToPlain(frames))
}

// DecodeWireBytes decodes an `RMBP` binary stream back into flat slots and sentence frames.
func DecodeWireBytes(raw []byte) ([]BlindTokenAtom, [][]BlindTokenAtom, error) {
	terms, frames, err := plain.DecodeWireBytes(raw)
	if err != nil {
		return nil, nil, err
	}
	return atomsFromPlain(terms), framesFromPlain(frames), nil
}
