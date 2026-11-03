// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"bytes"
	"errors"
	"sync"

	"github.com/whiteravens20/darknode/internal/game"
)

// ErrTooManyStreams is returned when an account or the server has as many
// open streams as it allows.
var ErrTooManyStreams = errors.New("too many open streams")

// streamQueue is how many deltas wait for one browser. A browser that falls
// this far behind — a tab the machine suspended, a connection that stopped
// reading — loses a delta, sees the gap in the numbering and asks for a fresh
// snapshot. That is cheaper than holding the simulation back for it.
const streamQueue = 32

// Hub tracks the open state streams. It is also the game's Publisher: the
// simulation hands it deltas, and it numbers and queues them per stream.
type Hub struct {
	maxPerAccount, maxTotal int

	mu        sync.Mutex
	byAccount map[int64]map[*subscriber]struct{}
	total     int
}

type subscriber struct {
	account   int64
	tokenHash []byte
	// events carries the deltas waiting to be written to this stream.
	events chan streamEvent
	// seq is the number of the last delta meant for this stream, delivered
	// or dropped. The hub holds the lock when it touches this.
	seq uint64
	// closed is closed when the stream must end.
	closed chan struct{}
	once   sync.Once
}

// streamEvent is one numbered delta on its way to one browser.
type streamEvent struct {
	kind  string
	delta game.Delta
}

func (sub *subscriber) close() { sub.once.Do(func() { close(sub.closed) }) }

// NewHub returns a Hub that allows maxPerAccount streams per account and
// maxTotal in all.
func NewHub(maxPerAccount, maxTotal int) *Hub {
	return &Hub{maxPerAccount: maxPerAccount, maxTotal: maxTotal, byAccount: map[int64]map[*subscriber]struct{}{}}
}

// Subscribe registers a stream of the session with tokenHash. Number one of
// the sequence is reserved for the snapshot the handler sends first, so a
// delta published before it was written still comes after it.
func (h *Hub) Subscribe(account int64, tokenHash []byte) (*subscriber, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total >= h.maxTotal || len(h.byAccount[account]) >= h.maxPerAccount {
		return nil, ErrTooManyStreams
	}
	sub := &subscriber{
		account:   account,
		tokenHash: tokenHash,
		events:    make(chan streamEvent, streamQueue),
		seq:       1,
		closed:    make(chan struct{}),
	}
	if h.byAccount[account] == nil {
		h.byAccount[account] = map[*subscriber]struct{}{}
	}
	h.byAccount[account][sub] = struct{}{}
	h.total++
	return sub, nil
}

// Unsubscribe removes a stream when it ends.
func (h *Hub) Unsubscribe(sub *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.byAccount[sub.account]
	if _, ok := subs[sub]; !ok {
		return
	}
	delete(subs, sub)
	if len(subs) == 0 {
		delete(h.byAccount, sub.account)
	}
	h.total--
}

// Publish sends one delta to every stream of the account.
func (h *Hub) Publish(account int64, kind string, d game.Delta) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.byAccount[account] {
		h.deliver(sub, kind, d)
	}
}

// Broadcast sends one delta to every open stream.
func (h *Hub) Broadcast(kind string, d game.Delta) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subs := range h.byAccount {
		for sub := range subs {
			h.deliver(sub, kind, d)
		}
	}
}

// deliver numbers the delta for one stream and queues it. The caller holds
// the lock.
//
// A queue with no room spends the number without delivering anything: the
// next delta the browser does receive is out of sequence, which is how it
// learns that what it holds is no longer the server's state.
func (h *Hub) deliver(sub *subscriber, kind string, d game.Delta) {
	sub.seq++
	select {
	case sub.events <- streamEvent{kind: kind, delta: d.WithSeq(sub.seq)}:
	default:
	}
}

// CloseSession ends the streams of one session.
func (h *Hub) CloseSession(tokenHash []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subs := range h.byAccount {
		for sub := range subs {
			if bytes.Equal(sub.tokenHash, tokenHash) {
				sub.close()
			}
		}
	}
}

// CloseAccount ends every stream of an account.
func (h *Hub) CloseAccount(account int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.byAccount[account] {
		sub.close()
	}
}

// CloseAll ends every stream, for a shutdown.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subs := range h.byAccount {
		for sub := range subs {
			sub.close()
		}
	}
}
