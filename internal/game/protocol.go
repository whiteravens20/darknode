// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import "time"

// The kinds of event a browser's stream carries. A stream opens with a
// snapshot of the whole state and carries deltas from then on, each naming
// only what changed.
const (
	// DeltaSnapshot is the whole state: sent when a stream opens, and again
	// after a resync.
	DeltaSnapshot = "snapshot"
	// DeltaRun is the run's state after something changed it.
	DeltaRun = "run"
	// DeltaConnection is where the player is connected, and how far the
	// connection has got.
	DeltaConnection = "connection"
	// DeltaTrace is how far the trace has come down the route.
	DeltaTrace = "trace"
	// DeltaAlert is something that happened without the player asking: an
	// administrator closing a layer, a connection cut.
	DeltaAlert = "alert"
	// DeltaWorld is the shared clock, sent to everyone as it moves.
	DeltaWorld = "world"
	// DeltaResync tells a browser to open the stream again, because the
	// server could not say what changed.
	DeltaResync = "resync"
	// DeltaEnded says the session is over: a sign-out elsewhere, or a
	// recovery. The browser must not reconnect. It carries nothing and ends
	// the stream, so it is the one event with no sequence number to keep.
	DeltaEnded = "ended"
)

// Delta is one event of a stream. Every one carries a sequence number, given
// per stream as it is sent, so a browser that misses an event sees the gap
// and asks for a fresh snapshot. Nothing else detects a lost delta: the
// stream is one-way, and a browser cannot tell a quiet server from a dropped
// event.
//
// WithSeq returns a numbered copy rather than numbering the delta in place,
// because two browsers of one account are at different points in the
// sequence and must not share a counter.
type Delta interface {
	WithSeq(seq uint64) Delta
}

// WorldDelta is the clock the whole instance shares.
type WorldDelta struct {
	Seq       uint64    `json:"seq"`
	WorldTime time.Time `json:"world_time"`
}

// WithSeq returns a copy of the delta numbered seq.
func (d WorldDelta) WithSeq(seq uint64) Delta { d.Seq = seq; return d }

// RunDelta is the run's state, or null once there is no run.
type RunDelta struct {
	Seq uint64   `json:"seq"`
	Run *RunView `json:"run"`
}

// WithSeq returns a copy of the delta numbered seq.
func (d RunDelta) WithSeq(seq uint64) Delta { d.Seq = seq; return d }

// ConnectionDelta is the live connection, or null once there is none.
type ConnectionDelta struct {
	Seq        uint64          `json:"seq"`
	Connection *ConnectionView `json:"connection"`
}

// WithSeq returns a copy of the delta numbered seq.
func (d ConnectionDelta) WithSeq(seq uint64) Delta { d.Seq = seq; return d }

// TraceDelta is the live trace, or null once none runs.
type TraceDelta struct {
	Seq   uint64     `json:"seq"`
	Trace *TraceView `json:"trace"`
}

// WithSeq returns a copy of the delta numbered seq.
func (d TraceDelta) WithSeq(seq uint64) Delta { d.Seq = seq; return d }

// AlertDelta is a line the game says to the player of its own accord.
type AlertDelta struct {
	Seq  uint64 `json:"seq"`
	Text string `json:"text"`
	// Kind colours the line in the terminal, as a command's output is
	// coloured.
	Kind string `json:"kind,omitempty"`
}

// WithSeq returns a copy of the delta numbered seq.
func (d AlertDelta) WithSeq(seq uint64) Delta { d.Seq = seq; return d }

// ResyncDelta asks a browser to open the stream again.
type ResyncDelta struct {
	Seq uint64 `json:"seq"`
}

// WithSeq returns a copy of the delta numbered seq.
func (d ResyncDelta) WithSeq(seq uint64) Delta { d.Seq = seq; return d }
