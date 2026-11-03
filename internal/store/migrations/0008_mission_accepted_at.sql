-- Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

-- When a contract was taken on. What counts as done has to have been done
-- after that moment: otherwise a player could take the file first and accept
-- the contract for it afterwards.
ALTER TABLE mission ADD COLUMN accepted_at timestamptz;
