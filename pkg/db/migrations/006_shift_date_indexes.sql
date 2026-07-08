-- Shift listings and single-date change validation filter allocations and
-- alterations by shift_date, so index it on both tables.
CREATE INDEX idx_allocation_shift_date ON allocation(shift_date);
CREATE INDEX idx_alteration_shift_date ON alteration(shift_date);
