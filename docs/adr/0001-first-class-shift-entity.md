# Shift is a first-class entity

Status: accepted

A shift used to be an inference: a `shift_date` string scattered across
`allocation`, `alteration`, and `availability_request`, with its properties
(closed, size, preallocations) re-derived from config rrules on every read and
its existence implied by date arithmetic over `rotation.start + 7×i`. We
introduced a `shift` table — `id UUID PK`, `date DATE NOT NULL UNIQUE`,
`rota_id UUID NOT NULL` — minted by `DefineRota` in the same transaction as the
rotation, and made it the sole authority on which shifts belong to a rota
(replacing all `CalculateShiftDates` call sites).

## Decisions and their reasons

- **Surrogate UUID despite the unique date.** Dates are the external language
  of the system (forms, emails, historical responses) and `UNIQUE(date)`
  preserves that, but identity is the UUID so that two-shifts-per-day remains
  possible without re-keying the schema again. We considered `date` as the
  natural PK and rejected it for that reason alone.
- **`allocation` and `alteration` re-keyed to `shift_id`; `shift_date` and
  `rota_id` dropped from them.** One fact stated once — the shift knows its
  date and its rota. Go structs keep `ShiftDate`/`RotaID` fields hydrated via
  joins.
- **`availability_request` is rota-scoped, not shift-scoped.** One request row
  per volunteer per rota, answered by a single form covering all the rota's
  shifts. It keeps `rota_id` and gets no `shift_id`. Its `shift_date` column
  was write-only (always the rota's start, read by nothing) and was dropped.
- **`rotation.start` and `rotation.shift_count` dropped.** Both are derived in
  SQL (`MIN(shift.date)`, `COUNT(*)`) by `GetRotations`. No stored ordering
  column — that would be a cached copy of `MIN(shift.date)` that can drift.
  Invariant: a rotation always has ≥1 shift.
- **`closed` deliberately stays config-derived** (rrule evaluation at read
  time), even though it is the sharpest instance of the weirdness this change
  addresses. Moving it onto the shift row is deferred until admin tooling moves
  from the config file to the web server, because closing a shift in an
  already-defined rota currently happens by config edit and would otherwise
  need manual SQL. Do not "fix" this by snapshotting closed at mint time
  without also building the close/open command.
- **The v1 table is deliberately thin.** Notes, times, and size arrive as
  `ALTER TABLE` when their features do; the v1 job is identity plus the FK
  spine.
- **Backfill mints from rotation arithmetic, not child rows**, so rotations
  with no allocations or requests still get their shifts; child
  `(rota_id, shift_date)` pairs are verified against minted shifts before the
  FKs are added.

## Consequences

- Shift-date arithmetic exists in exactly one place (`DefineRota`, at mint
  time). Everything else reads shift rows.
- "Shift" in the glossary means the entity; the assignee-resolved projection
  served by shift listing is a "Shift View" (`CONTEXT.md`).
- The column drops land in a follow-up migration after all readers are on
  shift-table queries, so the re-keying step is not entangled with rewriting
  rotation ordering logic.
