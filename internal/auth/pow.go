// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/bits"
	"strconv"
	"sync"
	"time"
)

// Errors of the registration proof-of-work.
var (
	ErrPuzzleBusy      = errors.New("too many open challenges; try again shortly")
	ErrChallengeAbsent = errors.New("unknown or used challenge")
	ErrChallengeOld    = errors.New("challenge expired")
	ErrSolvedTooFast   = errors.New("form sent too quickly")
	ErrWrongSolution   = errors.New("wrong solution")
)

// PuzzleConfig sets the cost of the proof-of-work.
type PuzzleConfig struct {
	// BaseBits is the difficulty a quiet network gets: the number of leading
	// zero bits the hash must have. Each bit doubles the expected work.
	BaseBits int
	// MaxBits caps the difficulty however busy a network is.
	MaxBits int
	// TTL is how long a challenge stays valid.
	TTL time.Duration
	// MinAge is the earliest a solution is accepted after the challenge was
	// issued. A person needs a moment to fill in the form; a script does not.
	MinAge time.Duration
	// MaxOpen caps the challenges outstanding in total, MaxOpenPerKey those
	// of one network, so that asking for challenges cannot fill the memory.
	MaxOpen, MaxOpenPerKey int
}

// DefaultPuzzle returns the standard settings with the given base difficulty.
func DefaultPuzzle(baseBits int) PuzzleConfig {
	return PuzzleConfig{
		BaseBits: baseBits, MaxBits: min(baseBits+6, 30),
		TTL: 10 * time.Minute, MinAge: 2 * time.Second,
		MaxOpen: 10000, MaxOpenPerKey: 20,
	}
}

// Challenge is what a client gets: an ID to hash and the difficulty.
type Challenge struct {
	ID      string
	Bits    int
	Expires time.Time
}

type openChallenge struct {
	key    string
	bits   int
	issued time.Time
}

// Puzzle issues and checks the proof-of-work in front of registration.
type Puzzle struct {
	cfg    PuzzleConfig
	recent *Window

	mu      sync.Mutex
	open    map[string]openChallenge
	openFor map[string]int
}

// NewPuzzle returns a Puzzle with the given settings.
func NewPuzzle(cfg PuzzleConfig) *Puzzle {
	return &Puzzle{
		cfg:     cfg,
		recent:  NewWindow(1000, 10*time.Minute),
		open:    map[string]openChallenge{},
		openFor: map[string]int{},
	}
}

// Issue hands out a challenge to the network key. The more challenges that
// network asked for in the last ten minutes, the harder the new one: one bit
// more for every five.
func (p *Puzzle) Issue(key string, now time.Time) (Challenge, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.open) >= p.cfg.MaxOpen {
		p.pruneLocked(now)
		if len(p.open) >= p.cfg.MaxOpen {
			return Challenge{}, ErrPuzzleBusy
		}
	}
	if p.openFor[key] >= p.cfg.MaxOpenPerKey {
		return Challenge{}, ErrPuzzleBusy
	}

	n := p.recent.Count(key, now)
	p.recent.Add(key, now)
	difficulty := min(p.cfg.BaseBits+n/5, p.cfg.MaxBits)

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return Challenge{}, err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	p.open[id] = openChallenge{key: key, bits: difficulty, issued: now}
	p.openFor[key]++
	return Challenge{ID: id, Bits: difficulty, Expires: now.Add(p.cfg.TTL)}, nil
}

// Check accepts a solution once: the challenge is used up whatever the outcome.
func (p *Puzzle) Check(id, solution string, now time.Time) error {
	p.mu.Lock()
	c, ok := p.open[id]
	if ok {
		p.forgetLocked(id, c)
	}
	p.mu.Unlock()

	switch {
	case !ok:
		return ErrChallengeAbsent
	case now.Sub(c.issued) >= p.cfg.TTL:
		return ErrChallengeOld
	case now.Sub(c.issued) < p.cfg.MinAge:
		return ErrSolvedTooFast
	case !validSolution(solution) || LeadingZeroBits(digest(id, solution)) < c.bits:
		return ErrWrongSolution
	}
	return nil
}

// Prune forgets expired challenges.
func (p *Puzzle) Prune(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked(now)
	p.recent.Prune(now)
}

func (p *Puzzle) pruneLocked(now time.Time) {
	for id, c := range p.open {
		if now.Sub(c.issued) >= p.cfg.TTL {
			p.forgetLocked(id, c)
		}
	}
}

func (p *Puzzle) forgetLocked(id string, c openChallenge) {
	delete(p.open, id)
	if p.openFor[c.key]--; p.openFor[c.key] <= 0 {
		delete(p.openFor, c.key)
	}
}

// A solution is a decimal counter of at most 20 digits.
func validSolution(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func digest(id, solution string) []byte {
	sum := sha256.Sum256([]byte(id + ":" + solution))
	return sum[:]
}

// LeadingZeroBits counts the zero bits at the start of b.
func LeadingZeroBits(b []byte) int {
	n := 0
	for _, x := range b {
		if x != 0 {
			return n + bits.LeadingZeros8(x)
		}
		n += 8
	}
	return n
}

// Solve finds a solution by counting up from zero, as the browser does. The
// server never calls it; tests and tools do.
func Solve(id string, difficulty int) string {
	for i := uint64(0); ; i++ {
		s := strconv.FormatUint(i, 10)
		if LeadingZeroBits(digest(id, s)) >= difficulty {
			return s
		}
	}
}
