ALTER TABLE rotation ADD COLUMN allocated_datetime TIMESTAMPTZ;

-- Backfill existing rotations: 1 week before the first shift
UPDATE rotation SET allocated_datetime = start - INTERVAL '7 days';
