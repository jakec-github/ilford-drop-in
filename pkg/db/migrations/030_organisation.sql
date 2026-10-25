-- One server, many Organisations (ADR 0011, issue #256).
--
-- An Organisation is a drop-in with a rota of its own. Which ones exist is
-- configuration — the display name, the allowlists and the sheets all live in
-- drop_in_config.yaml — so this table holds the id and nothing else. It exists
-- so the rows below have something to reference: an id in config is inserted
-- here at startup, and never deleted.
CREATE TABLE organisation (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]*$')
);

-- Every row that already exists belonged to the one drop-in this database
-- served. Which one is a deployment's business and appears nowhere in this repo,
-- so it is given a placeholder here and the server renames the placeholder to
-- the configured Organisation at startup (db.SyncOrganisations). Every
-- reference below is ON UPDATE CASCADE so that rename is one statement.
--
-- A database with nothing in it has nothing to adopt, and gets no placeholder:
-- a fresh deployment may then list as many Organisations as it likes from the
-- start. The defaults below name the placeholder either way, which is safe on
-- empty tables because a foreign key is only checked against rows.
INSERT INTO organisation (id)
SELECT 'unassigned'
WHERE EXISTS (SELECT 1 FROM role)
   OR EXISTS (SELECT 1 FROM rotation)
   OR EXISTS (SELECT 1 FROM rota_defaults)
   OR EXISTS (SELECT 1 FROM standing_preallocation)
   OR EXISTS (SELECT 1 FROM published_rota_sheet);

-- The roots carry the Organisation directly: everything else hangs off one of
-- them. Added with the placeholder as a default so existing rows are adopted,
-- then the default is dropped — a new row must say whose it is.
ALTER TABLE role ADD COLUMN organisation_id TEXT NOT NULL DEFAULT 'unassigned'
    REFERENCES organisation(id) ON UPDATE CASCADE;
ALTER TABLE role ALTER COLUMN organisation_id DROP DEFAULT;

ALTER TABLE rotation ADD COLUMN organisation_id TEXT NOT NULL DEFAULT 'unassigned'
    REFERENCES organisation(id) ON UPDATE CASCADE;
ALTER TABLE rotation ALTER COLUMN organisation_id DROP DEFAULT;
-- The target of the composite key the Shift's copy is held to.
ALTER TABLE rotation ADD CONSTRAINT rotation_id_organisation_key UNIQUE (id, organisation_id);

-- A Shift reaches its Organisation through its Rotation, but carries a copy so
-- one-Shift-per-date can be an index per Organisation. The composite key keeps
-- the copy honest: a Shift cannot name a different Organisation from its rota.
ALTER TABLE shift ADD COLUMN organisation_id TEXT NOT NULL DEFAULT 'unassigned';
ALTER TABLE shift ALTER COLUMN organisation_id DROP DEFAULT;
ALTER TABLE shift ADD CONSTRAINT shift_rota_organisation_fkey
    FOREIGN KEY (rota_id, organisation_id) REFERENCES rotation(id, organisation_id) ON UPDATE CASCADE;

ALTER TABLE standing_preallocation ADD COLUMN organisation_id TEXT NOT NULL DEFAULT 'unassigned'
    REFERENCES organisation(id) ON UPDATE CASCADE;
ALTER TABLE standing_preallocation ALTER COLUMN organisation_id DROP DEFAULT;

-- Uniqueness that was global is per Organisation now. Names are kept, because
-- the code reports a clash by the constraint's name.
ALTER TABLE role DROP CONSTRAINT role_name_key;
ALTER TABLE role ADD CONSTRAINT role_name_key UNIQUE (organisation_id, name);

DROP INDEX shift_start_date_key;
CREATE UNIQUE INDEX shift_start_date_key ON shift (organisation_id, (start_at::date));

DROP INDEX idx_standing_preallocation_volunteer;
CREATE UNIQUE INDEX idx_standing_preallocation_volunteer
    ON standing_preallocation (organisation_id, volunteer_id, rrule)
    WHERE volunteer_id IS NOT NULL;

-- The two singletons become one row per Organisation, keyed by it.
ALTER TABLE rota_defaults ADD COLUMN organisation_id TEXT NOT NULL DEFAULT 'unassigned'
    REFERENCES organisation(id) ON UPDATE CASCADE;
ALTER TABLE rota_defaults ALTER COLUMN organisation_id DROP DEFAULT;
ALTER TABLE rota_defaults DROP CONSTRAINT rota_defaults_pkey;
ALTER TABLE rota_defaults DROP COLUMN id;
ALTER TABLE rota_defaults ADD PRIMARY KEY (organisation_id);

ALTER TABLE published_rota_sheet ADD COLUMN organisation_id TEXT NOT NULL DEFAULT 'unassigned'
    REFERENCES organisation(id) ON UPDATE CASCADE;
ALTER TABLE published_rota_sheet ALTER COLUMN organisation_id DROP DEFAULT;
ALTER TABLE published_rota_sheet DROP CONSTRAINT published_rota_sheet_pkey;
ALTER TABLE published_rota_sheet DROP COLUMN singleton;
ALTER TABLE published_rota_sheet ADD PRIMARY KEY (organisation_id);

-- The default Shape is keyed by Role, and a Role is already one Organisation's,
-- so it needs no column of its own: it is read through the Role.
