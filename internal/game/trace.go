// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"fmt"
	"math"
	"time"

	"github.com/whiteravens20/darknode/internal/world"
)

// The trace is the clock a break-in runs against. It is a cost in
// trace-seconds spread over the nodes of the route, burned at a rate the
// target's security and its monitor set:
//
//	cost  = target + Σ relays + gateway
//	rate  = security / SecDiv × monitor
//	time  = cost / rate
//
// Every number here is the balance of the whole game: the tables of what a
// gateway can and cannot take were computed from exactly these.
const (
	// CostTarget is what the target itself costs a trace.
	CostTarget = 6.0
	// CostRelay is what one relay costs, before its organisation's
	// resistance is applied.
	CostRelay = 10.0
	// CostGateway is what the last hop costs, before the gateway's own
	// security is applied.
	CostGateway = 12.0
	// GateDefence is how much each level of the gateway's security adds to
	// that last hop.
	GateDefence = 0.25
	// SecDiv turns the target's security into a rate.
	SecDiv = 5.5
	// MonitorRate is how much faster a trace runs while the target's monitor
	// is watching. Masking the monitor before the trace starts holds it at
	// one for the whole trace.
	MonitorRate = 1.6
	// RootResist is how much harder a hop the player holds root on is for a
	// trace: the node is theirs, and it does not help.
	RootResist = 1.5
)

// How an administrator answers a trace on a server that has someone watching
// it: a monitor, or security enough to be worth a person's time.
const (
	// AdminMinSecurity is the security from which a server has an
	// administrator even without a monitor.
	AdminMinSecurity = 5
	// AdminClosesLayer is the point at which one of the layers the player
	// opened is closed again.
	AdminClosesLayer = 0.60
	// AdminKicks is the point at which the player is thrown off the target.
	// The trace does not stop; it goes on through the rest of the route.
	AdminKicks = 0.70
)

// Resist is how much work a hop through this kind of organisation costs a
// trace. An ISP keeps the least and answers the slowest; a government office
// answers at once.
func Resist(t world.ServerType) float64 {
	switch t {
	case world.TypeDarkNodeTest, world.TypeDarkNodeInternal:
		return 2.5
	case world.TypeISP:
		return 2.0
	case world.TypeUniversity:
		return 1.6
	case world.TypeCorporate, world.TypeLANHost, world.TypeStockMarket:
		return 0.8
	case world.TypeBank, world.TypeSocialSecurity:
		return 0.5
	case world.TypeGovernment, world.TypeCriminalDatabase:
		return 0.4
	}
	// A public service is never a relay and never a target, so it has no
	// resistance of its own.
	return 1.0
}

// traceTool is the program that shows a trace exactly. Without it a player
// sees a coarse bar and no hop.
const traceTool = "trace_meter"

// hop is one node of the route a trace walks, from the target back to the
// gateway.
type hop struct {
	name string
	cost float64
}

// trace is a live trace. It outlives the connection that started it: being
// thrown off the target does not stop the trace coming down the route.
type trace struct {
	hops  []hop
	total float64
	rate  float64
	// done is how much of the cost has been burned, in trace-seconds.
	done float64
	// admin says whether anybody is watching this server: a monitor, or
	// security enough to be worth a person's time. It is read when the trace
	// starts, because after a kick there is no connection left to ask.
	admin bool
	// closedLayer and kicked record the administrator's answers, so each
	// happens once.
	closedLayer, kicked bool
	// reported is when the browser was last told where the trace stands.
	reported time.Time
}

// newTrace works out the cost and the rate of a trace against this
// connection, as they stand the moment it starts. A monitor already masked
// stays masked for the whole trace, which is what makes masking it first
// worth doing.
func newTrace(c *Connection, gatewaySecurity int, rooted func(string) bool, now time.Time) *trace {
	t := &trace{reported: now}

	t.hops = append(t.hops, hop{name: c.Target.Name, cost: CostTarget})
	// The trace walks the route backwards: the relay nearest the target
	// first, the one nearest the gateway last.
	for i := len(c.Route) - 1; i >= 0; i-- {
		relay := c.Route[i]
		resist := Resist(relay.Type())
		if rooted(relay.ID) {
			resist *= RootResist
		}
		t.hops = append(t.hops, hop{name: relay.Name, cost: CostRelay * resist})
	}
	t.hops = append(t.hops, hop{
		name: "your gateway",
		cost: CostGateway * (1 + GateDefence*float64(gatewaySecurity)),
	})
	for _, h := range t.hops {
		t.total += h.cost
	}

	monitor := 1.0
	if c.Target.Monitor && !c.open[LayerMonitor] {
		monitor = MonitorRate
	}
	t.rate = float64(c.Target.Security) / SecDiv * monitor
	t.admin = c.Target.Monitor || c.Target.Security >= AdminMinSecurity
	return t
}

// TraceTime is how long a trace takes from start to the gateway, for the
// balance tables and for what the player is told.
func (t *trace) TraceTime() time.Duration {
	if t.rate <= 0 {
		return 0
	}
	return time.Duration(t.total / t.rate * float64(time.Second))
}

// progress is how far the trace has come, from 0 to 1.
func (t *trace) progress() float64 {
	if t.total <= 0 {
		return 1
	}
	return math.Min(t.done/t.total, 1)
}

// at returns the hop the trace stands on: the one whose share of the cost
// contains what has been burned so far.
func (t *trace) at() int {
	burned := t.done
	for i, h := range t.hops {
		if burned < h.cost {
			return i
		}
		burned -= h.cost
	}
	return len(t.hops) - 1
}

// advance burns dt of real time and reports the progress afterwards.
func (t *trace) advance(dt time.Duration) float64 {
	t.done += t.rate * dt.Seconds()
	return t.progress()
}

// view is what the browser is told. Without the TraceMeter a player sees
// roughly how far the trace has come and nothing more: the exact number and
// the hop it stands on are what the program buys.
func (t *trace) view(exact bool) *TraceView {
	v := &TraceView{Exact: exact, Progress: t.progress()}
	if !exact {
		// Rounded down to quarters, so the bar is honest but coarse.
		v.Progress = math.Floor(v.Progress*4) / 4
		return v
	}
	v.Hop = t.at()
	v.HopName = t.hops[v.Hop].name
	if t.rate > 0 {
		v.Left = math.Max((t.total-t.done)/t.rate, 0)
	}
	return v
}

// traceLines writes a trace the way the terminal shows it.
func (t *trace) lines(exact bool) []Line {
	v := t.view(exact)
	if !exact {
		return []Line{
			{Text: "TRACE", Kind: KindHead},
			{Text: fmt.Sprintf("  Somewhere past %.0f%%. A TraceMeter would tell you exactly.", v.Progress*100)},
		}
	}
	out := []Line{
		{Text: "TRACE", Kind: KindHead},
		{Text: fmt.Sprintf("  %.1f%% of the way, standing on %s", v.Progress*100, v.HopName)},
		{Text: fmt.Sprintf("  %.1f s to your gateway at this rate", v.Left)},
	}
	for i, h := range t.hops {
		mark := "  "
		if i == v.Hop {
			mark = "→ "
		}
		out = append(out, Line{
			Text: fmt.Sprintf("  %s%-32s %5.1f trace-s", mark, h.name, h.cost),
			Kind: KindDim,
		})
	}
	return out
}
