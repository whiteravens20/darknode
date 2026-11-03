-- Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

-- What the player took, what they destroyed, and what they were paid to do.
--
-- The catalogue is never written to: a file a player deletes is deleted for
-- that run and nobody else's, which is why the state sits here beside the run
-- rather than in the world.
CREATE TABLE gateway_file (
    run_id    bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    server    text NOT NULL,
    path      text NOT NULL,
    size_mb   integer NOT NULL CHECK (size_mb > 0),
    copied_at timestamptz NOT NULL,
    PRIMARY KEY (run_id, server, path)
);

CREATE TABLE run_file (
    run_id     bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    server     text NOT NULL,
    path       text NOT NULL,
    deleted    boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    PRIMARY KEY (run_id, server, path)
);

-- A contract. It is generated for one run from the catalogue, so its target
-- is always a server the player can reach, and what counts as done is read
-- from the state above, never from what the browser claims.
CREATE TABLE mission (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id              bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    kind                text NOT NULL,
    target              text NOT NULL,
    file                text NOT NULL,
    client              text NOT NULL,
    difficulty          text NOT NULL,
    reward              bigint NOT NULL CHECK (reward > 0),
    reputation_required integer NOT NULL,
    reputation_gain     integer NOT NULL,
    -- deadline is how much online time the player has from accepting it, in
    -- seconds; zero means there is none.
    deadline            bigint NOT NULL DEFAULT 0,
    state               text NOT NULL,
    -- accepted_at_exposure is the run's online time when it was accepted.
    accepted_at_exposure bigint,
    created_at          timestamptz NOT NULL
);

CREATE INDEX mission_run_idx ON mission (run_id, state);
