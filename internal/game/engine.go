// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Tick is the simulation's pace. One ticker drives every live run: a
// goroutine per player per tick would be hundreds of wake-ups a second for
// work that takes microseconds, so the loop advances them all in one pass.
// BenchmarkEngineAdvance measures that pass.
const Tick = 100 * time.Millisecond

// exposureFlush is how often the online time counted in memory is written to
// the database. In between, the loop spares the database a write per player
// per tick; a crash or a restart loses at most this much of a player's time.
const exposureFlush = 5 * time.Second

// Publisher is how the simulation reaches the browsers that are watching. The
// HTTP side implements it; the game knows nothing about streams.
type Publisher interface {
	// Publish sends one delta to every stream of one account.
	Publish(account int64, kind string, d Delta)
	// Broadcast sends one delta to every open stream.
	Broadcast(kind string, d Delta)
}

// Engine is the simulation loop: the one place where time passes. Everything
// that happens by itself while a player is online is advanced here, from the
// clock the whole world shares to the online time a run is judged by, and
// later the trace and the running tools.
//
// It takes the time from Env.Now, never from the database: one clock decides,
// and the same clock is the one the tests set.
type Engine struct {
	env *Env
	out Publisher
	log *slog.Logger

	mu   sync.Mutex
	live map[int64]*liveRun
	// worldMinute is the world minute last broadcast, so the clock is sent
	// when it changes and not a dozen times a second.
	worldMinute time.Time
}

// liveRun is what the loop keeps for one player who is online.
type liveRun struct {
	// streams is how many of the account's browsers are watching. Online
	// time is counted while at least one is.
	streams int
	// part is online time of less than a second, waiting to become one.
	part time.Duration
	// owed is whole seconds counted but not yet written to the database.
	owed int64
}

// NewEngine returns the loop for env, publishing through out.
func NewEngine(env *Env, out Publisher, log *slog.Logger) *Engine {
	return &Engine{env: env, out: out, log: log, live: map[int64]*liveRun{}}
}

// Join records that one more stream of the account is open, which is what
// being online means (§2.4 of the design): the player's time starts counting.
func (e *Engine) Join(account int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.live[account]
	if r == nil {
		r = &liveRun{}
		e.live[account] = r
	}
	r.streams++
}

// Leave records that one of the account's streams closed. The part of a
// second not yet counted is dropped, which is the same rounding a restart
// does.
func (e *Engine) Leave(account int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.live[account]
	if r == nil {
		return
	}
	if r.streams > 0 {
		r.streams--
	}
	if r.streams == 0 {
		r.part = 0
		if r.owed == 0 {
			delete(e.live, account)
		}
	}
}

// Online reports how many accounts are being counted as online.
func (e *Engine) Online() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, r := range e.live {
		if r.streams > 0 {
			n++
		}
	}
	return n
}

// Run advances the simulation until ctx ends, then writes down what it
// counted. It is the only goroutine that ticks.
func (e *Engine) Run(ctx context.Context) {
	e.mu.Lock()
	e.worldMinute = e.env.Clock.Now(e.env.Now()).Truncate(time.Minute)
	e.mu.Unlock()

	ticker := time.NewTicker(Tick)
	defer ticker.Stop()
	flush := time.NewTicker(exposureFlush)
	defer flush.Stop()

	for {
		select {
		case <-ctx.Done():
			// The time already counted is worth writing down even though the
			// context that would have carried the query is finished.
			write, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			e.flush(write)
			cancel()
			return
		case <-ticker.C:
			e.step(Tick, e.env.Now())
		case <-flush.C:
			e.flush(ctx)
		}
	}
}

// step advances the simulation by dt and publishes what changed. It touches
// the database never and the clock once, so a tick costs the same whether one
// player is online or a hundred.
func (e *Engine) step(dt time.Duration, now time.Time) {
	worldTime := e.env.Clock.Now(now)
	minute := worldTime.Truncate(time.Minute)

	e.mu.Lock()
	// An account keeps its entry after its last stream closed until the
	// seconds it owes are written down, so what counts as online here is a
	// stream, never the entry.
	watching := 0
	for _, r := range e.live {
		if r.streams == 0 {
			continue
		}
		watching++
		r.part += dt
		if r.part >= time.Second {
			seconds := int64(r.part / time.Second)
			r.owed += seconds
			r.part -= time.Duration(seconds) * time.Second
		}
	}
	moved := !minute.Equal(e.worldMinute)
	if moved {
		e.worldMinute = minute
	}
	e.mu.Unlock()

	// The clock is the same for everyone, so it goes out once, to everyone.
	if moved && watching > 0 {
		e.out.Broadcast(DeltaWorld, WorldDelta{WorldTime: minute.UTC()})
	}
}

// flush writes the counted online time to the database. Accounts that owe the
// same number of seconds are updated in one statement, which is the usual
// case: everyone online for the whole window owes the same.
//
// A failed write loses the seconds it carried, deliberately: they are worth
// less than a loop that grows a backlog it can never clear.
func (e *Engine) flush(ctx context.Context) {
	for seconds, accounts := range e.takeOwed() {
		if err := e.env.Store.AddExposure(ctx, accounts, seconds); err != nil && ctx.Err() == nil {
			e.log.Error("count online time", "err", err, "accounts", len(accounts), "seconds", seconds)
		}
	}
}

// takeOwed empties the counters and returns the accounts grouped by the
// seconds they owe. Accounts that owe nothing and have no stream left are
// forgotten here.
func (e *Engine) takeOwed() map[int64][]int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	var bySeconds map[int64][]int64
	for account, r := range e.live {
		if r.owed > 0 {
			if bySeconds == nil {
				bySeconds = map[int64][]int64{}
			}
			bySeconds[r.owed] = append(bySeconds[r.owed], account)
			r.owed = 0
		}
		if r.streams == 0 {
			delete(e.live, account)
		}
	}
	return bySeconds
}
