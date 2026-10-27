-- Shifts may share a day; they may not share a moment (issue #294, ADR 0013).
-- An Organisation running a morning and an evening session runs two Shifts on
-- one date, and nothing about that is a mistake. Two Shifts running at once
-- would be, so the rule the index stated about dates is restated about times.
--
-- The constraint inherits the index's other job (issue #41, hazard B1): two
-- rotas defined at the same moment and minting the same time still cannot both
-- commit, because the second insert waits on the first and is then refused.
--
-- tsrange is half-open, '[)': a Shift ending at 12:00 and one starting at 12:00
-- do not overlap. Closed Shifts are included, as they were in the index — a
-- Closed Shift still holds its slot, so reopening it can never clash.
--
-- The code reports a clash by this constraint's name.
CREATE EXTENSION IF NOT EXISTS btree_gist;

DROP INDEX shift_start_date_key;

ALTER TABLE shift ADD CONSTRAINT shift_no_overlap
    EXCLUDE USING gist (organisation_id WITH =, tsrange(start_at, end_at) WITH &&);
