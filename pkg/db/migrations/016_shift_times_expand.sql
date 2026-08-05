-- Expand phase of the shift-time refactor (issue #133, ADR 0007).
--
-- A Shift gains the times it runs at. They are local wall-clock times in the
-- drop-in's own zone, held as TIMESTAMP *without* time zone; the zone stays a
-- settings property. The obvious choice is timestamptz and it is the wrong one:
-- the contract phase (#135) drops shift.date and enforces one-Shift-per-date
-- with a unique index on the derived date, and `timestamp::date` is IMMUTABLE
-- where the timestamptz equivalent is only STABLE and so cannot be indexed.
--
-- `shift.date` is untouched and still the source for every reader. Nothing
-- reads these columns yet: #134 moves the readers over, #135 drops the column.

ALTER TABLE shift ADD COLUMN start_at TIMESTAMP;
ALTER TABLE shift ADD COLUMN end_at TIMESTAMP;

-- Backfill every existing Shift from its date and the default times. `date +
-- time` is exactly the wall-clock timestamp wanted, computed by Postgres with
-- no zone conversion anywhere in it.
--
-- A deployment whose admin has not filled the settings in yet backfills
-- nothing, and that is deliberate. The alternative — failing this migration —
-- would deadlock: migrations run at server start, and the Settings screen the
-- times are typed into is served by the server that would then refuse to boot.
-- The NOTICE below says what is left to do; #135 is where a Shift without times
-- stops being tolerable, and by then an admin has had to set them anyway,
-- because allocation has refused without them since #128.
UPDATE shift s
SET start_at = s.date + d.shift_start_time,
    end_at = s.date + d.shift_end_time
FROM rota_defaults d
WHERE d.shift_start_time IS NOT NULL
  AND d.shift_end_time IS NOT NULL;

DO $$
DECLARE
    untimed INT;
BEGIN
    SELECT count(*) INTO untimed FROM shift WHERE start_at IS NULL;
    IF untimed > 0 THEN
        RAISE NOTICE 'shift times: % shift(s) left without times because the drop-in''s default shift times are not set; set them on Admin -> Settings', untimed;
    END IF;
END $$;

-- Both or neither. A Shift with a start and no end describes nothing, and
-- leaving that reachable would mean every later reader carrying a branch for a
-- state the domain has no meaning for.
ALTER TABLE shift ADD CONSTRAINT shift_times_set_together CHECK (
    (start_at IS NULL) = (end_at IS NULL)
);

-- A shift ends the evening it starts, the same narrowing rota_defaults already
-- makes: these columns are that setting written onto a date, so a session
-- running past midnight would land here as one ending before it began.
ALTER TABLE shift ADD CONSTRAINT shift_ends_after_start CHECK (
    start_at IS NULL OR end_at > start_at
);
