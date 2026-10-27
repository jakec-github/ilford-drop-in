-- An Override can move the Shifts it matches and change how long they run
-- (issue #297, ADR 0013), alongside the Shape and Pins it already sets.
--
-- Both are optional, and NULL is "leave it": the start the pattern generated,
-- and the pattern's length. Every Override written before this means exactly
-- that, so nothing already stored changes.
--
-- start_time moves a Shift within its own day, in the pattern's wall-clock
-- time (ADR 0007); an Override never moves a Shift to another day.
-- length_minutes is how long the Shift runs from wherever it starts, bounded as
-- the pattern's own length is.
ALTER TABLE override
    ADD COLUMN start_time TIME,
    ADD COLUMN length_minutes INTEGER
        CHECK (length_minutes > 0 AND length_minutes <= 1440);
