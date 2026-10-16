// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/whiteravens20/darknode/internal/legal"
	"github.com/whiteravens20/darknode/internal/store"
)

// Kinds of output line, which the terminal colours.
const (
	KindPlain = ""
	KindHead  = "head"
	KindDim   = "dim"
	KindOK    = "ok"
	KindError = "error"
)

// Line is one line of a command's output.
type Line struct {
	Text string `json:"text"`
	Kind string `json:"kind,omitempty"`
}

// Result is what a command printed, and whether it changed the player's
// state, in which case the server sends a new snapshot.
type Result struct {
	Lines   []Line
	Changed bool
}

// MaxLineLength is the longest command line accepted, in bytes.
const MaxLineLength = 1000

// ClientCommands are handled by the terminal itself and never reach the server.
var ClientCommands = []string{"clear", "history", "logout"}

type command struct {
	name    string
	usage   string
	summary string
	needRun bool
	run     func(e *Env, ctx context.Context, p Player, args []string) (Result, error)
}

var commands []command

func init() {
	commands = []command{
		{name: "help", usage: "help", summary: "list the commands", run: (*Env).help},
		{name: "whoami", usage: "whoami", summary: "show your agent name", run: (*Env).whoami},
		{name: "status", usage: "status", summary: "your money, hardware and software", needRun: true, run: (*Env).status},
		{name: "servers", usage: "servers", summary: "the servers in your address book", needRun: true, run: (*Env).servers},
		{name: "run", usage: "run new", summary: "start a new run", run: (*Env).runCmd},
		{name: "about", usage: "about", summary: "licence, source code and credits", run: (*Env).about},
	}
}

// Execute runs one command line for the player.
func (e *Env) Execute(ctx context.Context, p Player, line string) (Result, error) {
	if len(line) > MaxLineLength {
		return errorResult("That line is too long."), nil
	}
	args, err := Tokenize(line)
	if errors.Is(err, ErrUnclosedQuote) {
		return errorResult("A quote is not closed."), nil
	}
	if err != nil {
		return Result{}, err
	}
	if len(args) == 0 {
		return Result{}, nil
	}
	name := strings.ToLower(args[0])
	for _, c := range ClientCommands {
		if name == c {
			return errorResult(fmt.Sprintf("%q is handled by your terminal.", name)), nil
		}
	}
	for _, c := range commands {
		if c.name != name {
			continue
		}
		if c.needRun {
			r, err := e.Store.ActiveRun(ctx, p.AccountID)
			if err != nil {
				return Result{}, err
			}
			if r == nil {
				return errorResult("You have no active run. Type `run new` to start one."), nil
			}
		}
		return c.run(e, ctx, p, args[1:])
	}
	return errorResult(fmt.Sprintf("Unknown command %q. Type `help` for the list.", args[0])), nil
}

func (e *Env) help(_ context.Context, _ Player, _ []string) (Result, error) {
	r := Result{Lines: []Line{{Text: "COMMANDS", Kind: KindHead}}}
	for _, c := range commands {
		r.Lines = append(r.Lines, Line{Text: fmt.Sprintf("  %-12s %s", c.usage, c.summary)})
	}
	r.Lines = append(r.Lines,
		Line{Text: fmt.Sprintf("  %-12s %s", "clear", "clear the screen")},
		Line{Text: fmt.Sprintf("  %-12s %s", "history", "the commands you typed")},
		Line{Text: fmt.Sprintf("  %-12s %s", "logout", "sign out")},
	)
	return r, nil
}

func (e *Env) whoami(_ context.Context, p Player, _ []string) (Result, error) {
	return Result{Lines: []Line{{Text: "Agent " + p.Username}}}, nil
}

func (e *Env) status(ctx context.Context, p Player, _ []string) (Result, error) {
	s, err := e.Snapshot(ctx, p)
	if err != nil {
		return Result{}, err
	}
	run := s.Run
	if run == nil {
		return errorResult("You have no active run. Type `run new` to start one."), nil
	}
	g := run.Gateway
	var sw []string
	for _, x := range run.Software {
		sw = append(sw, fmt.Sprintf("%s v%d", x.Name, x.Version))
	}
	if len(sw) == 0 {
		sw = []string{"none"}
	}
	row := func(label, value string) Line { return Line{Text: fmt.Sprintf("  %-11s %s", label, value)} }
	return Result{Lines: []Line{
		{Text: "STATUS", Kind: KindHead},
		row("Agent", p.Username),
		row("World time", FormatWorldTime(s.WorldTime)),
		row("Time online", FormatGameDuration(run.ExposureSeconds)),
		row("Balance", fmt.Sprintf("$%d clean, $%d dirty", run.Balance.Clean, run.Balance.Dirty)),
		row("Reputation", fmt.Sprint(run.Reputation)),
		row("Gateway", fmt.Sprintf("CPU %.1f GHz, RAM %d GB, modem %d Mbps, storage %d GB, %d NIC, security %d",
			float64(g.CPUMHz)/1000, g.RAMMB/1024, g.ModemMbps, g.StorageMB/1000, g.NICCount, g.Security)),
		row("Software", strings.Join(sw, ", ")),
		row("Links", fmt.Sprintf("%d known servers; type `servers`", run.KnownServers)),
	}}, nil
}

func (e *Env) servers(ctx context.Context, p Player, _ []string) (Result, error) {
	r, err := e.Store.ActiveRun(ctx, p.AccountID)
	if err != nil {
		return Result{}, err
	}
	out := Result{Lines: []Line{{Text: fmt.Sprintf("  %-16s %-30s %s", "ADDRESS", "NAME", "TYPE"), Kind: KindHead}}}
	for _, id := range r.Known {
		s, ok := e.World.ServerByID(id)
		if !ok {
			continue // a server removed from the catalogue since the run began
		}
		out.Lines = append(out.Lines, Line{Text: fmt.Sprintf("  %-16s %-30s %s", s.IP, s.Name, s.Type())})
	}
	return out, nil
}

func (e *Env) runCmd(ctx context.Context, p Player, args []string) (Result, error) {
	if len(args) != 1 || strings.ToLower(args[0]) != "new" {
		return errorResult("Usage: run new"), nil
	}
	err := e.StartRun(ctx, p)
	if errors.Is(err, store.ErrRunActive) {
		return errorResult("You already have an active run."), nil
	}
	if err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Lines: []Line{
		{Text: "New run started. Your gateway is online.", Kind: KindOK},
		{Text: "Type `status` to see what you have.", Kind: KindDim},
	}}, nil
}

func (e *Env) about(_ context.Context, _ Player, _ []string) (Result, error) {
	r := Result{}
	for _, l := range legal.Lines() {
		r.Lines = append(r.Lines, Line{Text: l})
	}
	return r, nil
}

func errorResult(msg string) Result {
	return Result{Lines: []Line{{Text: msg, Kind: KindError}}}
}

// ErrUnclosedQuote is returned by Tokenize for a line with an odd number of quotes.
var ErrUnclosedQuote = errors.New("unclosed quote")

// Tokenize splits a command line into words. Double quotes keep spaces inside
// one word; a backslash inside quotes escapes the next character.
func Tokenize(line string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord, quoted, escaped := false, false, false
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quoted && r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
			inWord = true
		case !quoted && (r == ' ' || r == '\t'):
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quoted {
		return nil, ErrUnclosedQuote
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}
