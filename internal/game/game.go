// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// Package game is the game's logic. Everything that decides an outcome runs
// here, on the server; the browser only shows the state and sends commands.
package game

import (
	"context"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
)

// Env is what the game needs to run: the database, the world catalogue and
// the clocks.
type Env struct {
	Store *store.Store
	World *world.World
	Clock Clock
	// Now returns the real time; tests replace it.
	Now func() time.Time
}

// Player is the signed-in account a command or a snapshot is for.
type Player struct {
	AccountID int64
	Username  string
}

// StartRun begins a new run for the player with the starting gateway, money,
// software and address book. It fails with store.ErrRunActive when the
// player already has a run.
func (e *Env) StartRun(ctx context.Context, p Player) error {
	n := store.NewRun{
		AccountID: p.AccountID,
		Gateway:   StartGateway,
		Bank:      e.World.Start.Bank.ID,
		Balance:   StartBalance,
	}
	for _, sw := range e.World.Start.Software {
		n.Software = append(n.Software, store.Installed{Software: sw.ID, Version: sw.Version})
	}
	for _, s := range e.World.Start.Links {
		n.Known = append(n.Known, s.ID)
	}
	_, err := e.Store.CreateRun(ctx, n)
	return err
}
