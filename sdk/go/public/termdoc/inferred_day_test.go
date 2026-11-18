// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package termdoc

import "testing"

func TestInferredOrdinalDayRoundTrip(t *testing.T) {
	for _, day := range []int64{1, 2, 65535, 65536, 0x7FFFFFFF, 0xFFFF0000, 0xFFFFFFFF} {
		sent, slot := PackInferredOrdinalDay(day)
		if got := UnpackInferredOrdinalDay(sent, slot); got != int64(uint32(day)) {
			t.Fatalf("day %d round-trip = %d", day, got)
		}
	}
	if UnpackInferredOrdinalDay(0, 0) != 0 {
		t.Fatalf("zero composite must decode to 0 (no inferred day)")
	}
	if sent, slot := PackInferredOrdinalDay(0x00010002); sent != 0x0001 || slot != 0x0002 {
		t.Fatalf("pack(0x00010002) = (%#04x,%#04x)", sent, slot)
	}
}
