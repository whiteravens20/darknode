// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/store/storetest"
	"github.com/whiteravens20/darknode/internal/world"
)

func testWorld(t *testing.T) *world.World {
	t.Helper()
	w, err := world.Load(fstest.MapFS{
		"world.yaml": {Data: []byte(`
epoch: 2049-01-01T00:00:00Z
start:
  links: [practice, home-bank]
  bank: home-bank
  software: [{ id: pass_breaker, version: 1 }]
`)},
		"software.yaml": {Data: []byte(`
software:
  - id: pass_breaker
    name: PassBreaker
    description: A test program.
    versions: [{ version: 1, price: 1000, size_mb: 2, cpu_mhz: 0, ram_mb: 0 }]
`)},
		"orgs/academy.yaml": {Data: []byte(`
id: academy
name: Practice Academy
type: DARKNODE_TEST
location: { x: 10, y: 10 }
servers: [{ id: practice, name: Practice Machine, ip: 10.0.0.1, security: 1 }]
`)},
		"orgs/bank.yaml": {Data: []byte(`
id: bank
name: Home Bank
type: BANK
location: { x: 20, y: 20 }
servers: [{ id: home-bank, name: Home Bank, ip: 172.16.0.1, security: 5 }]
`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func testEnv(t *testing.T) (*Env, Player) {
	t.Helper()
	s := storetest.Open(t)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, store.NewAccount{
		Username: "Tester", PasswordHash: "x", SessionHash: []byte("s"), SessionExpires: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	launched := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	env := &Env{
		Store: s,
		World: testWorld(t),
		Clock: Clock{Epoch: time.Date(2049, 1, 1, 0, 0, 0, 0, time.UTC), Launched: launched},
		Now:   func() time.Time { return launched.Add(90 * time.Second) },
	}
	return env, Player{AccountID: a.ID, Username: a.Username}
}

func text(r Result) string {
	var b strings.Builder
	for _, l := range r.Lines {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}

func TestTokenize(t *testing.T) {
	cases := map[string][]string{
		"":                       nil,
		"   ":                    nil,
		"status":                 {"status"},
		"  run   new ":           {"run", "new"},
		`cat "my file.txt"`:      {"cat", "my file.txt"},
		`say "a \"quoted\" one"`: {"say", `a "quoted" one`},
		`x ""`:                   {"x", ""},
	}
	for in, want := range cases {
		got, err := Tokenize(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Tokenize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Tokenize(`cat "open`); !errors.Is(err, ErrUnclosedQuote) {
		t.Errorf("an unclosed quote was accepted: %v", err)
	}
}

func TestCommandsWithoutARun(t *testing.T) {
	env, p := testEnv(t)
	ctx := context.Background()
	for _, line := range []string{"status", "servers"} {
		r, err := env.Execute(ctx, p, line)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text(r), "run new") || r.Lines[0].Kind != KindError {
			t.Errorf("%s without a run: %q", line, text(r))
		}
	}
	r, _ := env.Execute(ctx, p, "whoami")
	if text(r) != "Agent Tester\n" {
		t.Errorf("whoami = %q", text(r))
	}
	r, _ = env.Execute(ctx, p, "HELP")
	if !strings.Contains(text(r), "run new") || !strings.Contains(text(r), "logout") {
		t.Errorf("help does not list the commands: %q", text(r))
	}
	r, _ = env.Execute(ctx, p, "frobnicate now")
	if !strings.Contains(text(r), `Unknown command "frobnicate"`) {
		t.Errorf("unknown command: %q", text(r))
	}
	r, _ = env.Execute(ctx, p, "clear")
	if !strings.Contains(text(r), "handled by your terminal") {
		t.Errorf("a client command reached the server unexplained: %q", text(r))
	}
	r, _ = env.Execute(ctx, p, strings.Repeat("x", MaxLineLength+1))
	if !strings.Contains(text(r), "too long") {
		t.Errorf("an over-long line: %q", text(r))
	}
}

func TestRunNewThenStatusAndServers(t *testing.T) {
	env, p := testEnv(t)
	ctx := context.Background()
	r, err := env.Execute(ctx, p, "run new")
	if err != nil || !r.Changed || r.Lines[0].Kind != KindOK {
		t.Fatalf("run new: %+v %v", r, err)
	}
	r, _ = env.Execute(ctx, p, "run new")
	if !strings.Contains(text(r), "already have an active run") || r.Changed {
		t.Fatalf("second run new: %q", text(r))
	}

	r, err = env.Execute(ctx, p, "status")
	if err != nil {
		t.Fatal(err)
	}
	out := text(r)
	for _, want := range []string{
		"Agent       Tester",
		"World time  2049-01-01 01:30", // 90 real seconds after launch
		"Balance     $100 clean, $0 dirty",
		"CPU 2.0 GHz, RAM 4 GB, modem 100 Mbps, storage 256 GB, 2 NIC, security 1",
		"PassBreaker v1",
		"2 known servers",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}

	r, _ = env.Execute(ctx, p, "servers")
	out = text(r)
	if !strings.Contains(out, "10.0.0.1") || !strings.Contains(out, "Home Bank") || len(r.Lines) != 3 {
		t.Errorf("servers:\n%s", out)
	}
}

func TestSnapshot(t *testing.T) {
	env, p := testEnv(t)
	ctx := context.Background()
	s, err := env.Snapshot(ctx, p)
	if err != nil || s.Run != nil || s.Account.Username != "Tester" {
		t.Fatalf("snapshot without a run: %+v %v", s, err)
	}
	if err := env.StartRun(ctx, p); err != nil {
		t.Fatal(err)
	}
	s, err = env.Snapshot(ctx, p)
	if err != nil || s.Run == nil {
		t.Fatalf("snapshot with a run: %+v %v", s, err)
	}
	if s.Run.Balance.Clean != StartBalance || s.Run.Gateway != (GatewayView{2000, 4096, 100, 256000, 2, 1}) {
		t.Errorf("run view: %+v", s.Run)
	}
	if len(s.Run.Software) != 1 || s.Run.Software[0].Name != "PassBreaker" || s.Run.KnownServers != 2 {
		t.Errorf("software %+v, known %d", s.Run.Software, s.Run.KnownServers)
	}
}
