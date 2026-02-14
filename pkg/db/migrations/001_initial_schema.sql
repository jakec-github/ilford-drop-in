CREATE TABLE rotation (
    id UUID PRIMARY KEY,
    start DATE NOT NULL,
    shift_count INT NOT NULL
);

CREATE TABLE availability_request (
    id UUID NOT NULL,
    rota_id UUID NOT NULL REFERENCES rotation(id),
    shift_date DATE NOT NULL,
    volunteer_id TEXT NOT NULL,
    form_id TEXT NOT NULL,
    form_url TEXT NOT NULL,
    form_sent BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (id, form_sent)
);

CREATE TABLE allocation (
    id UUID PRIMARY KEY,
    rota_id UUID NOT NULL REFERENCES rotation(id),
    shift_date DATE NOT NULL,
    role TEXT NOT NULL,
    volunteer_id TEXT,
    custom_entry TEXT
);

CREATE INDEX idx_availability_request_rota ON availability_request(rota_id);
CREATE INDEX idx_allocation_rota ON allocation(rota_id);
