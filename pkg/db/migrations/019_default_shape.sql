-- The default Shape (issue #129, ADR 0006).
--
-- The Shape a Shift asks for — which Roles, and how many Seats of each — as
-- every rota's starting point. It is part of the Rota Defaults, and the last
-- piece of `defaultShiftSize` to leave drop_in_config.yaml: a single number
-- could only describe a rota with one Role, and every Role's count is now
-- stated rather than derived from a size and a set of ceilings.
--
-- Rows rather than JSON, which is the one thing about this table worth arguing
-- (ADR 0006). It is the only setting that references Roles, and a live Shape
-- must never name a Role that does not exist. A foreign key guarantees that;
-- JSON cannot. It is also the same shape of storage as `shift_requirement`,
-- which holds this per Shift once Shifts own their Shapes (#137).
--
-- It hangs off no rota_defaults row. That table is a singleton whose row is
-- created by the first save, and a Shape an admin has stated is not waiting on
-- the shift times being stated too — so the Seats stand on their own, and an
-- empty table means what it says: nobody has decided yet.
CREATE TABLE default_shape (
    -- One entry per Role, which is what the primary key says: a Shape asking
    -- for a Role twice is two answers to one question, not a bigger Shape.
    role_id UUID PRIMARY KEY REFERENCES role(id),

    -- How many Seats of this Role a Shift asks for. At least one, because a
    -- Shape asking for nothing of a Role is a Role the Shape does not name —
    -- removing a Role from the Shape deletes its row.
    --
    -- The Role's own ceiling is not enforced here: `role.max` can be lowered
    -- after a Shape is stored, and a CHECK against another table's column is
    -- not something Postgres will hold. The service refuses a Shape that
    -- exceeds a ceiling on the way in, and the solver's per-Role cap is what
    -- makes an already-stored one harmless.
    seats INT NOT NULL CHECK (seats >= 1)
);
