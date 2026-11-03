-- Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

-- Owing money. A balance far enough below nothing starts a window the player
-- has to climb out of, and the window runs on their own clock, like
-- everything else that is meant to be felt while playing.
ALTER TABLE run
    -- debt_since is the online time at which the window started, or null
    -- while the player owes nothing worth chasing.
    ADD COLUMN debt_since bigint,
    -- debt_charged is how many game-days of interest have been added, so a
    -- day is never charged twice.
    ADD COLUMN debt_charged integer NOT NULL DEFAULT 0,
    -- debt_missed is how many windows have run out. The first costs the
    -- player their gear; the second ends the run.
    ADD COLUMN debt_missed integer NOT NULL DEFAULT 0;
