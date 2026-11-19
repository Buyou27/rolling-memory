// plugin: rolling-memory v0.0.58
package rollingmemory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDetectActiveHarnessLabelHonestDefault locks the honest default: no live
// lease means no current host. A legacy single-carrier document, an expired
// lease, or a missing file all resolve to the empty label; the console renders
// "unknown" instead of borrowing the last writer's brand.
func TestDetectActiveHarnessLabelHonestDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
	if got := detectActiveHarnessLabel(home); got != "" {
		t.Fatalf("missing ledger: label = %q, want empty (unknown)", got)
	}
	stateDir := rollingMemoryStateDir(home)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	statePath := filepath.Join(stateDir, "active_workspace.json")

	writeLedger := func(leaseDelta time.Duration, host, label string) {
		t.Helper()
		now := time.Now()
		doc := map[string]any{
			"version": 2,
			"sessions": map[string]any{
				host + ":s1": map[string]any{
					"host":         host,
					"label":        label,
					"session_id":   "s1",
					"lease_until":  now.Add(leaseDelta).Unix(),
					"last_seen_at": now.Unix(),
				},
			},
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal ledger: %v", err)
		}
		if err := os.WriteFile(statePath, raw, 0o600); err != nil {
			t.Fatalf("write ledger: %v", err)
		}
	}

	writeLedger(time.Minute, "workbuddy", "WorkBuddy")
	if got := detectActiveHarnessLabel(home); got != "WorkBuddy" {
		t.Fatalf("live workbuddy label = %q, want WorkBuddy", got)
	}
	writeLedger(time.Minute, "zcode", "ZCode")
	if got := detectActiveHarnessLabel(home); got != "ZCode" {
		t.Fatalf("live zcode label = %q, want ZCode", got)
	}
	writeLedger(-time.Minute, "zcode", "ZCode")
	if got := detectActiveHarnessLabel(home); got != "" {
		t.Fatalf("expired lease label = %q, want empty", got)
	}
	if err := os.WriteFile(statePath, []byte(`{"carrier":"harness:zcode"}`), 0o600); err != nil {
		t.Fatalf("write legacy presence: %v", err)
	}
	if got := detectActiveHarnessLabel(home); got != "" {
		t.Fatalf("legacy carrier label = %q, want empty", got)
	}
}
