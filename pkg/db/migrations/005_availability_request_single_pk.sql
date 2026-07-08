-- Collapse the duplicate-row form_sent pattern: a request is one row and
-- form_sent is updated in place. Remove unsent rows superseded by a sent
-- duplicate, then re-key the table on id alone.
DELETE FROM availability_request unsent
USING availability_request sent
WHERE unsent.id = sent.id
  AND unsent.form_sent = FALSE
  AND sent.form_sent = TRUE;

ALTER TABLE availability_request DROP CONSTRAINT availability_request_pkey;
ALTER TABLE availability_request ADD PRIMARY KEY (id);
