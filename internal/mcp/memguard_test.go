// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import "testing"

func TestColdBuildMemoryLimit(t *testing.T) {
	const gib = int64(1 << 30)
	cases := []struct {
		name  string
		state daemonMemoryState
		want  int64
	}{
		{
			name:  "auto on a large machine leaves the steady cap but opens the window",
			state: daemonMemoryState{TotalRAM: 48 * gib, LimitBytes: 4 * gib, LimitSource: "auto", ConfigValue: "auto"},
			want:  36 * gib,
		},
		{
			name:  "explicit cap above the window keeps the larger cap",
			state: daemonMemoryState{TotalRAM: 16 * gib, LimitBytes: 14 * gib, LimitSource: "explicit", ConfigValue: "14GiB"},
			want:  14 * gib,
		},
		{
			name:  "operator GOMEMLIMIT stays authoritative",
			state: daemonMemoryState{TotalRAM: 48 * gib, LimitBytes: 2 * gib, LimitSource: "env", ConfigValue: "auto"},
			want:  0,
		},
		{
			name:  "a disabled limit already has the runtime default",
			state: daemonMemoryState{TotalRAM: 48 * gib, LimitSource: "off", ConfigValue: "off"},
			want:  0,
		},
		{
			name:  "unknown physical memory keeps current behaviour",
			state: daemonMemoryState{LimitBytes: 4 * gib, LimitSource: "auto", ConfigValue: "auto"},
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := coldBuildMemoryLimit(tc.state); got != tc.want {
				t.Fatalf("coldBuildMemoryLimit(%+v) = %d, want %d", tc.state, got, tc.want)
			}
		})
	}
}
