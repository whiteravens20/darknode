// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"testing"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
)

// walkSetup builds a break-in that has already happened: a run, a route, and
// the lines left on each node, with the wipes the test asks for.
type walkSetup struct {
	target string
	route  []string
	// wiped are the servers whose lines were cleared, and withV1 says the
	// clearing was done with the version that leaves a mark.
	wiped  []string
	withV1 bool
	// severity is what was done on the target.
	severity int
}

// afterTheBreakIn writes the logs and opens the walk, returning the run and
// the investigation, or nil when none was opened.
func afterTheBreakIn(t *testing.T, env *Env, account int64, s walkSetup) (*store.Run, *store.Investigation) {
	t.Helper()
	ctx := context.Background()
	run, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	catalogue := env.World

	var lines []store.LogEntry
	hops := len(s.route)
	for i, id := range s.route {
		source := ""
		if i > 0 {
			relay, _ := catalogue.ServerByID(s.route[i-1])
			source = relay.IP.String()
		}
		lines = append(lines, store.LogEntry{
			Server: id, HopIndex: hops - i, WorldTime: env.Clock.Now(env.Now()),
			Action: ActionRelay, SourceIP: source,
		})
	}
	source := ""
	if hops > 0 {
		relay, _ := catalogue.ServerByID(s.route[hops-1])
		source = relay.IP.String()
	}
	lines = append(lines,
		store.LogEntry{Server: s.target, WorldTime: env.Clock.Now(env.Now()),
			Action: ActionConnect, SourceIP: source},
		store.LogEntry{Server: s.target, WorldTime: env.Clock.Now(env.Now()),
			Action: ActionRead, SourceIP: source, Severity: s.severity})
	if err := env.Store.AddLogs(ctx, run.ID, lines); err != nil {
		t.Fatal(err)
	}
	version := 2
	if s.withV1 {
		version = 1
	}
	for _, id := range s.wiped {
		if _, err := env.Store.WipeLogs(ctx, run.ID, id, version); err != nil {
			t.Fatal(err)
		}
	}

	target, ok := catalogue.ServerByID(s.target)
	if !ok {
		t.Fatalf("no server %q in the test world", s.target)
	}
	var route []*world.Server // the catalogue's own type
	for _, id := range s.route {
		relay, _ := catalogue.ServerByID(id)
		route = append(route, relay)
	}
	opened, err := env.OpenInvestigation(ctx, run, target, route)
	if err != nil {
		t.Fatal(err)
	}
	walks, err := env.Store.InvestigationsOfRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !opened {
		if len(walks) != 0 {
			t.Fatalf("no walk was opened but %d stand against the run", len(walks))
		}
		return run, nil
	}
	if len(walks) != 1 {
		t.Fatalf("%d walks against the run, want 1", len(walks))
	}
	return run, &walks[0]
}

// runWalk steps a walk until it stops and returns everything that happened.
func runWalk(t *testing.T, env *Env, in *store.Investigation, run *store.Run) investigationOutcome {
	t.Helper()
	ctx := context.Background()
	var all investigationOutcome
	for range 20 {
		if in.State != store.InvestigationRunning {
			break
		}
		out, err := env.StepInvestigation(ctx, in, run)
		if err != nil {
			t.Fatal(err)
		}
		all.alerts = append(all.alerts, out.alerts...)
		all.fine += out.fine
		if out.seized != "" {
			all.seized = out.seized
		}
		all.arrested = all.arrested || out.arrested
		if err := env.Store.SaveInvestigation(ctx, *in); err != nil {
			t.Fatal(err)
		}
	}
	if in.State == store.InvestigationRunning {
		t.Fatal("the walk never stopped")
	}
	return all
}

// TestAClearedLogLeavesNothingToInvestigate is what a clean break-in buys:
// the lines are gone, nothing loud was done, and nobody comes.
func TestAClearedLogLeavesNothingToInvestigate(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	_, walk := afterTheBreakIn(t, env, accounts[0], walkSetup{
		target: "home-bank", route: []string{"relay-1"},
		wiped: []string{"home-bank", "relay-1"}, severity: 1,
	})
	if walk != nil {
		t.Error("a walk was opened although every line had been cleared")
	}
}

// TestALoudActIsNoticedEvenWithACleanLog is the other side of it: a file that
// is gone is gone, whatever the log says — but with no line to follow, the
// walk dies where it began.
func TestALoudActIsNoticedEvenWithACleanLog(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	run, walk := afterTheBreakIn(t, env, accounts[0], walkSetup{
		target: "home-bank", route: []string{"relay-1"},
		wiped: []string{"home-bank", "relay-1"}, severity: 3,
	})
	if walk == nil {
		t.Fatal("destroying something went unnoticed")
	}
	out := runWalk(t, env, walk, run)
	if walk.State != store.InvestigationStalled {
		t.Errorf("the walk ended as %s, want it to stall at the target", walk.State)
	}
	if out.fine != 0 || out.seized != "" || out.arrested {
		t.Errorf("a walk with no trail still cost something: %+v", out)
	}
}

// TestTheWalkStopsAtTheFirstClearedLog is why a route has to be cleaned hop
// by hop: one cleared log ends the trail, and the relays behind it are never
// read.
func TestTheWalkStopsAtTheFirstClearedLog(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	run, walk := afterTheBreakIn(t, env, accounts[0], walkSetup{
		target: "home-bank", route: []string{"relay-1", "relay-2"},
		wiped: []string{"relay-2"}, severity: 1,
	})
	if walk == nil {
		t.Fatal("an unwiped target started no walk")
	}
	out := runWalk(t, env, walk, run)
	if walk.State != store.InvestigationStalled {
		t.Errorf("the walk ended as %s, want STALLED at the cleared relay", walk.State)
	}
	if out.arrested {
		t.Error("the walk reached the gateway through a cleared log")
	}
}

// TestWhatTheRouteLengthDecides is the table of §6.5 in code: with no relay
// an unbroken trail is an arrest, with one it is a fine, and with two it is a
// fine, then the gear, then the arrest.
func TestWhatTheRouteLengthDecides(t *testing.T) {
	for _, c := range []struct {
		what                string
		target              string
		route               []string
		fined, seized, held bool
	}{
		{"no relay: straight to the gateway", "home-bank", nil, false, false, true},
		{"one relay: a fine", "home-bank", []string{"relay-1"}, true, false, true},
		{"two relays: a fine, then the gear", "home-bank",
			[]string{"relay-1", "relay-2"}, true, true, true},
	} {
		t.Run(c.what, func(t *testing.T) {
			env, _, accounts := engineEnv(t, "agent")
			run, walk := afterTheBreakIn(t, env, accounts[0], walkSetup{
				target: c.target, route: c.route, severity: 2,
			})
			if walk == nil {
				t.Fatal("no walk was opened")
			}
			out := runWalk(t, env, walk, run)
			if (out.fine > 0) != c.fined {
				t.Errorf("fine %d, want fined=%v", out.fine, c.fined)
			}
			if (out.seized != "") != c.seized {
				t.Errorf("seized %q, want seized=%v", out.seized, c.seized)
			}
			if out.arrested != c.held {
				t.Errorf("arrested %v, want %v", out.arrested, c.held)
			}
			if c.held && walk.State != store.InvestigationAtGateway {
				t.Errorf("the walk ended as %s, want it at the gateway", walk.State)
			}
		})
	}
}

// TestACompetentOrganisationRebuildsOneHop is what the cheapest wiper costs:
// a bank reads the mark version one leaves and gets one hop further, once.
func TestACompetentOrganisationRebuildsOneHop(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	// The bank is competent, and the relay next to the target was cleared
	// with the version that leaves a mark.
	run, walk := afterTheBreakIn(t, env, accounts[0], walkSetup{
		target: "home-bank", route: []string{"relay-1", "relay-2"},
		wiped: []string{"relay-2"}, withV1: true, severity: 1,
	})
	if walk == nil {
		t.Fatal("no walk was opened")
	}
	out := runWalk(t, env, walk, run)
	if !walk.Reconstructed {
		t.Error("the bank did not rebuild the hop the mark gave it")
	}
	if out.fine == 0 && !out.arrested && out.seized == "" {
		t.Error("the rebuilt hop cost the player nothing at all")
	}

	// A company would not have managed it: reading what is left of a cleared
	// log takes people a bank has and a firm does not.
	env2, _, accounts2 := engineEnv(t, "agent")
	run2, walk2 := afterTheBreakIn(t, env2, accounts2[0], walkSetup{
		target: "fort-main", route: []string{"relay-1", "relay-2"},
		wiped: []string{"relay-2"}, withV1: true, severity: 1,
	})
	if walk2 == nil {
		t.Fatal("no walk was opened against the company")
	}
	runWalk(t, env2, walk2, run2)
	if walk2.Reconstructed {
		t.Error("a company rebuilt a hop from a mark it cannot read")
	}
}

// TestAFineIsProportionateAndCapped keeps a first penalty from ending a run:
// a flat amount for what was done plus a share of what the player has, never
// more than the ceiling.
func TestAFineIsProportionateAndCapped(t *testing.T) {
	env, _ := testEnv(t)
	for _, c := range []struct {
		severity int
		balance  int64
		want     int64
	}{
		{0, 0, FineFlat},              // never nothing
		{1, 100, FineFlat + 30},       // flat plus three tenths
		{2, 1000, 2*FineFlat + 300},   //
		{3, 10000, 3*FineFlat + 3000}, //
		{5, 1000000, FineCap},         // the ceiling holds
		{1, -500, FineFlat},           // a debt is not taxed further
	} {
		if got := env.fine(c.severity, c.balance); got != c.want {
			t.Errorf("severity %d with $%d: fine $%d, want $%d", c.severity, c.balance, got, c.want)
		}
	}
}

// TestANoticedOrganisationTakesLongerToReact pins the start delays: the state
// answers at once, a university takes a term, and nothing watches a practice
// machine.
func TestANoticedOrganisationTakesLongerToReact(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	run, walk := afterTheBreakIn(t, env, accounts[0], walkSetup{
		target: "home-bank", route: []string{"relay-1"}, severity: 1,
	})
	if walk == nil {
		t.Fatal("no walk was opened")
	}
	if want := run.ExposureSeconds + 12; walk.Due != want {
		t.Errorf("a bank starts looking at %d seconds of play, want %d", walk.Due, want)
	}
	if got := env.hopTime("relay-1"); got != 10 {
		t.Errorf("a hop through an ISP takes %d seconds of play, want 10", got)
	}
	if got := env.hopTime("fort-main"); got != 25 {
		t.Errorf("a hop through a company takes %d seconds of play, want 25", got)
	}
	if got := env.hopTime("home-bank"); got != 40 {
		t.Errorf("a hop through a bank takes %d seconds of play, want 40", got)
	}
}
