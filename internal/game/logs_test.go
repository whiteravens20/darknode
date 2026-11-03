// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"strings"
	"testing"

	"github.com/whiteravens20/darknode/internal/store"
)

// TestEveryNodeOfTheRouteWritesDownWhereTheTrafficCameFrom is the evidence a
// break-in leaves. Each relay records the node before it, so the lines can be
// walked back one hop at a time; the target records the connection itself.
func TestEveryNodeOfTheRouteWritesDownWhereTheTrafficCameFrom(t *testing.T) {
	env, p, ctx := start(t)
	say(t, env, p, ctx, "bounce add 203.0.113.50")
	say(t, env, p, ctx, "bounce add 203.0.113.51")
	say(t, env, p, ctx, "connect 172.16.0.1")

	run, err := env.Store.ActiveRun(ctx, p.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := env.Store.LogsOfRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("a route of two relays and a target left %d line(s), want 3: %+v", len(entries), entries)
	}
	byServer := map[string]store.LogEntry{}
	for _, entry := range entries {
		byServer[entry.Server] = entry
	}
	// The first relay was talked to by the gateway itself, which no log can
	// name; the second was talked to by the first.
	for _, c := range []struct {
		server, action, from string
		hop                  int
	}{
		{"relay-1", ActionRelay, "", 2},
		{"relay-2", ActionRelay, "203.0.113.50", 1},
		{"home-bank", ActionConnect, "203.0.113.51", 0},
	} {
		entry, ok := byServer[c.server]
		if !ok {
			t.Errorf("%s wrote nothing down", c.server)
			continue
		}
		if entry.Action != c.action || entry.SourceIP != c.from || entry.HopIndex != c.hop {
			t.Errorf("%s logged %s from %q at hop %d; want %s from %q at hop %d",
				c.server, entry.Action, entry.SourceIP, entry.HopIndex, c.action, c.from, c.hop)
		}
		if entry.Wiped() {
			t.Errorf("%s's line is already cleared", c.server)
		}
	}

	// A public service is read, not broken into: there is nothing for a line
	// to be evidence of.
	say(t, env, p, ctx, "disconnect")
	say(t, env, p, ctx, "bounce clear")
	say(t, env, p, ctx, "connect 10.10.10.50")
	say(t, env, p, ctx, "cat /pub/about.txt")
	after, err := env.Store.LogsOfRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(entries) {
		t.Errorf("reading the public register left %d new line(s)", len(after)-len(entries))
	}
}

// TestTheLogIsReadAndClearedWithRoot walks what a player does about the
// evidence: it cannot be seen without root, clearing needs the program, and
// the version used is remembered — version one leaves a mark.
func TestTheLogIsReadAndClearedWithRoot(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()
	account := accounts[0]
	p := Player{AccountID: account, Username: "agent"}
	target, _ := env.World.ServerByID("practice")
	run, _ := env.Store.ActiveRun(ctx, account)
	breaker, _ := env.World.SoftwareByID("pass_breaker")

	e.Join(account)
	if _, err := e.Connect(account, target, run); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.AddLogs(ctx, run.ID, connectionLogs(e.Connection(account), env.Clock.Now(env.Now()))); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.tick(ctx, Tick, env.Now())

	if out := say(t, env, p, ctx, "log"); !strings.Contains(out, "Not without root") {
		t.Errorf("the log was shown without root:\n%s", out)
	}

	if _, err := e.StartTool(account, run, breaker, 1); err != nil {
		t.Fatal(err)
	}
	for range 25 {
		*now = now.Add(Tick)
		e.tick(ctx, Tick, env.Now())
	}
	say(t, env, p, ctx, "cat /readme.txt")

	// The connection, the root and the read are all written down.
	out := say(t, env, p, ctx, "log")
	for _, want := range []string{ActionConnect, ActionRoot, ActionRead, "your gateway", "stands"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %q:\n%s", want, out)
		}
	}

	if out := say(t, env, p, ctx, "log wipe"); !strings.Contains(out, "cleared with LogWiper v1") ||
		!strings.Contains(out, "leaves a mark") {
		t.Errorf("clearing the log:\n%s", out)
	}
	if out := say(t, env, p, ctx, "log"); !strings.Contains(out, "cleared with v1") {
		t.Errorf("the log after clearing:\n%s", out)
	}
	if out := say(t, env, p, ctx, "log wipe"); !strings.Contains(out, "Nothing of yours left") {
		t.Errorf("clearing a log twice:\n%s", out)
	}
	if out := say(t, env, p, ctx, "log sideways"); !strings.Contains(out, "Usage") {
		t.Errorf("an invented subcommand:\n%s", out)
	}
}

// TestClearingAHopTheTraceHasNotReachedBreaksTheTrail is the one way out of a
// trace that is already coming, and the whole reason to work a relay out in
// advance: the line it would have followed back is gone, so it stops.
func TestClearingAHopTheTraceHasNotReachedBreaksTheTrail(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()
	account := accounts[0]
	p := Player{AccountID: account, Username: "agent"}
	relay, _ := env.World.ServerByID("relay-1")
	target, _ := env.World.ServerByID("home-bank")
	run, _ := env.Store.ActiveRun(ctx, account)

	// The run holds root on the relay from earlier work, and the player is
	// on the target through it.
	if err := env.Store.GrantRoot(ctx, run.ID, relay.ID, env.Now()); err != nil {
		t.Fatal(err)
	}
	if run, err := env.Store.ActiveRun(ctx, account); err != nil {
		t.Fatal(err)
	} else if err := e.AddRelay(account, relay, run.Gateway.NICCount); err != nil {
		t.Fatal(err)
	}
	run, _ = env.Store.ActiveRun(ctx, account)
	e.Join(account)
	if _, err := e.Connect(account, target, run); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.AddLogs(ctx, run.ID, connectionLogs(e.Connection(account), env.Clock.Now(env.Now()))); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.tick(ctx, Tick, env.Now())
	e.ArmTrace(account, run)

	tr, _ := e.Trace(account)
	if tr == nil {
		t.Fatal("no trace to break")
	}
	if at := tr.at(); at != 0 {
		t.Fatalf("the trace starts on hop %d, want the target", at)
	}

	// Clearing the log of the hop it already stands on changes nothing: it
	// has read that one.
	if e.BreakTrail(account, target.ID) {
		t.Error("clearing the target's log stopped a trace that had read it")
	}
	// The relay it has not reached is another matter.
	if !e.BreakTrail(account, relay.ID) {
		t.Fatal("clearing the relay's log did not break the trail")
	}
	if tr, _ := e.Trace(account); tr != nil {
		t.Error("the trace is still coming after the trail was broken")
	}

	// And through the command, as a player does it: off the target, route
	// cleared — a server cannot be a relay of its own route — and onto the
	// relay, where the run already holds root.
	say(t, env, p, ctx, "disconnect")
	e.ClearRoute(account)
	run, _ = env.Store.ActiveRun(ctx, account)
	if _, err := e.Connect(account, relay, run); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.tick(ctx, Tick, env.Now())
	if out := say(t, env, p, ctx, "log wipe"); !strings.Contains(out, "cleared with LogWiper v1") {
		t.Errorf("clearing the relay's log as a player:\n%s", out)
	}
}
