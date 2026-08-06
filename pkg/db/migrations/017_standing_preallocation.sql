-- Standing Preallocations, and one kind of Preallocation (issue #131, ADR 0006,
-- amending ADR 0003).
--
-- Two things happen here, and they are the same change seen from either end.
--
-- First, `manual_preallocation` becomes `preallocation`. The adjective only ever
-- meant "not from the config file", and Config Preallocations no longer exist:
-- there is one kind of Preallocation, an admin may remove any of them, and the
-- rule that a config pin outranked a manual one is gone with the concept.
--
-- Second, `standing_preallocation` arrives: the pins an admin expects to make
-- every rota, held in the Rota Defaults and used to seed ordinary
-- Preallocations when a Rotation is defined. It is a convenience at definition,
-- not a standing fact — nothing reads it after the seeding, so editing one
-- changes what the next rota starts from and never what an existing one holds.
ALTER TABLE manual_preallocation RENAME TO preallocation;

-- The constraint and index names the rename leaves behind still say "manual",
-- and they are what a failing write names in its error message. Renaming them
-- too keeps that message readable.
ALTER TABLE preallocation RENAME CONSTRAINT manual_preallocation_pkey TO preallocation_pkey;
ALTER TABLE preallocation RENAME CONSTRAINT manual_preallocation_shift_id_fkey TO preallocation_shift_id_fkey;
ALTER INDEX idx_manual_preallocation_shift RENAME TO idx_preallocation_shift;

CREATE TABLE standing_preallocation (
    id UUID PRIMARY KEY,

    -- Which Shifts of a rota this applies to, as the same kind of recurrence
    -- rule the config's Rota Overrides used ("the first Sunday of the month").
    -- A rule rather than an index into the rota because that is what the
    -- decision actually is: the team who always take the first Sunday take it
    -- whether that is a rota's first shift or its fifth.
    rrule TEXT NOT NULL,

    -- The Role the seeded Preallocation fills, by id rather than by name: these
    -- outlive any number of rotas, and a Role may be renamed at any time
    -- (ADR 0006). The Preallocations this seeds still record the name, as every
    -- per-rota row does.
    role_id UUID NOT NULL REFERENCES role(id),

    -- Exactly one of these, mirroring `preallocation` and `allocation`: a
    -- volunteer from the roster, or a free-text entry for a group or an outside
    -- body with no roster record.
    volunteer_id TEXT,
    custom_value TEXT,

    CONSTRAINT standing_preallocation_subject_check CHECK (
        (volunteer_id IS NOT NULL AND custom_value IS NULL)
        OR (volunteer_id IS NULL AND custom_value IS NOT NULL)
    )
);

-- One Standing Preallocation per subject per recurrence. Saying the same person
-- takes the first Sunday twice is not two promises, and the second is a slip
-- rather than a decision — including when the two name different Roles, since a
-- person fills at most one Seat on a Shift. Two *overlapping* rules naming one
-- person is a different thing and cannot be caught here; seeding collapses what
-- they contribute to a Shift instead.
CREATE UNIQUE INDEX idx_standing_preallocation_volunteer
    ON standing_preallocation (volunteer_id, rrule)
    WHERE volunteer_id IS NOT NULL;
CREATE UNIQUE INDEX idx_standing_preallocation_custom
    ON standing_preallocation (custom_value, rrule)
    WHERE custom_value IS NOT NULL;
