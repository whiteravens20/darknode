// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"time"

	"github.com/whiteravens20/darknode/internal/store"
)

// Starting values of a new run. Every number of the game's balance lives in
// files like this one, so it can be tuned without touching the logic.

// StartGateway is the hardware a new run begins with.
var StartGateway = store.Gateway{
	CPUMHz:    2000,
	RAMMB:     4096,
	ModemMbps: 100,
	StorageMB: 256000,
	NICCount:  2,
	Security:  1,
}

// StartBalance is the money in the first bank account, in credits.
const StartBalance = 100

// WorldScale is how many seconds of world time pass in one real second.
const WorldScale = 60

// Handshake is how long a connection takes to come up. It is short on
// purpose: the wait is a beat before the work, not an obstacle.
const Handshake = 500 * time.Millisecond

// OfflineGrace is how long a connection outlives the last browser watching
// it. A reload or a dropped carrier must not be a way out of a live trace, so
// the connection keeps running; when the grace passes with nobody watching,
// it closes exactly as a disconnect would.
const OfflineGrace = 15 * time.Second
