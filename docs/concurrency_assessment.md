# Concurrency hazard assessment

Issue #5. Written 2026-07-16 against `main` (41eb13c), which includes the
issue #8 double-allocation guard (PR #40).

## Context

The system began as a single-admin CLI. A web server (`cmd/server`) now runs
alongside it with one mutating endpoint (`POST /alterations`), and more
admin-triggered endpoints are planned. Two actors — two admins, or an admin and
an agent — can therefore interleave writes to the same rota state today.

The persistence layer (`pkg/db`) wraps each multi-statement write in a
transaction, so no single flow can commit half its rows. What is missing is
isolation across a flow's *read-validate-write* span: every service reads
state, validates in Go, then writes, with nothing preventing the state from
changing in between (a check-then-act, or TOCTOU, pattern). The only other
concurrency control in the codebase is the mutex on the server's volunteer
cache (`pkg/api/volunteercache.go`), which is sound but unrelated.

PR #40 established the repo's first cross-flow serialisation point: inside the
`InsertAllocationsAndSetAllocated` transaction it takes `SELECT
allocated_datetime FROM rotation WHERE id = $1 FOR UPDATE`, locking the
rotation row and reading the completed-allocation marker in one shot, and
refuses if `allocated_datetime` is already set. Several remediations below
reuse that same lock rather than inventing a second scheme.

## Mutating flows

| # | Flow | Entry points | Writes |
|---|------|--------------|--------|
| 1 | Define rota (`services.DefineRota`) | CLI `defineRota` | `rotation` + `shift` rows (one tx) |
| 2 | Request availability (`services.RequestAvailability`) | CLI `requestAvailability` | `availability_request` inserts (one tx); later `form_sent` update. External: creates Google Forms, sends emails |
| 3 | Send reminders (`services.SendAvailabilityReminders`) | CLI `sendAvailabilityReminders` | no DB writes; sends emails |
| 4 | Allocate rota (`services.AllocateRota`) | CLI `allocateRota` | `allocation` rows + `rotation.allocated_datetime` (one tx) |
| 5 | Change rota (`services.ChangeRota`) | CLI `changeRota` **and** `POST /alterations` | `cover` + `alteration` rows (one tx) |
| 6 | Publish rota (`services.PublishRota`) | CLI `publishRota` | no DB writes; overwrites Google Sheets tabs |
| 7 | Migrations (`db.RunMigrations`) | CLI startup, server startup | DDL + data migrations |

`addCovers.go` is an unimplemented stub. All other services
(`listShifts`, `viewResponses`, `viewHistoricalResponses`,
`volunteerCalendar`) are read-only.

## Hazards

### Real (can produce a bad committed state)

#### H1 — Change rota vs change rota on the same shift *(top rank)*

`ChangeRota` builds the shift's effective state (base allocations + existing
alterations), validates the proposed change against it in Go, then inserts —
three separate pool calls with no shared transaction or lock
(`pkg/core/services/changeRota.go`). The invariants it enforces ("in" not
already on the shift; "out" currently on the shift) hold only if nothing
commits between validation and insert.

Interleavings and outcomes (both actors validate against the same pre-state,
both commit):

- **Both add volunteer Y** → two `add` alterations for Y.
  `utils.ApplyAlterations` appends on every `add`, so Y appears twice on the
  published rota, and the "already on the shift" validation is permanently
  violated for that date.
- **A does out=X in=Y; B does out=X in=Z** → X removed, Y *and* Z added: the
  shift ends up one over its intended size, and each admin believes their
  swap is what happened. The double `remove` of X is masked (remove filters
  all matches), so the audit trail (two covers, each claiming to replace X)
  no longer describes the effective state.

This is the one flow already exposed to two actors today — the web endpoint
has no auth, and the CLI runs beside it — so it is both the most likely and
the most damaging hazard. Classification: **corruption** (invariant-violating
committed state that self-heals only by manual compensating alterations).

#### H2 — Change rota vs allocate rota on the same rota

`ChangeRota` happily targets a shift whose rota has no allocations yet (the
effective state is just empty), and `AllocateRota` knows nothing about
alterations. Interleaving: an admin creates an alteration adding volunteer Y
to a date while allocation for that rota is mid-solve; allocation then
commits, possibly allocating Y to the same date. Result: Y appears twice
(base allocation + `add` alteration), same terminal state as H1.
The same applies with the ordering reversed (validation reads pre-allocation
state, insert lands post-allocation).

PR #40's rotation-row `FOR UPDATE` gives allocation a lock, but `ChangeRota`
never takes it, so the two flows still interleave freely. Classification:
**corruption**, lower likelihood than H1 (allocation runs a handful of times a
quarter, and alterations against a not-yet-allocated rota are unusual).

#### H3 — Request availability vs request availability on the same rota

`RequestAvailability` reads the rota's requests, decides which volunteers lack
a sent request, creates a Google Form per volunteer, inserts rows, then sends
emails. Two concurrent runs both see volunteer V without a request, both
create a form, and both insert — nothing stops them: `availability_request`
has no uniqueness on `(rota_id, volunteer_id)` (only `id` is the PK,
migration 005). Result:

- V receives two emails with two different form links (irreversible external
  side effect).
- The rota now has two sent requests for V. `AllocateRota` feeds one
  `VolunteerAvailability` entry per request into the solver
  (`allocationHelpers.go`), so if V answered only one form the other reads as
  a non-response — the solver input for V is duplicated and contradictory.

Classification: **corruption** (duplicated persistent state with ambiguous
downstream meaning) plus unrecallable external effects. Likelihood grows once
availability moves to admin-triggered web endpoints (planned per the OIDC
sync plan).

### Staleness (wrong-but-recoverable, or intent mismatch)

- **S1 — "Latest rota" selection races with define rota.**
  `RequestAvailability` and `AllocateRota` both pick `FindLatestRotation` at
  read time. If `DefineRota` commits mid-run, the running flow continues
  against the previously-latest rota. Internally consistent; the operator's
  intent may be off by one rota. Worth revisiting when flows move to the API
  (pass an explicit `rota_id` instead of "latest").
- **S2 — Publish rota races with change rota.** The published sheet can miss
  an alteration committed mid-publish; re-publishing heals it. The
  allocations/alterations reads are two separate queries, but an alteration
  landing between them only adds to state the publish already read, so no
  torn output worse than staleness.
- **S3 — Volunteer cache TTL (server only, 5 min).** A `POST /alterations`
  may validate against a roster up to 5 minutes stale: a just-added volunteer
  is rejected (`ErrNotFound`), a just-deactivated one accepted. Self-healing.
- **S4 — Reminders race with responses.** A volunteer answering while
  `SendAvailabilityReminders` runs may get a redundant reminder. Harmless.

### Benign (a constraint or transaction already fails the loser loudly)

- **B1 — Define rota vs define rota.** Both runs compute the same start date
  and mint overlapping shifts, but `shift.date` is `UNIQUE` (migration 007)
  and the rotation + shifts insert is one transaction, so the second run
  fails wholesale and writes nothing. Note this guard is *incidental* — the
  unique index exists for ADR 0001 reasons, and nothing marks it as
  load-bearing for concurrency.
- **B2 — Allocate rota vs allocate rota.** Issue #8; guarded race-safely by
  PR #40: the insert transaction locks the rotation row with `FOR UPDATE`,
  reads `allocated_datetime` in the same statement, and refuses if it is set,
  so a racing run blocks on the lock and then observes the winner's
  timestamp. (Nuance: the guard keys on `allocated_datetime`, not on
  allocation rows existing — a pre-migration-002 rota with rows but a NULL
  timestamp would not be caught, but `AllocateRota` only ever targets the
  latest rota, so this is moot in practice.)
- **B3 — Concurrent migrations (server + CLI starting together).** Each
  migration runs in its own transaction and records itself in
  `schema_migrations` (PK on filename) inside that transaction; Postgres DDL
  is transactional, so the losing process fails loudly with nothing
  half-applied.
- **B4 — Volunteer cache.** Mutex-guarded with a refresh rate limit; correct.

## Ranking

1. **H1** — change rota vs change rota (live today via web + CLI; corrupts the
   invariant the flow exists to enforce).
2. **H3** — duplicate availability requests (corrupting and externally
   visible; likelihood rises with planned web endpoints).
3. **H2** — change rota vs allocate rota (same terminal state as H1, much
   rarer window).

## Proposed remediation — filed as #41

The three remediations are rolled into a single ticket, #41, covering:

1. **Serialise rota changes against the rotation row (H1 + H2).** Move
   `ChangeRota`'s effective-state read, validation, and
   `InsertCoverAndAlterations` into one transaction that first takes
   `SELECT … FOR UPDATE` on the target rota's rotation row (and the swap
   rota's, in a consistent order, when `swapDate` crosses rotas). This is the
   same lock the issue #8 guard takes in `InsertAllocationsAndSetAllocated`
   (PR #40), so it also closes the change-vs-allocate race. Per-rota locking
   is coarse but correct at this system's scale.
2. **One availability request per volunteer per rota (H3).** A unique index
   on `availability_request (rota_id, volunteer_id)` (after a deduplicating
   data migration), so the losing run's insert fails wholesale before its
   email loop starts. Emails and form creation are inherently
   non-transactional; the constraint only guarantees the DB can't record the
   duplicate and the loser stops before emailing.
3. **Pin down the incidental define-rota guard (B1, low priority).** A test
   (or at minimum comments on the migration and `InsertRotationAndShifts`)
   asserting that two rotas minting the same shift date cannot both commit,
   so a future schema change doesn't silently reopen the race.

## Out of scope, noted in passing

- `RequestAvailability`'s partial-failure behaviour (form created, insert
  fails → orphan form) is a sequential robustness issue, not a concurrency
  one.
- Auth on `POST /alterations` (tracked by the OIDC plan) reduces exposure but
  does not remove any hazard above: two authenticated admins race identically.
