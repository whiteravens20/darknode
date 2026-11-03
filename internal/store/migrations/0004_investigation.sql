-- Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

-- What happens after the player has gone. An organisation that finds lines in
-- its log works the route back one operator at a time, and it does so on the
-- player's own clock: the walk only moves while they are playing, which is
-- what makes online time a thing to spend carefully.
--
-- The walk is stored because it outlives the session: a player who signs out
-- in the middle of one comes back to it where it stood.
CREATE TABLE investigation (
    id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id   bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    -- target is the server that was broken into, and route the nodes between
    -- it and the gateway, in the order the walk visits them.
    target   text NOT NULL,
    route    text[] NOT NULL,
    -- severity is how heavy what was done there is, which decides a fine.
    severity integer NOT NULL DEFAULT 0,
    -- position is how far the walk has come: 0 at the target, then one per
    -- node of the route, and one past it is the player's gateway.
    position integer NOT NULL DEFAULT 0,
    state    text NOT NULL,
    -- due is the run's online time, in seconds, at which the next step falls.
    due      bigint NOT NULL,
    -- reconstructed records the one hop a competent organisation may rebuild
    -- from what a first-version wiper left behind.
    reconstructed boolean NOT NULL DEFAULT false,
    -- fined and seized record the answers already given, so each is given once.
    fined    boolean NOT NULL DEFAULT false,
    seized   boolean NOT NULL DEFAULT false,
    started_at timestamptz NOT NULL
);

CREATE INDEX investigation_due_idx ON investigation (state, due);
