-- Closed becomes a field on the Shift (issue #132, docs/allocation_journey_plan.md).
--
-- ADR 0001 deliberately left `closed` config-derived — an rrule re-evaluated on
-- every read — and said not to snapshot it without also building the close/open
-- command. That command exists now, so the flag moves onto the row.
--
-- A Shift is minted open, which is what the default states: closing one is a
-- deliberate act by an admin, and there is no stored list of known closure
-- dates. NOT NULL because "closed is unknown" is not a state the allocator
-- could do anything with.
ALTER TABLE shift ADD COLUMN closed BOOLEAN NOT NULL DEFAULT FALSE;

-- The existing shifts are NOT backfilled here. Which of them the config's
-- closed rrules matched could not be worked out in SQL — it needed the rrule
-- library and the config file — so it was done by `cli backfillShiftClosed`,
-- run once against each environment and then deleted along with the config key
-- it read, in the commit after the one that reintroduced it. It is in git
-- history if an environment is ever restored from a backup predating this.
