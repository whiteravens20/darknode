// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
)

// TestToolTimeMatchesTheBalanceTable pins the numbers the whole game is tuned
// against. Every deadline in it — the trace, the administrator's patience,
// what a starting gateway can and cannot take — was worked out from these,
// so a change here is a change to every one of them.
func TestToolTimeMatchesTheBalanceTable(t *testing.T) {
	const tolerance = 0.01
	for _, c := range []struct {
		what                              string
		security, version, cpuMHz, shared int
		want                              float64
	}{
		// The starting gateway: 2000 MHz, one program at a time, version 1.
		{"starting gateway against security 4", 4, 1, 2000, 1, 8.0},
		{"starting gateway against security 6", 6, 1, 2000, 1, 12.0},
		{"starting gateway against security 8", 8, 1, 2000, 1, 16.0},
		// The upgraded one of the balance tables: 6000 MHz and version 2.
		{"upgraded gateway against security 4", 4, 2, 6000, 1, 2.133},
		{"upgraded gateway against security 6", 6, 2, 6000, 1, 3.2},
		{"upgraded gateway against security 8", 8, 2, 6000, 1, 4.267},
		// A faster processor always helps, with no floor anywhere.
		{"security 8, version 1, 2000 MHz", 8, 1, 2000, 1, 16.0},
		{"security 8, version 1, 4000 MHz", 8, 1, 4000, 1, 8.0},
		{"security 8, version 1, 6000 MHz", 8, 1, 6000, 1, 5.333},
		{"security 8, version 1, 10000 MHz", 8, 1, 10000, 1, 3.2},
		// Sharing the processor is a limit, not a speed-up: two programs at
		// once take twice as long each.
		{"two programs at once", 4, 1, 2000, 2, 16.0},
		{"four programs at once", 4, 1, 2000, 4, 32.0},
		// A newer version saves a fifth of the work each time.
		{"version 3 against security 8", 8, 3, 2000, 1, 9.6},
	} {
		got := ToolTime(c.security, c.version, c.cpuMHz, c.shared).Seconds()
		if math.Abs(got-c.want) > tolerance {
			t.Errorf("%s: %.3f s, want %.3f s", c.what, got, c.want)
		}
	}
}

// TestMaxParallelFollowsTheMemory is what stops a player from starting
// everything at once: the gateway's memory, one slot each.
func TestMaxParallelFollowsTheMemory(t *testing.T) {
	for ram, want := range map[int]int{2048: 1, 4096: 2, 8192: 4, 16384: 8, 1024: 1} {
		if got := maxParallel(store.Gateway{RAMMB: ram}); got != want {
			t.Errorf("%d MB of memory holds %d programs, want %d", ram, got, want)
		}
	}
}

// TestTheWayInIsOpenedFromTheOutsideIn is the order the server enforces: the
// proxy, then the firewall, then the password, and the monitor whenever you
// like. The browser never decides this.
func TestTheWayInIsOpenedFromTheOutsideIn(t *testing.T) {
	env, p, ctx := start(t)
	say(t, env, p, ctx, "connect 172.16.0.9") // proxy, firewall, monitor, security 2

	if out := say(t, env, p, ctx, "run pass_breaker"); !strings.Contains(out, "proxy") {
		t.Errorf("the password was attacked through a closed proxy:\n%s", out)
	}
	if out := say(t, env, p, ctx, "run firewall_opener"); !strings.Contains(out, "proxy") {
		t.Errorf("the firewall was attacked through a closed proxy:\n%s", out)
	}
	// The monitor stands outside the order, so it can be masked at once.
	if out := say(t, env, p, ctx, "run monitor_mask"); !strings.Contains(out, "MonitorMask v1 running") {
		t.Errorf("masking the monitor first:\n%s", out)
	}
	if out := say(t, env, p, ctx, "run monitor_mask"); !strings.Contains(out, "already running") {
		t.Errorf("the same program twice:\n%s", out)
	}
	// Two fit in the starting gateway's memory; a third does not.
	if out := say(t, env, p, ctx, "run proxy_opener"); !strings.Contains(out, "ProxyOpener v1 running") {
		t.Errorf("opening the proxy:\n%s", out)
	}
	if out := say(t, env, p, ctx, "run firewall_opener"); !strings.Contains(out, "2 program(s) fit") {
		t.Errorf("a third program in a two-slot gateway:\n%s", out)
	}
	// What the gateway cannot run at all is refused before it starts.
	if out := say(t, env, p, ctx, "run decrypter"); !strings.Contains(out, "do not have") {
		t.Errorf("a program the run does not own:\n%s", out)
	}
	if out := say(t, env, p, ctx, "run nonsense"); !strings.Contains(out, "No program is called") {
		t.Errorf("a program nobody wrote:\n%s", out)
	}
	if out := say(t, env, p, ctx, "stop proxy_opener"); !strings.Contains(out, "Stopped") {
		t.Errorf("stopping a program:\n%s", out)
	}
	if out := say(t, env, p, ctx, "stop proxy_opener"); !strings.Contains(out, "not running") {
		t.Errorf("stopping it twice:\n%s", out)
	}
}

// TestAProgramFinishesInTheLoopAndOpensItsLayer is the whole of what a
// program does: it takes processor time, the loop gives it that time, and
// when it has had enough the layer is open — the password layer being the one
// that gives root.
func TestAProgramFinishesInTheLoopAndOpensItsLayer(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	out := &recorder{}
	e := NewEngine(env, out, quietLogger())
	account := accounts[0]
	target, ok := env.World.ServerByID("practice") // security 1, no layers but the password
	if !ok {
		t.Fatal("the test world has no practice machine")
	}
	run, err := env.Store.ActiveRun(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	breaker, _ := env.World.SoftwareByID("pass_breaker")

	e.Join(account)
	if _, err := e.Connect(account, target, run.Gateway); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.step(Tick, env.Now())
	if _, err := e.StartTool(account, run, breaker, 1); err != nil {
		t.Fatal(err)
	}

	// Security 1 at 2000 MHz with one program running: two seconds of work.
	want := ToolTime(target.Security, 1, run.Gateway.CPUMHz, 1)
	if want != 2*time.Second {
		t.Fatalf("the practice machine takes %s, want 2s", want)
	}
	for range int(want/Tick) - 1 {
		*now = now.Add(Tick)
		e.step(Tick, env.Now())
	}
	if conn := e.Connection(account); conn.Root {
		t.Fatal("root before the program had finished")
	}
	*now = now.Add(Tick)
	e.step(Tick, env.Now())

	conn := e.Connection(account)
	if !conn.Root {
		t.Error("the password layer did not give root when the program finished")
	}
	if len(conn.tools) != 0 {
		t.Errorf("%d program(s) still running after finishing", len(conn.tools))
	}
	// The browser was told, and the last thing it was told carries no
	// running programs and the layer open.
	sent := out.all()
	if len(sent) == 0 {
		t.Fatal("nothing was published while a program ran")
	}
	last, ok := sent[len(sent)-1].delta.(ConnectionDelta)
	if !ok {
		t.Fatalf("the last delta is a %T", sent[len(sent)-1].delta)
	}
	if !last.Connection.Root || len(last.Connection.Tools) != 0 {
		t.Errorf("the last delta says root=%v, %d running", last.Connection.Root, len(last.Connection.Tools))
	}
}

// TestTwoProgramsEachTakeTwiceAsLong is the shared processor as a limit: what
// runs together runs slower, each of them.
func TestTwoProgramsEachTakeTwiceAsLong(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	account := accounts[0]
	target, _ := env.World.ServerByID("fort-main") // security 2, with a monitor
	run, err := env.Store.ActiveRun(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := env.World.SoftwareByID("proxy_opener")
	mask, _ := env.World.SoftwareByID("monitor_mask")

	e.Join(account)
	if _, err := e.Connect(account, target, run.Gateway); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.step(Tick, env.Now())
	if _, err := e.StartTool(account, run, proxy, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTool(account, run, mask, 1); err != nil {
		t.Fatal(err)
	}

	// Alone each would need four seconds; together they need eight, so after
	// four they are halfway.
	alone := ToolTime(target.Security, 1, run.Gateway.CPUMHz, 1)
	for range int(alone / Tick) {
		*now = now.Add(Tick)
		e.step(Tick, env.Now())
	}
	view := e.ConnectionView(account)
	if len(view.Tools) != 2 {
		t.Fatalf("%d program(s) running, want 2", len(view.Tools))
	}
	for _, tool := range view.Tools {
		if math.Abs(tool.Progress-0.5) > 0.02 {
			t.Errorf("%s is %.0f%% done after the time one program alone would need, want about 50%%",
				tool.Name, tool.Progress*100)
		}
	}
}

// TestLeavingClosesWhatWasOpened is the rule that makes a break-in cost
// something every time: the layers belong to the connection, not to the run.
func TestLeavingClosesWhatWasOpened(t *testing.T) {
	env, now, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	account := accounts[0]
	target, _ := env.World.ServerByID("practice")
	run, _ := env.Store.ActiveRun(context.Background(), account)
	breaker, _ := env.World.SoftwareByID("pass_breaker")

	e.Join(account)
	if _, err := e.Connect(account, target, run.Gateway); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Handshake)
	e.step(Tick, env.Now())
	if _, err := e.StartTool(account, run, breaker, 1); err != nil {
		t.Fatal(err)
	}
	for range 25 {
		*now = now.Add(Tick)
		e.step(Tick, env.Now())
	}
	if conn := e.Connection(account); !conn.Root {
		t.Fatal("no root after two and a half seconds on a security 1 machine")
	}

	if _, err := e.Disconnect(account); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Connect(account, target, run.Gateway); err != nil {
		t.Fatal(err)
	}
	conn := e.Connection(account)
	if conn.Root {
		t.Error("coming back to a server kept the root from the last visit")
	}
	if len(conn.tools) != 0 {
		t.Error("a new connection carries the programs of the old one")
	}
}
