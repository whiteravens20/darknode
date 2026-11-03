// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"fmt"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
)

// What a line in a server's log says was done. Every node of a route writes
// down where the traffic came from; the target writes down the connection and
// each privileged act after it.
const (
	ActionRelay   = "RELAY"
	ActionConnect = "CONNECT"
	ActionRoot    = "ROOT"
	ActionRead    = "READ"
)

// logWiper is the program that clears a log, and its version decides how
// clean the job is.
const logWiper = "log_wiper"

// connectionLogs is what a connection writes down as it is made: every relay
// records the node the traffic came from, which is the line an investigation
// follows back, and the target records the connection itself.
//
// A public service writes nothing: reading what it publishes starts no trace
// and no investigation, so there is nothing for a line to be evidence of.
func connectionLogs(conn *Connection, worldTime time.Time) []store.LogEntry {
	if conn.Target.Type().Public() {
		return nil
	}
	// The connection runs gateway → relay 1 → … → relay k → target, while the
	// hops are numbered from the target outwards.
	hops := len(conn.Route)
	out := make([]store.LogEntry, 0, hops+1)
	for i, relay := range conn.Route {
		source := "" // empty means the player's own gateway
		if i > 0 {
			source = conn.Route[i-1].IP.String()
		}
		out = append(out, store.LogEntry{
			Server: relay.ID, HopIndex: hops - i, WorldTime: worldTime,
			Action: ActionRelay, SourceIP: source,
		})
	}
	source := ""
	if hops > 0 {
		source = conn.Route[hops-1].IP.String()
	}
	out = append(out, store.LogEntry{
		Server: conn.Target.ID, HopIndex: 0, WorldTime: worldTime,
		Action: ActionConnect, SourceIP: source,
	})
	return out
}

// logAct writes down one privileged act on the server the player is on.
func (e *Env) logAct(ctx context.Context, runID int64, conn *Connection, action string) error {
	if conn.Target.Type().Public() {
		return nil
	}
	source := ""
	if n := len(conn.Route); n > 0 {
		source = conn.Route[n-1].IP.String()
	}
	return e.Store.AddLogs(ctx, runID, []store.LogEntry{{
		Server: conn.Target.ID, HopIndex: 0, WorldTime: e.Clock.Now(e.Now()),
		Action: action, SourceIP: source,
	}})
}

func (e *Env) logCmd(ctx context.Context, c cmd) (Result, error) {
	wipe := len(c.args) == 1 && c.args[0] == "wipe"
	if len(c.args) > 0 && !wipe {
		return errorResult("Usage: log, or log wipe"), nil
	}
	if !c.conn.Root {
		return errorResult("Not without root here. The log is not yours to read yet."), nil
	}
	if wipe {
		return e.wipeLog(ctx, c)
	}

	entries, err := e.Store.LogsOn(ctx, c.run.ID, c.conn.Target.ID)
	if err != nil {
		return Result{}, err
	}
	if len(entries) == 0 {
		return Result{Lines: []Line{
			{Text: fmt.Sprintf("Nothing of yours in the log on %s.", c.conn.Target.Name), Kind: KindOK},
		}}, nil
	}
	out := Result{Lines: []Line{{
		Text: fmt.Sprintf("  %-19s %-9s %-16s %s", "WHEN", "WHAT", "FROM", "STATE"),
		Kind: KindHead,
	}}}
	for _, entry := range entries {
		from := entry.SourceIP
		if from == "" {
			from = "your gateway"
		}
		state := "stands"
		if entry.Wiped() {
			state = fmt.Sprintf("cleared with v%d", entry.WipedWith)
		}
		out.Lines = append(out.Lines, Line{
			Text: fmt.Sprintf("  %-19s %-9s %-16s %s",
				FormatWorldTime(entry.WorldTime), entry.Action, from, state),
		})
	}
	out.Lines = append(out.Lines, Line{
		Text: "  These lines are the only evidence you were here. `log wipe` clears them.",
		Kind: KindDim,
	})
	return out, nil
}

// wipeLog clears this run's lines on the server the player is on. Clearing
// the log of a hop the trace has not reached yet breaks the trail it is
// following, which is the one way out once an administrator has cut the
// player off the target.
func (e *Env) wipeLog(ctx context.Context, c cmd) (Result, error) {
	version := 0
	for _, i := range c.run.Software {
		if i.Software == logWiper && i.Version > version {
			version = i.Version
		}
	}
	if version == 0 {
		return e.missingTool(logWiper), nil
	}
	cleared, err := e.Store.WipeLogs(ctx, c.run.ID, c.conn.Target.ID, version)
	if err != nil {
		return Result{}, err
	}
	if cleared == 0 {
		return Result{Lines: []Line{
			{Text: "Nothing of yours left in this log.", Kind: KindOK},
		}}, nil
	}

	lines := []Line{{
		Text: fmt.Sprintf("%d line(s) cleared with LogWiper v%d.", cleared, version),
		Kind: KindOK,
	}}
	switch version {
	case 1:
		lines = append(lines, Line{
			Text: "  It leaves a mark. Somebody competent and patient could still read it.",
			Kind: KindDim,
		})
	case 3:
		lines = append(lines, Line{
			Text: "  Nothing left, and nothing to say the log was touched at all.",
			Kind: KindDim,
		})
	}

	live, err := e.live()
	if err != nil {
		return Result{}, err
	}
	if live.BreakTrail(c.p.AccountID, c.conn.Target.ID) {
		lines = append(lines,
			Line{Text: "The trace was following that hop. The trail ends here.", Kind: KindOK},
			Line{Text: "  It has nothing left to follow, and it has stopped.", Kind: KindDim})
	}
	return Result{Lines: lines}, nil
}
