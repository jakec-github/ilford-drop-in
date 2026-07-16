-- One availability request per volunteer per rota (issue #41, hazard H3).
-- Two concurrent RequestAvailability runs could both see a volunteer as
-- unrequested and insert a row each; the unique constraint makes the losing
-- run's insert transaction fail wholesale before its email loop starts.

-- Dedupe existing offenders, keeping one row per (rota_id, volunteer_id):
-- prefer a sent request over an unsent one, then the lowest id for
-- determinism.
DELETE FROM availability_request
WHERE id NOT IN (
    SELECT DISTINCT ON (rota_id, volunteer_id) id
    FROM availability_request
    ORDER BY rota_id, volunteer_id, form_sent DESC, id
);

-- Named so error handling can key on it (db.InsertAvailabilityRequests maps
-- violations to ErrDuplicateAvailabilityRequest).
ALTER TABLE availability_request
    ADD CONSTRAINT availability_request_rota_volunteer_key UNIQUE (rota_id, volunteer_id);
