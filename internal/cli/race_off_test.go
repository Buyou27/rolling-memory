// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build !race

package cli

// raceDetectorEnabled lets the heaviest scale cases keep their functional
// assertions while the race detector's multiplicative slowdown would push them
// past any sane wall-clock hang guard on shared CI runners.
const raceDetectorEnabled = false
