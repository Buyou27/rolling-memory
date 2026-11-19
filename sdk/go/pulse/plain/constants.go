// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import "math"

// eulerNumber is Euler's number as a named universal constant.
const eulerNumber = math.E

// eulerSquared is e² as the constant product (same double as math.E*math.E).
const eulerSquared = eulerNumber * eulerNumber

// transmissionPerHop is the per-hop transmission 1/e (one nat of surprisal per hop).
const transmissionPerHop = 1.0 / math.E
