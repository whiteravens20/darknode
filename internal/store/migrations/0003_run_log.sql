-- Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

-- What a break-in leaves behind. Every node of the route writes down where
-- the connection came from, and every privileged act on the target writes
-- down what was done: these lines are the only evidence that the player was
-- there, and an investigation walks them back one hop at a time.
--
-- The lines belong to the run, not to the world: one player never sees
-- another's, and the catalogue is never written to.
CREATE TABLE run_log (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id     bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    server     text NOT NULL,
    -- hop_index counts from the target outwards: 0 is the target, 1 the relay
    -- nearest it, and so on towards the gateway.
    hop_index  integer NOT NULL CHECK (hop_index >= 0),
    world_time timestamptz NOT NULL,
    action     text NOT NULL,
    -- source_ip is the node the connection came from, empty for the hop the
    -- player's own gateway talked to.
    source_ip  text NOT NULL DEFAULT '',
    -- wiped_with is the version of the LogWiper that cleared this line, or 0
    -- while it still stands. Version 1 leaves a mark a competent
    -- investigation can still read.
    wiped_with smallint NOT NULL DEFAULT 0 CHECK (wiped_with BETWEEN 0 AND 3)
);

CREATE INDEX run_log_run_idx ON run_log (run_id, server);
