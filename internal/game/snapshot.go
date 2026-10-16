// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"time"
)

// Snapshot is the whole state the client is shown, sent when its stream opens
// and again whenever the state changes.
type Snapshot struct {
	Account   AccountView `json:"account"`
	WorldTime time.Time   `json:"world_time"`
	Run       *RunView    `json:"run"`
}

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
	r, err := e.Store.ActiveRun(ctx, p.AccountID)
	if err != nil || r == nil {
		return s, err
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
	s.Run = v
	return s, nil
}
