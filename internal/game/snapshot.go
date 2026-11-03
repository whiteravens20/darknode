// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"time"

	"github.com/whiteravens20/darknode/internal/world"
)

// Snapshot is the whole state the client is shown, sent when its stream opens
// and after a resync. Everything that happens afterwards reaches the browser
// as a delta.
type Snapshot struct {
	Seq       uint64      `json:"seq"`
	Account   AccountView `json:"account"`
	WorldTime time.Time   `json:"world_time"`
	Run       *RunView    `json:"run"`
}

// WithSeq returns a copy of the snapshot numbered seq.
func (s Snapshot) WithSeq(seq uint64) Delta { s.Seq = seq; return s }

// AccountView is the signed-in account.
type AccountView struct {
	Username string `json:"username"`
}

// RunView is the active run, or nil in the snapshot when there is none.
type RunView struct {
	Reputation      int            `json:"reputation"`
	ExposureSeconds int64          `json:"exposure_seconds"`
	Balance         BalanceView    `json:"balance"`
	Gateway         GatewayView    `json:"gateway"`
	Software        []SoftwareView `json:"software"`
	KnownServers    int            `json:"known_servers"`
	// Route are the relays chosen for the next connection, in order from the
	// gateway outwards.
	Route []ServerView `json:"route"`
	// Connection is the live connection, or null when there is none.
	Connection *ConnectionView `json:"connection"`
}

// ServerView is a server as the player knows it: the address and the name,
// which is all one sees without probing it.
type ServerView struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
}

// ConnectionView is the live connection.
type ConnectionView struct {
	Target ServerView   `json:"target"`
	Route  []ServerView `json:"route"`
	State  string       `json:"state"`
	Root   bool         `json:"root"`
}

// serverView describes one server to the browser.
func serverView(s *world.Server) ServerView {
	return ServerView{IP: s.IP.String(), Name: s.Name}
}

// serverViews describes a list of servers to the browser.
func serverViews(servers []*world.Server) []ServerView {
	out := make([]ServerView, 0, len(servers))
	for _, s := range servers {
		out = append(out, serverView(s))
	}
	return out
}

// BalanceView is the money in the primary account.
type BalanceView struct {
	Clean int64 `json:"clean"`
	Dirty int64 `json:"dirty"`
}

// GatewayView is the player's hardware.
type GatewayView struct {
	CPUMHz    int `json:"cpu_mhz"`
	RAMMB     int `json:"ram_mb"`
	ModemMbps int `json:"modem_mbps"`
	StorageMB int `json:"storage_mb"`
	NICCount  int `json:"nic_count"`
	Security  int `json:"security"`
}

// SoftwareView is one installed program.
type SoftwareView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// Snapshot builds the player's current state.
func (e *Env) Snapshot(ctx context.Context, p Player) (Snapshot, error) {
	s := Snapshot{
		Account:   AccountView{Username: p.Username},
		WorldTime: e.Clock.Now(e.Now()).UTC(),
	}
	run, err := e.RunState(ctx, p)
	if err != nil {
		return s, err
	}
	s.Run = run
	return s, nil
}

// RunState builds the view of the player's active run, or nil when there is
// none. It is what a run delta carries, and part of a snapshot.
func (e *Env) RunState(ctx context.Context, p Player) (*RunView, error) {
	r, err := e.Store.ActiveRun(ctx, p.AccountID)
	if err != nil || r == nil {
		return nil, err
	}
	v := &RunView{
		Reputation:      r.Reputation,
		ExposureSeconds: r.ExposureSeconds,
		Gateway: GatewayView{
			CPUMHz: r.Gateway.CPUMHz, RAMMB: r.Gateway.RAMMB, ModemMbps: r.Gateway.ModemMbps,
			StorageMB: r.Gateway.StorageMB, NICCount: r.Gateway.NICCount, Security: r.Gateway.Security,
		},
		Software:     []SoftwareView{},
		KnownServers: len(r.Known),
	}
	for _, a := range r.Accounts {
		if a.Primary {
			v.Balance = BalanceView{Clean: a.Clean, Dirty: a.Dirty}
		}
	}
	for _, i := range r.Software {
		name := i.Software
		if sw, ok := e.World.SoftwareByID(i.Software); ok {
			name = sw.Name
		}
		v.Software = append(v.Software, SoftwareView{ID: i.Software, Name: name, Version: i.Version})
	}
	// The route and the connection are live: they are the loop's, not the
	// database's, and a run has them only while somebody is playing it.
	if e.Engine != nil {
		v.Route = serverViews(e.Engine.Route(p.AccountID))
		v.Connection = e.Engine.ConnectionView(p.AccountID)
	} else {
		v.Route = []ServerView{}
	}
	return v, nil
}
