-- Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

-- One row, written at the first start. Its launch time anchors the shared
-- world clock, so the clock never jumps when the server restarts.
CREATE TABLE instance (
    id          smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    launched_at timestamptz NOT NULL DEFAULT now()
);

-- A login. It holds no game state: that lives in its runs.
CREATE TABLE account (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username      text NOT NULL,
    username_key  text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (username_key = lower(username))
);

-- One-time recovery codes, stored as HMACs keyed with the server secret.
CREATE TABLE recovery_code (
    account_id bigint NOT NULL REFERENCES account (id) ON DELETE CASCADE,
    code_mac   bytea NOT NULL,
    used_at    timestamptz,
    PRIMARY KEY (account_id, code_mac)
);

-- Invite codes for an instance in invite mode, stored as HMACs.
CREATE TABLE invite (
    code_mac   bytea PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    used_by    bigint REFERENCES account (id) ON DELETE SET NULL,
    used_at    timestamptz
);

-- Sessions. The cookie carries a random token; only its SHA-256 is stored.
CREATE TABLE session (
    token_hash   bytea PRIMARY KEY,
    account_id   bigint NOT NULL REFERENCES account (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX session_account_idx ON session (account_id);
CREATE INDEX session_expires_idx ON session (expires_at);

-- A playthrough. An account has at most one active run.
CREATE TABLE run (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id       bigint NOT NULL REFERENCES account (id) ON DELETE CASCADE,
    active           boolean NOT NULL DEFAULT true,
    reputation       integer NOT NULL DEFAULT 0,
    exposure_seconds bigint NOT NULL DEFAULT 0 CHECK (exposure_seconds >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    ended_at         timestamptz,
    CHECK (active = (ended_at IS NULL))
);
CREATE UNIQUE INDEX run_one_active_idx ON run (account_id) WHERE active;

-- The player's own computer in a run.
CREATE TABLE gateway (
    run_id     bigint PRIMARY KEY REFERENCES run (id) ON DELETE CASCADE,
    cpu_mhz    integer NOT NULL CHECK (cpu_mhz > 0),
    ram_mb     integer NOT NULL CHECK (ram_mb > 0),
    modem_mbps integer NOT NULL CHECK (modem_mbps > 0),
    storage_mb integer NOT NULL CHECK (storage_mb > 0),
    nic_count  integer NOT NULL CHECK (nic_count > 0),
    security   integer NOT NULL CHECK (security BETWEEN 1 AND 10)
);

-- The player's bank accounts. bank_server is the ID of a bank in the catalogue.
CREATE TABLE bank_account (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id        bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    bank_server   text NOT NULL,
    balance_clean bigint NOT NULL DEFAULT 0,
    balance_dirty bigint NOT NULL DEFAULT 0 CHECK (balance_dirty >= 0),
    is_primary    boolean NOT NULL DEFAULT false
);
CREATE INDEX bank_account_run_idx ON bank_account (run_id);
CREATE UNIQUE INDEX bank_account_one_primary_idx ON bank_account (run_id) WHERE is_primary;

-- Programs installed on the gateway. software is an ID from the catalogue.
CREATE TABLE run_software (
    run_id   bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    software text NOT NULL,
    version  integer NOT NULL CHECK (version > 0),
    PRIMARY KEY (run_id, software, version)
);

-- The player's address book: servers whose addresses the player knows.
CREATE TABLE known_server (
    run_id   bigint NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    server   text NOT NULL,
    probed   boolean NOT NULL DEFAULT false,
    added_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, server)
);
