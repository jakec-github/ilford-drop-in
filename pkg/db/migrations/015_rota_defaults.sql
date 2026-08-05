-- Rota Defaults: the settings record (issue #128, ADR 0006).
--
-- One stored set of answers about how the drop-in as a whole runs, edited on
-- the Settings screen rather than in drop_in_config.yaml. It arrives holding
-- the default shift start, end and timezone; the default Shape, the allocation
-- toggles and the Standing Preallocations join it in later tickets.
--
-- It is a singleton, and the boolean primary key is what says so: `id` may only
-- ever be TRUE, so a second row cannot be inserted even by accident.
--
-- No row is created here and nothing is seeded into it. ADR 0006 leaves the
-- settings to an admin, and this migration runs against deployments whose
-- drop-in does not start when anybody else's does; the first save writes the
-- row. An empty table and a row of nulls mean the same thing, so the reader
-- treats them the same. Empty settings block allocation, with a message naming
-- what is missing, and nothing else: the rota still renders and availability
-- still works while an admin has yet to fill them in.
CREATE TABLE rota_defaults (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),

    -- Wall-clock times of day in the drop-in's own timezone, NULL until an
    -- admin sets them. TIME rather than TEXT so the database rejects "half
    -- seven" without every reader having to.
    shift_start_time TIME,
    shift_end_time TIME,

    -- An IANA zone name ("Europe/London"), the zone the two times above are
    -- read in. It is a settings property rather than a per-Shift one: the
    -- drop-in happens in one place.
    shift_timezone TEXT,

    -- A shift ends the evening it starts. Forbidding a session that runs past
    -- midnight is a real narrowing, and a deliberate one: the times are about
    -- to be written onto each Shift as a start and an end on the Shift's own
    -- date (#133), and "22:00 to 00:30" would silently become a shift ending
    -- twenty-one and a half hours before it began.
    CONSTRAINT rota_defaults_shift_ends_after_start CHECK (
        shift_start_time IS NULL
        OR shift_end_time IS NULL
        OR shift_end_time > shift_start_time
    )
);
