// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import "testing"

func TestUpdateRoomSpecAndPermissions(t *testing.T) {
	key, events, ok := parseWSRoomSpec(wsRoomUpdate)
	if !ok || key.kind != streamKindUpdate || len(events) != 1 || events[0] != "update" {
		t.Fatalf("rm/update room spec = %#v %v %v", key, events, ok)
	}
	if _, _, ok := parseWSRoomSpec("rm/update:extra"); ok {
		t.Fatal("rm/update must be an exact room name")
	}
	rules, op := wsRoomPermissionRules(streamKindUpdate)
	if op != "OR" || len(rules) != 3 ||
		rules[0] != "plugin/rolling-memory/system/version" ||
		rules[1] != "plugin/rolling-memory/system/update/check" ||
		rules[2] != "plugin/rolling-memory/status" {
		t.Fatalf("update room permissions = %v %s", rules, op)
	}
}

func TestUpdateStreamHubShape(t *testing.T) {
	hub := newUpdateStreamHub(streamKey{kind: streamKindUpdate}, t.TempDir())
	if hub == nil || len(hub.producers) != 1 {
		t.Fatalf("unexpected hub shape: %#v", hub)
	}
	if hub.interval != updateStreamInterval || updateStreamInterval <= 0 {
		t.Fatalf("hub interval = %s, want %s", hub.interval, updateStreamInterval)
	}
	if hub.producers[0].event != "update" || hub.producers[0].alwaysPush {
		t.Fatalf("producer shape = %#v", hub.producers[0])
	}
}
