// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"sync"
	"time"
)

// Window allows at most a fixed number of events per period for each key.
// It lives in memory only: no address is ever written to disk, and a restart
// forgets everything, which costs nothing worse than one fresh window.
type Window struct {
	limit  int
	period time.Duration

	mu     sync.Mutex
	events map[string][]time.Time
}

// NewWindow returns a Window of limit events per period.
func NewWindow(limit int, period time.Duration) *Window {
	return &Window{limit: limit, period: period, events: map[string][]time.Time{}}
}

// Allowed reports whether one more event for key fits now. It records nothing.
func (w *Window) Allowed(key string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.recent(key, now)) < w.limit
}

// Add records one event for key at now.
func (w *Window) Add(key string, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ev := append(w.recent(key, now), now)
	if len(ev) > w.limit {
		ev = ev[len(ev)-w.limit:] // older events cannot change the answer
	}
	w.events[key] = ev
}

// Count returns how many events key had within the period before now.
func (w *Window) Count(key string, now time.Time) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.recent(key, now))
}

// Prune forgets keys whose events are all older than the period.
func (w *Window) Prune(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key := range w.events {
		w.recent(key, now)
	}
}

// recent drops key's events that left the period and returns the rest.
// The caller holds the lock.
func (w *Window) recent(key string, now time.Time) []time.Time {
	ev := w.events[key]
	cut := 0
	for cut < len(ev) && now.Sub(ev[cut]) >= w.period {
		cut++
	}
	ev = ev[cut:]
	if len(ev) == 0 {
		delete(w.events, key)
		return nil
	}
	w.events[key] = ev
	return ev
}

// Backoff slows down repeated failures without locking anyone out: after a
// number of free failures in a row, every further attempt has to wait twice
// as long as the one before, up to a ceiling.
type Backoff struct {
	free   int
	max    time.Duration
	forget time.Duration

	mu    sync.Mutex
	state map[string]backoffState
}

type backoffState struct {
	fails int
	last  time.Time
}

// NewBackoff returns a Backoff that lets free failures pass, waits at most
// max between attempts, and forgets a key after forget without a failure.
func NewBackoff(free int, max, forget time.Duration) *Backoff {
	return &Backoff{free: free, max: max, forget: forget, state: map[string]backoffState{}}
}

// Wait returns how long key must still wait before its next attempt. Zero
// means it may try now.
func (b *Backoff) Wait(key string, now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.state[key]
	if !ok || now.Sub(s.last) >= b.forget {
		return 0
	}
	if s.fails < b.free {
		return 0
	}
	delay := b.max
	if shift := s.fails - b.free; shift < 16 {
		if d := time.Second << shift; d < b.max {
			delay = d
		}
	}
	if left := delay - now.Sub(s.last); left > 0 {
		return left
	}
	return 0
}

// Fail records a failed attempt for key.
func (b *Backoff) Fail(key string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state[key]
	if now.Sub(s.last) >= b.forget {
		s.fails = 0
	}
	s.fails++
	s.last = now
	b.state[key] = s
}

// Reset forgets key's failures, after a successful attempt.
func (b *Backoff) Reset(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, key)
}

// Prune forgets keys with no failure for longer than the forget period.
func (b *Backoff) Prune(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, s := range b.state {
		if now.Sub(s.last) >= b.forget {
			delete(b.state, key)
		}
	}
}
