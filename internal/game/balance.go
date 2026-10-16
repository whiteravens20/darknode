// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import "github.com/whiteravens20/darknode/internal/store"

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
