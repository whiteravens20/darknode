// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func TestWindowLimitsPerKeyAndPeriod(t *testing.T) {
	w := NewWindow(3, time.Hour)
	for i := 0; i < 3; i++ {
		if !w.Allowed("a", t0) {
			t.Fatalf("event %d refused below the limit", i+1)
		}
		w.Add("a", t0.Add(time.Duration(i)*time.Minute))
	}
	if w.Allowed("a", t0.Add(3*time.Minute)) {
		t.Fatal("a fourth event within the hour was allowed")
	}
	if !w.Allowed("b", t0.Add(3*time.Minute)) {
		t.Fatal("another key shares the limit")
	}
	// The first event leaves the window one hour after it happened.
	if w.Allowed("a", t0.Add(time.Hour-time.Nanosecond)) {
		t.Fatal("allowed before the oldest event left the window")
	}
	if !w.Allowed("a", t0.Add(time.Hour)) {
		t.Fatal("still refused after the oldest event left the window")
	}
}

func TestWindowKeepsMemoryBounded(t *testing.T) {
	w := NewWindow(2, time.Hour)
	for i := 0; i < 100; i++ {
		w.Add("a", t0.Add(time.Duration(i)*time.Second))
	}
	if n := len(w.events["a"]); n != 2 {
		t.Fatalf("a key holds %d events, want at most the limit of 2", n)
	}
	w.Prune(t0.Add(2 * time.Hour))
	if len(w.events) != 0 {
		t.Fatalf("prune left %d keys", len(w.events))
	}
}

func TestBackoffDoublesUpToTheCeiling(t *testing.T) {
	b := NewBackoff(3, 30*time.Second, 15*time.Minute)
	now := t0
	for i := 0; i < 3; i++ {
		if w := b.Wait("k", now); w != 0 {
			t.Fatalf("free failure %d had to wait %v", i+1, w)
		}
		b.Fail("k", now)
	}
	// After three failures: 1s, then 2s, 4s, 8s, 16s, then the 30s ceiling.
	for _, want := range []time.Duration{1, 2, 4, 8, 16, 30, 30} {
		want *= time.Second
		if got := b.Wait("k", now); got != want {
			t.Fatalf("wait %v, want %v", got, want)
		}
		now = now.Add(want)
		if got := b.Wait("k", now); got != 0 {
			t.Fatalf("still waiting %v after the delay passed", got)
		}
		b.Fail("k", now)
	}
}

func TestBackoffResetAndForget(t *testing.T) {
	b := NewBackoff(1, 30*time.Second, 15*time.Minute)
	b.Fail("k", t0)
	if b.Wait("k", t0) == 0 {
		t.Fatal("no wait after a failure past the free ones")
	}
	b.Reset("k")
	if b.Wait("k", t0) != 0 {
		t.Fatal("reset did not clear the wait")
	}
	b.Fail("k", t0)
	b.Fail("k", t0)
	later := t0.Add(15 * time.Minute)
	if b.Wait("k", later) != 0 {
		t.Fatal("failures were not forgotten after the forget period")
	}
	b.Fail("k", later)
	if got := b.Wait("k", later); got != time.Second {
		t.Fatalf("a failure after forgetting counts as the first again: wait %v, want 1s", got)
	}
	b.Prune(later.Add(16 * time.Minute))
	if len(b.state) != 0 {
		t.Fatalf("prune left %d keys", len(b.state))
	}
}
