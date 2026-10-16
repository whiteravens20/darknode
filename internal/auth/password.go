// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// Package auth holds the building blocks of sign-up and sign-in: password
// hashing, the registration proof-of-work, rate limits, client addresses and
// the random codes and tokens the server hands out.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are the cost settings of the password hash.
type Argon2Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// DefaultArgon2 costs 64 MiB of memory and three passes per hash.
var DefaultArgon2 = Argon2Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 1}

const (
	saltLen = 16
	keyLen  = 32
)

// Hasher hashes and checks passwords. It runs at most a fixed number of
// hashes at once, because each one holds its memory cost until it finishes.
type Hasher struct {
	params Argon2Params
	slots  chan struct{}
	dummy  string
}

// NewHasher returns a Hasher that runs at most concurrent hashes at a time.
func NewHasher(params Argon2Params, concurrent int) *Hasher {
	h := &Hasher{params: params, slots: make(chan struct{}, concurrent)}
	// A hash of a random password, checked when a sign-in names an unknown
	// account, so that answer takes as long as a wrong password.
	h.dummy = h.encode(make([]byte, saltLen), argon2.IDKey([]byte(rand.Text()), make([]byte, saltLen), params.Time, params.MemoryKiB, params.Threads, keyLen), params)
	return h
}

// Hash returns the encoded hash of password.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, keyLen)
	return h.encode(salt, key, h.params), nil
}

// Verify reports whether password matches the encoded hash, and whether the
// hash was made with other costs than the current ones and should be redone.
func (h *Hasher) Verify(ctx context.Context, encoded, password string) (ok, rehash bool, err error) {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return false, false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer h.release()
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, p != h.params, nil
}

// VerifyNothing spends the time of one password check and always fails. It
// answers a sign-in for an account that does not exist.
func (h *Hasher) VerifyNothing(ctx context.Context, password string) {
	_, _, _ = h.Verify(ctx, h.dummy, password)
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.slots }

func (h *Hasher) encode(salt, key []byte, p Argon2Params) string {
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

var errBadHash = errors.New("not an argon2id hash of this server")

func decode(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, errBadHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return Argon2Params{}, nil, nil, errBadHash
	}
	// Bounds keep a damaged row from making one check cost gigabytes.
	if p.MemoryKiB < 8*1024 || p.MemoryKiB > 256*1024 || p.Time < 1 || p.Time > 10 || p.Threads < 1 || p.Threads > 4 {
		return Argon2Params{}, nil, nil, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < saltLen {
		return Argon2Params{}, nil, nil, errBadHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < keyLen {
		return Argon2Params{}, nil, nil, errBadHash
	}
	return p, salt, key, nil
}
