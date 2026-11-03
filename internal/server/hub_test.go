// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"testing"

	"github.com/whiteravens20/darknode/internal/game"
)

// seqOf reads the number a delta was given.
func seqOf(t *testing.T, d game.Delta) uint64 {
	t.Helper()
	switch v := d.(type) {
	case game.WorldDelta:
		return v.Seq
	case game.RunDelta:
		return v.Seq
	case game.ResyncDelta:
		return v.Seq
	default:
		t.Fatalf("a delta of type %T has no number here", d)
		return 0
	}
}

// drain reads everything waiting for a stream and returns the numbers.
func drain(t *testing.T, sub *subscriber) []uint64 {
	t.Helper()
	var got []uint64
	for len(sub.events) > 0 {
		got = append(got, seqOf(t, (<-sub.events).delta))
	}
	return got
}

// TestADroppedDeltaLeavesAGapInTheNumbering is the whole reason the events are
// numbered. A browser that stops reading must not quietly keep a state the
// server has moved past: the delta it never got takes its number with it, and
// the next one it does get does not follow the last one.
func TestADroppedDeltaLeavesAGapInTheNumbering(t *testing.T) {
	h := NewHub(2, 10)
	sub, err := h.Subscribe(7, []byte("token"))
	if err != nil {
		t.Fatal(err)
	}
	if sub.seq != 1 {
		t.Fatalf("a new stream starts at %d; number one belongs to its snapshot", sub.seq)
	}

	// Two more deltas than the queue holds: nobody is reading.
	for range streamQueue + 2 {
		h.Publish(7, game.DeltaWorld, game.WorldDelta{})
	}

	got := drain(t, sub)
	if len(got) != streamQueue {
		t.Fatalf("the queue held %d deltas, want %d", len(got), streamQueue)
	}
	for i, n := range got {
		if want := uint64(i + 2); n != want {
			t.Fatalf("the queued deltas are numbered %v; number %d is %d, want %d", got, i, n, want)
		}
	}

	// The next delta that fits is numbered past the two that were lost.
	h.Publish(7, game.DeltaWorld, game.WorldDelta{})
	next := drain(t, sub)
	if len(next) != 1 {
		t.Fatalf("got %d deltas after draining the queue, want 1", len(next))
	}
	last := got[len(got)-1]
	if next[0] == last+1 {
		t.Fatalf("delta %d follows %d, so the two that were dropped left no gap", next[0], last)
	}
	if want := last + 3; next[0] != want {
		t.Errorf("the next delta is numbered %d, want %d: two were dropped", next[0], want)
	}
}

// TestEveryStreamIsNumberedOnItsOwn checks what two windows of one account
// need: each counts from its own snapshot, because each opened at a different
// moment.
func TestEveryStreamIsNumberedOnItsOwn(t *testing.T) {
	h := NewHub(2, 10)
	first, err := h.Subscribe(7, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	h.Publish(7, game.DeltaWorld, game.WorldDelta{})

	second, err := h.Subscribe(7, []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	h.Publish(7, game.DeltaWorld, game.WorldDelta{})

	if got := drain(t, first); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("the window that was open the whole time got %v, want [2 3]", got)
	}
	if got := drain(t, second); len(got) != 1 || got[0] != 2 {
		t.Errorf("the window that opened later got %v, want [2]", got)
	}
}

// TestADeltaReachesOnlyItsOwnAccount guards the one mistake that would show
// one player another player's state.
func TestADeltaReachesOnlyItsOwnAccount(t *testing.T) {
	h := NewHub(2, 10)
	mine, err := h.Subscribe(7, []byte("mine"))
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := h.Subscribe(8, []byte("theirs"))
	if err != nil {
		t.Fatal(err)
	}

	h.Publish(7, game.DeltaRun, game.RunDelta{})
	if got := drain(t, mine); len(got) != 1 {
		t.Errorf("the account the delta was for got %d deltas, want 1", len(got))
	}
	if got := drain(t, theirs); len(got) != 0 {
		t.Errorf("another account got %d deltas, want none", len(got))
	}

	// The clock, on the other hand, is everybody's.
	h.Broadcast(game.DeltaWorld, game.WorldDelta{})
	if got := drain(t, mine); len(got) != 1 {
		t.Errorf("a broadcast reached one account %d times, want 1", len(got))
	}
	if got := drain(t, theirs); len(got) != 1 {
		t.Errorf("a broadcast reached the other account %d times, want 1", len(got))
	}
}
