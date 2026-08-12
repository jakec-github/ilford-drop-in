-- An organisation may be promised twice (issue #195).
--
-- One custom entry per recurrence said that naming the same subject twice was a
-- slip. That is true of a volunteer — a person fills at most one Seat on a
-- Shift — and false of a custom entry, which is usually an organisation, and an
-- organisation routinely sends two people. The index made an admin invent
-- "St Mary's (2)" to say the ordinary thing.
--
-- The volunteer index stays: it is the rule that is still a rule.
DROP INDEX idx_standing_preallocation_custom;
