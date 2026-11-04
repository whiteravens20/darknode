// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
)

// TestTransferTimeFollowsTheModem is the modem's one job, and the reason a
// big file is a long exposure.
func TestTransferTimeFollowsTheModem(t *testing.T) {
	for _, c := range []struct {
		sizeMB, modem int
		want          float64
	}{
		{15, 100, 1.2}, {50, 100, 4.0}, {50, 1000, 0.4}, {1, 100, 0.08},
	} {
		if got := TransferTime(c.sizeMB, c.modem).Seconds(); got != c.want {
			t.Errorf("%d MB at %d Mbps takes %.2f s, want %.2f s", c.sizeMB, c.modem, got, c.want)
		}
	}
}

// TestTheBoardDrawsWorkFromTheCatalogue keeps the generator from inventing
// anything: every contract points at a server that is in the world, that a
// player can reach, and that has something worth the trouble on it.
func TestTheBoardDrawsWorkFromTheCatalogue(t *testing.T) {
	env, p, ctx := start(t)
	out := say(t, env, p, ctx, "missions")
	if !strings.Contains(out, "WORK") || !strings.Contains(out, "rep 0") {
		t.Fatalf("the board reads:\n%s", out)
	}

	run, err := env.Store.ActiveRun(ctx, p.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	missions, err := env.Store.MissionsOfRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(missions) == 0 {
		t.Fatal("the board is empty")
	}
	if len(missions) > MaxAvailable {
		t.Fatalf("%d contracts on a board of %d", len(missions), MaxAvailable)
	}
	seen := map[string]bool{}
	for _, m := range missions {
		target, ok := env.World.ServerByID(m.Target)
		if !ok {
			t.Fatalf("contract %d points at %q, which is not in the world", m.ID, m.Target)
		}
		if target.Type().Public() {
			t.Errorf("contract %d points at a public service", m.ID)
		}
		if seen[m.Target] {
			t.Errorf("two contracts point at %s at once", m.Target)
		}
		seen[m.Target] = true
		found := false
		for _, f := range target.Files {
			if f.Path == m.File && f.Sensitive {
				found = true
			}
		}
		if !found {
			t.Errorf("contract %d asks for %q, which is not a sensitive file of %s", m.ID, m.File, m.Target)
		}
		if m.ReputationRequired > run.Reputation+8 {
			t.Errorf("contract %d wants a reputation of %d from a player with %d",
				m.ID, m.ReputationRequired, run.Reputation)
		}
		if m.Reward <= 0 || m.Kind != MissionSteal && m.Kind != MissionDestroy {
			t.Errorf("contract %d: %s for $%d", m.ID, m.Kind, m.Reward)
		}
	}

	// Looking at the board again draws nothing new: it is already full of
	// what the catalogue can offer.
	say(t, env, p, ctx, "missions")
	again, err := env.Store.MissionsOfRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(missions) {
		t.Errorf("a second look drew %d more contracts", len(again)-len(missions))
	}
}

// onTarget connects the player to a server they already hold root on, with
// the loop running, and returns the engine driving it.
func onTarget(t *testing.T, env *Env, e *Engine, now *time.Time, account int64, server string) *store.Run {
	t.Helper()
	ctx := context.Background()
	run, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	target, ok := env.World.ServerByID(server)
	if !ok {
		t.Fatalf("no server %q in the test world", server)
	}
	if err := env.Store.GrantRoot(ctx, run.ID, target.ID, env.Now()); err != nil {
		t.Fatal(err)
	}
	run, err = env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	e.Join(account)
	if _, err := e.Connect(account, target, run); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.tick(ctx, Tick, env.Now())
	return run
}

// tickFor runs the loop for a stretch of real time.
func tickFor(env *Env, e *Engine, now *time.Time, d time.Duration) {
	for range int(d / Tick) {
		*now = now.Add(Tick)
		e.tick(context.Background(), Tick, env.Now())
	}
}

// TestAContractIsDoneByDoingIt is the rule the prototype broke: what counts
// is the state of the run — the copy on the gateway, the file that is gone —
// and never what the client claims.
func TestAContractIsDoneByDoingIt(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()
	account := accounts[0]
	p := Player{AccountID: account, Username: "agent"}

	// A contract to steal the ledger of the company, written by hand so the
	// test does not depend on the draw.
	run, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Store.AddMissions(ctx, []store.Mission{{
		RunID: run.ID, Kind: MissionSteal, Target: "fort-main", File: "/data/ledger.db",
		Client: "Somebody", Difficulty: "TRIVIAL", Reward: 900, ReputationGain: 1,
		State: store.MissionAvailable, CreatedAt: env.Now(),
	}}); err != nil {
		t.Fatal(err)
	}

	if out := say(t, env, p, ctx, "mission accept 1"); !strings.Contains(out, "Taken on") {
		t.Fatalf("accepting the contract:\n%s", out)
	}
	if out := say(t, env, p, ctx, "mission submit 1"); !strings.Contains(out, "You do not have /data/ledger.db") {
		t.Errorf("submitting without having done it:\n%s", out)
	}

	// Do the work: on the target, with root, take the copy.
	onTarget(t, env, e, now, account, "fort-main")
	if out := say(t, env, p, ctx, "cp /data/ledger.db"); !strings.Contains(out, "coming down at 100 Mbps") {
		t.Fatalf("copying the file:\n%s", out)
	}
	// Four megabytes at a hundred megabits is a third of a second.
	tickFor(env, e, now, time.Second)
	files, err := env.Store.GatewayFiles(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "/data/ledger.db" {
		t.Fatalf("the gateway holds %+v", files)
	}
	if out := say(t, env, p, ctx, "status"); !strings.Contains(out, "On gateway  /data/ledger.db") {
		t.Errorf("status does not show what was taken:\n%s", out)
	}

	out := say(t, env, p, ctx, "mission submit 1")
	if !strings.Contains(out, "Delivered") || !strings.Contains(out, "$900") {
		t.Fatalf("submitting after doing it:\n%s", out)
	}
	after, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(StartBalance + 900); balance(after) != want {
		t.Errorf("the account holds $%d, want $%d", balance(after), want)
	}
	if after.Reputation != 1 {
		t.Errorf("the reputation is %d, want 1", after.Reputation)
	}
	if out := say(t, env, p, ctx, "mission submit 1"); !strings.Contains(out, "not one of yours") {
		t.Errorf("submitting a finished contract:\n%s", out)
	}
}

// TestWhatWasAlreadyDoneDoesNotCount closes the obvious way round a contract:
// take the file first, accept the work afterwards.
func TestWhatWasAlreadyDoneDoesNotCount(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()
	account := accounts[0]
	p := Player{AccountID: account, Username: "agent"}
	run, _ := env.Store.ActiveRun(ctx, account)

	// The copy is taken first.
	onTarget(t, env, e, now, account, "fort-main")
	say(t, env, p, ctx, "cp /data/ledger.db")
	tickFor(env, e, now, time.Second)
	*now = now.Add(time.Second)

	if err := env.Store.AddMissions(ctx, []store.Mission{{
		RunID: run.ID, Kind: MissionSteal, Target: "fort-main", File: "/data/ledger.db",
		Client: "Somebody", Difficulty: "TRIVIAL", Reward: 900, ReputationGain: 1,
		State: store.MissionAvailable, CreatedAt: env.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	say(t, env, p, ctx, "mission accept 1")
	if out := say(t, env, p, ctx, "mission submit 1"); !strings.Contains(out, "before you took the contract") {
		t.Errorf("submitting work done beforehand:\n%s", out)
	}
}

// TestADestroyedFileIsGoneForThisRunAlone is the whole of the per-run
// overlay: the player's deletion is real for them and invisible to everybody
// else, and the catalogue is untouched.
func TestADestroyedFileIsGoneForThisRunAlone(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent", "other")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()
	account, other := accounts[0], accounts[1]
	p := Player{AccountID: account, Username: "agent"}
	run, _ := env.Store.ActiveRun(ctx, account)

	if err := env.Store.AddMissions(ctx, []store.Mission{{
		RunID: run.ID, Kind: MissionDestroy, Target: "fort-main", File: "/data/ledger.db",
		Client: "Somebody", Difficulty: "TRIVIAL", Reward: 700, ReputationGain: 1,
		State: store.MissionAvailable, CreatedAt: env.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	say(t, env, p, ctx, "mission accept 1")
	onTarget(t, env, e, now, account, "fort-main")
	if out := say(t, env, p, ctx, "mission submit 1"); !strings.Contains(out, "is still there") {
		t.Errorf("submitting before destroying anything:\n%s", out)
	}

	if out := say(t, env, p, ctx, "rm /data/ledger.db"); !strings.Contains(out, "is gone") {
		t.Fatalf("destroying the file:\n%s", out)
	}
	if out := say(t, env, p, ctx, "ls"); strings.Contains(out, "/data/ledger.db") {
		t.Errorf("the file is still listed after being destroyed:\n%s", out)
	}
	if out := say(t, env, p, ctx, "cat /data/ledger.db"); !strings.Contains(out, "No file at") {
		t.Errorf("a destroyed file can still be read:\n%s", out)
	}
	if out := say(t, env, p, ctx, "mission submit 1"); !strings.Contains(out, "Delivered") {
		t.Errorf("submitting after destroying it:\n%s", out)
	}

	// Another player's run sees the file exactly as it was.
	otherP := Player{AccountID: other, Username: "other"}
	onTarget(t, env, e, now, other, "fort-main")
	if out := say(t, env, otherP, ctx, "ls"); !strings.Contains(out, "/data/ledger.db") {
		t.Errorf("one player's deletion reached another player's world:\n%s", out)
	}
}

// TestADeadlineRunsOnTheClockOfPlay is what makes online time something to
// spend carefully: the deadline does not pass while the player is away.
func TestADeadlineRunsOnTheClockOfPlay(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	ctx := context.Background()
	account := accounts[0]
	p := Player{AccountID: account, Username: "agent"}
	run, _ := env.Store.ActiveRun(ctx, account)

	if err := env.Store.AddMissions(ctx, []store.Mission{{
		RunID: run.ID, Kind: MissionDestroy, Target: "fort-main", File: "/data/ledger.db",
		Client: "Somebody", Difficulty: "EASY", Reward: 2000, ReputationGain: 2,
		Deadline: 72 * GameHour, State: store.MissionAvailable, CreatedAt: env.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	say(t, env, p, ctx, "mission accept 1")
	if out := say(t, env, p, ctx, "missions"); !strings.Contains(out, "of play left") {
		t.Errorf("the board does not show the time left:\n%s", out)
	}

	// Three days of the game's clock pass while the player plays.
	if err := env.Store.AddExposure(ctx, []int64{account}, 72*GameHour+1); err != nil {
		t.Fatal(err)
	}
	if out := say(t, env, p, ctx, "mission submit 1"); !strings.Contains(out, "past the deadline") {
		t.Errorf("submitting out of time:\n%s", out)
	}
	missions, err := env.Store.MissionsOfRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if missions[0].State != store.MissionFailed {
		t.Errorf("the contract is %s, want FAILED", missions[0].State)
	}
}

// TestAContractTooBigIsRefused keeps the reputation gate real.
func TestAContractTooBigIsRefused(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	ctx := context.Background()
	account := accounts[0]
	p := Player{AccountID: account, Username: "agent"}
	run, _ := env.Store.ActiveRun(ctx, account)

	if err := env.Store.AddMissions(ctx, []store.Mission{{
		RunID: run.ID, Kind: MissionSteal, Target: "fort-main", File: "/data/ledger.db",
		Client: "Somebody", Difficulty: "INSANE", Reward: 20000, ReputationRequired: 25,
		ReputationGain: 8, State: store.MissionAvailable, CreatedAt: env.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if out := say(t, env, p, ctx, "mission accept 1"); !strings.Contains(out, "wants a reputation of 25") {
		t.Errorf("accepting beyond the player's name:\n%s", out)
	}
	if out := say(t, env, p, ctx, "mission accept 9"); !strings.Contains(out, "no contract 9") {
		t.Errorf("accepting a contract that does not exist:\n%s", out)
	}
	if out := say(t, env, p, ctx, "mission sideways 1"); !strings.Contains(out, "Usage") {
		t.Errorf("an invented subcommand:\n%s", out)
	}
}

// TestTheBoardAlwaysOffersSomethingWithinReach is a lock-out that only
// playing found. The draw reaches one band above the player's name on
// purpose, and a run of those filled the board with work nobody at that name
// could take; with the board full, nothing new was ever drawn, and the run
// could not go on.
func TestTheBoardAlwaysOffersSomethingWithinReach(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	ctx := context.Background()
	run, err := env.Store.ActiveRun(ctx, accounts[0])
	if err != nil {
		t.Fatal(err)
	}

	// A board full of work far beyond a new agent's name.
	var beyond []store.Mission
	for i := range MaxAvailable {
		beyond = append(beyond, store.Mission{
			RunID: run.ID, Kind: MissionSteal, Target: fmt.Sprintf("out-of-reach-%d", i),
			File: "/data/something.db", Client: "Somebody", Difficulty: "INSANE",
			Reward: 20000, ReputationRequired: 25, ReputationGain: 8,
			State: store.MissionAvailable, CreatedAt: env.Now(),
		})
	}
	if err := env.Store.AddMissions(ctx, beyond); err != nil {
		t.Fatal(err)
	}

	missions, err := env.fillBoard(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	within := 0
	for _, m := range missions {
		if m.State == store.MissionAvailable && m.ReputationRequired <= run.Reputation {
			within++
		}
	}
	if within == 0 {
		t.Fatalf("a board of %d contracts and not one a player of reputation %d could take",
			len(missions), run.Reputation)
	}
}
