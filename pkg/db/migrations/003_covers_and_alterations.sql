CREATE TABLE cover (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reason TEXT NOT NULL,
    user_email TEXT NOT NULL
);

CREATE TABLE alteration (
    id UUID PRIMARY KEY,
    shift_date DATE NOT NULL,
    rota_id UUID NOT NULL REFERENCES rotation(id),
    direction TEXT NOT NULL CHECK (direction IN ('add', 'remove')),
    volunteer_id TEXT,
    custom_value TEXT,
    cover_id UUID NOT NULL REFERENCES cover(id),
    set_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_alteration_rota ON alteration(rota_id);
CREATE INDEX idx_alteration_cover ON alteration(cover_id);
