// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"testing"
	"time"
)

func TestWorldClockRunsSixtyTimesFaster(t *testing.T) {
	epoch := time.Date(2049, 1, 1, 0, 0, 0, 0, time.UTC)
	launched := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	c := Clock{Epoch: epoch, Launched: launched}
	cases := map[time.Duration]string{
		0:                "2049-01-01 00:00",
		time.Minute:      "2049-01-01 01:00", // one real minute is one world hour
		24 * time.Minute: "2049-01-02 00:00", // 24 real minutes are one world day
		-time.Hour:       "2049-01-01 00:00", // a clock set back never runs the world backwards
	}
	for real, want := range cases {
		if got := FormatWorldTime(c.Now(launched.Add(real))); got != want {
			t.Errorf("after %v: %s, want %s", real, got, want)
		}
	}
}

func TestFormatGameDuration(t *testing.T) {
	cases := map[int64]string{
		0:          "0d 00h 00m",
		1:          "0d 00h 01m",
		60:         "0d 01h 00m",
		24*60 + 61: "1d 01h 01m",
	}
	for secs, want := range cases {
		if got := FormatGameDuration(secs); got != want {
			t.Errorf("FormatGameDuration(%d) = %s, want %s", secs, got, want)
		}
	}
}
