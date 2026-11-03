// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/store/storetest"
)

// published is one delta a test publisher was handed.
type published struct {
	account int64
	kind    string
	delta   Delta
}

// recorder is a Publisher that keeps what it was given.
type recorder struct {
	mu   sync.Mutex
	sent []published
}

func (r *recorder) Publish(account int64, kind string, d Delta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, published{account: account, kind: kind, delta: d})
}

func (r *recorder) Broadcast(kind string, d Delta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, published{kind: kind, delta: d})
}

func (r *recorder) all() []published {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]published(nil), r.sent...)
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// engineEnv returns an Env whose clock the test moves by hand, with an
// account for each name given and a run started for it.
func engineEnv(t *testing.T, names ...string) (*Env, *time.Time, []int64) {
	t.Helper()
	s := storetest.Open(t)
	ctx := context.Background()
	launched := time.Date(2049, 6, 1, 12, 0, 0, 0, time.UTC)
	now := launched
	env := &Env{
		Store: s,
		World: testWorld(t),
		Clock: Clock{Epoch: time.Date(2049, 1, 1, 0, 0, 0, 0, time.UTC), Launched: launched},
		Now:   func() time.Time { return now },
	}
	var accounts []int64
	for _, name := range names {
		a, err := s.CreateAccount(ctx, store.NewAccount{
			Username: name, PasswordHash: "x", SessionHash: []byte(name),
			SessionStarted: now, SessionExpires: now.Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := env.StartRun(ctx, Player{AccountID: a.ID, Username: a.Username}); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, a.ID)
	}
	return env, &now, accounts
}

// exposure reads the online time written down for an account's run.
func exposure(t *testing.T, env *Env, account int64) int64 {
	t.Helper()
	r, err := env.Store.ActiveRun(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatalf("account %d has no active run", account)
	}
	return r.ExposureSeconds
}

// TestOnlineTimeIsCountedOnlyWhileAStreamIsOpen pins the rule the game is
// built on: the clock a run is judged by runs while the player is watching and
// stops when the last window closes. It is also what the old five-second ticker got
// wrong: it gave everyone online at the tick a full five seconds, whether
// they had been there for five or for one.
func TestOnlineTimeIsCountedOnlyWhileAStreamIsOpen(t *testing.T) {
	env, _, accounts := engineEnv(t, "watcher")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()

	// Not online yet: time passes and nothing is counted.
	for range 20 {
		e.step(Tick, env.Now())
	}
	e.flush(ctx)
	if got := exposure(t, env, accounts[0]); got != 0 {
		t.Fatalf("online time counted before a stream was open: %d", got)
	}

	e.Join(accounts[0])
	if e.Online() != 1 {
		t.Fatalf("Online() = %d, want 1", e.Online())
	}
	for range 25 { // two and a half seconds
		e.step(Tick, env.Now())
	}
	e.flush(ctx)
	if got := exposure(t, env, accounts[0]); got != 2 {
		t.Fatalf("online time = %d, want 2 whole seconds of two and a half", got)
	}

	// The stream closes; the half second that had not become a whole one is
	// dropped, and nothing is counted afterwards.
	e.Leave(accounts[0])
	if e.Online() != 0 {
		t.Fatalf("Online() = %d after the stream closed, want 0", e.Online())
	}
	for range 30 {
		e.step(Tick, env.Now())
	}
	e.flush(ctx)
	if got := exposure(t, env, accounts[0]); got != 2 {
		t.Fatalf("online time = %d after the stream closed, want it to have stopped at 2", got)
	}
}

// TestEachRunIsCountedForItsOwnTime is why the loop groups what it owes
// instead of adding the same number to everyone online: two players who
// joined at different moments have been online for different lengths of time.
func TestEachRunIsCountedForItsOwnTime(t *testing.T) {
	env, _, accounts := engineEnv(t, "early", "late")
	e := NewEngine(env, &recorder{}, quietLogger())

	e.Join(accounts[0])
	for range 30 { // three seconds with one player online
		e.step(Tick, env.Now())
	}
	e.Join(accounts[1])
	for range 10 { // one more second, now with both
		e.step(Tick, env.Now())
	}
	e.flush(context.Background())

	if got := exposure(t, env, accounts[0]); got != 4 {
		t.Errorf("the player who joined first has %d seconds, want 4", got)
	}
	if got := exposure(t, env, accounts[1]); got != 1 {
		t.Errorf("the player who joined later has %d seconds, want 1", got)
	}
}

// TestTheWorldClockIsBroadcastWhenItsMinuteChanges checks the one thing the
// loop sends to everybody: the shared clock. It goes out when the minute
// changes, not on every tick, and only when somebody is there to read it.
func TestTheWorldClockIsBroadcastWhenItsMinuteChanges(t *testing.T) {
	env, now, accounts := engineEnv(t, "watcher")
	out := &recorder{}
	e := NewEngine(env, out, quietLogger())
	e.Join(accounts[0])
	// Run() would do this; the test drives the steps itself.
	e.worldMinute = env.Clock.Now(env.Now()).Truncate(time.Minute)

	// A world minute is one real second (WorldScale = 60), so nine ticks
	// cannot move it.
	for range 9 {
		*now = now.Add(Tick)
		e.step(Tick, env.Now())
	}
	if sent := out.all(); len(sent) != 0 {
		t.Fatalf("the clock was sent before its minute changed: %v", sent)
	}

	*now = now.Add(Tick)
	e.step(Tick, env.Now())
	sent := out.all()
	if len(sent) != 1 {
		t.Fatalf("got %d deltas when the world minute changed, want 1: %v", len(sent), sent)
	}
	if sent[0].kind != DeltaWorld || sent[0].account != 0 {
		t.Errorf("got %q for account %d, want a broadcast of %q", sent[0].kind, sent[0].account, DeltaWorld)
	}
	world, ok := sent[0].delta.(WorldDelta)
	if !ok {
		t.Fatalf("the delta is a %T, want a WorldDelta", sent[0].delta)
	}
	if want := env.Clock.Now(env.Now()).Truncate(time.Minute).UTC(); !world.WorldTime.Equal(want) {
		t.Errorf("the clock says %s, want %s", world.WorldTime, want)
	}

	// Nobody online, nothing to send, however much time passes.
	e.Leave(accounts[0])
	out.mu.Lock()
	out.sent = nil
	out.mu.Unlock()
	for range 20 {
		*now = now.Add(Tick)
		e.step(Tick, env.Now())
	}
	if sent := out.all(); len(sent) != 0 {
		t.Errorf("the clock was sent with nobody watching: %v", sent)
	}
}

// BenchmarkEngineStep answers the question the design left open: whether one
// loop can carry every player, or whether each needs a goroutine of its own. One step advances every live run and is the work the
// server does ten times a second.
func BenchmarkEngineStep(b *testing.B) {
	launched := time.Date(2049, 6, 1, 12, 0, 0, 0, time.UTC)
	now := launched
	env := &Env{
		Clock: Clock{Epoch: time.Date(2049, 1, 1, 0, 0, 0, 0, time.UTC), Launched: launched},
		Now:   func() time.Time { return now },
	}
	e := NewEngine(env, &counter{}, quietLogger())
	for account := range int64(1000) {
		e.Join(account + 1)
	}
	e.worldMinute = env.Clock.Now(now).Truncate(time.Minute)

	b.ReportAllocs()
	for b.Loop() {
		now = now.Add(Tick)
		e.step(Tick, now)
	}
}

// counter is a Publisher that counts, for the benchmark.
type counter struct{ n int }

func (c *counter) Publish(int64, string, Delta) { c.n++ }
func (c *counter) Broadcast(string, Delta)      { c.n++ }
