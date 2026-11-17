// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJSONFileDuplicateKeyLastWins: duplicate keys must resolve like every host
// JSON parser (dict semantics, last value wins), so an install that rewrites
// the file registers exactly the entry the host will read.
func TestJSONFileDuplicateKeyLastWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"mcp":{"servers":{}},"mcp":{"servers":{"other":{"command":"other"}}}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := loadJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, found := objectGet(data, "mcp")
	if !found {
		t.Fatalf("mcp key missing: %#v", data)
	}
	object, ok := asObject(value)
	if !ok {
		t.Fatalf("mcp is not an object: %#v", value)
	}
	serversValue, found := objectGet(object, "servers")
	if !found {
		t.Fatalf("servers missing: %#v", object)
	}
	servers, ok := asObject(serversValue)
	if !ok {
		t.Fatalf("servers is not an object: %#v", serversValue)
	}
	if _, ok := objectGet(servers, "other"); !ok {
		t.Fatalf("last duplicate value lost: %#v", servers)
	}
	text, err := dumpJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(text, `"mcp"`) != 1 {
		t.Fatalf("duplicate key survived the round-trip:\n%s", text)
	}
}
