// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package tokenzr

import "math"

// eulerNumber is Euler's number as a named universal constant. It must stay
// bit-identical to math.E: the sealed prior anchors below preserve historical
// values exactly, and a one-ulp drift would change their readings.
const eulerNumber = math.E

// transmissionPerHop is the per-hop transmission 1/e used by the sealed
// initial-prior anchor composition (one nat of surprisal per hop).
const transmissionPerHop = 1.0 / math.E
