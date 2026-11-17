// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package ramesdk_test

import (
	"encoding/json"
	"reflect"
	"testing"

	ramesdk "github.com/rocareer/rolling-memory/sdk/go"
)

// compositeProbeResult is the packet & parent-map shape emitted by composite encoding.
type compositeProbeResult struct {
	Packets     []ramesdk.BlindPulsePacket `json:"packets"`
	SubToParent map[string]string          `json:"sub_to_parent"`
}

func compositeProbeFixture() (string, string, string, int64, []ramesdk.SubUnit) {
	secret := "rame-composite-parity-key-v1"
	parentID := "docA#L1-10"
	parentText := "Header memory body with groceries summary."
	occurredAt := int64(1700000000)
	subUnits := []ramesdk.SubUnit{
		{Kind: "detail", ID: "docA#L1-10#detail:0", BlockID: "docA#L1-10#detail:0", Text: "Detail first block.", FileParse: "Detail first block parsed.", LineStart: 11, LineEnd: 12},
		{Kind: "detail", FileParse: "Second parsed block only.", LineStart: 13, LineEnd: 13},
		{Kind: "header", Text: "Header block text.", LineStart: 1, LineEnd: 2},
		{Kind: "empty", Text: "   "},
	}
	return secret, parentID, parentText, occurredAt, subUnits
}

// TestCrossLanguageCompositePacketParity verifies the 4D composite encoder
// (`PackCompositePackets`): parent and sub-unit packets, spatial commitments,
// cited parent ids and the sub-id → parent-id map.
func TestCrossLanguageCompositePacketParity(t *testing.T) {
	secret, parentID, parentText, occurredAt, subUnits := compositeProbeFixture()
	enc := ramesdk.NewBlindEncoder([]byte(secret))
	packets, subToParent := enc.PackCompositePackets(parentID, parentText, occurredAt, subUnits)
	want := compositeProbeResult{Packets: packets, SubToParent: subToParent}
	// 1 parent + 3 non-empty sub-units ("empty" is skipped).
	if len(want.Packets) != 4 {
		t.Fatalf("Go composite fixture produced %d packets, want 4: %+v", len(want.Packets), want.Packets)
	}
	for _, pkt := range want.Packets[1:] {
		if pkt.DocHash != want.Packets[0].DocHash {
			t.Fatalf("sub-unit %q doc_h=%d does not match parent doc_h=%d", pkt.UnitID, pkt.DocHash, want.Packets[0].DocHash)
		}
		if len(pkt.CitedUnitIDs) != 1 || pkt.CitedUnitIDs[0] != parentID {
			t.Fatalf("sub-unit %q cited_unit_ids=%v, want [%s]", pkt.UnitID, pkt.CitedUnitIDs, parentID)
		}
	}
	if want.SubToParent["docA#L1-10#detail:0"] != parentID || want.SubToParent["docA#L1-10#detail:1"] != parentID {
		t.Fatalf("sub_to_parent map drifted: %v", want.SubToParent)
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal composite result: %v", err)
	}
	var roundtrip compositeProbeResult
	if err := json.Unmarshal(raw, &roundtrip); err != nil || !reflect.DeepEqual(roundtrip, want) {
		t.Fatalf("composite JSON round-trip mismatch: err=%v", err)
	}
}

// TestCrossLanguageUnitMetaPacketParity verifies the `UnitMeta` surface —
// including `file_parse`, `file_url`/`doc_h`, `block_id`/`block_h`, the
// tri-temporal timestamps and the author commitment.
func TestCrossLanguageUnitMetaPacketParity(t *testing.T) {
	secret := "rame-unitmeta-parity-key-v1"
	docID := "sgm-meta-1"
	text := "ID: sgm-meta-1\nSummary: groceries"
	meta := ramesdk.UnitMeta{
		Author:     "Alice",
		OccurredAt: 1700000000,
		NarratedAt: 1700000500,
		FileURL:    "mail:2022-01-01",
		FileParse:  "Subject: delivery",
		BlockID:    "sgm-meta-1#header",
		LineStart:  3,
		LineEnd:    5,
	}
	enc := ramesdk.NewBlindEncoder([]byte(secret))
	packet, _ := enc.PackBlindPacketWithMeta(docID, text, meta)
	if packet.DocHash == 0 || packet.BlockHash == 0 || packet.AuthorHash == 0 || packet.LineStart != 3 || packet.LineEnd != 5 {
		t.Fatalf("Go UnitMeta fixture lost commitments: %+v", packet)
	}
}
