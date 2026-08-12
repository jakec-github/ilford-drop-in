-- A Preallocation names its Role by id (issue #195, amending ADR 0003/0006).
--
-- `preallocation.role` held the Role's name as it read when the pin was made.
-- Rename the Role and every pin holding the old name was orphaned: a Shift's
-- Shape names Roles by id (021) and so reads under the new name, the pin
-- matched no Seat, and nothing said so — the promise was silently unkeepable.
--
-- 021's comment grouped this column with `allocation.role` and
-- `alteration.role`, which keep the name deliberately so a rota already made
-- reads as it was made. That is right about those two and wrong about this one.
-- A pin is not a record of what happened: it is a promise about what the solver
-- must still do, a live question like a Shape, and it has to survive a rename
-- the same way. Those two columns do not change.
ALTER TABLE preallocation ADD COLUMN role_id UUID REFERENCES role(id);

UPDATE preallocation SET role_id = role.id FROM role WHERE role.name = preallocation.role;

-- A pin whose name matches no Role is deleted rather than blocking the
-- migration. There is nothing to map it to, and it is already dead: its name
-- cannot match a Seat in any Shape, so no solve could ever have honoured it.
-- This should delete nothing — the `role` rows were minted from the very names
-- these pins were written with (013) — and the count is worth reading in the
-- deploy log before this reaches prod.
DELETE FROM preallocation WHERE role_id IS NULL;

ALTER TABLE preallocation ALTER COLUMN role_id SET NOT NULL;
ALTER TABLE preallocation DROP COLUMN role;
