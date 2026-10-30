-- A Shift pattern's entries carry their own start and length (issue #310,
-- ADR 0014), replacing the one Start from and one length the Rota Defaults
-- held beside the rule.
--
-- The stored pattern is cleared rather than converted: it was RRULE lines
-- whose times sat in BYHOUR and BYMINUTE, with no start or length of its own,
-- and guessing which DTSTART each line meant is not something to do to an
-- Organisation's rota behind its back. Define Rota refuses until the pattern
-- is written again, and says why.
UPDATE rota_defaults SET shift_pattern = NULL;

ALTER TABLE rota_defaults
    DROP COLUMN pattern_start_from,
    DROP COLUMN shift_length_minutes;

-- An Override's Start from moves into its rule, as the DTSTART;VALUE=DATE line
-- the rule now carries. It counts from the same date, so it matches the same
-- Shifts. An Override with no length still means the length of the entry that
-- made the Shift.
UPDATE override
SET rrule = 'DTSTART;VALUE=DATE:' || to_char(start_from, 'YYYYMMDD') || E'\n' || rrule;

ALTER TABLE override DROP COLUMN start_from;
