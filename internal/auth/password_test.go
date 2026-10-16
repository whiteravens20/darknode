// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Cheap costs keep the tests fast; the format and the logic are the same.
var testParams = Argon2Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1}

func TestHashAndVerify(t *testing.T) {
	ctx := context.Background()
	h := NewHasher(testParams, 2)
	encoded, err := h.Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=8192,t=1,p=1$") {
		t.Fatalf("unexpected encoding: %s", encoded)
	}
	ok, rehash, err := h.Verify(ctx, encoded, "correct horse battery")
	if err != nil || !ok || rehash {
		t.Fatalf("right password: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	ok, _, err = h.Verify(ctx, encoded, "correct horse batterY")
	if err != nil || ok {
		t.Fatalf("wrong password accepted: ok=%v err=%v", ok, err)
	}
}

func TestSamePasswordHashesDifferently(t *testing.T) {
	ctx := context.Background()
	h := NewHasher(testParams, 2)
	a, _ := h.Hash(ctx, "same password")
	b, _ := h.Hash(ctx, "same password")
	if a == b {
		t.Fatal("two hashes of one password are equal: the salt is not random")
	}
}

func TestRehashWhenCostsChange(t *testing.T) {
	ctx := context.Background()
	old := NewHasher(testParams, 1)
	encoded, _ := old.Hash(ctx, "a password here")
	stronger := NewHasher(Argon2Params{MemoryKiB: 16 * 1024, Time: 2, Threads: 1}, 1)
	ok, rehash, err := stronger.Verify(ctx, encoded, "a password here")
	if err != nil || !ok || !rehash {
		t.Fatalf("old hash under new costs: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
}

func TestDamagedHashesAreRefused(t *testing.T) {
	ctx := context.Background()
	h := NewHasher(testParams, 1)
	good, _ := h.Hash(ctx, "a password here")
	bad := []string{
		"",
		"plain text",
		strings.Replace(good, "argon2id", "argon2i", 1),
		strings.Replace(good, "v=19", "v=16", 1),
		strings.Replace(good, "m=8192", "m=99999999", 1), // would cost ~100 GB
		strings.Replace(good, "t=1", "t=0", 1),
		good[:len(good)-10],
	}
	for _, enc := range bad {
		if ok, _, err := h.Verify(ctx, enc, "a password here"); err == nil || ok {
			t.Errorf("accepted damaged hash %q", enc)
		}
	}
}

func TestVerifyNothingTakesTimeAndFails(t *testing.T) {
	ctx := context.Background()
	h := NewHasher(testParams, 1)
	start := time.Now()
	h.VerifyNothing(ctx, "anything")
	if time.Since(start) < time.Millisecond {
		t.Fatal("VerifyNothing returned at once; an unknown account would answer faster than a wrong password")
	}
}

func TestConcurrencyIsLimited(t *testing.T) {
	h := NewHasher(testParams, 1)
	h.slots <- struct{}{} // the only slot is taken
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "waits for a slot"); err == nil {
		t.Fatal("a hash ran although no slot was free")
	}
	<-h.slots
}
