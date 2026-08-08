-- The ranked column was originally added by editing 000001 in place. golang-migrate
-- records version 1 as applied, so that edit only ever reached databases created
-- afterwards: every older database kept a matches table with no ranked column, which
-- made a rated result read back as casual.
--
-- IF NOT EXISTS is what lets this run against both: a no-op where 000001 already
-- created the column, and the missing ALTER everywhere else.
ALTER TABLE matches ADD COLUMN IF NOT EXISTS ranked BOOLEAN NOT NULL DEFAULT FALSE;
