// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"bytes"
	"errors"
	"sync"
)

// ErrTooManyStreams is returned when an account or the server has as many
// open streams as it allows.
var ErrTooManyStreams = errors.New("too many open streams")

// Hub tracks the open state streams, so a change can reach every browser of
// an account and a sign-out can close its streams.
type Hub struct {
	maxPerAccount, maxTotal int

	mu        sync.Mutex
	byAccount map[int64]map[*subscriber]struct{}
	total     int
}

type subscriber struct {
	account   int64
	tokenHash []byte
	// changed is signalled when the account's state changed; one pending
	// signal is enough, so it has room for one.
	changed chan struct{}
	// closed is closed when the stream must end.
	closed chan struct{}
	once   sync.Once
}

func (sub *subscriber) close() { sub.once.Do(func() { close(sub.closed) }) }

// NewHub returns a Hub that allows maxPerAccount streams per account and
// maxTotal in all.
func NewHub(maxPerAccount, maxTotal int) *Hub {
	return &Hub{maxPerAccount: maxPerAccount, maxTotal: maxTotal, byAccount: map[int64]map[*subscriber]struct{}{}}
}

// Subscribe registers a stream of the session with tokenHash.
func (h *Hub) Subscribe(account int64, tokenHash []byte) (*subscriber, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total >= h.maxTotal || len(h.byAccount[account]) >= h.maxPerAccount {
		return nil, ErrTooManyStreams
	}
	sub := &subscriber{
		account:   account,
		tokenHash: tokenHash,
		changed:   make(chan struct{}, 1),
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

// Notify tells every stream of the account that its state changed.
func (h *Hub) Notify(account int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.byAccount[account] {
		select {
		case sub.changed <- struct{}{}:
		default: // a signal is already pending
		}
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

// Online returns the accounts with at least one open stream.
func (h *Hub) Online() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]int64, 0, len(h.byAccount))
	for account := range h.byAccount {
		out = append(out, account)
	}
	return out
}
