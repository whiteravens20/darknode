-- Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

-- Root that lasts. The layers a player opens are the connection's and close
-- with it, but the root won on a server stays for the rest of the run: that
-- is what makes working a route out in advance worth doing, and it is what a
-- trace reads when it asks whether the player holds the hop it is standing on.
CREATE TABLE run_root (
    run_id    bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    server    text NOT NULL,
    opened_at timestamptz NOT NULL,
    PRIMARY KEY (run_id, server)
);
