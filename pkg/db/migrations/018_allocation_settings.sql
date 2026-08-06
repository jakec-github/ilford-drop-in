-- Allocation Settings: which optional allocator rules apply (issue #130,
-- ADR 0006).
--
-- JSON rather than a column per rule, because constraints come and go and a
-- column each would make every arrival a migration. What is stored here is only
-- *answers*: the constraint registry in code stays the authority on which
-- toggles exist. An answer for a rule this build no longer has is ignored with
-- a warning, never an error — the lesson of the 3 August 2026 outage, when
-- strict key decoding in the config loader took the site down. A rule with no
-- stored answer is off, so nothing switches itself on behind an admin's back.
--
-- jsonb rather than json: the app reads keys out of it, and the answers are a
-- set of facts rather than a document whose formatting matters.
--
-- NULL until an admin saves the section, which reads the same as "{}" — every
-- rule off. Nothing is seeded (ADR 0006), so that is where a deployment starts.
ALTER TABLE rota_defaults ADD COLUMN allocation_settings JSONB;

-- An object, not an array or a bare number. The shape inside it is the app's
-- business and deliberately not described here — that is the whole point of
-- storing it as JSON — but "this column holds a mapping" is stable enough to
-- be worth the database enforcing.
ALTER TABLE rota_defaults ADD CONSTRAINT rota_defaults_allocation_settings_object CHECK (
    allocation_settings IS NULL OR jsonb_typeof(allocation_settings) = 'object'
);
