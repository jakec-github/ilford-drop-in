-- A Preallocation is renamed a Pin, the word Organisers already use for one
-- (issue #292, ADR 0013). A rename and nothing else: every row, constraint and
-- index carries over unchanged. standing_preallocation keeps its name until
-- Overrides replace it (#296).
ALTER TABLE preallocation RENAME TO pin;

ALTER TABLE pin RENAME CONSTRAINT preallocation_pkey TO pin_pkey;
ALTER TABLE pin RENAME CONSTRAINT preallocation_shift_id_fkey TO pin_shift_id_fkey;
ALTER TABLE pin RENAME CONSTRAINT preallocation_role_id_fkey TO pin_role_id_fkey;
ALTER INDEX idx_preallocation_shift RENAME TO idx_pin_shift;
