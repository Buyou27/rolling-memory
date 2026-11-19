// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package store

import (
	"sync"
	"testing"
)

func TestDuplicateTraceAudienceConflictFailsClosed(t *testing.T) {
	for _, first := range []string{"shared", "private"} {
		t.Run(first, func(t *testing.T) {
			st := newTestStore(t, "mem:self")
			other := "private"
			if first == "private" {
				other = "shared"
			}
			opts := TraceOptions{Session: "audience-conflict", Turn: 1, Audience: first}
			hash, inserted, err := st.AddTrace("mem:self", "synthetic duplicate", opts)
			if err != nil || !inserted {
				t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
			}
			opts.Audience = other
			if _, _, err := st.AddTrace("mem:self", "synthetic duplicate", opts); err == nil {
				t.Fatal("audience conflict accepted")
			}
			trace, found, err := st.Trace(hash)
			if err != nil || !found || trace.Audience != first {
				t.Fatalf("original trace changed: %#v found=%v err=%v", trace, found, err)
			}
		})
	}
}

func TestConcurrentDuplicateAudienceConflict(t *testing.T) {
	st := newTestStore(t, "mem:self")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, audience := range []string{"shared", "private"} {
		wg.Add(1)
		go func(audience string) {
			defer wg.Done()
			<-start
			_, _, err := st.AddTrace("mem:self", "concurrent duplicate", TraceOptions{Session: "concurrent", Audience: audience})
			results <- err
		}(audience)
	}
	close(start)
	wg.Wait()
	close(results)
	failures := 0
	for err := range results {
		if err != nil {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("audience conflicts=%d, want 1", failures)
	}
}
