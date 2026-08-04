-- Contract phase for the tokenised availability round (issue #80, ADR 0004,
-- docs/availability_web_forms_plan.md).
--
-- Migration 011 could not take the good name: renaming the legacy table does
-- not free its pkey, unique constraint and index names, which share a schema
-- namespace. Dropping the table does, so the rename can finally happen — and
-- the names it lands on are the ones pkg/db keys its error handling to.
--
-- The Google Forms answers this table pointed at were copied into
-- availability_response before the client that could read them was deleted. The
-- form_id and form_url columns go with it: there is no API left to hand them to.
DROP TABLE availability_request;

ALTER TABLE availability_request_v2 RENAME TO availability_request;

-- Foreign keys bind to the table OID, so availability_response follows the
-- rename without being touched. Only the names have to be moved by hand.
ALTER INDEX availability_request_v2_pkey RENAME TO availability_request_pkey;

ALTER TABLE availability_request RENAME CONSTRAINT
    availability_request_v2_rota_volunteer_key TO availability_request_rota_volunteer_key;

ALTER INDEX idx_availability_request_v2_rota RENAME TO idx_availability_request_rota;

-- Migration 011 declared these two inline (UNIQUE on token, REFERENCES on
-- rota_id), so Postgres named them after the table rather than the migration
-- doing it. Nothing keys on either name, but they are permanent and would be
-- the last trace of an interim table nobody will remember.
ALTER TABLE availability_request RENAME CONSTRAINT
    availability_request_v2_token_key TO availability_request_token_key;

ALTER TABLE availability_request RENAME CONSTRAINT
    availability_request_v2_rota_id_fkey TO availability_request_rota_id_fkey;
