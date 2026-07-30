-- Tokenised availability requests and stored responses (issue #31 slice 1,
-- ADR 0004, docs/availability_web_forms_plan.md).
--
-- Expand phase: these tables run *alongside* the legacy availability_request,
-- which is untouched, so the Google Forms path keeps working by construction
-- until the contract migration renames availability_request_v2 into place.
-- The interim table name is confined to pkg/db; the legacy table's pkey,
-- unique constraint and index names occupy the schema namespace and cannot be
-- freed without disturbing the very table we are not disturbing.

-- The tokenised replacement for availability_request. The link is the
-- volunteer's identity: /availability/{token}. There is no login.
CREATE TABLE availability_request_v2 (
    id UUID PRIMARY KEY,
    rota_id UUID NOT NULL REFERENCES rotation(id),
    volunteer_id TEXT NOT NULL,
    -- Stored raw, not hashed, so an admin can re-display a link to resend it:
    -- minting and sending are separate operations, and a hash would make a link
    -- displayable exactly once. It stops working once the rota is allocated,
    -- which bounds how long a leaked row stays useful (ADR 0004).
    token TEXT NOT NULL UNIQUE,
    -- NULL means minted but not yet sent — the mint/send split depends on
    -- telling the two apart. Nothing sets it until slice 3.
    sent_at TIMESTAMPTZ,
    CONSTRAINT availability_request_v2_rota_volunteer_key UNIQUE (rota_id, volunteer_id)
);

CREATE INDEX idx_availability_request_v2_rota ON availability_request_v2(rota_id);

-- One row per submission. A volunteer may resubmit freely; each submission is a
-- complete generation and the latest one before the cutoff wins, so a
-- point-in-time read ("was their answer in before we allocated?") is a plain
-- timestamp filter rather than a snapshot mechanism.
CREATE TABLE availability_response (
    id UUID PRIMARY KEY,
    availability_request_id UUID NOT NULL REFERENCES availability_request_v2(id),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_availability_response_request
    ON availability_response(availability_request_id, submitted_at DESC);

-- The positives in one generation. An absent row is a no, so every submission
-- must write a complete set — never a delta. Referencing shift_id finishes
-- ADR 0001: availability_request was the last table addressing shifts by date.
CREATE TABLE shift_availability (
    id UUID PRIMARY KEY,
    response_id UUID NOT NULL REFERENCES availability_response(id),
    shift_id UUID NOT NULL REFERENCES shift(id),
    -- PREFERRED has no consumer yet; admitting it now means expressing a
    -- preference later needs a UI and a solver weighting, not a migration.
    answer TEXT NOT NULL CHECK (answer IN ('YES', 'PREFERRED')),
    CONSTRAINT shift_availability_response_shift_key UNIQUE (response_id, shift_id)
);

CREATE INDEX idx_shift_availability_response ON shift_availability(response_id);
