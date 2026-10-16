// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func testPuzzle() *Puzzle {
	return NewPuzzle(PuzzleConfig{
		BaseBits: 8, MaxBits: 10, TTL: 10 * time.Minute, MinAge: 2 * time.Second,
		MaxOpen: 50, MaxOpenPerKey: 20,
	})
}

func TestLeadingZeroBits(t *testing.T) {
	cases := map[int][]byte{
		0:  {0x80, 0},
		1:  {0x40},
		8:  {0x00, 0xff},
		15: {0x00, 0x01},
		16: {0x00, 0x00},
	}
	for want, b := range cases {
		if got := LeadingZeroBits(b); got != want {
			t.Errorf("LeadingZeroBits(%x) = %d, want %d", b, got, want)
		}
	}
}

func TestSolutionIsAcceptedOnce(t *testing.T) {
	p := testPuzzle()
	c, err := p.Issue("net", t0)
	if err != nil {
		t.Fatal(err)
	}
	sol := Solve(c.ID, c.Bits)
	if err := p.Check(c.ID, sol, t0.Add(3*time.Second)); err != nil {
		t.Fatalf("a correct solution was refused: %v", err)
	}
	if err := p.Check(c.ID, sol, t0.Add(4*time.Second)); !errors.Is(err, ErrChallengeAbsent) {
		t.Fatalf("a solution was accepted twice: %v", err)
	}
}

func TestBadSolutionsAreRefusedAndUseTheChallengeUp(t *testing.T) {
	p := testPuzzle()
	cases := map[string]struct {
		solution func(Challenge) string
		at       time.Duration
		want     error
	}{
		"too fast":      {func(c Challenge) string { return Solve(c.ID, c.Bits) }, time.Second, ErrSolvedTooFast},
		"expired":       {func(c Challenge) string { return Solve(c.ID, c.Bits) }, 10 * time.Minute, ErrChallengeOld},
		"not a number":  {func(Challenge) string { return "12a" }, 3 * time.Second, ErrWrongSolution},
		"too long":      {func(Challenge) string { return "123456789012345678901" }, 3 * time.Second, ErrWrongSolution},
		"wrong counter": {func(c Challenge) string { return wrongSolution(c) }, 3 * time.Second, ErrWrongSolution},
	}
	for name, tc := range cases {
		c, err := p.Issue("net-"+name, t0)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Check(c.ID, tc.solution(c), t0.Add(tc.at)); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
		// Whatever the outcome, the challenge is gone; a retry needs a new one.
		if err := p.Check(c.ID, Solve(c.ID, c.Bits), t0.Add(3*time.Second)); !errors.Is(err, ErrChallengeAbsent) {
			t.Errorf("%s: the challenge survived a failed check: %v", name, err)
		}
	}
}

// wrongSolution returns the first counter that does not meet the difficulty.
func wrongSolution(c Challenge) string {
	for i := uint64(0); ; i++ {
		if s := strconv.FormatUint(i, 10); LeadingZeroBits(digest(c.ID, s)) < c.Bits {
			return s
		}
	}
}

func TestDifficultyRisesWithDemandAndStopsAtTheCap(t *testing.T) {
	p := testPuzzle()
	var seen []int
	for i := 0; i < 15; i++ {
		c, err := p.Issue("busy", t0.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, c.Bits)
	}
	// 8 bits for the first five, 9 for the next five, then the cap of 10.
	want := []int{8, 8, 8, 8, 8, 9, 9, 9, 9, 9, 10, 10, 10, 10, 10}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("difficulties %v, want %v", seen, want)
		}
	}
	quiet, _ := p.Issue("quiet", t0.Add(time.Minute))
	if quiet.Bits != 8 {
		t.Fatalf("a quiet network got %d bits; demand elsewhere must not raise it", quiet.Bits)
	}
}

func TestOpenChallengesAreCapped(t *testing.T) {
	p := testPuzzle()
	for i := 0; i < 20; i++ {
		if _, err := p.Issue("one-net", t0); err != nil {
			t.Fatalf("challenge %d refused: %v", i+1, err)
		}
	}
	if _, err := p.Issue("one-net", t0); !errors.Is(err, ErrPuzzleBusy) {
		t.Fatalf("one network opened more than its share: %v", err)
	}
	for i := 0; i < 30; i++ {
		_, _ = p.Issue("net-"+string(rune('a'+i)), t0)
	}
	if _, err := p.Issue("late", t0); !errors.Is(err, ErrPuzzleBusy) {
		t.Fatalf("the total cap was not enforced: %v", err)
	}
	// Once they expire, the space is free again.
	if _, err := p.Issue("late", t0.Add(11*time.Minute)); err != nil {
		t.Fatalf("expired challenges still held the space: %v", err)
	}
}
