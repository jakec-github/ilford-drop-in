-- Overrides replace Standing Preallocations (issue #296, ADR 0013).
--
-- An Override is one RRULE, with its own Start from, that picks out some of the
-- Shifts a Shift pattern generates and changes them as a rota is defined: a
-- whole Shape in place of the default one, and Pins. The Organisation's
-- Overrides are an ordered list and a later one wins, so each carries its
-- position.
--
-- Standing Preallocations are dropped rather than carried over. There are few of
-- them, and an Organiser re-enters each as an Override by hand once this ships;
-- their seeded Pins are ordinary Pins on the rotas that hold them and are not
-- touched.
DROP TABLE standing_preallocation;

CREATE TABLE override (
    id UUID PRIMARY KEY,
    organisation_id TEXT NOT NULL REFERENCES organisation(id) ON UPDATE CASCADE,
    -- Where it stands in the list: a later position wins. Gaps are harmless,
    -- since only the order is read. Deferred, so a reorder can pass two rows
    -- through each other's positions within one transaction.
    position INTEGER NOT NULL,
    rrule TEXT NOT NULL,
    -- The date the rule counts from, as DTSTART at 00:00.
    start_from DATE NOT NULL,
    -- What it matches: the Shift generated at exactly the time it names, or
    -- every Shift on the dates it names. Set from the rule when it is saved, so
    -- it is a fact on the row rather than something every reader re-derives.
    granularity TEXT NOT NULL CHECK (granularity IN ('time', 'date')),
    -- Whether it sets a Shape at all. A Shape that asks for nobody is a Shape
    -- with no seats, which only this flag tells apart from no Shape.
    sets_shape BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT override_position_unique UNIQUE (organisation_id, position)
        DEFERRABLE INITIALLY DEFERRED
);

-- An Override's Shape, the same two columns as default_shape: a Role it does
-- not name is a Role it asks for none of.
CREATE TABLE override_seat (
    override_id UUID NOT NULL REFERENCES override(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES role(id),
    seats INTEGER NOT NULL CHECK (seats > 0),
    PRIMARY KEY (override_id, role_id)
);

-- An Override's Pins: a Role, and a volunteer or a custom entry, as pin has.
CREATE TABLE override_pin (
    id UUID PRIMARY KEY,
    override_id UUID NOT NULL REFERENCES override(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES role(id),
    volunteer_id TEXT,
    custom_value TEXT,
    CONSTRAINT override_pin_subject CHECK ((volunteer_id IS NULL) <> (custom_value IS NULL))
);

-- One person once per Override: a person fills at most one Seat on a Shift. A
-- custom entry may repeat, as it may on a Shift.
CREATE UNIQUE INDEX idx_override_pin_volunteer ON override_pin (override_id, volunteer_id)
    WHERE volunteer_id IS NOT NULL;
