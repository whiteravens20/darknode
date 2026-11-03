// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
)

// Tick is the simulation's pace. One ticker drives every live run: a
// goroutine per player per tick would be hundreds of wake-ups a second for
// work that takes microseconds, so the loop advances them all in one pass.
// BenchmarkEngineAdvance measures that pass.
const Tick = 100 * time.Millisecond

// liveReport is how often a browser is told where the running programs and
// the trace have got to. Ten times a second is the simulation's pace, not a
// progress bar's.
const liveReport = 500 * time.Millisecond

// finishedTool is a program that came to an end in this tick.
type finishedTool struct {
	account int64
	tool    *running
	// runID, server and source say where root was won and from which node,
	// when that is what finished.
	runID  int64
	server string
	source string
	at     time.Time
}

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

// liveRun is what the loop keeps for one player: the time counted, and the
// state of the run that exists only while they are playing.
type liveRun struct {
	// streams is how many of the account's browsers are watching. Online
	// time is counted while at least one is.
	streams int
	// part is online time of less than a second, waiting to become one.
	part time.Duration
	// owed is whole seconds counted but not yet written to the database.
	owed int64
	// route are the relays chosen for the next connection.
	route []*world.Server
	// conn is the live connection, or nil.
	conn *Connection
	// trace is the trace coming down the route, or nil. It outlives the
	// connection: an administrator cutting the player off does not stop it.
	trace *trace
	// meter says whether the run has the program that shows the trace
	// exactly, and runID is the run the live state belongs to.
	meter bool
	runID int64
	// offlineAt is when the last browser stopped watching. What is live
	// outlives it by OfflineGrace, so a reload is not a way out.
	offlineAt time.Time
}

// NewEngine returns the loop for env, publishing through out.
// The env is given a reference back to the loop, because the commands a
// player types reach the live state — the route, the connection — through it.
func NewEngine(env *Env, out Publisher, log *slog.Logger) *Engine {
	e := &Engine{env: env, out: out, log: log, live: map[int64]*liveRun{}}
	env.Engine = e
	return e
}

// Join records that one more stream of the account is open, which is what
// being online means here: the player's time starts counting.
func (e *Engine) Join(account int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.liveLocked(account)
	r.streams++
	r.offlineAt = time.Time{}
}

// Leave records that one of the account's streams closed. The part of a
// second not yet counted is dropped, which is the same rounding a restart
// does. What the player had running stays for OfflineGrace, and the loop
// clears it from there.
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
		r.offlineAt = e.env.Now()
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
			e.tick(ctx, Tick, e.env.Now())
		case <-flush.C:
			e.flush(ctx)
			// The walks after a break-in and the windows on a debt are both
			// measured in online time, which has just been written down.
			e.advanceInvestigations(ctx)
			e.advanceDebts(ctx)
		}
	}
}

// tick advances the simulation by dt and writes down the few things a tick
// produces that have to outlive it: the root a program won, and the end of a
// run the trace caught. The advancing itself is step, which touches no
// database at all.
func (e *Engine) tick(ctx context.Context, dt time.Duration, now time.Time) {
	finished, busted, dropped, copied := e.step(dt, now)
	for _, f := range finished {
		if f.tool.Layer != LayerPassword {
			continue
		}
		// Root lasts for the run, and winning it is an act the server
		// writes down like any other.
		if err := e.env.Store.GrantRoot(ctx, f.runID, f.server, f.at); err != nil && ctx.Err() == nil {
			e.log.Error("write down root", "run", f.runID, "server", f.server, "err", err)
		}
		if err := e.env.Store.AddLogs(ctx, f.runID, []store.LogEntry{{
			Server: f.server, HopIndex: 0, WorldTime: e.env.Clock.Now(f.at),
			Action: ActionRoot, SourceIP: f.source,
		}}); err != nil && ctx.Err() == nil {
			e.log.Error("write down the root in the log", "run", f.runID, "server", f.server, "err", err)
		}
	}
	for _, f := range copied {
		// The copy is the player's now, and the server writes down that it
		// was taken.
		if err := e.env.Store.CopyToGateway(ctx, f.runID, store.GatewayFile{
			Server: f.server, Path: f.path, SizeMB: f.sizeMB, CopiedAt: f.at,
		}); err != nil && ctx.Err() == nil {
			e.log.Error("write down a copied file", "run", f.runID, "path", f.path, "err", err)
			continue
		}
		if err := e.env.Store.AddLogs(ctx, f.runID, []store.LogEntry{{
			Server: f.server, HopIndex: 0, WorldTime: e.env.Clock.Now(f.at),
			Action: ActionCopy, Severity: severityOf(ActionCopy, true),
		}}); err != nil && ctx.Err() == nil {
			e.log.Error("write down a copy in the log", "run", f.runID, "err", err)
		}
		e.out.Publish(f.account, DeltaAlert, AlertDelta{
			Text: fmt.Sprintf("%s is on your gateway.", f.path), Kind: KindOK,
		})
	}
	for _, d := range dropped {
		// The player stopped watching and the grace ran out: what they left
		// in the logs is read by whoever owns them, exactly as if they had
		// typed `disconnect`.
		run, err := e.env.Store.ActiveRun(ctx, d.account)
		if err != nil || run == nil {
			if err != nil && ctx.Err() == nil {
				e.log.Error("read the run a dropped connection belonged to", "account", d.account, "err", err)
			}
			continue
		}
		if _, err := e.env.OpenInvestigation(ctx, run, d.conn.Target, d.conn.Route); err != nil && ctx.Err() == nil {
			e.log.Error("open an investigation after a dropped connection", "run", run.ID, "err", err)
		}
	}
	for _, b := range busted {
		if err := e.env.Store.EndRun(ctx, b.runID, b.at); err != nil && ctx.Err() == nil {
			e.log.Error("end a run the trace caught", "run", b.runID, "err", err)
			continue
		}
		e.log.Info("run ended: the trace reached the gateway", "account", b.account, "run", b.runID)
		// The run is gone, and the loop cannot build a state without the
		// database, so the browser is asked to take a fresh snapshot.
		e.out.Publish(b.account, DeltaResync, ResyncDelta{})
	}
}

// bustedRun is a run the trace reached the gateway of.
type bustedRun struct {
	account int64
	runID   int64
	at      time.Time
}

// copiedFile is a file that finished coming down the line in this tick.
type copiedFile struct {
	account int64
	runID   int64
	server  string
	path    string
	sizeMB  int
	at      time.Time
}

// droppedConnection is a connection the grace ran out on. It counts as a
// disconnect in every way, logs included, so whatever was left in them is
// now somebody's to read.
type droppedConnection struct {
	account int64
	conn    *Connection
}

// step advances the simulation by dt, publishes what changed, and returns the
// programs that finished and the runs the trace caught. It touches the
// database never and the clock once, so a tick costs the same whether one
// player is online or a hundred.
func (e *Engine) step(dt time.Duration, now time.Time) ([]finishedTool, []bustedRun, []droppedConnection, []copiedFile) {
	minute := e.env.Clock.Now(now).Truncate(time.Minute)

	// Deltas go out after the lock is given up: the hub takes a lock of its
	// own, and the loop must never hold two at once.
	var connected, traced []int64
	var done []finishedTool
	var busted []bustedRun
	var dropped []droppedConnection
	var copied []copiedFile
	var alerts []alert

	e.mu.Lock()
	watching := 0
	for account, r := range e.live {
		online := r.streams > 0
		if online {
			watching++
			r.part += dt
			if r.part >= time.Second {
				seconds := int64(r.part / time.Second)
				r.owed += seconds
				r.part -= time.Duration(seconds) * time.Second
			}
		} else if now.Sub(r.offlineAt) >= OfflineGrace {
			// The grace is over with nobody watching: what was live is
			// closed, exactly as a disconnect would close it. An account
			// that owes nothing and holds nothing live is forgotten.
			if r.conn != nil {
				dropped = append(dropped, droppedConnection{account: account, conn: r.conn})
			}
			r.conn, r.trace = nil, nil
			if r.owed == 0 {
				delete(e.live, account)
			}
			continue
		}
		// Through the grace the simulation runs on: closing a tab is not a
		// way out of a trace that is already coming.
		if r.conn != nil && r.conn.State == StateConnecting && now.Sub(r.conn.opened) >= Handshake {
			r.conn.State = StateConnected
			r.conn.reported = now
			connected = append(connected, account)
			continue
		}
		if r.conn != nil {
			// The programs at work share the gateway's processor. A browser
			// is told how far they have got a few times a second, and at
			// once when one of them finishes.
			if finished := r.conn.advanceTools(dt, r.conn.cpuMHz); len(finished) > 0 {
				r.conn.reported = now
				connected = append(connected, account)
				for _, tool := range finished {
					source := ""
					if n := len(r.conn.Route); n > 0 {
						source = r.conn.Route[n-1].IP.String()
					}
					done = append(done, finishedTool{
						account: account, tool: tool,
						runID: r.conn.runID, server: r.conn.Target.ID, source: source, at: now,
					})
				}
			} else if (len(r.conn.tools) > 0 || r.conn.transfer != nil) &&
				now.Sub(r.conn.reported) >= liveReport {
				r.conn.reported = now
				connected = append(connected, account)
			}
			// A file comes down the line on the modem's time, not the
			// processor's, so it is advanced by the clock alone.
			if tr := r.conn.transfer; tr != nil {
				tr.left -= dt
				if tr.left <= 0 {
					copied = append(copied, copiedFile{
						account: account, runID: r.conn.runID, server: r.conn.Target.ID,
						path: tr.path, sizeMB: tr.sizeMB, at: now,
					})
					r.conn.transfer = nil
					r.conn.reported = now
					connected = append(connected, account)
				}
			}
		}
		if r.trace == nil {
			continue
		}

		progress := r.trace.advance(dt)
		switch {
		case progress >= 1:
			// The trace is at the gateway. This run is over.
			busted = append(busted, bustedRun{account: account, runID: r.runID, at: now})
			alerts = append(alerts, alert{account, "The trace reached your gateway. This run is over.", KindError})
			r.conn, r.trace = nil, nil
			continue

		case r.trace.admin && progress >= AdminKicks && !r.trace.kicked:
			r.trace.kicked = true
			if r.conn != nil {
				alerts = append(alerts, alert{account,
					fmt.Sprintf("An administrator on %s cut your connection. The trace is still coming.",
						r.conn.Target.Name), KindError})
				r.conn = nil
				connected = append(connected, account)
			}

		case r.trace.admin && progress >= AdminClosesLayer && !r.trace.closedLayer:
			r.trace.closedLayer = true
			if r.conn != nil {
				if layer := r.conn.closeOneLayer(); layer != "" {
					alerts = append(alerts, alert{account,
						fmt.Sprintf("Somebody on %s noticed and closed the %s again.",
							r.conn.Target.Name, layer), KindError})
					connected = append(connected, account)
				}
			}
		}
		if now.Sub(r.trace.reported) >= liveReport {
			r.trace.reported = now
			traced = append(traced, account)
		}
	}
	moved := !minute.Equal(e.worldMinute)
	if moved {
		e.worldMinute = minute
	}
	e.mu.Unlock()

	for _, account := range connected {
		e.out.Publish(account, DeltaConnection, ConnectionDelta{Connection: e.ConnectionView(account)})
	}
	for _, account := range traced {
		e.out.Publish(account, DeltaTrace, TraceDelta{Trace: e.TraceView(account)})
	}
	for _, a := range alerts {
		e.out.Publish(a.account, DeltaAlert, AlertDelta{Text: a.text, Kind: a.kind})
	}
	for _, f := range done {
		e.log.Info("program finished", "account", f.account, "program", f.tool.Software, "opens", string(f.tool.Layer))
	}
	// The clock is the same for everyone, so it goes out once, to everyone.
	if moved && watching > 0 {
		e.out.Broadcast(DeltaWorld, WorldDelta{WorldTime: minute.UTC()})
	}
	return done, busted, dropped, copied
}

// alert is a line the game says of its own accord, queued until the lock is
// given up.
type alert struct {
	account int64
	text    string
	kind    string
}

// ArmTrace starts the trace against the player's connection, if one is not
// running already. Everything that counts as an act on the target arms it:
// setting an intrusion program to work, or reading what needed root.
func (e *Engine) ArmTrace(account int64, run *store.Run) {
	now := e.env.Now()

	e.mu.Lock()
	r := e.live[account]
	if r == nil || r.conn == nil || r.trace != nil {
		e.mu.Unlock()
		return
	}
	r.runID = run.ID
	r.meter = hasTool(run, traceTool)
	r.trace = newTrace(r.conn, run.Gateway.Security, run.HasRoot, now)
	view := r.trace.view(r.meter)
	target := r.conn.Target.Name
	e.mu.Unlock()

	e.out.Publish(account, DeltaTrace, TraceDelta{Trace: view})
	e.out.Publish(account, DeltaAlert, AlertDelta{
		Text: fmt.Sprintf("%s has started working out where you are.", target),
		Kind: KindError,
	})
}

// BreakTrail is the one way out of a trace that is already coming: the logs
// on a hop it has not reached yet are cleared, so the line it would have
// followed back is gone and it stops where it stands.
//
// It reports whether that is what happened. Clearing the log of the hop the
// trace already stands on, or of one it has passed, changes nothing: it has
// read those lines already.
func (e *Engine) BreakTrail(account int64, server string) bool {
	e.mu.Lock()
	r := e.live[account]
	if r == nil || r.trace == nil {
		e.mu.Unlock()
		return false
	}
	at := r.trace.at()
	broke := false
	for i, h := range r.trace.hops {
		if h.id == server && i > at {
			broke = true
			break
		}
	}
	if broke {
		r.trace = nil
	}
	e.mu.Unlock()

	if broke {
		e.out.Publish(account, DeltaTrace, TraceDelta{Trace: nil})
		e.out.Publish(account, DeltaAlert, AlertDelta{
			Text: "The trail is broken. Whoever was following it has nothing left to follow.",
			Kind: KindOK,
		})
	}
	return broke
}

// Trace returns a copy of the player's live trace, or nil.
func (e *Engine) Trace(account int64) (*trace, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.live[account]
	if r == nil || r.trace == nil {
		return nil, false
	}
	copied := *r.trace
	return &copied, r.meter
}

// TraceView is what the browser is told about the trace.
func (e *Engine) TraceView(account int64) *TraceView {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.live[account]
	if r == nil || r.trace == nil {
		return nil
	}
	return r.trace.view(r.meter)
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
// seconds they owe. Forgetting an account that has stopped playing is the
// loop's own work, in step, because only it knows what is still live.
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
	}
	return bySeconds
}
