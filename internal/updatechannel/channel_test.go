// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package updatechannel

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{"": Dev, "stable": Stable, " BETA ": Beta, "dev": Dev}
	for input, want := range cases {
		got, err := Normalize(input)
		if err != nil || got != want {
			t.Fatalf("Normalize(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := Normalize("nightly"); err == nil {
		t.Fatal("unknown channel must be rejected")
	}
}
