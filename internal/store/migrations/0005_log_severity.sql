-- Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

-- How heavy each act was. The line in the log carries it, because that is
-- what an investigation reads: reading a file somebody marked sensitive is
-- not the same act as reading the public notice beside it, and what a fine
-- comes to depends on the difference.
ALTER TABLE run_log ADD COLUMN severity smallint NOT NULL DEFAULT 0 CHECK (severity >= 0);
