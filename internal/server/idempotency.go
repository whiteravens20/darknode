// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"sync"
	"time"
)

// Commands are idempotent through an id the browser makes up per command.
// The state stream is one-way, so a browser whose answer never arrived cannot
// tell whether the command ran; without this, a retried `run new` would read
// as a second run and come back as an error for something that had in fact
// worked.
const (
	// commandIDTTL is how long a command's answer is remembered. A retry
	// after a dropped connection comes within seconds; a player who repeats
	// the same line much later means it.
	commandIDTTL = 5 * time.Minute
	// commandIDPerAccount is how many answers one account's log holds. The
	// oldest goes when a new one does not fit, so the log cannot grow with
	// the number of commands a player types.
	commandIDPerAccount = 16
	// maxCommandIDLength is the longest id accepted. A browser sends a UUID.
	maxCommandIDLength = 64
)

// commandState is what the log knows about an id.
type commandState int

const (
	// commandFresh means the id is new and the command may run.
	commandFresh commandState = iota
	// commandRunning means the same id is being run right now.
	commandRunning
	// commandDone means the command already ran, and its answer is returned
	// instead of running it again.
	commandDone
)

// commandLog remembers each command an account ran under an id, so the same
// id never runs twice. It lives in memory only: a restart forgets the
// answers, which costs a retry that straddles it the chance to be recognised
// and nothing else.
type commandLog struct {
	mu  sync.Mutex
	log map[int64][]commandEntry
}

type commandEntry struct {
	id   string
	at   time.Time
	done bool
	// answer is what the command replied, kept so a retry gets the same
	// words the first attempt did.
	answer commandResponse
}

func newCommandLog() *commandLog {
	return &commandLog{log: map[int64][]commandEntry{}}
}

// reserve claims the id for the account. A fresh id is written down as
// running and the caller runs the command; otherwise the state says what is
// already happening to it, with the earlier answer when there is one.
func (l *commandLog) reserve(account int64, id string, now time.Time) (commandResponse, commandState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.recent(account, now)
	for _, e := range entries {
		if e.id != id {
			continue
		}
		if e.done {
			return e.answer, commandDone
		}
		return commandResponse{}, commandRunning
	}
	if len(entries) >= commandIDPerAccount {
		entries = entries[len(entries)-commandIDPerAccount+1:]
	}
	l.log[account] = append(entries, commandEntry{id: id, at: now})
	return commandResponse{}, commandFresh
}

// complete records what the command answered.
func (l *commandLog) complete(account int64, id string, answer commandResponse) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.log[account]
	for i := range entries {
		if entries[i].id == id {
			entries[i].done = true
			entries[i].answer = answer
			return
		}
	}
}

// release forgets a reservation whose command could not be run, so the
// browser may try it again.
func (l *commandLog) release(account int64, id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.log[account]
	for i := range entries {
		if entries[i].id == id {
			l.log[account] = append(entries[:i], entries[i+1:]...)
			if len(l.log[account]) == 0 {
				delete(l.log, account)
			}
			return
		}
	}
}

// Prune forgets the accounts whose answers have all expired.
func (l *commandLog) Prune(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for account := range l.log {
		l.recent(account, now)
	}
}

// recent drops the account's expired entries and returns the rest. The caller
// holds the lock.
func (l *commandLog) recent(account int64, now time.Time) []commandEntry {
	entries := l.log[account]
	cut := 0
	for cut < len(entries) && now.Sub(entries[cut].at) >= commandIDTTL {
		cut++
	}
	entries = entries[cut:]
	if len(entries) == 0 {
		delete(l.log, account)
		return nil
	}
	l.log[account] = entries
	return entries
}

// validCommandID reports whether an id is one a browser could have made: a
// short run of printable ASCII, which a UUID is.
func validCommandID(id string) bool {
	if id == "" || len(id) > maxCommandIDLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c <= ' ' || c > '~' {
			return false
		}
	}
	return true
}
