// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package ramesdk_test

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	ramesdk "github.com/rocareer/rolling-memory/sdk/go"
)

// wideFProbeVectors 冻结两组探针载荷，覆盖 RMBP v1 `FlagWideF`（8-bit F 列）的全部编码分支：
//   - "palette-wide"：12 槽 / 6 个唯一 (B,F) 组合 ⇒ 调色板多进制打包分支，且 F 跨越旧 4-bit 天花板
//     （0x00 / 0x0F 边界 + 0x10 谐波声明位 + 0x18 形态组合 + 0x95 / 0xFF 预留高位极值）；
//   - "dict-wide"：300 槽全唯一 (B,F) ⇒ 唯一组合数 > 255 强制关闭调色板、走字典直写列分支，
//     且全部 F ∈ 0x10..0x1F 使宽 F 列在两个分支上都成为必由之路。
func wideFProbeVectors() map[string][]ramesdk.BlindTokenAtom {
	palette := []ramesdk.BlindTokenAtom{
		{H: 0x10, RawBitsX10: 10, ClassFlags: 0x00},
		{H: 0x20, RawBitsX10: 10, ClassFlags: 0x0F},
		{H: 0x30, RawBitsX10: 120, ClassFlags: 0x10},
		{H: 0x40, RawBitsX10: 120, ClassFlags: 0x18},
		{H: 0x50, RawBitsX10: 200, ClassFlags: 0x95},
		{H: 0x60, RawBitsX10: 200, ClassFlags: 0xFF},
	}
	var paletteWide []ramesdk.BlindTokenAtom
	for i := 0; i < 12; i++ {
		paletteWide = append(paletteWide, palette[i%len(palette)])
	}
	var dictWide []ramesdk.BlindTokenAtom
	for i := 0; i < 300; i++ {
		dictWide = append(dictWide, ramesdk.BlindTokenAtom{
			H:          uint64(0x1000 + i),
			RawBitsX10: uint16(10 + i),
			ClassFlags: uint8(0x10 + i%16),
		})
	}
	return map[string][]ramesdk.BlindTokenAtom{
		"palette-wide": paletteWide,
		"dict-wide":    dictWide,
	}
}

func wideFCanonical(slots []ramesdk.BlindTokenAtom) string {
	var sb strings.Builder
	for i, a := range slots {
		if i > 0 {
			sb.WriteByte('|')
		}
		sb.WriteString(strconv.FormatUint(a.H, 10))
		sb.WriteByte(':')
		sb.WriteString(strconv.Itoa(int(a.RawBitsX10)))
		sb.WriteByte(':')
		sb.WriteString(strconv.Itoa(int(a.ClassFlags)))
	}
	return sb.String()
}

// TestWideFCrossLanguageBitExactParity 钉死 8-bit `ClassFlags`（0x10..0xFF 预留域）在
// 编解码器上的逐位一致：产出基载荷 → 解码为原子三元组并原样重编码 → 重编码字节与解码三元组逐位一致。
func TestWideFCrossLanguageBitExactParity(t *testing.T) {
	vectors := wideFProbeVectors()
	names := []string{"palette-wide", "dict-wide"}

	for _, name := range names {
		slots := vectors[name]
		b64, err := ramesdk.EncodeWirePayload(slots, [][]ramesdk.BlindTokenAtom{slots})
		if err != nil {
			t.Fatalf("%s: EncodeWirePayload failed: %v", name, err)
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatalf("%s: decode base64: %v", name, err)
		}
		if raw[5]&0x02 == 0 {
			t.Fatalf("%s: 探针载荷必须置 FlagWideF（header flags=%#x），否则本测试未覆盖宽 F 分支", name, raw[5])
		}
		decSlots, decFrames, err := ramesdk.DecodeWirePayload(b64)
		if err != nil {
			t.Fatalf("%s: DecodeWirePayload failed: %v", name, err)
		}
		high := false
		for _, a := range decSlots {
			if a.ClassFlags > 0x0F {
				high = true
			}
		}
		if !high {
			t.Fatalf("%s: 探针向量解码后必须含 F > 0x0F 的原子", name)
		}
		reB64, err := ramesdk.EncodeWirePayload(decSlots, decFrames)
		if err != nil {
			t.Fatalf("%s: Go 重编码失败: %v", name, err)
		}
		if reB64 != b64 {
			t.Fatalf("%s: Go 解码→重编码不收敛\nwant %s\ngot  %s", name, b64, reB64)
		}
		if wideFCanonical(decSlots) != wideFCanonical(slots) {
			t.Fatalf("%s: 解码三元组漂移", name)
		}
	}
}
