-- Manual preallocations (ADR 0003, ticket #39).
--
-- A first-class, per-shift, operator-editable set of pins that union with config
-- preallocations at allocation time. Each row mirrors the allocation row shape
-- (role, nullable volunteer_id, nullable custom_value) and references its shift
-- directly (ADR 0001): rota and date live on the shift, never denormalised here.
-- The set is freely mutable (rows added/deleted) while the shift's rota is
-- unallocated; it is frozen once allocation runs (enforced in the service).
CREATE TABLE manual_preallocation (
    id UUID PRIMARY KEY,
    shift_id UUID NOT NULL REFERENCES shift(id),
    role TEXT NOT NULL,
    volunteer_id TEXT,
    custom_value TEXT
);

CREATE INDEX idx_manual_preallocation_shift ON manual_preallocation(shift_id);
