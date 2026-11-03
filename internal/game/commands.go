// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/whiteravens20/darknode/internal/legal"
	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
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

// cmd is what a command is given: who typed it, the words after its name, and
// the state it said it needs.
type cmd struct {
	p    Player
	args []string
	// run is the player's active run, for a command that needs one.
	run *store.Run
	// conn is the live connection, for a command that needs one.
	conn *Connection
}

type command struct {
	name    string
	usage   string
	summary string
	// needRun loads the active run and refuses the command without one.
	needRun bool
	// needConn also requires a live connection. It implies needRun.
	needConn bool
	run      func(e *Env, ctx context.Context, c cmd) (Result, error)
}

var commands []command

func init() {
	commands = []command{
		{name: "help", usage: "help", summary: "list the commands", run: (*Env).help},
		{name: "whoami", usage: "whoami", summary: "show your agent name", run: (*Env).whoami},
		{name: "status", usage: "status", summary: "your money, hardware and software", needRun: true, run: (*Env).status},
		{name: "servers", usage: "servers", summary: "the addresses you know", needRun: true, run: (*Env).servers},
		{name: "lookup", usage: "lookup <text>", summary: "find an address by name, or a name by address", needRun: true, run: (*Env).lookup},
		{name: "probe", usage: "probe <ip>", summary: "see a server's type and defences before connecting", needRun: true, run: (*Env).probe},
		{name: "bounce", usage: "bounce …", summary: "build the route your connection takes", needRun: true, run: (*Env).bounce},
		{name: "connect", usage: "connect <ip>", summary: "connect to a server through your route", needRun: true, run: (*Env).connect},
		{name: "disconnect", usage: "disconnect", summary: "close the connection", needConn: true, run: (*Env).disconnect},
		{name: "links", usage: "links [page]", summary: "read the register (connect to it first)", needConn: true, run: (*Env).links},
		{name: "trace", usage: "trace", summary: "how far anybody has got in finding you", needRun: true, run: (*Env).traceCmd},
		{name: "log", usage: "log [wipe]", summary: "the lines you left here, and how to clear them", needConn: true, run: (*Env).logCmd},
		{name: "ls", usage: "ls", summary: "the files on the server you are on", needConn: true, run: (*Env).ls},
		{name: "cat", usage: "cat <path>", summary: "read a file", needConn: true, run: (*Env).cat},
		{name: "cp", usage: "cp <path>", summary: "copy a file to your gateway", needConn: true, run: (*Env).cp},
		{name: "rm", usage: "rm <path>", summary: "destroy a file", needConn: true, run: (*Env).rm},
		{name: "missions", usage: "missions", summary: "the contracts on the board and yours", needRun: true, run: (*Env).missions},
		{name: "mission", usage: "mission …", summary: "accept, submit or abandon a contract", needRun: true, run: (*Env).missionCmd},
		{name: "tools", usage: "tools", summary: "the programs on your gateway", needRun: true, run: (*Env).tools},
		{name: "bank", usage: "bank", summary: "your accounts and what is in them", needRun: true, run: (*Env).bank},
		{name: "shop", usage: "shop [buy …]", summary: "the programs for sale, and buying one", needRun: true, run: (*Env).shop},
		{name: "run", usage: "run new|<program>", summary: "start a run, or set a program to work", run: (*Env).runCmd},
		{name: "stop", usage: "stop <program>", summary: "stop a program and free the processor", needConn: true, run: (*Env).stop},
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
		call := cmd{p: p, args: args[1:]}
		if c.needRun || c.needConn {
			r, err := e.Store.ActiveRun(ctx, p.AccountID)
			if err != nil {
				return Result{}, err
			}
			if r == nil {
				return errorResult("You have no active run. Type `run new` to start one."), nil
			}
			call.run = r
		}
		if c.needConn {
			live, err := e.live()
			if err != nil {
				return Result{}, err
			}
			call.conn = live.Connection(p.AccountID)
			if call.conn == nil {
				return errorResult("You are not connected to anything. Type `connect <ip>`."), nil
			}
		}
		return c.run(e, ctx, call)
	}
	return errorResult(fmt.Sprintf("Unknown command %q. Type `help` for the list.", args[0])), nil
}

// live returns the simulation loop, which holds the route and the connection.
// A served instance always has one; a game built without one cannot connect.
func (e *Env) live() (*Engine, error) {
	if e.Engine == nil {
		return nil, errors.New("the game is running without a simulation loop")
	}
	return e.Engine, nil
}

func (e *Env) help(_ context.Context, _ cmd) (Result, error) {
	r := Result{Lines: []Line{{Text: "COMMANDS", Kind: KindHead}}}
	for _, c := range commands {
		r.Lines = append(r.Lines, Line{Text: fmt.Sprintf("  %-14s %s", c.usage, c.summary)})
	}
	r.Lines = append(r.Lines,
		Line{Text: fmt.Sprintf("  %-14s %s", "clear", "clear the screen")},
		Line{Text: fmt.Sprintf("  %-14s %s", "history", "the commands you typed")},
		Line{Text: fmt.Sprintf("  %-14s %s", "logout", "sign out")},
	)
	return r, nil
}

func (e *Env) whoami(_ context.Context, c cmd) (Result, error) {
	return Result{Lines: []Line{{Text: "Agent " + c.p.Username}}}, nil
}

func (e *Env) status(ctx context.Context, c cmd) (Result, error) {
	s, err := e.Snapshot(ctx, c.p)
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
	lines := []Line{
		{Text: "STATUS", Kind: KindHead},
		row("Agent", c.p.Username),
		row("World time", FormatWorldTime(s.WorldTime)),
		row("Time online", FormatGameDuration(run.ExposureSeconds)),
		row("Balance", fmt.Sprintf("$%d clean, $%d dirty", run.Balance.Clean, run.Balance.Dirty)),
		row("Reputation", fmt.Sprint(run.Reputation)),
		row("Gateway", fmt.Sprintf("CPU %.1f GHz, RAM %d GB, modem %d Mbps, storage %d GB, %d NIC, security %d",
			float64(g.CPUMHz)/1000, g.RAMMB/1024, g.ModemMbps, g.StorageMB/1000, g.NICCount, g.Security)),
		row("Software", strings.Join(sw, ", ")),
		row("Links", fmt.Sprintf("%d known servers; type `servers`", run.KnownServers)),
		row("Route", routeText(run.Route)),
	}
	if conn := run.Connection; conn != nil {
		access := "no root"
		if conn.Root {
			access = "root"
		}
		lines = append(lines, row("Connection",
			fmt.Sprintf("%s (%s) — %s, %s", conn.Target.Name, conn.Target.IP, strings.ToLower(conn.State), access)))
		if len(conn.Layers) > 0 {
			var layers []string
			for _, l := range conn.Layers {
				state := "closed"
				if l.Open {
					state = "open"
				}
				layers = append(layers, fmt.Sprintf("%s %s", l.Name, state))
			}
			lines = append(lines, row("Layers", strings.Join(layers, ", ")))
		}
		for _, tool := range conn.Tools {
			lines = append(lines, row("Running",
				fmt.Sprintf("%s v%d — %.0f%%, about %.1f s left", tool.Name, tool.Version, tool.Progress*100, tool.Left)))
		}
	} else {
		lines = append(lines, row("Connection", "none"))
	}
	if taken, err := e.gatewayFiles(ctx, c.run); err == nil && len(taken) > 0 {
		var names []string
		for _, f := range taken {
			names = append(names, f.Path)
		}
		lines = append(lines, row("On gateway", strings.Join(names, ", ")))
	}
	if tr := run.Trace; tr != nil {
		if tr.Exact {
			lines = append(lines, row("Trace",
				fmt.Sprintf("%.1f%%, on %s, %.1f s from your gateway", tr.Progress*100, tr.HopName, tr.Left)))
		} else {
			lines = append(lines, row("Trace", fmt.Sprintf("somewhere past %.0f%%", tr.Progress*100)))
		}
	}
	return Result{Lines: lines}, nil
}

// routeText writes a route the way a player reads it.
func routeText(route []ServerView) string {
	if len(route) == 0 {
		return "straight at the target, no relays"
	}
	names := make([]string, 0, len(route))
	for _, s := range route {
		names = append(names, s.IP)
	}
	return fmt.Sprintf("%d relay(s): %s", len(route), strings.Join(names, " → "))
}

func (e *Env) servers(_ context.Context, c cmd) (Result, error) {
	out := Result{Lines: []Line{{
		Text: fmt.Sprintf("  %-16s %-31s %s", "ADDRESS", "NAME", "WHAT YOU KNOW OF IT"),
		Kind: KindHead,
	}}}
	for _, k := range c.run.Known {
		s, ok := e.World.ServerByID(k.Server)
		if !ok {
			continue // a server removed from the catalogue since the run began
		}
		known := "not probed"
		if k.Probed {
			known = fmt.Sprintf("%s, security %d, %s", s.Type(), s.Security, layersText(s))
		}
		if c.run.HasRoot(s.ID) {
			known += " — you hold root here"
		}
		out.Lines = append(out.Lines, Line{
			Text: fmt.Sprintf("  %-16s %-31s %s", s.IP, s.Name, known),
		})
	}
	out.Lines = append(out.Lines, Line{Text: "  `probe <ip>` tells you what stands in the way.", Kind: KindDim})
	return out, nil
}

// layersText names a server's layers in the order they have to be opened.
func layersText(s *world.Server) string {
	var layers []string
	if s.Proxy {
		layers = append(layers, "proxy")
	}
	if s.Firewall {
		layers = append(layers, "firewall")
	}
	layers = append(layers, "password")
	if s.Monitor {
		layers = append(layers, "monitor")
	}
	if s.Type().Public() {
		return "open to read, no trace"
	}
	return strings.Join(layers, " + ")
}

// probeTool is the program that lookup and probe need.
const probeTool = "address_probe"

// hasTool reports whether the run has the program installed.
func hasTool(run *store.Run, id string) bool {
	for _, i := range run.Software {
		if i.Software == id {
			return true
		}
	}
	return false
}

// missingTool is the one wording for a command whose program the player does
// not have.
func (e *Env) missingTool(id string) Result {
	name := id
	if sw, ok := e.World.SoftwareByID(id); ok {
		name = sw.Name
	}
	return errorResult(fmt.Sprintf("That needs %s, which you do not have. The shop sells it.", name))
}

func (e *Env) lookup(_ context.Context, c cmd) (Result, error) {
	if len(c.args) != 1 {
		return errorResult("Usage: lookup <text>"), nil
	}
	if !hasTool(c.run, probeTool) {
		return e.missingTool(probeTool), nil
	}
	query := strings.ToLower(c.args[0])

	// The register knows every organisation; an address, though, is only
	// given for one this run already has.
	out := Result{}
	for _, k := range c.run.Known {
		s, ok := e.World.ServerByID(k.Server)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(s.Name), query) || strings.Contains(s.IP.String(), query) ||
			strings.Contains(strings.ToLower(s.Org.Name), query) {
			out.Lines = append(out.Lines, Line{Text: fmt.Sprintf("  %-16s %s", s.IP, s.Name)})
		}
	}
	if len(out.Lines) > 0 {
		return out, nil
	}
	for _, org := range e.World.Orgs {
		if strings.Contains(strings.ToLower(org.Name), query) {
			return Result{Lines: []Line{
				{Text: fmt.Sprintf("The register has %s, but you do not have its address.", org.Name)},
				{Text: "Addresses come from contracts and from what you read on other machines.", Kind: KindDim},
			}}, nil
		}
	}
	return errorResult(fmt.Sprintf("Nothing you know matches %q.", c.args[0])), nil
}

func (e *Env) probe(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) != 1 {
		return errorResult("Usage: probe <ip>"), nil
	}
	if !hasTool(c.run, probeTool) {
		return e.missingTool(probeTool), nil
	}
	s, out := e.knownServer(c.run, c.args[0])
	if s == nil {
		return out, nil
	}
	probed, err := e.Store.MarkProbed(ctx, c.run.ID, s.ID)
	if err != nil {
		return Result{}, err
	}
	if !probed {
		return errorResult("You do not know that address."), nil
	}
	row := func(label, value string) Line { return Line{Text: fmt.Sprintf("  %-10s %s", label, value)} }
	return Result{Changed: true, Lines: []Line{
		{Text: fmt.Sprintf("PROBE %s", s.IP), Kind: KindHead},
		row("Name", s.Name),
		row("Owner", s.Org.Name),
		row("Type", string(s.Type())),
		row("Security", fmt.Sprint(s.Security)),
		row("Layers", layersText(s)),
	}}, nil
}

// knownServer resolves an address the player typed to a server the run knows.
// The answer for an address that is not in the world and for one the run has
// never heard of is the same, so probing cannot be used to map the catalogue.
func (e *Env) knownServer(run *store.Run, text string) (*world.Server, Result) {
	ip, ok := addr(text)
	if !ok {
		return nil, errorResult(fmt.Sprintf("%q is not an address.", text))
	}
	s, found := e.World.ServerByIP(ip)
	if !found || !run.Knows(s.ID) {
		return nil, errorResult("You do not know that address. `servers` lists the ones you have.")
	}
	return s, Result{}
}

func (e *Env) bounce(_ context.Context, c cmd) (Result, error) {
	live, err := e.live()
	if err != nil {
		return Result{}, err
	}
	action := "list"
	if len(c.args) > 0 {
		action = strings.ToLower(c.args[0])
	}
	switch action {
	case "list":
		route := serverViews(live.Route(c.p.AccountID))
		lines := []Line{{Text: "ROUTE", Kind: KindHead}, {Text: "  " + routeText(route)}}
		lines = append(lines, Line{
			Text: fmt.Sprintf("  Your gateway has %d network card(s), so at most %d relay(s).",
				c.run.Gateway.NICCount, c.run.Gateway.NICCount),
			Kind: KindDim,
		})
		return Result{Lines: lines}, nil

	case "clear":
		live.ClearRoute(c.p.AccountID)
		return Result{Changed: true, Lines: []Line{{Text: "Route cleared.", Kind: KindOK}}}, nil

	case "add":
		if len(c.args) != 2 {
			return errorResult("Usage: bounce add <ip>"), nil
		}
		relay, out := e.knownServer(c.run, c.args[1])
		if relay == nil {
			return out, nil
		}
		switch err := live.AddRelay(c.p.AccountID, relay, c.run.Gateway.NICCount); {
		case errors.Is(err, ErrPublicRelay):
			return errorResult("A public service will not pass a break-in through. Use an ISP or a university."), nil
		case errors.Is(err, ErrRelayAlreadyOnIt):
			return errorResult("That relay is already on the route."), nil
		case errors.Is(err, ErrRouteFull):
			return errorResult(fmt.Sprintf("Your gateway has %d network card(s); that is the longest route it can hold.",
				c.run.Gateway.NICCount)), nil
		case err != nil:
			return Result{}, err
		}
		route := serverViews(live.Route(c.p.AccountID))
		return Result{Changed: true, Lines: []Line{
			{Text: fmt.Sprintf("%s added to the route.", relay.Name), Kind: KindOK},
			{Text: "  " + routeText(route)},
		}}, nil

	default:
		return errorResult("Usage: bounce list | bounce add <ip> | bounce clear"), nil
	}
}

func (e *Env) connect(ctx context.Context, c cmd) (Result, error) {
	live, err := e.live()
	if err != nil {
		return Result{}, err
	}
	if len(c.args) != 1 {
		return errorResult("Usage: connect <ip>"), nil
	}
	target, out := e.knownServer(c.run, c.args[0])
	if target == nil {
		return out, nil
	}
	conn, err := live.Connect(c.p.AccountID, target, c.run)
	switch {
	case errors.Is(err, ErrAlreadyConnected):
		return errorResult("You are connected to something already. `disconnect` first."), nil
	case errors.Is(err, ErrTargetIsOnTheRoute):
		return errorResult("That server is a relay on your own route. `bounce clear` and build it again."), nil
	case errors.Is(err, ErrRouteFull):
		return errorResult("Your route is longer than your gateway's network cards allow."), nil
	case err != nil:
		return Result{}, err
	}

	// Every node of the route writes down where the traffic came from. That
	// is what an investigation reads, and what `log wipe` is for.
	if entries := connectionLogs(conn, e.Clock.Now(e.Now())); len(entries) > 0 {
		if err := e.Store.AddLogs(ctx, c.run.ID, entries); err != nil {
			return Result{}, err
		}
	}

	lines := []Line{{Text: fmt.Sprintf("Connecting to %s (%s)…", target.Name, target.IP)}}
	if len(conn.Route) > 0 {
		lines = append(lines, Line{Text: "  " + routeText(serverViews(conn.Route)), Kind: KindDim})
	}
	if target.Type().Public() {
		lines = append(lines, Line{
			Text: "Connected. It is a public service: what it publishes is open to read, and nothing here traces you.",
			Kind: KindOK,
		}, Line{
			Text: "  What it keeps to itself is still behind root, like anywhere else.",
			Kind: KindDim,
		})
		return Result{Lines: lines}, nil
	}
	lines = append(lines,
		Line{Text: fmt.Sprintf("Connected without root. In the way: %s.", layersText(target)), Kind: KindOK},
		Line{Text: "  `tools` lists what you can set against it, outermost layer first.", Kind: KindDim})
	return Result{Lines: lines}, nil
}

func (e *Env) disconnect(ctx context.Context, c cmd) (Result, error) {
	live, err := e.live()
	if err != nil {
		return Result{}, err
	}
	conn, err := live.Disconnect(c.p.AccountID)
	if errors.Is(err, ErrNoConnection) {
		return errorResult("You are not connected to anything."), nil
	}
	if err != nil {
		return Result{}, err
	}
	// What was left in the logs is now somebody else's problem to read. The
	// player is told nothing about it: that is what clearing a log buys.
	if _, err := e.OpenInvestigation(ctx, c.run, conn.Target, conn.Route); err != nil {
		return Result{}, err
	}

	lines := []Line{{Text: fmt.Sprintf("Disconnected from %s.", conn.Target.Name), Kind: KindOK}}
	if !conn.Target.Type().Public() {
		lines = append(lines, Line{
			Text: "Anything you opened there is closed again; going back means opening it from the start.",
			Kind: KindDim,
		})
	}
	return Result{Lines: lines}, nil
}

// ls and cat are the two things root is for in this milestone: seeing what a
// server holds, and reading it. A public service is open to read without one,
// which is what makes the register and the handbook readable from the start.

func (e *Env) ls(ctx context.Context, c cmd) (Result, error) {
	if c.conn.State != StateConnected {
		return errorResult("The connection is still coming up."), nil
	}
	// What this run destroyed is not there any more, for this run alone.
	gone, err := e.Store.DeletedFiles(ctx, c.run.ID, c.conn.Target.ID)
	if err != nil {
		return Result{}, err
	}
	var files []world.File
	for _, f := range c.conn.Target.Files {
		if _, deleted := gone[f.Path]; !deleted {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return Result{Lines: []Line{{Text: "Nothing on this machine.", Kind: KindDim}}}, nil
	}
	out := Result{Lines: []Line{{
		Text: fmt.Sprintf("  %-28s %8s  %s", "PATH", "SIZE", "WHAT IT IS"),
		Kind: KindHead,
	}}}
	for _, f := range files {
		var marks []string
		if f.Sensitive {
			marks = append(marks, "sensitive")
		}
		if f.Encrypted {
			marks = append(marks, "encrypted")
		}
		if !e.mayRead(c.conn, f) {
			marks = append(marks, "needs root")
		}
		what := "plain"
		if len(marks) > 0 {
			what = strings.Join(marks, ", ")
		}
		out.Lines = append(out.Lines, Line{
			Text: fmt.Sprintf("  %-28s %6d MB  %s", f.Path, f.SizeMB, what),
		})
	}
	return out, nil
}

func (e *Env) cat(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) != 1 {
		return errorResult("Usage: cat <path>"), nil
	}
	if c.conn.State != StateConnected {
		return errorResult("The connection is still coming up."), nil
	}
	gone, err := e.Store.DeletedFiles(ctx, c.run.ID, c.conn.Target.ID)
	if err != nil {
		return Result{}, err
	}
	for _, f := range c.conn.Target.Files {
		if f.Path != c.args[0] {
			continue
		}
		if _, deleted := gone[f.Path]; deleted {
			break
		}
		if !e.mayRead(c.conn, f) {
			return errorResult("Not without root here. Open the password layer first."), nil
		}
		if f.Encrypted {
			return errorResult("The file is encrypted. A Decrypter has to be run against it first."), nil
		}
		// Reading what needed root is an act against the server, so it is
		// one of the things that starts a trace. Reading what a public
		// service publishes is not: looking around is safe.
		if !c.conn.Target.Type().Public() || f.Sensitive {
			if live, err := e.live(); err == nil {
				live.ArmTrace(c.p.AccountID, c.run)
			}
			if err := e.logAct(ctx, c.run.ID, c.conn, ActionRead, f.Sensitive); err != nil {
				return Result{}, err
			}
		}
		out := Result{Lines: []Line{{Text: f.Path, Kind: KindHead}}}
		for _, line := range strings.Split(strings.TrimRight(f.Content, "\n"), "\n") {
			out.Lines = append(out.Lines, Line{Text: "  " + line})
		}
		return out, nil
	}
	return errorResult(fmt.Sprintf("No file at %q. `ls` lists what is here.", c.args[0])), nil
}

// mayRead reports whether the player may read a file on the connection they
// have. A public service hands out what is not sensitive; everything else
// needs root, which is what the password layer gives.
func (e *Env) mayRead(conn *Connection, f world.File) bool {
	if conn.Root {
		return true
	}
	return conn.Target.Type().Public() && !f.Sensitive
}

func (e *Env) traceCmd(_ context.Context, c cmd) (Result, error) {
	live, err := e.live()
	if err != nil {
		return Result{}, err
	}
	tr, exact := live.Trace(c.p.AccountID)
	if tr == nil {
		return Result{Lines: []Line{{Text: "Nobody is working out where you are.", Kind: KindOK}}}, nil
	}
	return Result{Lines: tr.lines(exact)}, nil
}

// registerPage is how many organisations one page of the register holds.
const registerPage = 10

func (e *Env) links(_ context.Context, c cmd) (Result, error) {
	if c.conn.Target.Type() != world.TypeInterNIC {
		return errorResult("The register is on the InterNIC machine; you are connected to something else."), nil
	}
	if c.conn.State != StateConnected {
		return errorResult("The connection is still coming up."), nil
	}
	page := 1
	if len(c.args) == 1 {
		n, err := strconv.Atoi(c.args[0])
		if err != nil || n < 1 {
			return errorResult("Usage: links [page]"), nil
		}
		page = n
	}
	pages := (len(e.World.Orgs) + registerPage - 1) / registerPage
	if page > pages {
		return errorResult(fmt.Sprintf("The register has %d page(s).", pages)), nil
	}

	// The register holds every organisation with an address; what it does not
	// hand out is the address itself, unless this run already has it.
	out := Result{Lines: []Line{
		{Text: fmt.Sprintf("THE REGISTER — page %d of %d", page, pages), Kind: KindHead},
		{Text: fmt.Sprintf("  %-32s %-16s %s", "ORGANISATION", "ADDRESS", "KIND"), Kind: KindDim},
	}}
	for _, org := range e.World.Orgs[(page-1)*registerPage : min(page*registerPage, len(e.World.Orgs))] {
		address := "—"
		for _, s := range org.Servers {
			if c.run.Knows(s.ID) {
				address = s.IP.String()
				break
			}
		}
		out.Lines = append(out.Lines, Line{
			Text: fmt.Sprintf("  %-32s %-16s %s", org.Name, address, org.Type),
		})
	}
	if page < pages {
		out.Lines = append(out.Lines, Line{Text: fmt.Sprintf("  `links %d` for the next page.", page+1), Kind: KindDim})
	}
	return out, nil
}

// newRunWord is the one word `run` does not read as the name of a program.
// The catalogue is not allowed to name a program after it, so the two uses of
// `run` can never collide.
const newRunWord = "new"

func (e *Env) tools(_ context.Context, c cmd) (Result, error) {
	out := Result{Lines: []Line{{
		Text: fmt.Sprintf("  %-16s %-20s %s", "PROGRAM", "NAME", "WHAT IT DOES"),
		Kind: KindHead,
	}}}
	for _, i := range c.run.Software {
		sw, ok := e.World.SoftwareByID(i.Software)
		if !ok {
			continue // a program dropped from the catalogue since it was bought
		}
		does := sw.Description
		if layer := toolLayer[sw.ID]; layer != "" {
			does = fmt.Sprintf("opens the %s layer", layer)
		}
		out.Lines = append(out.Lines, Line{
			Text: fmt.Sprintf("  %-16s %-20s %s", sw.ID, fmt.Sprintf("%s v%d", sw.Name, i.Version), does),
		})
	}
	out.Lines = append(out.Lines,
		Line{Text: fmt.Sprintf("  At most %d run at once, and they share the processor: two take twice as long each.",
			maxParallel(c.run.Gateway)), Kind: KindDim},
		Line{Text: "  `run <program>` sets one to work on the server you are connected to.", Kind: KindDim},
	)
	return out, nil
}

func (e *Env) runCmd(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) == 0 {
		return errorResult("Usage: run new, or run <program>"), nil
	}
	if strings.ToLower(c.args[0]) == newRunWord {
		if len(c.args) != 1 {
			return errorResult("Usage: run new"), nil
		}
		err := e.StartRun(ctx, c.p)
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
	return e.runTool(ctx, c)
}

// runTool sets a program to work on the server the player is connected to.
// It loads the run and the connection itself, because `run` is also how a run
// is started and so cannot ask the dispatcher for either.
func (e *Env) runTool(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) != 1 {
		return errorResult("Usage: run <program>"), nil
	}
	live, err := e.live()
	if err != nil {
		return Result{}, err
	}
	run, err := e.Store.ActiveRun(ctx, c.p.AccountID)
	if err != nil {
		return Result{}, err
	}
	if run == nil {
		return errorResult("You have no active run. Type `run new` to start one."), nil
	}
	id := strings.ToLower(c.args[0])
	sw, ok := e.World.SoftwareByID(id)
	if !ok {
		return errorResult(fmt.Sprintf("No program is called %q. `tools` lists yours.", c.args[0])), nil
	}
	version := 0
	for _, i := range run.Software {
		if i.Software == sw.ID && i.Version > version {
			version = i.Version
		}
	}
	if version == 0 {
		return e.missingTool(sw.ID), nil
	}

	tool, err := live.StartTool(c.p.AccountID, run, sw, version)
	switch {
	case errors.Is(err, ErrNoConnection):
		return errorResult("You are not connected to anything. Type `connect <ip>`."), nil
	case errors.Is(err, ErrToolRunning):
		return errorResult(fmt.Sprintf("%s is already running here.", sw.Name)), nil
	case errors.Is(err, ErrLayerOpen):
		return errorResult(fmt.Sprintf("The %s layer is open already.", toolLayer[sw.ID])), nil
	case errors.Is(err, ErrLayerAbsent):
		return errorResult(fmt.Sprintf("This server has no %s layer.", toolLayer[sw.ID])), nil
	case errors.Is(err, ErrLayerOutOfOrder):
		conn := live.Connection(c.p.AccountID)
		return errorResult(fmt.Sprintf("Not yet: the %s layer is outside it and still closed.", conn.nextLayer())), nil
	case errors.Is(err, ErrTooManyTools):
		return errorResult(fmt.Sprintf("Only %d program(s) fit in this gateway's memory at once.",
			maxParallel(run.Gateway))), nil
	case errors.Is(err, ErrNotEnoughCPU):
		return errorResult(fmt.Sprintf("%s needs a faster processor than this gateway has.", sw.Name)), nil
	case errors.Is(err, ErrNotEnoughRAM):
		return errorResult(fmt.Sprintf("%s needs more memory than this gateway has.", sw.Name)), nil
	case errors.Is(err, ErrToolNotInstalled):
		return e.missingTool(sw.ID), nil
	case err != nil:
		return Result{}, err
	}

	conn := live.Connection(c.p.AccountID)
	running := len(conn.tools)
	left := ToolTime(conn.Target.Security, tool.Version, run.Gateway.CPUMHz, running)
	lines := []Line{{
		Text: fmt.Sprintf("%s v%d running against %s — about %s.",
			sw.Name, tool.Version, conn.Target.Name, roundSeconds(left)),
		Kind: KindOK,
	}}
	if running > 1 {
		lines = append(lines, Line{
			Text: fmt.Sprintf("  %d programs share the processor, so each takes %d times as long.", running, running),
			Kind: KindDim,
		})
	}
	return Result{Lines: lines}, nil
}

func (e *Env) stop(_ context.Context, c cmd) (Result, error) {
	if len(c.args) != 1 {
		return errorResult("Usage: stop <program>"), nil
	}
	live, err := e.live()
	if err != nil {
		return Result{}, err
	}
	id := strings.ToLower(c.args[0])
	if err := live.StopTool(c.p.AccountID, id); errors.Is(err, ErrToolNotRunning) {
		return errorResult("That program is not running here."), nil
	} else if err != nil {
		return Result{}, err
	}
	return Result{Lines: []Line{{
		Text: "Stopped. What it had done is lost; the processor is free again.",
		Kind: KindOK,
	}}}, nil
}

// roundSeconds writes a duration the way the terminal says it.
func roundSeconds(d time.Duration) string {
	return fmt.Sprintf("%.1f s", d.Seconds())
}

func (e *Env) about(_ context.Context, _ cmd) (Result, error) {
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
