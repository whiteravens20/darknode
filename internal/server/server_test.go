// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/whiteravens20/darknode/internal/auth"
	"github.com/whiteravens20/darknode/internal/config"
	"github.com/whiteravens20/darknode/internal/game"
	"github.com/whiteravens20/darknode/internal/server"
	"github.com/whiteravens20/darknode/internal/store/storetest"
	"github.com/whiteravens20/darknode/internal/world"
)

// A tiny world, so the test does not depend on the catalogue that ships.
var testWorldFS = fstest.MapFS{
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
    description: A program.
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
}

// testServer is a running server with a database of its own, and a clock the
// test moves by hand.
type testServer struct {
	*httptest.Server
	t    *testing.T
	now  time.Time
	http *http.Client
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	db := storetest.Open(t)
	w, err := world.Load(testWorldFS)
	if err != nil {
		t.Fatal(err)
	}
	ts := &testServer{t: t, now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	now := func() time.Time { return ts.now }

	cfg := config.Config{
		Listen:       ":0",
		Secret:       []byte("0123456789abcdef0123456789abcdef"),
		Registration: config.RegistrationOpen,
		PowBits:      4, // cheap: the point here is the flow, not the cost
		CookieSecure: false,
	}
	env := &game.Env{
		Store: db,
		World: w,
		Clock: game.Clock{Epoch: w.Epoch, Launched: ts.now},
		Now:   now,
	}
	srv := server.New(server.Options{
		Config: cfg, Store: db, Game: env,
		// Cheap hashing: these tests check the flow, not the cost of a hash.
		Hasher: auth.NewHasher(auth.Argon2Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1}, 2),
		Puzzle: auth.NewPuzzle(auth.PuzzleConfig{
			BaseBits: 4, MaxBits: 6, TTL: time.Minute, MinAge: 2 * time.Second,
			MaxOpen: 100, MaxOpenPerKey: 50,
		}),
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version: "test",
		Now:     now,
	})

	ts.Server = httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	ts.http = &http.Client{Jar: jar}
	return ts
}

// do sends a request and returns the status and the decoded body.
func (ts *testServer) do(method, path, body string) (int, map[string]any) {
	ts.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		ts.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.http.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

// registerPlayer walks the whole registration: ask for a puzzle, solve it,
// wait out its minimum age, and send the form.
func (ts *testServer) registerPlayer(username, password string) map[string]any {
	ts.t.Helper()
	status, challenge := ts.do(http.MethodPost, "/api/auth/challenge", "{}")
	if status != http.StatusOK {
		ts.t.Fatalf("challenge: %d %v", status, challenge)
	}
	id, _ := challenge["id"].(string)
	bits, _ := challenge["bits"].(float64)
	minAge, _ := challenge["min_age_ms"].(float64)
	if minAge <= 0 {
		ts.t.Fatal("the challenge does not say its minimum age")
	}
	solution := auth.Solve(id, int(bits))

	ts.now = ts.now.Add(time.Duration(minAge) * time.Millisecond)
	body, _ := json.Marshal(map[string]string{
		"username": username, "password": password,
		"challenge": id, "solution": solution, "website": "",
	})
	status, out := ts.do(http.MethodPost, "/api/auth/register", string(body))
	if status != http.StatusOK {
		ts.t.Fatalf("register: %d %v", status, out)
	}
	return out
}

// command runs one terminal line and returns its output as text.
func (ts *testServer) command(line string) string {
	ts.t.Helper()
	body, _ := json.Marshal(map[string]string{"line": line})
	status, out := ts.do(http.MethodPost, "/api/cmd", string(body))
	if status != http.StatusOK {
		ts.t.Fatalf("command %q: %d %v", line, status, out)
	}
	lines, _ := out["lines"].([]any)
	var sb strings.Builder
	for _, l := range lines {
		m, _ := l.(map[string]any)
		text, _ := m["text"].(string)
		sb.WriteString(text + "\n")
	}
	return sb.String()
}

// TestTheWayIn is the milestone's exit path: register, see the session, start
// a run, read its status, sign out.
func TestTheWayIn(t *testing.T) {
	ts := newTestServer(t)

	out := ts.registerPlayer("zero_cool", "a-decent-password")
	codes, _ := out["recovery_codes"].([]any)
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(codes), auth.RecoveryCodeCount)
	}

	status, session := ts.do(http.MethodGet, "/api/session", "")
	if status != http.StatusOK || session["username"] != "zero_cool" {
		t.Fatalf("session: %d %v", status, session)
	}

	if got := ts.command("status"); !strings.Contains(got, "run new") {
		t.Fatalf("status before a run: %q", got)
	}
	if got := ts.command("run new"); !strings.Contains(got, "New run started") {
		t.Fatalf("run new: %q", got)
	}
	got := ts.command("status")
	for _, want := range []string{"zero_cool", "$100 clean", "PassBreaker v1", "2 known servers"} {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q:\n%s", want, got)
		}
	}
	if got := ts.command("servers"); !strings.Contains(got, "10.0.0.1") {
		t.Errorf("servers: %q", got)
	}

	if status, _ := ts.do(http.MethodPost, "/api/auth/logout", ""); status != http.StatusOK {
		t.Fatalf("logout: %d", status)
	}
	if status, _ := ts.do(http.MethodGet, "/api/session", ""); status != http.StatusUnauthorized {
		t.Fatalf("session after signing out: %d", status)
	}
}

func TestSignInAgainKeepsTheRun(t *testing.T) {
	ts := newTestServer(t)
	ts.registerPlayer("acid_burn", "a-decent-password")
	ts.command("run new")
	ts.do(http.MethodPost, "/api/auth/logout", "")

	// The name is matched without case.
	status, _ := ts.do(http.MethodPost, "/api/auth/login", `{"username":"ACID_BURN","password":"a-decent-password"}`)
	if status != http.StatusOK {
		t.Fatalf("sign in: %d", status)
	}
	if got := ts.command("status"); !strings.Contains(got, "$100 clean") {
		t.Fatalf("the run did not survive: %q", got)
	}
	if got := ts.command("run new"); !strings.Contains(got, "already have an active run") {
		t.Fatalf("a second run was allowed: %q", got)
	}
}

func TestWrongPasswordAndUnknownNameAnswerAlike(t *testing.T) {
	ts := newTestServer(t)
	ts.registerPlayer("player", "a-decent-password")
	ts.do(http.MethodPost, "/api/auth/logout", "")

	_, wrong := ts.do(http.MethodPost, "/api/auth/login", `{"username":"player","password":"not-it"}`)
	_, unknown := ts.do(http.MethodPost, "/api/auth/login", `{"username":"nobody","password":"not-it"}`)
	if wrong["message"] != unknown["message"] || wrong["message"] == "" {
		t.Fatalf("the answers differ: %v vs %v", wrong, unknown)
	}
}

func TestRecoveryCodeWorksOnce(t *testing.T) {
	ts := newTestServer(t)
	out := ts.registerPlayer("player", "a-decent-password")
	codes, _ := out["recovery_codes"].([]any)
	code, _ := codes[0].(string)
	ts.do(http.MethodPost, "/api/auth/logout", "")

	body, _ := json.Marshal(map[string]string{"username": "player", "code": code, "password": "a-brand-new-one"})
	status, result := ts.do(http.MethodPost, "/api/auth/recover", string(body))
	if status != http.StatusOK {
		t.Fatalf("recover: %d %v", status, result)
	}
	if left, _ := result["codes_left"].(float64); int(left) != auth.RecoveryCodeCount-1 {
		t.Errorf("codes left = %v, want %d", result["codes_left"], auth.RecoveryCodeCount-1)
	}
	// The browser is signed in, and the new password works.
	if status, _ := ts.do(http.MethodGet, "/api/session", ""); status != http.StatusOK {
		t.Errorf("not signed in after recovery: %d", status)
	}
	ts.do(http.MethodPost, "/api/auth/logout", "")
	if status, _ := ts.do(http.MethodPost, "/api/auth/login", `{"username":"player","password":"a-brand-new-one"}`); status != http.StatusOK {
		t.Errorf("the new password does not work: %d", status)
	}
	ts.do(http.MethodPost, "/api/auth/logout", "")

	// The same code a second time is refused.
	status, _ = ts.do(http.MethodPost, "/api/auth/recover", string(body))
	if status != http.StatusUnauthorized {
		t.Errorf("a used code was accepted again: %d", status)
	}
}

func TestProtectedEndpointsNeedASession(t *testing.T) {
	ts := newTestServer(t)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/session", ""},
		{http.MethodPost, "/api/cmd", `{"line":"status"}`},
		{http.MethodGet, "/api/stream", ""},
	} {
		if status, _ := ts.do(c.method, c.path, c.body); status != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d, want 401", c.method, c.path, status)
		}
	}
}

func TestRequestsFromAnotherSiteAreRefused(t *testing.T) {
	ts := newTestServer(t)
	ts.registerPlayer("player", "a-decent-password")

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/cmd", strings.NewReader(`{"line":"status"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://somewhere-else.example")
	resp, err := ts.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a request from another site was allowed: %d", resp.StatusCode)
	}
}

func TestSecurityHeadersAndHealth(t *testing.T) {
	ts := newTestServer(t)
	resp, err := ts.http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: %d", resp.StatusCode)
	}
	for header, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
	} {
		if got := resp.Header.Get(header); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", header, got, want)
		}
	}
}

func TestBadBodiesAreRefused(t *testing.T) {
	ts := newTestServer(t)
	cases := map[string]struct {
		body        string
		contentType bool
	}{
		"not json":        {"{", true},
		"unknown field":   {`{"username":"a","password":"b","nonsense":1}`, true},
		"no content type": {`{"username":"a","password":"b"}`, false},
		"too large":       {`{"username":"` + strings.Repeat("x", 70*1024) + `"}`, true},
	}
	for name, tc := range cases {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		if tc.contentType {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := ts.http.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, resp.StatusCode)
		}
	}
}
