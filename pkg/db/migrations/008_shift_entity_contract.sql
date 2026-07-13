-- Contract phase of the first-class Shift entity (ADR 0001, spec #3, ticket #19).
--
-- Every reader is now on the shift table: allocation and alteration dates come
-- from the joined shift, rotation start/size are derived by GetRotations, and
-- nothing reads the vestigial availability_request date. Drop the legacy
-- columns so each fact is stated once. Dropping a column also drops any index
-- defined solely on it (idx_allocation_rota, idx_allocation_shift_date,
-- idx_alteration_rota, idx_alteration_shift_date), so those need no explicit
-- DROP INDEX.

-- allocation: the shift knows its rota and date (via shift_id -> shift).
ALTER TABLE allocation DROP COLUMN rota_id;
ALTER TABLE allocation DROP COLUMN shift_date;

-- alteration: likewise re-keyed to shift_id in the expand phase.
ALTER TABLE alteration DROP COLUMN rota_id;
ALTER TABLE alteration DROP COLUMN shift_date;

-- availability_request stays rota-scoped (rota_id kept), but its shift_date was
-- write-only — always the rota's start, read by nothing.
ALTER TABLE availability_request DROP COLUMN shift_date;

-- rotation: start and shift_count are derived from the rota's shifts
-- (MIN(shift.date), COUNT(*)) rather than stored, so a cached copy cannot drift.
ALTER TABLE rotation DROP COLUMN start;
ALTER TABLE rotation DROP COLUMN shift_count;
