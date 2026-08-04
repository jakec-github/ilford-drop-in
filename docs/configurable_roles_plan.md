# Configurable roles: design

Design for issue #4. Supersedes the previous contents of this file, which
mapped the blast radius across the Go allocator — deleted since (#34) — and is
no longer a useful guide.

Decision record: [ADR 0005](adr/0005-roles-as-jobs-volunteers-hold.md).
Language: `CONTEXT.md` (Role, Seat, Shape). Note that `CONTEXT.md` now
carries the agreed language ahead of the code; none of it is implemented yet.

Tracks were part of this design and have been dropped — see the ADR for why.
A person simply fills one Seat per Shift.

## The model

A **Role** is a job on a Shift. A volunteer **holds** the Roles they will do,
and only a holder may be allocated to one — eligibility is exact match, with no
mapping layer and no open-to-all shortcut.

A person fills **at most one Role on a Shift**, however many they hold. Emma
may hold Team lead, Service volunteer and Food collector; on any given Shift
she does exactly one of them.

A **Shape** is which Roles a Shift needs and how many **Seats** of each. The
Shift owns it from mint. It is editable while the Rotation is unallocated and
fixed once the allocator has run — the manual-preallocation rule (ADR 0003).

### Config

```yaml
requiresMale: true

roles:
  - name: Team lead          max: 1  priority: 1
  - name: Assistant TL       max: 1  priority: 2
  - name: Food collector     max: 1  priority: 3
  - name: Service volunteer          priority: 4
  - name: Hot food           max: 1  priority: 5

defaultShape:
  - role: Team lead          count: 1
  - role: Assistant TL       count: 1
  - role: Food collector     count: 1
  - role: Service volunteer  count: 6
```

`max` is the ceiling — how many of that Role a Shift may ever hold, however it
is edited. Omitted means no ceiling. `priority` orders the filling of Seats
when people are scarce. Both are role-level so that a per-date Shape override
cannot forget to cap team leads.

`requiresMale` is Shift-level: every open Shift must have a male allocated
somewhere or leave a Seat open, whichever Role it belongs to.

### Roster

Roles are held in tick-box columns found by the ` - Role` suffix, so the
sheet's many unread columns stay invisible and the data-validated tick cannot
be mistyped. The Role name comes first because that is what someone scanning
the header row reads:

| Unique ID | First name | … | Team lead - Role | Assistant TL - Role | Service volunteer - Role | Food collector - Role |
|---|---|---|---|---|---|---|
| XYZ | Emma | | ✓ | ✓ | ✓ | |
| ABC | Michael | | | ✓ | ✓ | |
| DEF | Priya | | | | ✓ | ✓ |
| GHI | Sam | | | | | ✓ |

Config stays authoritative for which Roles exist. A ` - Role` column config
does not name warns and does nothing; a configured Role with no column warns
too, since nobody can hold it.

## Rules

**Allocating.** The solver fills Seats up to each Shape count, in Role priority
order, never exceeding a count. Counts are targets, not minimums: a Shift with
no available team lead still allocates with that Seat empty, exactly as today.
A person takes at most one Seat on a Shift. With `requiresMale` set, every open
Shift must either have a male allocated or leave some Seat open, so one can be
added by hand afterwards.

**Editing an allocated rota.** The Role is named explicitly on every add —
`inferRole()` goes. Ceilings alone govern edits, not the frozen Shape: adding a
Food collector to a week whose Shape asked for none is fine; a second Team lead
never is. Placing someone in a Role they do not hold warns and proceeds.

**Pinning.** Every preallocation names a Role, custom entries included — which
is what lets `"St John's team"` hold Hot food. Pinning someone to a Role they
do not hold was to be an error, as the team-lead equivalent was then; #109
reversed that, and the pin now grants the Role for that one shift (ADR 0005,
amended). Pinning to a Role the shift has no Seat for is still an error.

Headcount is distinct people, so no Role needs a "counts toward shift size"
flag.

## What this replaces

| Today | Becomes |
|---|---|
| `RoleTeamLead` / `RoleVolunteer` constants, `Role.IsValid()` | Roles resolved from config |
| `defaultShiftSize`, `RotaOverride.shiftSize` | `defaultShape`, Shape overrides |
| `RotaOverride.preallocatedTeamLeadID` | A pin naming the Team lead Role |
| `Volunteer.Role` (single) | The set of Roles a volunteer holds |
| `Member.is_team_lead`, `VolunteerView.seat_cost` | Held Roles; no seat cost |
| `x[(volunteer, shift)]` | `x[(volunteer, shift, role)]` + attendance var |
| `at_most_one_team_lead` constraint | Per-Role ceilings |
| `shift_capacity` counting ordinary seats | Per-Role Seat counts |
| `male_required`'s two hardcoded escapes | Any open Seat, under `requiresMale` |
| `OutputShift.team_lead_id` + `volunteer_ids` | Role-tagged assignments |
| `inferRole()` in `changeRota` | An explicit Role on every add |
| `PublishedRotaRow.TeamLead`, hand-typed `HotFood` / `Collection` | A column per capped Role, list cell for uncapped |

## Blast radius

Config and model: `internal/config/config.go`, `pkg/core/model/models.go`.

Roster: `pkg/clients/sheetsclient/volunteers.go` (currently rejects any value
that is not one of the two constants), `internal/devmode/roster.go`,
`test_data/volunteers.csv`.

Solver contract and pyallocator: `pkg/core/allocator/cpsat_contract.go`,
`init.go`, `types.go`; `pyallocator/src/pyallocator/` — `domain.py`,
`problem.py`, `model_builder.py`, `solution.py`, and the `constraints/` and
`preferences/` packages.

Services: `allocateRota.go`, `changeRota.go`, `publishRota.go`,
`viewResponses.go`, `preallocations.go`, `allocationHelpers.go`,
`listShifts.go`, `volunteerCalendar.go`, `utils/alterations.go`.

Edges: `pkg/api/preallocations.go` and `volunteers.go`; `cmd/cli/commands/`
(`allocate_rota.go`, `publish_rota.go`, `view_responses.go`);
`pkg/clients/sheetsclient/rotas.go`; `web/src/api.ts`, `types.ts`,
`RotaViewer.tsx`, `AdminVolunteers.tsx`.

Database: a `shift_requirement` table (slice 2). `allocation.role`,
`alteration.role` and `manual_preallocation.role` stay `TEXT` holding the Role
name, so historical rows remain readable when a Role is retired.

## Slices

**S1 — Roles as data, behaviour unchanged.** Config gains roles; the roster
moves to ` - Role` columns; the solver assigns Roles; services, CLI and
published output stop special-casing team leads. Configured with the two
existing Roles, output is identical to today, so the existing test suite is the
oracle for the solver rewrite — the genuinely risky part. No user-visible
change.

**S2 — Shifts own their Shape.** `shift_requirement` table written at
define-rota, editable over HTTP and in the admin UI while the Rotation is
unallocated, frozen at allocation. `defaultShiftSize` and
`RotaOverride.shiftSize` retire in favour of `defaultShape` and Shape overrides.

**S3 — The new Roles.** Assistant TL, Food collector and Hot food turned on:
config entries, roster columns, published columns, and the rota editor's Role
picker. Mostly configuration by this point. Note that each new Seat costs a
distinct person — a collector cannot also serve — so the default Shape's
`Service volunteer` count is a decision to take here, not an afterthought.

## Migration

**Every team lead must hold both Team lead and Service volunteer.** Today a
team lead is only a team lead on the roster, yet the allocator routinely places
non-designated leads in ordinary seats (`solution.py` extracts one designated
lead per shift and reports the rest as ordinary volunteers). Ticking only
`Team lead - Role` would remove them from ordinary Seats and S1 would not be
behaviour-preserving on its first solve.

`Service volunteer` keeps its name, so existing `allocation.role` values need
no migration. The roster's existing single `Role` dropdown column is retired
once the tick columns are populated.

## Deliberately not in scope

- **Multi-tenancy.** Roles becoming rows is what a second organisation would
  need; scoping them to one is a separate axis, cheap to add later and
  expensive to speculate on now.
- **Generalised volunteer attributes.** `requiresMale` stays a named
  Shift-level flag rather than becoming a configurable cover requirement over
  arbitrary attributes.
- **Tracks.** Groups of mutually exclusive Roles, so one person could lead the
  serving line-up and collect the food on the same Shift. Designed, then
  dropped as not worth its cost (ADR 0005). Anyone needing two jobs at once
  gets a Role naming the combination.
- **Collection as its own activity.** Food collection is a Role on the drop-in
  Shift, not a separate scheduling stream with its own dates and its own
  availability round. That reading was considered and rejected as a much larger
  change colliding with the availability work in #75/#76.
- **Role authoring in the UI.** Roles are configured in YAML; Shapes are edited
  per Shift in the UI (S2). If role authoring moves to the database later, it
  should move wholesale rather than being seeded from config, so there is never
  a moment with two sources of truth.
