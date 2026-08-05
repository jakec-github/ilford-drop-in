-- Roles become rows (ADR 0006, ticket #126).
--
-- The jobs volunteers hold stop being a `roles:` list in drop_in_config.yaml and
-- become data the app owns, so an admin can edit them on a screen rather than an
-- operator editing a file and redeploying.
--
-- A Role is permanent: there is no deletion and no retirement, deliberately, so
-- no reference to a Role can ever dangle and no reader has to ask whether a Role
-- is still offered. The id is what later tables reference, so a rename never
-- breaks a reference; the name stays unique because the volunteer roster is a
-- Google Sheet that names Roles by string and nothing else can tell two Roles
-- with the same name apart.
--
-- Priority is deliberately NOT unique. It orders the filling of Seats, and a
-- unique index would make the settings screen's reordering a dance of temporary
-- values; equal priorities simply fill in a stable order.
--
-- No rows are seeded here. ADR 0006 leaves the settings to an admin, and this
-- migration runs against databases whose Roles are not the same as anybody
-- else's. The dev stack seeds its own (internal/devmode); a real deployment
-- inserts them once by hand.
CREATE TABLE role (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    -- The ceiling: how many of this Role a Shift may ever hold. NULL is
    -- uncapped, which is the Role a Shift's size is spent on.
    max INT CHECK (max IS NULL OR max >= 1),
    priority INT NOT NULL,
    -- A palette token (model.RoleColours), never a colour value: the app owns
    -- what each token looks like in each theme.
    colour TEXT NOT NULL
);
