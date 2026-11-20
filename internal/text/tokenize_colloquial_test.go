// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package text

import "testing"

// TestIsHighGainTokenColloquialDemotion pins that colloquial chat tokens (laughter, expressive
// elongation) never enter the Primary Gain Tier, while numeric/currency tokens remain high-gain.
// Apostrophe-less contractions are deliberately not demoted (see tokenzr.IsBPEContractedAuxStem).
func TestIsHighGainTokenColloquialDemotion(t *testing.T) {
	for _, in := range []string{"hahaha", "hehehe", "sooooo", "ummmmm"} {
		if IsHighGainToken(in) {
			t.Errorf("IsHighGainToken(%q)=true want false (colloquial token must stay out of Primary Gain Tier)", in)
		}
	}
	for _, in := range []string{"$5", "#42", "５"} {
		if !IsHighGainToken(in) {
			t.Errorf("IsHighGainToken(%q)=false want true (numeric/currency token)", in)
		}
	}
}
