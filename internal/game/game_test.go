// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"errors"
	"reflect"
	"slices"
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
  links: [practice, home-bank, relay-1, relay-2, relay-3, registry, fort-main]
  bank: home-bank
  software:
    - { id: pass_breaker, version: 1 }
    - { id: address_probe, version: 1 }
    - { id: firewall_opener, version: 1 }
    - { id: proxy_opener, version: 1 }
    - { id: monitor_mask, version: 1 }
`)},
		"software.yaml": {Data: []byte(`
software:
  - id: pass_breaker
    name: PassBreaker
    description: A test program.
    versions: [{ version: 1, price: 1000, size_mb: 2, cpu_mhz: 0, ram_mb: 0 }]
  - id: address_probe
    name: AddressProbe
    description: Looks a server up before you connect.
    versions: [{ version: 1, price: 1500, size_mb: 1, cpu_mhz: 0, ram_mb: 0 }]
  - id: firewall_opener
    name: FirewallOpener
    description: Opens the firewall.
    versions: [{ version: 1, price: 1500, size_mb: 2, cpu_mhz: 0, ram_mb: 0 }]
  - id: proxy_opener
    name: ProxyOpener
    description: Opens the proxy.
    versions: [{ version: 1, price: 2000, size_mb: 2, cpu_mhz: 0, ram_mb: 0 }]
  - id: monitor_mask
    name: MonitorMask
    description: Hides a connection from a monitor.
    versions: [{ version: 1, price: 2500, size_mb: 2, cpu_mhz: 0, ram_mb: 0 }]
  - id: decrypter
    name: Decrypter
    description: Heavy on the processor and the memory.
    versions: [{ version: 1, price: 3000, size_mb: 3, cpu_mhz: 3000, ram_mb: 8192 }]
`)},
		"orgs/academy.yaml": {Data: []byte(`
id: academy
name: Practice Academy
type: DARKNODE_TEST
location: { x: 10, y: 10 }
servers:
  - id: practice
    name: Practice Machine
    ip: 10.0.0.1
    security: 1
    files:
      - { path: /readme.txt, size_mb: 1, content: "Nothing here matters.\nTry your programs." }
      - { path: /notes/plans.txt, size_mb: 2, sensitive: true, content: What the owner would rather keep. }
`)},
		"orgs/bank.yaml": {Data: []byte(`
id: bank
name: Home Bank
type: BANK
location: { x: 20, y: 20 }
servers: [{ id: home-bank, name: Home Bank, ip: 172.16.0.1, security: 5, firewall: true }]
`)},
		"orgs/carrier.yaml": {Data: []byte(`
id: carrier
name: Test Carrier
type: ISP
location: { x: 30, y: 30 }
servers:
  - { id: relay-1, name: Test Relay One, ip: 203.0.113.50, security: 2 }
  - { id: relay-2, name: Test Relay Two, ip: 203.0.113.51, security: 2 }
  - { id: relay-3, name: Test Relay Three, ip: 203.0.113.52, security: 2 }
`)},
		"orgs/fort.yaml": {Data: []byte(`
id: fort
name: Fort Holdings
type: CORPORATE
location: { x: 50, y: 50 }
servers:
  - id: fort-main
    name: Fort Holdings
    ip: 172.16.0.9
    security: 2
    proxy: true
    firewall: true
    monitor: true
    files:
      - { path: /data/ledger.db, size_mb: 4, sensitive: true, content: The ledger. }
`)},
		"orgs/registry.yaml": {Data: []byte(`
id: registry
name: Test Registry
type: INTERNIC
location: { x: 40, y: 40 }
servers:
  - id: registry
    name: Test Registry
    ip: 10.10.10.50
    security: 4
    files:
      - { path: /pub/about.txt, size_mb: 1, content: The public register of every organisation. }
      - { path: /data/registry.db, size_mb: 9, sensitive: true, content: The entries nobody paid to publish. }
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
	// The commands that build a route or open a connection work on live
	// state, which the loop holds, so a test of them needs one.
	NewEngine(env, &recorder{}, quietLogger())
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

// requote writes a token as a quoted word, so that Tokenize reads it back
// unchanged whatever is inside it.
func requote(token string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range token {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// FuzzTokenize throws anything at the one piece of the game that reads what a
// player typed. Tokenize stands in front of every command, so a line that
// makes it panic would take the request down, and a line it reads two
// different ways would make a command do something else than it says.
//
// The property: whatever Tokenize found, writing those words back as quoted
// words and reading them again gives the same words. That is what keeps a
// file name with a space, a quote or a backslash in it whole on its way
// through the terminal.
func FuzzTokenize(f *testing.F) {
	for _, seed := range []string{
		"", "   ", "status", "run new", "  run   new ",
		`cat "my file.txt"`, `say "a \"quoted\" one"`, `cat "back\\slash"`,
		`x ""`, `cat "open`, "\t\ttabs\t", "ęóąśłżźćń", "\x00\x01",
		strings.Repeat("a ", 100), strings.Repeat(`"`, 9),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		tokens, err := Tokenize(line)
		if err != nil {
			if !errors.Is(err, ErrUnclosedQuote) {
				t.Fatalf("Tokenize(%q) failed with an unexpected error: %v", line, err)
			}
			if tokens != nil {
				t.Fatalf("Tokenize(%q) returned %q together with an error", line, tokens)
			}
			return
		}
		written := make([]string, 0, len(tokens))
		for _, token := range tokens {
			written = append(written, requote(token))
		}
		again, err := Tokenize(strings.Join(written, " "))
		if err != nil {
			t.Fatalf("Tokenize(%q) = %q; written back as %s it no longer parses: %v",
				line, tokens, strings.Join(written, " "), err)
		}
		if !slices.Equal(again, tokens) {
			t.Fatalf("Tokenize(%q) = %q; written back and read again it is %q", line, tokens, again)
		}
	})
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
		"7 known servers",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}

	// Until a server is probed, the player sees the address and the name and
	// nothing else: not the type, not the security, not the layers.
	r, _ = env.Execute(ctx, p, "servers")
	out = text(r)
	for _, want := range []string{"10.0.0.1", "Home Bank", "not probed"} {
		if !strings.Contains(out, want) {
			t.Errorf("servers lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "BANK") || strings.Contains(out, "security 5") {
		t.Errorf("servers gives away what has not been probed:\n%s", out)
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
	if len(s.Run.Software) != 5 || s.Run.Software[0].Name != "AddressProbe" || s.Run.KnownServers != 7 {
		t.Errorf("software %+v, known %d", s.Run.Software, s.Run.KnownServers)
	}
}
