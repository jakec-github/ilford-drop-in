# The allocation journey: design

Design for issue #117 — bringing the whole path from defining a rota to
allocating it into the app, and moving the settings that path depends on out of
the config file.

Decision records: [ADR 0006](adr/0006-domain-settings-in-the-app.md),
[ADR 0007](adr/0007-shift-start-as-local-time.md),
[ADR 0008](adr/0008-draft-allocations.md). Language: `CONTEXT.md`, which now
carries the agreed vocabulary ahead of the code — `Rota Defaults`, `Allocation
Settings`, `Draft Rota Allocation`, `Standing Preallocation`, `Retired Role`.

Roles S2 (#90) is a prerequisite, not part of this, with one scope change noted
below.

## The journey

One rota is in flight at a time. Everything below happens to that rota, and
nothing gates anything else — Shapes, pins and availability move in parallel,
which is why the screen is a screen and not a wizard.

1. **Define.** Shift count and start date, prefilled with the Sunday after the
   last rota's end. Shape and times prefilled from Rota Defaults, editable
   there. Standing Preallocations seed ordinary pins. Defining is refused while
   an unallocated rota exists.
2. **Prepare.** Per-Shift edits while the Rotation is unallocated: Shape, open
   or closed, start and end, pins.
3. **Ask.** The availability round is minted and sent, as today.
4. **Watch.** A Draft Rota Allocation is re-solved on a six-hourly tick and on
   demand, shown in the rota view as dashed-border chips, with its solve
   outcome and whether inputs have moved since.
5. **Allocate.** Re-solve, compare the result against the draft, commit and
   stamp on a match. From here the rota is the rota, changes are Alterations,
   and the next rota can be defined.

**Discard** is available at any point before allocation and destroys the
rotation, its Shifts, Shapes, pins, round and responses in one transaction —
including sent responses, behind a confirmation that says how many. Without it,
one mistyped shift count wedges the whole system, because only allocation ends
a rota's life.

## What freezes, and when

The rule is **allocator inputs freeze at allocation; descriptive things do
not.**

| Thing | Allocator input? | After allocation |
|---|---|---|
| Shape | yes | fixed |
| Closed | yes | fixed |
| Shift date (via `start_at`) | yes | fixed |
| Preallocations | yes | spent; changes are Alterations |
| Shift times | no — the solver works in dates | still editable |
| Allocation Settings | yes, but live and global | unchanged by allocation; see ADR 0006 |

## Settings

`drop_in_config.yaml` keeps operator concerns only: sheet ids, database URL,
gmail, server, dev mode, admin emails. Everything else moves to Rota Defaults on
the admin settings screen — the Roles that exist, the default Shape, default
shift times and timezone, Standing Preallocations, and the Allocation Settings.

The switchable rules, all defaulting off when unanswered, all already
enumerated in `constraints/__init__.py`:

| Setting | Today |
|---|---|
| max frequency | `maxAllocationFrequency` in config; becomes a toggle **and** a value |
| male cover | `requiresMale` in config plus `male_required`; two halves of one idea, merged |
| no back-to-back | always on |
| one shift per month | coded, commented out of the default list |

The fundamental six constraints and all four preferences are not switchable — a
rota without them is not a rota, and the preferences are a weighted objective
rather than a rule.

## Admin area

| Tab | Becomes |
|---|---|
| Volunteers | unchanged |
| Config | the settings screen — Rota Defaults, Roles, Standing Preallocations |
| Availability | folded into Allocation |
| Rota | folded into Allocation |
| **Allocation** | the journey: define form when nothing is in flight, otherwise the working screen |

An allocated rota has no stage of its own — the Allocation tab points at the
rota page, which was already showing the draft.

## CLI

`allocate_rota`, `define_rota` and (subject to checking #64's editor covers it)
`change_rota` are deleted. `allocate_rota` is the one that matters: the design
rests on allocating the rota you were shown, and a command that solves and
commits in one step cannot honour that. `publish_rota` stays — publishing to the
Sheet is distribution, a different act with a different audience.
`view_historical_responses` and `list_volunteers` stay.

## Slices

Each is a vertical slice, API and screen together.

1. **Roles become rows.** Table with a stable id; name, max, priority, colour;
   retire rather than delete; management on the settings screen; `roles` leaves
   config. Land PR #125 (role colours in config) first so colour moves once.
   **Blocks #90.**
2. **#90 Roles S2 — Shifts own their Shape.** As ticketed, with one change:
   `shift_requirement` seats reference a Role **id** with a foreign key, not a
   name. Historical tables keep names as `TEXT`.
3. **Shifts carry their start and end.** `start_at` / `end_at` as local
   `TIMESTAMP`, `shift.date` deleted, unique index on `start_at::date`,
   migration composing both from each Shift's date and the current config
   times. Closes #32 and part of #23.
4. **Rota Defaults and the settings screen.** Settings record, allocation
   settings as `jsonb`, default Shape as rows, default times and timezone,
   Standing Preallocations. `defaultShape`, `requiresMale`,
   `maxAllocationFrequency`, shift times and `rotaOverrides` leave config;
   Config Preallocations are deleted outright. Invalid settings block
   allocation and nothing else.
5. **One rota in flight.** Define screen (count, start date, Shape, times,
   seeded pins), refusal while an unallocated rota exists, discard.
6. **Draft Rota Allocations.** `draft_allocation` table, solve outcome, dirty flag,
   six-hourly tick, re-solve on demand, dashed chips in the rota view.
7. **Allocate in the app.** Re-solve, output-hash comparison, commit and stamp;
   `allocate_rota` deleted.
8. **The Allocation tab.** Merges the Rota and Availability tabs into one
   two-state screen; `define_rota` deleted.

## Deliberately not in scope

- **Provenance of an allocated rota.** No record of which toggles produced it —
  ADR 0006.
- **A negative pin.** No way to forbid a placement before allocation — ADR 0008.
  The intended answer is admin-edited availability, which is its own feature.
- **Admins editing a volunteer's availability.** Wanted, and the right fix for
  "not her", but a separate feature with its own value; smuggling it in as a
  draft-editing affordance would design it badly.
- **Publishing.** The rota Sheet may be vestigial next to the rota page and the
  calendar feed, but that is a question about how volunteers read the rota, not
  about allocation.
- **Cadence.** Weekly, on the start date's weekday, not exposed. Fortnightly or
  hand-picked dates would pull the rest of #23 into this work, and the
  one-Shift-per-date rule is the seam most likely to break.
- **Roles S3.** The new Roles turn on afterwards, as configuration — except
  that they are now configuration in the app.
