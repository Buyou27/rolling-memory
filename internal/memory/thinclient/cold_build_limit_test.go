// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"runtime/debug"
	"testing"
)

// The cold-build window must survive the per-request clone, otherwise a recall
// that happens to trigger the first build would run inside the steady-state cap.
func TestColdBuildMemoryLimitSharedWithClones(t *testing.T) {
	v := &Vault{coldBuildMemoryLimit: new(int64)}
	if got := v.coldBuildLimit(); got != 0 {
		t.Fatalf("default window = %d, want 0", got)
	}
	v.SetColdBuildMemoryLimit(8 << 30)
	if got := v.coldBuildLimit(); got != 8<<30 {
		t.Fatalf("window = %d, want %d", got, int64(8<<30))
	}
	clone := v.CloneWithSharedSlotCache()
	if got := clone.coldBuildLimit(); got != 8<<30 {
		t.Fatalf("clone window = %d, want %d", got, int64(8<<30))
	}
	v.SetColdBuildMemoryLimit(0)
	if got := clone.coldBuildLimit(); got != 0 {
		t.Fatalf("clone window after reset = %d, want 0", got)
	}
	if got := (&Vault{}).coldBuildLimit(); got != 0 {
		t.Fatalf("nil-pointer window = %d, want 0", got)
	}
}

// The window is a temporary raise: the previous steady-state limit must come
// back once the build (and its persist) is done.
func TestApplyColdBuildWindowRestoresPreviousLimit(t *testing.T) {
	const steady = int64(1 << 30)
	const window = int64(4 << 30)
	original := debug.SetMemoryLimit(-1)
	debug.SetMemoryLimit(steady)
	t.Cleanup(func() { debug.SetMemoryLimit(original) })

	restore := applyColdBuildWindow(window)
	if restore == nil {
		t.Fatal("a larger window must be applied")
	}
	if got := debug.SetMemoryLimit(-1); got != window {
		t.Fatalf("runtime limit during build = %d, want %d", got, window)
	}
	restore()
	if got := debug.SetMemoryLimit(-1); got != steady {
		t.Fatalf("runtime limit after build = %d, want %d", got, steady)
	}
	if applyColdBuildWindow(0) != nil {
		t.Fatal("a zero window must leave the runtime untouched")
	}
	if applyColdBuildWindow(steady/2) != nil {
		t.Fatal("a window below the current limit must leave the runtime untouched")
	}
}
