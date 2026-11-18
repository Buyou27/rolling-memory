// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Daemon memory limit presets. The value is the Go soft memory limit
// (runtime/debug.SetMemoryLimit) applied when the background engine daemon
// starts; it bounds the garbage collector's heap target, not resident memory
// held by the OS page cache.
const (
	// MemoryLimitAuto derives the cap from physical RAM (1/8) inside the
	// [AutoMemoryLimitFloor, AutoMemoryLimitCeiling] range.
	MemoryLimitAuto = "auto"
	// MemoryLimitOff disables the explicit cap. A GOMEMLIMIT environment
	// value, when present, still applies through the Go runtime.
	MemoryLimitOff = "off"
)

// Auto memory limit bounds. A daemon that carries the full inverted index and
// the .zcsr mmap layer cannot run usefully below 1 GiB (the collector would
// thrash on every query), and a single-user desktop client gains nothing from
// a cap above 4 GiB.
const (
	AutoMemoryLimitFloor   int64 = 1 << 30
	AutoMemoryLimitCeiling int64 = 4 << 30
	// ExplicitMemoryLimitFloor is the smallest explicit cap accepted from
	// configuration. Below this the index plus query working set does not fit.
	ExplicitMemoryLimitFloor int64 = 64 << 20
	// ExplicitMemoryLimitCeiling rejects obviously mistyped values such as
	// "512TiB" instead of letting them reach the runtime as an effective no-op.
	ExplicitMemoryLimitCeiling int64 = 1 << 40
)

// MemoryLimitResolved is the outcome of resolving a configured memory limit.
// Bytes is 0 when no explicit cap should be applied; Source reports where the
// decision came from (auto / explicit / off / unavailable).
type MemoryLimitResolved struct {
	Value  string
	Bytes  int64
	Source string
}

// ParseMemoryLimit validates a memory_limit setting and returns its canonical
// form plus the byte count (0 for auto/off). Accepted inputs: "auto", "off",
// an empty string (treated as auto), or an integer byte count with an optional
// binary unit suffix (B, KiB, MiB, GiB, TiB; case-insensitive, optional space).
func ParseMemoryLimit(raw string) (string, int64, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", MemoryLimitAuto:
		return MemoryLimitAuto, 0, true
	case MemoryLimitOff:
		return MemoryLimitOff, 0, true
	}
	bytes, ok := parseMemorySize(value)
	if !ok {
		return "", 0, false
	}
	if bytes < ExplicitMemoryLimitFloor || bytes > ExplicitMemoryLimitCeiling {
		return "", 0, false
	}
	return formatMemorySize(bytes), bytes, true
}

// ResolveMemoryLimit turns a configured value into the concrete byte cap to
// apply. totalRAM is the physical memory reported by the OS; 0 means the probe
// failed and auto resolves to no cap (unavailable) instead of guessing.
func ResolveMemoryLimit(raw string, totalRAM int64) MemoryLimitResolved {
	canonical, explicit, ok := ParseMemoryLimit(raw)
	if !ok {
		canonical, explicit = MemoryLimitAuto, 0
	}
	if canonical == MemoryLimitOff {
		return MemoryLimitResolved{Value: MemoryLimitOff, Source: "off"}
	}
	if canonical != MemoryLimitAuto {
		return MemoryLimitResolved{Value: canonical, Bytes: explicit, Source: "explicit"}
	}
	if totalRAM <= 0 {
		return MemoryLimitResolved{Value: MemoryLimitAuto, Source: "unavailable"}
	}
	derived := totalRAM / 8
	if derived < AutoMemoryLimitFloor {
		derived = AutoMemoryLimitFloor
	}
	if derived > AutoMemoryLimitCeiling {
		derived = AutoMemoryLimitCeiling
	}
	return MemoryLimitResolved{Value: formatMemorySize(derived), Bytes: derived, Source: "auto"}
}

func parseMemorySize(value string) (int64, bool) {
	digitsEnd := 0
	for digitsEnd < len(value) && value[digitsEnd] >= '0' && value[digitsEnd] <= '9' {
		digitsEnd++
	}
	if digitsEnd == 0 {
		return 0, false
	}
	digits := value[:digitsEnd]
	unit := strings.TrimSpace(value[digitsEnd:])
	multiplier := int64(1)
	switch unit {
	case "", "b", "byte", "bytes":
		multiplier = 1
	case "k", "kb", "kib":
		multiplier = 1 << 10
	case "m", "mb", "mib":
		multiplier = 1 << 20
	case "g", "gb", "gib":
		multiplier = 1 << 30
	case "t", "tb", "tib":
		multiplier = 1 << 40
	default:
		return 0, false
	}
	parsed, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	if parsed > math.MaxInt64/multiplier {
		return 0, false
	}
	return parsed * multiplier, true
}

func formatMemorySize(bytes int64) string {
	switch {
	case bytes >= int64(1<<40) && bytes%int64(1<<40) == 0:
		return fmt.Sprintf("%dTiB", bytes/(1<<40))
	case bytes%int64(1<<30) == 0:
		return fmt.Sprintf("%dGiB", bytes/(1<<30))
	case bytes%int64(1<<20) == 0:
		return fmt.Sprintf("%dMiB", bytes/(1<<20))
	case bytes%int64(1<<10) == 0:
		return fmt.Sprintf("%dKiB", bytes/(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
