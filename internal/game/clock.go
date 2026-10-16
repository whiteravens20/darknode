// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"fmt"
	"time"
)

// Clock is the shared world clock. It runs at WorldScale times real speed
// from the world's epoch, counted from the moment the instance first started,
// so every player sees the same date and a restart does not move it.
type Clock struct {
	Epoch    time.Time
	Launched time.Time
}

// Now returns the world time at the real time now.
func (c Clock) Now(now time.Time) time.Time {
	elapsed := now.Sub(c.Launched)
	if elapsed < 0 {
		elapsed = 0
	}
	return c.Epoch.Add(elapsed * WorldScale)
}

// FormatWorldTime prints a world time as the game shows it.
func FormatWorldTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04")
}

// FormatGameDuration prints real seconds of play as world time: days, hours
// and minutes.
func FormatGameDuration(realSeconds int64) string {
	minutes := realSeconds * WorldScale / 60
	return fmt.Sprintf("%dd %02dh %02dm", minutes/(24*60), minutes/60%24, minutes%60)
}
