-- Expand phase of the first-class Shift entity (ADR 0001, spec #3, ticket #14).
--
-- Create the shift table, backfill one shift per rotation-Sunday from rotation
-- arithmetic, verify every allocation and alteration already references a
-- minted shift, then add NOT NULL shift references to allocation and
-- alteration while KEEPING their legacy rota_id/shift_date columns.
-- availability_request is deliberately untouched (rota-scoped, in flight).

-- A Shift: surrogate UUID identity, a unique date (the external language), and
-- a NOT NULL reference to the rotation that minted it.
CREATE TABLE shift (
    id UUID PRIMARY KEY,
    date DATE NOT NULL UNIQUE,
    rota_id UUID NOT NULL REFERENCES rotation(id)
);

CREATE INDEX idx_shift_rota ON shift(rota_id);

-- Backfill: mint a shift for every rotation's consecutive Sundays
-- (start + 7*i, for i in 0..shift_count-1). Minted from rotation arithmetic,
-- not child rows, so rotations with no allocations still get their shifts.
INSERT INTO shift (id, date, rota_id)
SELECT gen_random_uuid(), r.start + (g.i * 7), r.id
FROM rotation r
CROSS JOIN LATERAL generate_series(0, r.shift_count - 1) AS g(i);

-- Verify every allocation and alteration (rota_id, shift_date) pair matches a
-- minted shift, failing the migration loudly on any mismatch so the re-keying
-- cannot silently corrupt references.
DO $$
DECLARE
    orphans INT;
BEGIN
    SELECT count(*) INTO orphans
    FROM allocation a
    LEFT JOIN shift s ON s.rota_id = a.rota_id AND s.date = a.shift_date
    WHERE s.id IS NULL;
    IF orphans > 0 THEN
        RAISE EXCEPTION 'shift backfill: % allocation row(s) reference a (rota, date) with no minted shift', orphans;
    END IF;

    SELECT count(*) INTO orphans
    FROM alteration a
    LEFT JOIN shift s ON s.rota_id = a.rota_id AND s.date = a.shift_date
    WHERE s.id IS NULL;
    IF orphans > 0 THEN
        RAISE EXCEPTION 'shift backfill: % alteration row(s) reference a (rota, date) with no minted shift', orphans;
    END IF;
END $$;

-- Add the shift reference to allocation, backfill it, then enforce NOT NULL.
-- The legacy rota_id/shift_date columns stay until the contract phase.
ALTER TABLE allocation ADD COLUMN shift_id UUID REFERENCES shift(id);
UPDATE allocation a SET shift_id = s.id
FROM shift s WHERE s.rota_id = a.rota_id AND s.date = a.shift_date;
ALTER TABLE allocation ALTER COLUMN shift_id SET NOT NULL;
CREATE INDEX idx_allocation_shift ON allocation(shift_id);

-- Same for alteration.
ALTER TABLE alteration ADD COLUMN shift_id UUID REFERENCES shift(id);
UPDATE alteration a SET shift_id = s.id
FROM shift s WHERE s.rota_id = a.rota_id AND s.date = a.shift_date;
ALTER TABLE alteration ALTER COLUMN shift_id SET NOT NULL;
CREATE INDEX idx_alteration_shift ON alteration(shift_id);
