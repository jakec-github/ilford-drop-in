-- Rotas are defined from a Shift pattern (issue #295, ADR 0013). The one start
-- and end time an Organisation used to have become a pattern of RRULE lines, the
-- date it counts from, and one length every generated Shift runs for.
--
-- The pattern and its Start from are left empty: nobody can write one for an
-- Organisation but its Organisers, so each environment's is entered by hand after
-- this ships, and Define Rota refuses until it is. The length is the one thing
-- the old times already said, so it is carried over.
ALTER TABLE rota_defaults
    ADD COLUMN shift_pattern TEXT,
    ADD COLUMN pattern_start_from DATE,
    ADD COLUMN shift_length_minutes INTEGER
        CONSTRAINT rota_defaults_shift_length_positive CHECK (shift_length_minutes > 0);

UPDATE rota_defaults
SET shift_length_minutes = EXTRACT(EPOCH FROM shift_end_time - shift_start_time)::integer / 60
WHERE shift_start_time IS NOT NULL AND shift_end_time IS NOT NULL;

ALTER TABLE rota_defaults
    DROP CONSTRAINT rota_defaults_shift_ends_after_start,
    DROP COLUMN shift_start_time,
    DROP COLUMN shift_end_time;

-- Where a rota stopped: the generated start of its last Shift, which is where
-- the next rota carries on from. Stored rather than read off the last Shift,
-- because that Shift's start can be edited afterwards and the pattern cannot
-- follow it there. Every existing rota stopped at its last Shift's start, since
-- no start has been moved by a pattern yet.
ALTER TABLE rotation ADD COLUMN stopped_after TIMESTAMP;

UPDATE rotation r
SET stopped_after = (SELECT MAX(s.start_at) FROM shift s WHERE s.rota_id = r.id);

ALTER TABLE rotation ALTER COLUMN stopped_after SET NOT NULL;
