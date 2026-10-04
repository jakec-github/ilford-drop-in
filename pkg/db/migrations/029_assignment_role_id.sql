-- Allocations, Alterations and draft Seats name their Role by id (issue #222,
-- amending ADR 0006).
--
-- These three kept the Role's name as TEXT so that "a rota already made reads
-- as it was made" after a rename. It never did. A Shift's Shape names its Roles
-- by id (021), so after a rename the Shape read under the new name and the
-- people in it under the old one: the rota page drew them in a group of their
-- own beside a Seat that looked empty, the published sheet filed them under
-- Unknown role, the calendar stopped saying what anybody was on as, and the
-- draft counted their Seats as unfilled. A past rota read as neither the old
-- one nor the new one.
--
-- A Role is permanent (ADR 0006), so an id can never dangle, and a rename is a
-- better label for the same job — a different job is a new Role. Every
-- reference to a Role is by id now; its name is read from `role` at the moment
-- something is shown.

-- An unmatched name has nowhere to go, and unlike 025's dead pins these rows
-- are people who worked a shift, so none may be dropped and none may quietly
-- lose its Role. Production was checked clean before this was written; anywhere
-- that is not stops here and names what to map, rather than migrating half a
-- rota.
DO $$
DECLARE
    unmatched TEXT;
BEGIN
    SELECT string_agg(DISTINCT t.role, ', ') INTO unmatched
    FROM (
        SELECT role FROM allocation
        UNION ALL SELECT role FROM alteration WHERE role IS NOT NULL
        UNION ALL SELECT role FROM draft_allocation
    ) t
    WHERE NOT EXISTS (SELECT 1 FROM role r WHERE r.name = t.role);

    IF unmatched IS NOT NULL THEN
        RAISE EXCEPTION 'rows name Roles that do not exist: %. Rename the rows or the Roles so every name matches a Role, then re-run.', unmatched;
    END IF;
END $$;

ALTER TABLE allocation ADD COLUMN role_id UUID REFERENCES role(id);
UPDATE allocation SET role_id = role.id FROM role WHERE role.name = allocation.role;
ALTER TABLE allocation ALTER COLUMN role_id SET NOT NULL;
ALTER TABLE allocation DROP COLUMN role;

-- Nullable, as `role` was: a removal brings nobody in and so has no Role, and
-- an add written before 004 recorded none.
ALTER TABLE alteration ADD COLUMN role_id UUID REFERENCES role(id);
UPDATE alteration SET role_id = role.id FROM role WHERE role.name = alteration.role;
ALTER TABLE alteration DROP COLUMN role;

ALTER TABLE draft_allocation ADD COLUMN role_id UUID REFERENCES role(id);
UPDATE draft_allocation SET role_id = role.id FROM role WHERE role.name = draft_allocation.role;
ALTER TABLE draft_allocation ALTER COLUMN role_id SET NOT NULL;
ALTER TABLE draft_allocation DROP COLUMN role;
