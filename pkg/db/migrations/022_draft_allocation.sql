-- Draft Rota Allocations (issue #141, ADR 0008).
--
-- An unallocated Rotation carries a speculative rota — solved from whatever
-- availability, Shapes and pins exist so far — so an admin watches the rota take
-- shape instead of discovering it at the moment of allocation.
--
-- It lives in tables of its own rather than as `allocation` rows with a null
-- stamp. Five call sites read `allocation` today and two of them are public:
-- GET /api/shifts carries no admin gate, and /calendars/{filename} pushes to
-- calendar apps volunteers have already subscribed. A null-stamp draft would
-- give all five a new obligation to join to `rotation` and check, and one
-- forgotten join would publish a speculative rota to volunteers' phones for the
-- whole availability window. Separate tables make that leak unrepresentable
-- rather than merely forbidden, and leave `allocation` meaning what CONTEXT.md
-- says it means.

-- The draft itself: one per Rotation, replaced entire each time it is solved.
-- Named for the rota because that is its scope — it is made of draft
-- Allocations, but it is never partial and no single one of them means anything
-- on its own (CONTEXT.md).
--
-- The solve outcome lives here rather than being derived from the rows, because
-- "infeasible" and "four Seats unfilled" are what an admin needs during the
-- availability window and neither is legible from the rows: an infeasible solve
-- and a rota nobody has solved yet both store no Seats.
CREATE TABLE draft_rota_allocation (
    -- The primary key, not just a reference: a Rotation has one draft or none.
    --
    -- ON DELETE CASCADE because a draft is part of its Rotation rather than
    -- something that outlives one. Nothing deletes a Rotation today; discarding
    -- an unallocated rota (#139) will, and a draft left behind by one would
    -- name a rota that is not there.
    rota_id UUID PRIMARY KEY REFERENCES rotation(id) ON DELETE CASCADE,

    -- When the solve ran. An admin reading a draft is really asking how old it
    -- is, since the inputs move under it all through the availability window.
    solved_at TIMESTAMPTZ NOT NULL,

    -- Whether the solver found a rota satisfying every hard constraint.
    -- INFEASIBLE is a well-formed outcome, not a failure to record: it is the
    -- answer an admin most needs to see early, while there is still time to fix
    -- the input.
    success BOOLEAN NOT NULL,

    -- CP-SAT's own verdict (OPTIMAL, FEASIBLE, INFEASIBLE, ...) and the value it
    -- scored, kept verbatim. Not an enum: the vocabulary belongs to the solver,
    -- and a new status it learns to emit must not fail a write here.
    solver_status TEXT NOT NULL,
    objective_value BIGINT NOT NULL,

    -- Solve time, group and variable counts, the constraint families applied.
    -- JSONB rather than columns because this is the solver's diagnostic bag
    -- rather than a record of the drop-in: nothing queries inside it, and
    -- pyallocator may add to it without a migration here.
    diagnostics JSONB NOT NULL
);

-- One Seat of a draft: exactly the shape of an `allocation` row, because it
-- becomes one when the rota is allocated.
--
-- Keyed solely by shift_id, like `allocation` — the Shift is the sole authority
-- on rota and date (ADR 0001) — so the draft it belongs to is reached by way of
-- its Shift's rota. The two tables are always written together in one
-- transaction, which is what keeps the rows and the outcome describing the same
-- solve.
CREATE TABLE draft_allocation (
    id UUID PRIMARY KEY,

    -- ON DELETE CASCADE for the reason `shift_requirement` gives: a draft Seat
    -- is part of the rota being drafted, and a Shift deleted by #139 must not
    -- leave one behind. `allocation` deliberately does not cascade — those rows
    -- are the record of a rota that ran.
    shift_id UUID NOT NULL REFERENCES shift(id) ON DELETE CASCADE,

    -- The Role name, as `allocation` keeps it and for the same reason: this is
    -- a rota as solved, and it must keep reading as it was solved even after
    -- the Role is renamed.
    role TEXT NOT NULL,

    -- Exactly one of these, mirroring `allocation`: a volunteer the solver
    -- placed, or the free text a Preallocation pinned.
    volunteer_id TEXT,
    custom_entry TEXT
);

-- Every read of a draft is by Shift, either one Shift's or a whole rota's.
CREATE INDEX idx_draft_allocation_shift ON draft_allocation(shift_id);
