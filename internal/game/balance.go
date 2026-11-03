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

// The cost of running a program against a server. A program's work is the
// processor time it needs; how long that takes depends on the gateway's
// processor and on how many programs share it.
//
//	work     = BaseTool × security × version factor × CPURef   [MHz·s]
//	progress = cpu_mhz / running  per second
//
// So one program on the starting gateway (2000 MHz) against a security 4
// server takes 2 × 4 × 1.0 = 8 seconds, and two programs at once take twice
// as long each: sharing the processor is a limit, never a speed-up.
const (
	// BaseTool is the work one level of security costs, in seconds at CPURef.
	BaseTool = 2 * time.Second
	// CPURef is the processor the base time is measured at.
	CPURef = 2000
	// RAMPerSlot is how much memory one running program reserves, so the
	// gateway's memory is what caps how many may run at once.
	RAMPerSlot = 2048
)

// VersionFactor is how much of the work a version of a program saves: each
// one is a fifth faster than the one before.
func VersionFactor(version int) float64 {
	switch version {
	case 1:
		return 1.0
	case 2:
		return 0.8
	default:
		return 0.6
	}
}

// Handshake is how long a connection takes to come up. It is short on
// purpose: the wait is a beat before the work, not an obstacle.
const Handshake = 500 * time.Millisecond

// OfflineGrace is how long a connection outlives the last browser watching
// it. A reload or a dropped carrier must not be a way out of a live trace, so
// the connection keeps running; when the grace passes with nobody watching,
// it closes exactly as a disconnect would.
const OfflineGrace = 15 * time.Second
