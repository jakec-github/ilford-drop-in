-- Shifts own their Shape (issue #137, ADR 0005, ADR 0006).
--
-- A Shift's Shape — which Roles it needs and how many Seats of each — stops
-- being recomputed from the settings on every read and becomes rows of its own,
-- written when the rota is defined and never touched by a later edit to the
-- settings.
--
-- The bug this closes is quiet and total: until now, editing the default Shape
-- rewrote what every *past* Shift had asked for, because there was nowhere else
-- for a Shift's Shape to come from. A rota allocated for one Team lead and four
-- Service volunteers re-read as asking for six of something else, and nothing
-- said so. Enumerated, stored Seats make that unrepresentable.
--
-- It is the same storage as `default_shape`, one Shift down: rows rather than
-- JSON, so the foreign key can guarantee that a live Shape never names a Role
-- that does not exist (ADR 0006).
CREATE TABLE shift_requirement (
    -- ON DELETE CASCADE because a Shape is part of its Shift rather than
    -- something that outlives one. Nothing deletes a Shift today; discarding an
    -- unallocated rota (#139) will, and a Shape left behind by one would name a
    -- Shift that is not there.
    shift_id UUID NOT NULL REFERENCES shift(id) ON DELETE CASCADE,

    -- By id, with a foreign key, not by name. A Role may be renamed at any time
    -- and a Shape has to survive it. The historical tables — allocation.role,
    -- alteration.role, preallocation.role — deliberately do the opposite and
    -- keep the name as TEXT, so a rota already made reads as it was made. The
    -- difference is what each row is for: those record what happened, this
    -- records what is still being asked for.
    role_id UUID NOT NULL REFERENCES role(id),

    -- How many Seats of this Role the Shift asks for. At least one, because a
    -- Shape asking for nothing of a Role is a Role the Shape does not name.
    --
    -- A count is a target, not a minimum: a Shift routinely allocates with
    -- Seats empty, and the solver reports an unfilled Seat by simply not
    -- filling it. The Role's own ceiling is not enforced here, for the reason
    -- default_shape gives: `role.max` can be lowered after a Shape is stored,
    -- and Postgres will not hold a CHECK against another table's column. The
    -- service refuses an over-ceiling Shape on the way in, and the solver's
    -- per-Role cap makes an already-stored one harmless.
    seats INT NOT NULL CHECK (seats >= 1),

    -- One entry per Role per Shift: a Shape asking for a Role twice is two
    -- answers to one question. The composite key also indexes the shift_id
    -- lookups every read does, since it leads on shift_id.
    PRIMARY KEY (shift_id, role_id)
);

-- Backfill: every Shift that already exists gets the default Shape, which is
-- exactly the Shape it was being allocated against — the readers this migration
-- replaces all resolved a Shift's Seats from `default_shape` live. So a rota
-- defined before this change allocates to the same answer after it.
--
-- An empty `default_shape` writes nothing and is not an error. That is a
-- deployment where nobody has stated the Shape yet, and allocation already
-- refuses there; inventing Seats for it would be inventing a rota. Such a
-- deployment must state the default Shape *before* this migration runs if it
-- has a rota in flight, since the Shape is minted at define and, until #138,
-- cannot be edited afterwards.
INSERT INTO shift_requirement (shift_id, role_id, seats)
SELECT s.id, d.role_id, d.seats
FROM shift s
CROSS JOIN default_shape d;
