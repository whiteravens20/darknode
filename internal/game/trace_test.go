// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/whiteravens20/darknode/internal/world"
)

// server builds a target or a relay for the formula tests.
func server(name string, kind world.ServerType, security int, proxy, firewall, monitor bool) *world.Server {
	return &world.Server{
		ID: name, Name: name, Security: security,
		Proxy: proxy, Firewall: firewall, Monitor: monitor,
		Org: &world.Org{ID: name, Name: name, Type: kind},
	}
}

// relays returns n ISP relays, which is what the balance tables bounce
// through.
func relays(n int) []*world.Server {
	var out []*world.Server
	for range n {
		out = append(out, server("relay", world.TypeISP, 2, false, false, false))
	}
	return out
}

// traceAgainst builds the trace the balance tables describe: a target, a
// number of relays, a gateway security, and whether the monitor was masked
// before the trace started.
func traceAgainst(target *world.Server, bounces, gatewaySecurity int, spoofed bool) *trace {
	conn := &Connection{Target: target, Route: relays(bounces), open: map[Layer]bool{}}
	if spoofed {
		conn.open[LayerMonitor] = true
	}
	return newTrace(conn, gatewaySecurity, func(string) bool { return false }, time.Time{})
}

// TestTraceCostAndTimeMatchTheBalanceTables pins every row of the three
// reference servers. These numbers decide whether the game is playable: the
// time a break-in takes was weighed against exactly these, and the
// administrator's deadline at seventy per cent of them.
func TestTraceCostAndTimeMatchTheBalanceTables(t *testing.T) {
	arc := server("ARC", world.TypeCorporate, 4, false, true, true)
	gcu := server("Global Credit Union", world.TypeBank, 6, true, true, true)
	federal := server("Federal Administration", world.TypeGovernment, 8, true, true, true)

	for _, c := range []struct {
		what                     string
		target                   *world.Server
		bounces, gatewaySecurity int
		spoofed                  bool
		cost, rate, traceSeconds float64
	}{
		// Security 4, a firewall and a monitor.
		{"ARC, start, no bounce, no mask", arc, 0, 1, false, 21, 1.164, 18.0},
		{"ARC, start, no bounce, masked", arc, 0, 1, true, 21, 0.727, 28.9},
		{"ARC, start, two bounces, no mask", arc, 2, 1, false, 61, 1.164, 52.4},
		{"ARC, upgraded, no bounce, no mask", arc, 0, 5, false, 33, 1.164, 28.4},
		// Security 6, proxy, firewall and monitor.
		{"GCU, start, no bounce, no mask", gcu, 0, 1, false, 21, 1.745, 12.0},
		{"GCU, start, two bounces, no mask", gcu, 2, 1, false, 61, 1.745, 34.9},
		{"GCU, start, two bounces, masked", gcu, 2, 1, true, 61, 1.091, 55.9},
		{"GCU, upgraded, two bounces, no mask", gcu, 2, 5, false, 73, 1.745, 41.8},
		{"GCU, upgraded, two bounces, masked", gcu, 2, 5, true, 73, 1.091, 66.9},
		// Security 8, the hardest target of the catalogue.
		{"Federal, start, two bounces, masked", federal, 2, 1, true, 61, 1.455, 41.9},
		{"Federal, upgraded, no bounce, masked", federal, 0, 5, true, 33, 1.455, 22.7},
		{"Federal, upgraded, two bounces, masked", federal, 2, 5, true, 73, 1.455, 50.2},
		{"Federal, upgraded, four bounces, masked", federal, 4, 5, true, 113, 1.455, 77.7},
		{"Federal, upgraded, four bounces, no mask", federal, 4, 5, false, 113, 2.327, 48.6},
	} {
		tr := traceAgainst(c.target, c.bounces, c.gatewaySecurity, c.spoofed)
		if math.Abs(tr.total-c.cost) > 0.01 {
			t.Errorf("%s: cost %.2f trace-s, want %.2f", c.what, tr.total, c.cost)
		}
		if math.Abs(tr.rate-c.rate) > 0.005 {
			t.Errorf("%s: rate %.3f, want %.3f", c.what, tr.rate, c.rate)
		}
		if got := tr.TraceTime().Seconds(); math.Abs(got-c.traceSeconds) > 0.1 {
			t.Errorf("%s: %.1f s to the gateway, want %.1f s", c.what, got, c.traceSeconds)
		}
	}
}

// TestARootedRelayHoldsTheTraceBackLonger is the reason to work a route out
// in advance: a hop the player owns costs the trace half again as much.
func TestARootedRelayHoldsTheTraceBackLonger(t *testing.T) {
	target := server("ARC", world.TypeCorporate, 4, false, true, true)
	conn := &Connection{Target: target, Route: relays(1), open: map[Layer]bool{}}

	plain := newTrace(conn, 1, func(string) bool { return false }, time.Time{})
	rooted := newTrace(conn, 1, func(string) bool { return true }, time.Time{})
	if want := plain.total + CostRelay*Resist(world.TypeISP)*(RootResist-1); math.Abs(rooted.total-want) > 0.01 {
		t.Errorf("a rooted relay makes the trace cost %.2f, want %.2f", rooted.total, want)
	}
	if rooted.TraceTime() <= plain.TraceTime() {
		t.Error("holding root on a relay did not buy any time")
	}
}

// TestTheTraceStandsOnTheHopItHasReached is what the player is shown: not a
// number alone but where on the route the trace is, which decides whether
// there is anything left to do about it.
func TestTheTraceStandsOnTheHopItHasReached(t *testing.T) {
	target := server("ARC", world.TypeCorporate, 4, false, true, true)
	tr := traceAgainst(target, 2, 1, false)
	// [target 6, relay 20, relay 20, gateway 15]
	for _, c := range []struct {
		done float64
		hop  int
	}{{0, 0}, {5.9, 0}, {6.1, 1}, {25.9, 1}, {26.1, 2}, {45.9, 2}, {46.1, 3}, {100, 3}} {
		tr.done = c.done
		if got := tr.at(); got != c.hop {
			t.Errorf("%.1f trace-s burned: standing on hop %d, want %d", c.done, got, c.hop)
		}
	}
}

// TestWithoutTheMeterTheTraceIsOnlyRoughlyKnown keeps the program worth
// buying: the exact number and the hop are what it buys, and the server
// decides that, not the page.
func TestWithoutTheMeterTheTraceIsOnlyRoughlyKnown(t *testing.T) {
	tr := traceAgainst(server("ARC", world.TypeCorporate, 4, false, true, true), 0, 1, false)
	tr.done = tr.total * 0.57

	rough := tr.view(false)
	if rough.Exact || rough.Progress != 0.5 || rough.HopName != "" || rough.Left != 0 {
		t.Errorf("without the meter the trace reads %+v", rough)
	}
	if out := text(Result{Lines: tr.lines(false)}); !strings.Contains(out, "past 50%") ||
		!strings.Contains(out, "TraceMeter") {
		t.Errorf("the rough reading says:\n%s", out)
	}

	exact := tr.view(true)
	if !exact.Exact || math.Abs(exact.Progress-0.57) > 0.001 || exact.HopName == "" || exact.Left <= 0 {
		t.Errorf("with the meter the trace reads %+v", exact)
	}
	if out := text(Result{Lines: tr.lines(true)}); !strings.Contains(out, "57.0%") ||
		!strings.Contains(out, "ARC") {
		t.Errorf("the exact reading says:\n%s", out)
	}
}

// TestOnlyAnActAgainstTheServerStartsTheTrace is the rule that keeps
// reconnaissance safe and makes every way in cost the same: looking around
// starts nothing, masking the monitor starts nothing, and working on a layer
// or reading what needed root starts everything.
func TestOnlyAnActAgainstTheServerStartsTheTrace(t *testing.T) {
	env, p, ctx := start(t)
	live := env.Engine

	// Reading what a public service publishes is safe.
	say(t, env, p, ctx, "connect 10.10.10.50")
	say(t, env, p, ctx, "ls")
	say(t, env, p, ctx, "cat /pub/about.txt")
	if tr, _ := live.Trace(p.AccountID); tr != nil {
		t.Error("reading the public register started a trace")
	}
	if out := say(t, env, p, ctx, "trace"); !strings.Contains(out, "Nobody is working out") {
		t.Errorf("trace with nothing running:\n%s", out)
	}
	say(t, env, p, ctx, "disconnect")

	// Masking the monitor is preparation: it runs before anybody notices.
	say(t, env, p, ctx, "connect 172.16.0.9")
	say(t, env, p, ctx, "run monitor_mask")
	if tr, _ := live.Trace(p.AccountID); tr != nil {
		t.Error("masking the monitor started the trace it is meant to slow")
	}

	// Working on the way in does start it. The mask has only been set going,
	// not finished, so the monitor is still watching and the trace runs at
	// the faster rate: masking is worth something only once it is done.
	say(t, env, p, ctx, "run proxy_opener")
	tr, _ := live.Trace(p.AccountID)
	if tr == nil {
		t.Fatal("opening the proxy did not start a trace")
	}
	if want := 2.0 / SecDiv * MonitorRate; math.Abs(tr.rate-want) > 0.001 {
		t.Errorf("the trace runs at %.3f, want %.3f: the mask had not finished", tr.rate, want)
	}
	if out := say(t, env, p, ctx, "trace"); !strings.Contains(out, "TRACE") {
		t.Errorf("trace while one runs:\n%s", out)
	}
	// Leaving of your own accord ends the live trace.
	say(t, env, p, ctx, "disconnect")
	if tr, _ := live.Trace(p.AccountID); tr != nil {
		t.Error("the live trace outlived the connection the player left")
	}
}

// TestTheAdministratorAnswersTheTrace walks the three answers of §5.7 in one
// run: a layer closed again at sixty per cent, the connection cut at seventy,
// and the run over when the trace reaches the gateway. The trace does not
// stop when the player is cut off — that is the whole of decision 26.
func TestTheAdministratorAnswersTheTrace(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	out := &recorder{}
	e := NewEngine(env, out, quietLogger())
	ctx := context.Background()
	account := accounts[0]
	target, _ := env.World.ServerByID("fort-main") // a monitor, so somebody is watching
	run, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := env.World.SoftwareByID("proxy_opener")

	e.Join(account)
	if _, err := e.Connect(account, target, run); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.tick(ctx, Tick, env.Now())
	// Opening the proxy arms the trace; the layer opens along the way.
	if _, err := e.StartTool(account, run, proxy, 1); err != nil {
		t.Fatal(err)
	}

	tr, _ := e.Trace(account)
	if tr == nil {
		t.Fatal("no trace after working on the way in")
	}
	if !tr.admin {
		t.Fatal("a server with a monitor has nobody watching it")
	}

	var closed, kicked, over bool
	for range 2000 {
		*now = now.Add(Tick)
		e.tick(ctx, Tick, env.Now())
		tr, _ := e.Trace(account)
		if tr == nil {
			over = true
			break
		}
		if tr.closedLayer && !closed {
			closed = true
			if tr.progress() < AdminClosesLayer {
				t.Errorf("a layer was closed at %.0f%%, before the administrator would act", tr.progress()*100)
			}
		}
		if tr.kicked && !kicked {
			kicked = true
			if tr.progress() < AdminKicks {
				t.Errorf("the connection was cut at %.0f%%", tr.progress()*100)
			}
			if e.Connection(account) != nil {
				t.Error("the player is still connected after being cut off")
			}
		}
		if kicked && e.Connection(account) == nil && tr.progress() > AdminKicks+0.05 && tr.progress() < 1 {
			// The trace keeps coming with the player off the target.
			if tr.done <= tr.total*AdminKicks {
				t.Error("the trace stopped when the player was cut off")
			}
		}
	}
	if !closed {
		t.Error("the administrator never closed a layer")
	}
	if !kicked {
		t.Error("the administrator never cut the connection")
	}
	if !over {
		t.Fatal("the trace never reached the gateway")
	}

	// The run is over, and the browser was told to take a fresh snapshot.
	if r, err := env.Store.ActiveRun(ctx, account); err != nil {
		t.Fatal(err)
	} else if r != nil {
		t.Error("the trace reached the gateway and the run is still active")
	}
	resync := false
	for _, s := range out.all() {
		if _, ok := s.delta.(ResyncDelta); ok {
			resync = true
		}
	}
	if !resync {
		t.Error("the browser was never asked for a fresh snapshot after the run ended")
	}
}

// TestMaskingTheMonitorFirstHoldsTheTraceDown is what the mask is for: run to
// the end before anybody is working on the way in, it holds the trace at its
// slower rate for the whole of it. That is the difference between the two
// verdicts of the balance tables for a starting gateway.
func TestMaskingTheMonitorFirstHoldsTheTraceDown(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()
	account := accounts[0]
	target, _ := env.World.ServerByID("fort-main") // security 2, with a monitor
	run, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	mask, _ := env.World.SoftwareByID("monitor_mask")
	proxy, _ := env.World.SoftwareByID("proxy_opener")

	e.Join(account)
	if _, err := e.Connect(account, target, run); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.tick(ctx, Tick, env.Now())

	if _, err := e.StartTool(account, run, mask, 1); err != nil {
		t.Fatal(err)
	}
	// Security 2 at 2000 MHz: four seconds, and nothing is watching for a
	// trace while it runs.
	for range 45 {
		*now = now.Add(Tick)
		e.tick(ctx, Tick, env.Now())
	}
	if tr, _ := e.Trace(account); tr != nil {
		t.Fatal("the mask started a trace by itself")
	}

	if _, err := e.StartTool(account, run, proxy, 1); err != nil {
		t.Fatal(err)
	}
	tr, _ := e.Trace(account)
	if tr == nil {
		t.Fatal("opening the proxy did not start a trace")
	}
	if want := 2.0 / SecDiv; math.Abs(tr.rate-want) > 0.001 {
		t.Errorf("with the monitor masked the trace runs at %.3f, want %.3f", tr.rate, want)
	}
	// The mask slows the trace; it does not send the administrator home. A
	// server with a monitor has somebody watching it, masked or not, and
	// that person still answers the trace at their thresholds.
	if !tr.admin {
		t.Error("masking the monitor left the server with nobody watching it")
	}
}
