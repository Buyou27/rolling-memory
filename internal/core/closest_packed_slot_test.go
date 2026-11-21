// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math/rand"
	"sort"
	"testing"
)

// closestPackedSlotRef is the two-pointer reference kept for differential tests.
func closestPackedSlotRef(left, right []uint32) int {
	nL, nR := len(left), len(right)
	if nL == 0 || nR == 0 {
		return -1
	}
	if (left[nL-1]>>16) < (right[0]>>16) || (right[nR-1]>>16) < (left[0]>>16) {
		return -1
	}
	closest := -1
	for x, y := 0, 0; x < nL && y < nR; {
		lSent, rSent := left[x]>>16, right[y]>>16
		switch {
		case lSent < rSent:
			x++
		case lSent > rSent:
			y++
		default:
			lSlot, rSlot := int(left[x]&0xFFFF), int(right[y]&0xFFFF)
			diff := lSlot - rSlot
			if diff < 0 {
				diff = -diff
			}
			if closest < 0 || diff < closest {
				closest = diff
				if closest == 0 {
					return 0
				}
			}
			if lSlot < rSlot {
				x++
			} else {
				y++
			}
		}
	}
	return closest
}

func packedPositions(rng *rand.Rand, n, maxSent, maxSlot int) []uint32 {
	set := make(map[uint32]struct{}, n)
	for i := 0; i < n; i++ {
		sent := rng.Intn(maxSent)
		slot := rng.Intn(maxSlot)
		set[uint32(sent)<<16|uint32(slot)] = struct{}{}
	}
	out := make([]uint32, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestClosestPackedSlotSinglePosition(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for trial := 0; trial < 20000; trial++ {
		nL := 1 + rng.Intn(6)
		nR := 1 + rng.Intn(6)
		if trial%3 == 0 {
			nL = 1
		}
		if trial%5 == 0 {
			nR = 1
		}
		left := packedPositions(rng, nL, 6, 30)
		right := packedPositions(rng, nR, 6, 30)
		got := closestPackedSlot(left, right)
		want := closestPackedSlotRef(left, right)
		if got != want {
			t.Fatalf("trial=%d left=%v right=%v got=%d want=%d", trial, left, right, got, want)
		}
	}
}
