// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// encoder_facade.go 把本地子集的编码面（词项编码器、分词、RMBP 编解码、谐波与
// 闭集查找、脉冲信号评估）统一委托到唯一实现 pulse/plain；本文件只做公开 DTO
// 适配与传输层补全（atoms_b64），不复制任何编码逻辑。
package local

import (
	"encoding/base64"
	"fmt"

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

func SplitWords(sent string) []string { return plain.SplitWords(sent) }
func FormatPlaintextWithMeta(rawText string, meta UnitMeta) string {
	return plain.FormatPlaintextWithMeta(rawText, meta)
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
	return &BlindEncoder{enc: plain.NewBlindEncoder(secret)}
}

func (e *BlindEncoder) KeyedHash64(token string) uint64 { return e.enc.KeyedHash64(token) }

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

func (e *BlindEncoder) PackBlindPacket(docID string, text string, occurredAt int64) (BlindPulsePacket, []string) {
	pkt, sents := e.enc.PackBlindPacket(docID, text, occurredAt)
	return wireReadyPacket(pkt), sents
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
