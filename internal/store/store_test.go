// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/store/storetest"
)

func TestMigrateTwiceIsHarmless(t *testing.T) {
	s := storetest.Open(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}
}

func TestLaunchedAtIsStable(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	first, err := s.LaunchedAt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.LaunchedAt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Fatalf("launch time moved from %v to %v", first, second)
	}
}

func newAccount(name string) store.NewAccount {
	return store.NewAccount{
		Username:       name,
		PasswordHash:   "hash",
		RecoveryMACs:   [][]byte{[]byte("code-1"), []byte("code-2")},
		SessionHash:    []byte("session-" + name),
		SessionStarted: time.Now(),
		SessionExpires: time.Now().Add(time.Hour),
	}
}

func TestUsernamesAreCaseInsensitive(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	if _, err := s.CreateAccount(ctx, newAccount("Zero_Cool")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(ctx, newAccount("zero_cool")); !errors.Is(err, store.ErrUsernameTaken) {
		t.Fatalf("a name differing only in case was accepted: %v", err)
	}
	a, err := s.AccountByUsername(ctx, "ZERO_COOL")
	if err != nil || a.Username != "Zero_Cool" {
		t.Fatalf("lookup ignoring case: %+v %v", a, err)
	}
}

func TestInviteIsUsedOnce(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	if err := s.CreateInvites(ctx, [][]byte{[]byte("invite-1")}); err != nil {
		t.Fatal(err)
	}
	first := newAccount("first")
	first.InviteMAC = []byte("invite-1")
	if _, err := s.CreateAccount(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := newAccount("second")
	second.InviteMAC = []byte("invite-1")
	if _, err := s.CreateAccount(ctx, second); !errors.Is(err, store.ErrInviteInvalid) {
		t.Fatalf("a used invite was accepted: %v", err)
	}
	if _, err := s.AccountByUsername(ctx, "second"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the refused registration left an account behind: %v", err)
	}
}

func TestRecoverUsesCodeOnceAndEndsSessions(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, newAccount("player"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByToken(ctx, []byte("session-player"), time.Now()); err != nil {
		t.Fatalf("the first session was not created: %v", err)
	}
	newSession := []byte("after-recovery")
	if err := s.Recover(ctx, a.ID, []byte("code-1"), "new-hash", newSession, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByToken(ctx, []byte("session-player"), time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the old session survived the recovery: %v", err)
	}
	if _, err := s.SessionByToken(ctx, newSession, time.Now()); err != nil {
		t.Fatalf("the new session is missing: %v", err)
	}
	if err := s.Recover(ctx, a.ID, []byte("code-1"), "again", []byte("x"), time.Now(), time.Now().Add(time.Hour)); !errors.Is(err, store.ErrCodeInvalid) {
		t.Fatalf("a used code was accepted again: %v", err)
	}
	got, _ := s.AccountByUsername(ctx, "player")
	if got.PasswordHash != "new-hash" {
		t.Fatalf("password hash = %q", got.PasswordHash)
	}
	if left, _ := s.RecoveryCodesLeft(ctx, a.ID); left != 1 {
		t.Fatalf("codes left = %d, want 1", left)
	}
}

func TestExpiredSessionIsNotFound(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, newAccount("player"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, a.ID, []byte("old"), time.Now().Add(-time.Hour), time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByToken(ctx, []byte("old"), time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an expired session was returned: %v", err)
	}
	n, err := s.DeleteExpiredSessions(ctx, time.Now())
	if err != nil || n != 1 {
		t.Fatalf("deleted %d expired sessions, err %v", n, err)
	}
}

// TestSessionAgeFollowsTheServerClock pins what the store must not do: decide
// a session's age by the database's own clock. The dates here are far from
// today's on purpose, so a return to now() fails the test on any day.
func TestSessionAgeFollowsTheServerClock(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, newAccount("player"))
	if err != nil {
		t.Fatal(err)
	}
	long := time.Date(2001, 1, 1, 12, 0, 0, 0, time.UTC)
	token := []byte("by-the-clock")
	if err := s.CreateSession(ctx, a.ID, token, long, long.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByToken(ctx, token, long.Add(time.Minute)); err != nil {
		t.Fatalf("a session the clock says is current was not found: %v", err)
	}
	if _, err := s.SessionByToken(ctx, token, long.Add(2*time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a session past its expiry was returned: %v", err)
	}

	// Being used moves the expiry, and records when, by the same clock.
	if err := s.TouchSession(ctx, token, long.Add(time.Minute), long.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	sess, err := s.SessionByToken(ctx, token, long.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("the session was not found after being used: %v", err)
	}
	if !sess.CreatedAt.Equal(long) || !sess.LastSeenAt.Equal(long.Add(time.Minute)) {
		t.Fatalf("created %v, last seen %v", sess.CreatedAt, sess.LastSeenAt)
	}

	// So does the sweeper.
	if n, err := s.DeleteExpiredSessions(ctx, long.Add(2*time.Hour)); err != nil || n != 0 {
		t.Fatalf("deleted %d sessions that had not expired, err %v", n, err)
	}
	if n, err := s.DeleteExpiredSessions(ctx, long.Add(4*time.Hour)); err != nil || n != 1 {
		t.Fatalf("deleted %d expired sessions, err %v", n, err)
	}
}

func TestOneActiveRunPerAccount(t *testing.T) {
	s := storetest.Open(t)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, newAccount("player"))
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.ActiveRun(ctx, a.ID); err != nil || r != nil {
		t.Fatalf("a new account has a run: %+v %v", r, err)
	}
	n := store.NewRun{
		AccountID: a.ID,
		Gateway:   store.Gateway{CPUMHz: 2000, RAMMB: 4096, ModemMbps: 100, StorageMB: 256000, NICCount: 2, Security: 1},
		Bank:      "home-bank",
		Balance:   100,
		Software:  []store.Installed{{Software: "pass_breaker", Version: 1}},
		Known:     []string{"home-bank", "test-1"},
	}
	if _, err := s.CreateRun(ctx, n); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(ctx, n); !errors.Is(err, store.ErrRunActive) {
		t.Fatalf("a second active run was created: %v", err)
	}
	r, err := s.ActiveRun(ctx, a.ID)
	if err != nil || r == nil {
		t.Fatalf("active run: %+v %v", r, err)
	}
	if r.Gateway.CPUMHz != 2000 || len(r.Accounts) != 1 || r.Accounts[0].Clean != 100 || !r.Accounts[0].Primary {
		t.Fatalf("run state: %+v", r)
	}
	if len(r.Software) != 1 || len(r.Known) != 2 || r.Known[0].Server != "home-bank" {
		t.Fatalf("software %v, known %v", r.Software, r.Known)
	}
	if r.Known[0].Probed {
		t.Error("a server the run has only heard of is marked as probed")
	}
	if !r.Knows("test-1") || r.Knows("never-heard-of") {
		t.Error("Knows does not answer for the addresses the run has")
	}

	// Probing is what makes a server's type and defences visible, so the flag
	// has to survive the round trip; an address the run does not have cannot
	// be probed at all.
	switch probed, err := s.MarkProbed(ctx, r.ID, "test-1"); {
	case err != nil:
		t.Fatal(err)
	case !probed:
		t.Fatal("MarkProbed did not find a server the run knows")
	}
	if probed, err := s.MarkProbed(ctx, r.ID, "never-heard-of"); err != nil {
		t.Fatal(err)
	} else if probed {
		t.Error("MarkProbed accepted a server the run does not know")
	}
	r, err = s.ActiveRun(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range r.Known {
		if (k.Server == "test-1") != k.Probed {
			t.Errorf("after probing test-1, %s has probed=%v", k.Server, k.Probed)
		}
	}
	if err := s.AddExposure(ctx, []int64{a.ID}, 5); err != nil {
		t.Fatal(err)
	}
	r, _ = s.ActiveRun(ctx, a.ID)
	if r.ExposureSeconds != 5 {
		t.Fatalf("exposure = %d, want 5", r.ExposureSeconds)
	}
}
