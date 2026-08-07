-- A Draft Rota Allocation knows whether its inputs have moved (issue #142,
-- ADR 0008).
--
-- A draft is solved from the availability, Shapes, pins and settings of the
-- moment, and every one of those goes on moving all through the availability
-- window. Without a record of that, a draft is a timestamp an admin has to
-- reason about: "solved four hours ago" says nothing about whether it is still
-- the answer.
--
-- Dirtiness is derived rather than stored as a flag, which is what makes it
-- race-free. The Rotation stamps when an input last moved; the draft keeps the
-- stamp it read at the start of its solve; the two being different is what
-- "dirty" means. A flag would have to be cleared by the solve that ran, and a
-- change landing during that solve — thirty seconds is long enough — would be
-- cleared away with it. Here the same change simply moves the Rotation's stamp
-- past the one the draft captured, and the draft reads as dirty until it is
-- solved again.

-- When an allocator input last moved under this Rotation: an availability
-- response, a Shape edit, a Shift opened or closed or moved to another day, a
-- Preallocation, a Role, an Allocation Settings change.
--
-- NULL is a Rotation nothing has moved under yet, which is where every rota
-- starts. It is never cleared: the draft is what catches up.
--
-- A Shift's times are deliberately not one of these. The solver works in dates
-- (ADR 0007), so moving a session from 19:30 to 20:00 changes nothing it could
-- answer differently — only moving its start onto another day does.
ALTER TABLE rotation ADD COLUMN inputs_changed_at TIMESTAMPTZ;

-- The Rotation's stamp as it stood when this solve began. Compared with the
-- Rotation's current one to say whether the draft is still speaking about the
-- inputs as they are.
--
-- Read at the start of the solve rather than the end, so a change landing while
-- the solver runs leaves the draft dirty. Erring that way costs a re-solve; the
-- other way loses the change.
ALTER TABLE draft_rota_allocation ADD COLUMN inputs_changed_at TIMESTAMPTZ;

-- What the solve was asked for and what it managed: every Seat of every open
-- Shift's Shape, and the ones it put somebody in.
--
-- Stored rather than counted from the rows, because the rows cannot answer it.
-- Seats asked comes from the Shapes, and those go on being edited after the
-- solve — counting them later would report the question as it stands now
-- against an answer given to an older one. This is the solve's own report of
-- what it faced.
--
-- The default is for the migration alone: it fills the drafts already stored,
-- and is then dropped so that a write forgetting to say is loud rather than
-- silently reporting a rota that asked for nobody.
ALTER TABLE draft_rota_allocation ADD COLUMN seats_asked INTEGER NOT NULL DEFAULT 0;
ALTER TABLE draft_rota_allocation ADD COLUMN seats_filled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE draft_rota_allocation ALTER COLUMN seats_asked DROP DEFAULT;
ALTER TABLE draft_rota_allocation ALTER COLUMN seats_filled DROP DEFAULT;
