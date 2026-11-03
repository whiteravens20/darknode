// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
)

// start begins a run and returns the environment ready to take commands.
func start(t *testing.T) (*Env, Player, context.Context) {
	t.Helper()
	env, p := testEnv(t)
	ctx := context.Background()
	if r, err := env.Execute(ctx, p, "run new"); err != nil || !r.Changed {
		t.Fatalf("run new: %+v %v", r, err)
	}
	return env, p, ctx
}

// say runs a command line and returns what it printed.
func say(t *testing.T, env *Env, p Player, ctx context.Context, line string) string {
	t.Helper()
	r, err := env.Execute(ctx, p, line)
	if err != nil {
		t.Fatalf("%q: %v", line, err)
	}
	return text(r)
}

// TestProbeIsWhatMakesAServerVisible pins the rule that makes planning a
// break-in a decision: an address on its own tells a player nothing, and the
// probe is what turns it into a type, a security level and a list of layers.
func TestProbeIsWhatMakesAServerVisible(t *testing.T) {
	env, p, ctx := start(t)

	if out := say(t, env, p, ctx, "probe 172.16.0.1"); !strings.Contains(out, "Type       BANK") ||
		!strings.Contains(out, "Security   5") || !strings.Contains(out, "firewall + password") {
		t.Errorf("probe says:\n%s", out)
	}
	// What the probe found is remembered, so the list of addresses carries it.
	if out := say(t, env, p, ctx, "servers"); !strings.Contains(out, "BANK, security 5") {
		t.Errorf("servers after the probe:\n%s", out)
	}
	// An address nobody gave the player is not a way to map the world: the
	// answer is the same as for an address that does not exist at all.
	unknown := say(t, env, p, ctx, "probe 10.0.0.9")
	missing := say(t, env, p, ctx, "probe 192.168.99.99")
	if !strings.Contains(unknown, "do not know that address") || unknown != missing {
		t.Errorf("an unknown address and a non-existent one answer differently:\n%s\n%s", unknown, missing)
	}
	if out := say(t, env, p, ctx, "probe not-an-address"); !strings.Contains(out, "is not an address") {
		t.Errorf("probe of a word:\n%s", out)
	}

	// Without the program there is nothing to probe with.
	if hasTool(&store.Run{Software: []store.Installed{{Software: "log_wiper"}}}, probeTool) {
		t.Error("hasTool says a run with no probe has one")
	}
	if out := text(env.missingTool(probeTool)); !strings.Contains(out, "AddressProbe") {
		t.Errorf("the refusal does not name the program: %q", out)
	}
}

// TestLookupAnswersOnlyForWhatTheRunKnows keeps the register from being a way
// around discovery: it tells a player that an organisation exists, never
// where it is.
func TestLookupAnswersOnlyForWhatTheRunKnows(t *testing.T) {
	env, p, ctx := start(t)

	if out := say(t, env, p, ctx, "lookup bank"); !strings.Contains(out, "172.16.0.1") {
		t.Errorf("lookup of a known organisation:\n%s", out)
	}
	if out := say(t, env, p, ctx, "lookup 203.0.113.50"); !strings.Contains(out, "Test Relay One") {
		t.Errorf("lookup of a known address:\n%s", out)
	}
	if out := say(t, env, p, ctx, "lookup nothing-like-this"); !strings.Contains(out, "Nothing you know matches") {
		t.Errorf("lookup of a stranger:\n%s", out)
	}
}

// TestARouteIsRelaysTheGatewayCanHold is the whole of the route's rules: only
// servers the player knows, never a public service, never twice, and never
// more than the gateway has network cards for.
func TestARouteIsRelaysTheGatewayCanHold(t *testing.T) {
	env, p, ctx := start(t)

	if out := say(t, env, p, ctx, "bounce"); !strings.Contains(out, "no relays") ||
		!strings.Contains(out, "2 network card") {
		t.Errorf("an empty route reads:\n%s", out)
	}
	if out := say(t, env, p, ctx, "bounce add 10.10.10.50"); !strings.Contains(out, "public service") {
		t.Errorf("a public service was taken as a relay:\n%s", out)
	}
	if out := say(t, env, p, ctx, "bounce add 203.0.113.50"); !strings.Contains(out, "added to the route") {
		t.Errorf("adding a relay:\n%s", out)
	}
	if out := say(t, env, p, ctx, "bounce add 203.0.113.50"); !strings.Contains(out, "already on the route") {
		t.Errorf("the same relay twice:\n%s", out)
	}
	if out := say(t, env, p, ctx, "bounce add 203.0.113.51"); !strings.Contains(out, "added to the route") {
		t.Errorf("adding a second relay:\n%s", out)
	}
	// The starting gateway has two network cards, so the third is refused.
	if out := say(t, env, p, ctx, "bounce add 203.0.113.52"); !strings.Contains(out, "2 network card") {
		t.Errorf("a third relay on a two-card gateway:\n%s", out)
	}
	if out := say(t, env, p, ctx, "bounce"); !strings.Contains(out, "2 relay(s)") {
		t.Errorf("the route reads:\n%s", out)
	}
	if out := say(t, env, p, ctx, "status"); !strings.Contains(out, "203.0.113.50 → 203.0.113.51") {
		t.Errorf("status does not show the route:\n%s", out)
	}
	if out := say(t, env, p, ctx, "bounce clear"); !strings.Contains(out, "Route cleared") {
		t.Errorf("clearing the route:\n%s", out)
	}
	if out := say(t, env, p, ctx, "bounce"); !strings.Contains(out, "no relays") {
		t.Errorf("the route after clearing:\n%s", out)
	}
	if out := say(t, env, p, ctx, "bounce sideways"); !strings.Contains(out, "Usage") {
		t.Errorf("an invented subcommand:\n%s", out)
	}
}

// TestConnectingToAServerAndLeavingIt walks the states a connection has: one
// at a time, through the route it was given, and gone when the player leaves.
func TestConnectingToAServerAndLeavingIt(t *testing.T) {
	env, p, ctx := start(t)

	if out := say(t, env, p, ctx, "disconnect"); !strings.Contains(out, "not connected") {
		t.Errorf("disconnecting from nothing:\n%s", out)
	}
	say(t, env, p, ctx, "bounce add 203.0.113.50")

	out := say(t, env, p, ctx, "connect 172.16.0.1")
	if !strings.Contains(out, "Connecting to Home Bank") || !strings.Contains(out, "without root") ||
		!strings.Contains(out, "firewall") {
		t.Errorf("connecting to the bank:\n%s", out)
	}
	if out := say(t, env, p, ctx, "status"); !strings.Contains(out, "Home Bank") ||
		!strings.Contains(out, "connecting") || !strings.Contains(out, "no root") {
		t.Errorf("status while connecting:\n%s", out)
	}
	if out := say(t, env, p, ctx, "connect 10.0.0.1"); !strings.Contains(out, "connected to something already") {
		t.Errorf("a second connection:\n%s", out)
	}
	if out := say(t, env, p, ctx, "disconnect"); !strings.Contains(out, "Disconnected from Home Bank") ||
		!strings.Contains(out, "closed again") {
		t.Errorf("disconnecting:\n%s", out)
	}
	if out := say(t, env, p, ctx, "status"); !strings.Contains(out, "Connection  none") {
		t.Errorf("status after disconnecting:\n%s", out)
	}

	// A server cannot be a relay of its own route.
	if out := say(t, env, p, ctx, "connect 203.0.113.50"); !strings.Contains(out, "relay on your own route") {
		t.Errorf("connecting to a relay of the route:\n%s", out)
	}
}

// TestThePublicRegisterIsOpenAndPaginated is the one place a new player can
// go and read: no layers, no trace, and a list that comes a page at a time.
func TestThePublicRegisterIsOpenAndPaginated(t *testing.T) {
	env, p, ctx := start(t)

	out := say(t, env, p, ctx, "connect 10.10.10.50")
	if !strings.Contains(out, "public service") || !strings.Contains(out, "nothing here traces you") {
		t.Errorf("connecting to the register:\n%s", out)
	}
	out = say(t, env, p, ctx, "links")
	if !strings.Contains(out, "THE REGISTER") || !strings.Contains(out, "Home Bank") ||
		!strings.Contains(out, "Test Carrier") {
		t.Errorf("the register reads:\n%s", out)
	}
	// An address is shown for an organisation the run knows, and withheld for
	// one it does not.
	if !strings.Contains(out, "172.16.0.1") {
		t.Errorf("the register hides an address the run has:\n%s", out)
	}
	if out := say(t, env, p, ctx, "links 99"); !strings.Contains(out, "page(s)") {
		t.Errorf("a page past the end:\n%s", out)
	}
	if out := say(t, env, p, ctx, "links two"); !strings.Contains(out, "Usage") {
		t.Errorf("a page that is not a number:\n%s", out)
	}

	// The register is a machine, not a command: it has to be read where it is.
	say(t, env, p, ctx, "disconnect")
	if out := say(t, env, p, ctx, "links"); !strings.Contains(out, "not connected") {
		t.Errorf("links with no connection:\n%s", out)
	}
	say(t, env, p, ctx, "connect 172.16.0.1")
	if out := say(t, env, p, ctx, "links"); !strings.Contains(out, "on the InterNIC machine") {
		t.Errorf("links on the wrong machine:\n%s", out)
	}
}

// TestTheHandshakeFinishesInTheLoop checks that a connection comes up by the
// simulation's clock and not by the command that asked for it.
func TestTheHandshakeFinishesInTheLoop(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	account := accounts[0]
	target, ok := env.World.ServerByID("home-bank")
	if !ok {
		t.Fatal("the test world has no bank")
	}

	e.Join(account)
	if _, err := e.Connect(account, target, 2); err != nil {
		t.Fatal(err)
	}
	if conn := e.Connection(account); conn.State != StateConnecting {
		t.Fatalf("a new connection is %s, want %s", conn.State, StateConnecting)
	}
	for range 4 { // 400 ms, short of the handshake
		*now = now.Add(Tick)
		e.step(Tick, env.Now())
	}
	if conn := e.Connection(account); conn.State != StateConnecting {
		t.Errorf("the connection came up after 400 ms, before the handshake was over")
	}
	*now = now.Add(Tick)
	e.step(Tick, env.Now())
	conn := e.Connection(account)
	if conn.State != StateConnected {
		t.Errorf("after the handshake the connection is %s, want %s", conn.State, StateConnected)
	}
	if conn.Root {
		t.Error("a connection to a server with layers starts with root")
	}
}

// TestAConnectionOutlivesTheBrowserOnlyForTheGrace is decision 14 in code: a
// closed tab is not a way out. The connection keeps running for the grace, so
// a reload costs nothing, and then it closes as a disconnect would.
func TestAConnectionOutlivesTheBrowserOnlyForTheGrace(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	account := accounts[0]
	target, _ := env.World.ServerByID("home-bank")

	e.Join(account)
	if _, err := e.Connect(account, target, 2); err != nil {
		t.Fatal(err)
	}
	e.Leave(account)

	// A reload: the browser is gone for a moment and comes back.
	*now = now.Add(OfflineGrace - time.Second)
	e.step(Tick, env.Now())
	if e.Connection(account) == nil {
		t.Fatal("the connection was dropped inside the grace, so a reload would end a run")
	}
	e.Join(account)
	*now = now.Add(2 * OfflineGrace)
	e.step(Tick, env.Now())
	if e.Connection(account) == nil {
		t.Fatal("the connection was dropped although a browser came back")
	}

	// Gone for good this time.
	e.Leave(account)
	*now = now.Add(OfflineGrace)
	e.step(Tick, env.Now())
	if e.Connection(account) != nil {
		t.Error("the connection outlived the grace with nobody watching")
	}
}
