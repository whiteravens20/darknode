// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"fmt"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
)

// After a break-in the organisation reads its log and walks the route back,
// one operator at a time. It does so on the player's own clock: the walk
// moves only while they are online, so the time they spend playing is the
// time an investigation has to work with.
//
// Everything here is in exposure-minutes, and one of those is one second of
// online time.
const (
	// InvestigationHop is the work of getting one operator to say where the
	// traffic came from, before its resistance is applied.
	InvestigationHop = 20
	// LogV3Delay is what the best wiper buys: the organisation does not even
	// notice for that much longer, because nothing says the log was touched.
	LogV3Delay = 30
	// Reconstruct is what a competent organisation needs to rebuild the one
	// hop a first-version wiper left a mark of.
	Reconstruct = 60
	// FineFlat and FinePart make up a fine, and FineCap limits it. A first
	// penalty must not put a starting player straight into a spiral of debt.
	FineFlat = 200
	FinePart = 0.3
	FineCap  = 20000
)

// investigationStart is how long an organisation of this kind takes to notice
// a break-in at all, in exposure-minutes. A government office answers at
// once; a university takes a term.
func investigationStart(t world.ServerType) int64 {
	switch t {
	case world.TypeGovernment, world.TypeCriminalDatabase:
		return 8
	case world.TypeBank, world.TypeSocialSecurity:
		return 12
	case world.TypeStockMarket:
		return 15
	case world.TypeCorporate, world.TypeLANHost:
		return 40
	case world.TypeNews:
		return 60
	case world.TypeUniversity:
		return 120
	case world.TypeISP:
		return 180
	}
	// The test machines, and anything else nobody is paid to watch.
	return 240
}

// competent reports whether this kind of organisation can rebuild a hop from
// what a first-version wiper left behind.
func competent(t world.ServerType) bool {
	return t == world.TypeGovernment || t == world.TypeCriminalDatabase || t == world.TypeBank
}

// federal reports whether reaching the hop next to the gateway is already
// enough for an arrest: the state does not fine, it comes for you.
func federal(t world.ServerType) bool {
	return t == world.TypeGovernment || t == world.TypeSocialSecurity || t == world.TypeCriminalDatabase
}

// severityOf is what one act is worth. Reading what somebody marked
// sensitive counts; reading the notice beside it does not. Taking a copy is
// worse, and destroying something worse again.
func severityOf(action string, sensitive bool) int {
	switch action {
	case ActionRead:
		if sensitive {
			return 1
		}
		return 0
	case ActionCopy:
		return 2
	case ActionDelete:
		return 3
	}
	return 0
}

// OpenInvestigation starts the walk a break-in deserves, if it deserves one.
// What the player left in the target's log is what the organisation has to go
// on: with every line cleared there is nothing to begin from, unless what was
// done is loud enough to be noticed by itself.
//
// It reports whether a walk was started.
func (e *Env) OpenInvestigation(ctx context.Context, run *store.Run, target *world.Server, route []*world.Server) (bool, error) {
	if target.Type().Public() {
		return false, nil
	}
	lines, err := e.Store.LogsOn(ctx, run.ID, target.ID)
	if err != nil {
		return false, err
	}

	points, standing, wipedWithV1 := 0, false, false
	for _, line := range lines {
		points += line.Severity
		switch {
		case !line.Wiped():
			standing = true
		case line.WipedWith == 1:
			wipedWithV1 = true
		}
	}
	if federal(target.Type()) {
		points *= 2
	}
	// A cleared log and a quiet visit leave nothing to notice. A loud act is
	// noticed by its own result — a file gone is a file gone — but then the
	// organisation has no line to follow and the walk stalls at the target.
	loud := points >= 2
	if !standing && !loud {
		return false, nil
	}

	due := investigationStart(target.Type())
	if !standing && wipedWithV1 {
		// Nothing says when it happened, so it takes longer to be noticed.
		due += LogV3Delay
	}
	walk := make([]string, 0, len(route))
	for i := len(route) - 1; i >= 0; i-- {
		walk = append(walk, route[i].ID)
	}
	_, err = e.Store.StartInvestigation(ctx, store.Investigation{
		RunID: run.ID, Target: target.ID, Route: walk, Severity: points,
		State: store.InvestigationRunning, Due: run.ExposureSeconds + due,
		StartedAt: e.Now(),
	})
	return err == nil, err
}

// investigationOutcome is what one step of a walk did, for the player and for
// the loop that has to write it down.
type investigationOutcome struct {
	alerts []string
	// fine is money owed, and seized the program taken.
	fine   int64
	seized string
	// arrested ends the run.
	arrested bool
}

// StepInvestigation moves one walk one node further and says what came of it.
// The walk stops for good at the first log it finds cleared: the line it
// would have followed is gone.
func (e *Env) StepInvestigation(ctx context.Context, in *store.Investigation, run *store.Run) (investigationOutcome, error) {
	var out investigationOutcome
	target, ok := e.World.ServerByID(in.Target)
	if !ok {
		in.State = store.InvestigationStalled
		return out, nil
	}
	origin := target.Type()
	relays := len(in.Route)

	// Where the walk stands now, and what it would read there.
	server := in.Target
	if in.Position > 0 && in.Position <= relays {
		server = in.Route[in.Position-1]
	}
	lines, err := e.Store.LogsOn(ctx, run.ID, server)
	if err != nil {
		return out, err
	}
	trail, onlyV1 := false, len(lines) > 0
	for _, line := range lines {
		if !line.Wiped() {
			trail = true
		}
		if line.WipedWith != 1 {
			onlyV1 = false
		}
	}

	if !trail {
		// The log here is clear. A competent organisation can rebuild one
		// hop from what the first version of a wiper leaves behind, once.
		if competent(origin) && onlyV1 && !in.Reconstructed {
			in.Reconstructed = true
			in.Due = run.ExposureSeconds + Reconstruct
			in.Position++
			return out, nil
		}
		// Nothing is said to the player: silence is exactly what clearing a
		// log buys, and somebody told the trail had died would know when it
		// was safe to stop playing.
		in.State = store.InvestigationStalled
		return out, nil
	}

	// The trail stands here, so the walk moves on to the next node and the
	// penalty for reaching it is weighed.
	in.Position++
	reached := in.Position // 1..relays are the relays, relays+1 is the gateway
	switch {
	case reached > relays:
		in.State = store.InvestigationAtGateway
		out.arrested = true
		out.alerts = append(out.alerts, fmt.Sprintf(
			"%s followed the trail to your gateway. They know who you are.", target.Org.Name))
		return out, nil

	// The hop next to the gateway: with a route of one it is a fine, with a
	// longer one they are close enough to take your things — and against a
	// federal target it is an arrest either way.
	case reached == relays:
		switch {
		case federal(origin):
			in.State = store.InvestigationAtGateway
			out.arrested = true
			out.alerts = append(out.alerts, fmt.Sprintf(
				"%s traced you to the last link before your gateway. They did not stop to write.",
				target.Org.Name))
			return out, nil
		case relays >= 2 && !in.Seized:
			in.Seized = true
			out.seized = "one program"
		case !in.Fined:
			in.Fined = true
			out.fine = e.fine(in.Severity, balance(run))
		}

	default:
		if !in.Fined {
			in.Fined = true
			out.fine = e.fine(in.Severity, balance(run))
		}
	}

	next := in.Target
	if in.Position <= relays {
		next = in.Route[in.Position-1]
	}
	in.Due = run.ExposureSeconds + e.hopTime(next)
	return out, nil
}

// hopTime is how long the walk takes to get through one node: the less its
// operator keeps and the less it cares to help, the longer it takes.
func (e *Env) hopTime(server string) int64 {
	resist := 1.0
	if s, ok := e.World.ServerByID(server); ok {
		resist = Resist(s.Type())
	}
	return int64(float64(InvestigationHop)/resist + 0.5)
}

// fine is what an organisation asks for: a flat amount for what was done, a
// share of what the player has, and a ceiling so that a first penalty is not
// the end of a run.
func (e *Env) fine(severity int, balance int64) int64 {
	amount := int64(FineFlat*severity) + int64(FinePart*float64(max(balance, 0)))
	return min(max(amount, FineFlat), FineCap)
}

// advanceInvestigations moves every walk whose next step has come. It runs
// with the online time the loop has just written down, which is the clock the
// walks are measured on.
//
// Each step is small and rare — one node of one route, at most once every
// twenty seconds of play — so the work here is nothing beside the tick.
func (e *Engine) advanceInvestigations(ctx context.Context) {
	due, err := e.env.Store.DueInvestigations(ctx)
	if err != nil {
		if ctx.Err() == nil {
			e.log.Error("read the investigations that are due", "err", err)
		}
		return
	}
	for _, in := range due {
		run, err := e.env.Store.ActiveRun(ctx, in.AccountID)
		if err != nil {
			e.log.Error("read the run an investigation is against", "run", in.RunID, "err", err)
			continue
		}
		if run == nil || run.ID != in.RunID {
			continue // the run is over; nobody is left to answer for it
		}
		out, err := e.env.StepInvestigation(ctx, &in, run)
		if err != nil {
			e.log.Error("move an investigation", "investigation", in.ID, "err", err)
			continue
		}
		if err := e.env.Store.SaveInvestigation(ctx, in); err != nil {
			e.log.Error("write down an investigation", "investigation", in.ID, "err", err)
			continue
		}
		e.applyOutcome(ctx, in, run, out)
	}
}

// applyOutcome carries out what a step of an investigation decided, and tells
// the player.
func (e *Engine) applyOutcome(ctx context.Context, in store.Investigation, run *store.Run, out investigationOutcome) {
	changed := false

	if out.fine > 0 {
		left, err := e.env.Store.Fine(ctx, run.ID, out.fine)
		if err != nil {
			e.log.Error("take a fine", "run", run.ID, "err", err)
		} else {
			changed = true
			out.alerts = append(out.alerts, fmt.Sprintf(
				"A fine of $%s. You have $%s left.", money(out.fine), money(left)))
		}
	}
	if out.seized != "" {
		if name, err := e.seizeDearest(ctx, run); err != nil {
			e.log.Error("seize a program", "run", run.ID, "err", err)
		} else if name != "" {
			changed = true
			out.alerts = append(out.alerts, fmt.Sprintf("They came for your gear and took %s.", name))
		}
	}
	if out.arrested {
		if err := e.env.Store.EndRun(ctx, run.ID, e.env.Now()); err != nil {
			e.log.Error("end a run after an arrest", "run", run.ID, "err", err)
		} else {
			e.log.Info("run ended: an investigation reached the gateway", "run", run.ID)
			e.out.Publish(in.AccountID, DeltaAlert, AlertDelta{
				Text: "They have you. This run is over.", Kind: KindError,
			})
			for _, text := range out.alerts {
				e.out.Publish(in.AccountID, DeltaAlert, AlertDelta{Text: text, Kind: KindError})
			}
			e.out.Publish(in.AccountID, DeltaResync, ResyncDelta{})
			return
		}
	}

	for _, text := range out.alerts {
		e.out.Publish(in.AccountID, DeltaAlert, AlertDelta{Text: text, Kind: KindError})
	}
	if changed {
		// Money or programs moved, so the whole run state goes out again.
		if view, err := e.env.RunState(ctx, Player{AccountID: in.AccountID}); err == nil {
			e.out.Publish(in.AccountID, DeltaRun, RunDelta{Run: view})
		}
	}
}

// seizeDearest takes the most expensive program off the gateway and names it.
// A seizure is meant to hurt, so it is not the cheapest thing in the pile.
func (e *Engine) seizeDearest(ctx context.Context, run *store.Run) (string, error) {
	if run == nil {
		return "", nil
	}
	best, price, version := "", -1, 0
	for _, i := range run.Software {
		sw, ok := e.env.World.SoftwareByID(i.Software)
		if !ok {
			continue
		}
		v, ok := sw.Version(i.Version)
		if !ok || v.Price <= price {
			continue
		}
		best, price, version = i.Software, v.Price, i.Version
	}
	if best == "" {
		return "", nil
	}
	if err := e.env.Store.SeizeSoftware(ctx, run.ID, best, version); err != nil {
		return "", err
	}
	name := best
	if sw, ok := e.env.World.SoftwareByID(best); ok {
		name = sw.Name
	}
	return fmt.Sprintf("%s v%d", name, version), nil
}
