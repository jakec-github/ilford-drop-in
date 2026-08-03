# Issue #89 — Roles S1: Roles as data

## Context

Roles are hardcoded today as two constants (`model.RoleTeamLead`,
`model.RoleVolunteer`) with `Role.IsValid()` gating the roster, and the
team-lead concept is smeared across ~16 behavioural sites: a `seat_cost` of 0 in
the solver, an `at_most_one_team_lead` constraint, a single-valued
`preallocatedTeamLeadID`, an `inferRole()` guess in the rota editor, a dedicated
`TeamLead` column in the published sheet, and a `"lead" | "volunteer"` union in
the frontend. Adding Assistant TL, Food collector or Hot food (#91) is
impossible until that is data.

S1 de-hardcodes without changing what the system does. Config gains `roles`; the
roster moves to `<name> - Role` tick columns; the solver assigns Roles instead of
designating a lead after the fact. Configured with the two existing Roles, output
matches today — which makes the existing test suite the oracle for the solver
rewrite, the genuinely risky part.

Design: [`configurable_roles_plan.md`](configurable_roles_plan.md),
[ADR 0005](adr/0005-roles-as-jobs-volunteers-hold.md).
Language already agreed in `CONTEXT.md` (Role, Seat, Shape). Tracks were part of
the design and were dropped before implementation: a person fills one Seat per
Shift, full stop, and male cover is a Shift-level rule.

### Three decisions taken up front

**1. One accepted divergence from byte-identity.** `at_most_one_team_lead`
counts per *group*, so today a second team-lead holder cannot attend a shift at
all — not even in an ordinary seat. Once the ceiling applies per-Role, they can
take a Service volunteer Seat. ADR 0005 deletes the per-group hack deliberately,
and the migration note (every team lead also holds Service volunteer) exists
precisely so leads appear in ordinary Seats. Same knock-on: a team lead pinned
via `preallocatedVolunteerIDs` is silently promoted to lead today by
`solution.py`; under role-named pins they stay a Service volunteer. Both
divergences are accepted, must be called out in the PR body, and the acceptance
criterion reads "identical except where two team-lead holders now share a
shift".

**2. Go sends a Shape.** The contract's `size` becomes
`shape: [{role, count}]`, computed Go-side: each capped Role gets `max` Seats,
the single uncapped Role gets `shiftSize` Seats (custom preallocations eat into
it). Config validation requires exactly one uncapped Role in S1. S2 then only
changes where the Shape is read from — no second contract change.

**3. The frontend widens to role names.** `Role` becomes `string` end-to-end;
the API stops speaking `teamLead: boolean`. Role pickers keep two hardcoded
options until S3 adds a roles endpoint.

## Commit sequence

Thirteen commits, each building and passing `scripts/check.sh`. Commits 6–9 are
the solver; they are deliberately fine-grained because that is where the risk
is.

---

### 1. Make the pyallocator suite runnable and green

Nothing runs pytest — not `scripts/check.sh`, not `.github/workflows/ci.yml` —
and the suite is red on main: `pyallocator/tests/test_end_to_end.py:228` asserts
`constraints_applied` contains `one_shift_per_month`, but
`constraints/__init__.py:47` leaves it out of `DEFAULT_CONSTRAINTS`. An oracle
nobody runs is not an oracle.

- Fix the `constraints_applied` assertion; check whether `verify_solution`
  (`test_end_to_end.py`) still enforces the one-per-month rule and drop it if
  so. Run the whole suite and fix anything else red.
- `scripts/check.sh`: bootstrap `pyallocator/.venv` if absent
  (`python3 -m venv` + `pip install -e "pyallocator[dev]"`), then
  `pyallocator/.venv/bin/pytest pyallocator/tests`. Slot it after `go test`.
- `.github/workflows/ci.yml`: add `actions/setup-python` with the version from
  `pyproject.toml`'s `requires-python`.

Files: `pyallocator/tests/test_end_to_end.py`, `scripts/check.sh`,
`.github/workflows/ci.yml`.

### 2. Capture the behaviour oracle

Add a golden test asserting the solved rota for `make_e2e_input()`, written as
rota *content* — `{shift index → (lead id, sorted volunteer ids, customs)}` —
not raw contract JSON, so it survives the field renames in commits 5 and 9.
`solver.py` already pins the seed and a single worker, and
`test_end_to_end_is_deterministic` proves the rota is stable, so this is safe.

Also relax `test_end_to_end.py:227` and `tests/constraints/test_structural.py:21`,
which assert exact `num_variables` counts that role vars will change; decide now
that `Diagnostics.num_variables` counts every var in the model.

Files: `pyallocator/tests/test_end_to_end.py`, new
`pyallocator/tests/testdata/e2e_rota.json`, `tests/constraints/test_structural.py`.

### 3. Roles in the domain model and config

Domain types in `pkg/core/model/models.go` — role *names* stay plain `string`
on the wire, in the DB and on allocations, so no migration is needed
(`allocation.role`, `alteration.role`, `manual_preallocation.role` are already
untyped `TEXT`):

```go
type Role  struct { Name string; Max *int; Priority int }  // Max nil = uncapped
type Roles struct { /* ordered by priority, indexed by name */ }
func (r Roles) ByName(string) (Role, bool)
func (r Roles) ByPriority() []Role
func (r Roles) Uncapped() (Role, bool)
```

`internal/config/config.go` gains `roles` and a Shift-level `requiresMale`, and
unmarshals into those. `validator.v10` tags cannot express the cross-field
rules, so they go in the manual loop in `Validate()` (`config.go:167`) next to
the existing `ParseRRule` check: names unique and non-empty, priorities unique,
`max >= 1`, and **exactly one uncapped Role** — with a comment naming S2 as
what lifts that restriction.

Same commit, the `RotaOverride` preallocation keys collapse into one role-named
list, since every preallocation must name a Role and the solver contract needs
it:

```yaml
preallocations:
  - volunteerID: XYZ
    role: Team lead
  - custom: "St John's team"
    role: Service volunteer
```

replacing `customPreallocations`, `preallocatedVolunteerIDs` and
`preallocatedTeamLeadID`. **Deploy note for the PR body: the live config on the
droplet needs the same edit.** `DefaultShiftSize` and `RotaOverride.ShiftSize`
stay untouched (S2 retires them).

`requiresMale: true` in every config preserves today's behaviour, which is
unconditional; the flag exists so it can be turned off rather than because
anyone will.

Nothing consumes the new fields yet. Update `drop_in_config.dev.yaml`,
`drop_in_config.test.yaml`, `deploy/drop_in_config.dev.yaml`, and
`internal/config/config_test.go` (which already round-trips `RotaOverride`).

Keep `RoleTeamLead` / `RoleVolunteer` / `IsValid()` alive for now — commit 13
deletes them, so the intermediate commits still build.

### 4. Roster: `<name> - Role` tick columns

`pkg/clients/sheetsclient/volunteers.go:95` discovers columns by exact header
match from a fixed `volunteerFields` list and hard-errors on any Role value that
is not one of the two constants (`:144-147`). Replace with suffix discovery:

- Scan the header for ` - Role` suffixed columns; a column config does not name
  logs a warning and is ignored; a configured Role with no column logs a
  warning too. The legacy `Role` dropdown column does not match the suffix, so
  the two can sit side by side while the ticks are being filled in.
- A truthy tick (`TRUE`/`✓`/`yes`, trimmed, case-insensitive) means held.
- `model.Volunteer.Role Role` → `Roles []string`, plus
  `func (v Volunteer) Holds(role string) bool`.
- `ParseVolunteers` needs the role table. `internal/devmode/roster.go:52` calls
  it with no config in scope, so pass `model.Roles` as a parameter through both
  `sheetsclient.ListVolunteers` (already takes `*config.Config`) and
  `devmode.LoadVolunteers`.

`test_data/volunteers.csv`: replace the `Role` column with
`Team lead - Role` and `Service volunteer - Role`. **Every one of the 6 team
leads gets both ticks** — the acceptance criterion, and without it they vanish
from ordinary Seats on the first solve.

Downstream sites keep compiling by swapping `vol.Role == model.RoleTeamLead` for
`vol.Holds(string(model.RoleTeamLead))`; behaviour is unchanged in this commit.

There is no `volunteers_test.go` today — add one covering suffix discovery, both
warning cases, and truthiness. Update `internal/devmode/roster_test.go`.

### 5. Contract input carries Roles, Shape and role-named pins

`pkg/core/allocator/cpsat_contract.go` and pyallocator's `serialization.py` /
`domain.py`, in lockstep — `pkg/core/services/cpsatContract_test.go` holds
inline JSON goldens for exactly this reason.

| Today | Becomes |
|---|---|
| `CpsatMember.IsTeamLead bool` | `Roles []string` (`"roles"`) |
| `CpsatShift.Size int` | `Shape []{Role, Count}` (`"shape"`) |
| `PreallocatedVolunteerIDs`, `PreallocatedTeamLeadID`, `CustomPreallocations` | `Preallocations []{VolunteerID, Custom, Role}` |
| — | top-level `Roles []{Name, Max, Priority}`, `RequiresMale bool` |

Go side: `BuildCpsatInput` (`cpsat_contract.go:96`) computes the Shape per shift
from `shiftSize` + role maxes; `allocator.ShiftOverride` and `InitShifts`
(`init.go:261`) carry role-named pins — note the per-date union semantics
(append for lists, last-wins for the single TL, closed wipes everything) now
apply uniformly to one list. Drop the "group has N team leads (max 1)" error at
`init.go:73`.

Python side: parse the new fields, and keep behaviour by deriving
`is_team_lead = "Team lead" in roles` and the old `seat_cost` from the Shape's
uncapped count. **Deliberately ugly and deliberately temporary** — it keeps this
commit a pure contract change with the golden from commit 2 still passing.
Commit 6 starts removing it.

### 6. Solver: attendance and role variables, no behaviour change

The refactor that makes the rest safe. `model_builder.py:build` creates:

```python
role_var[(v.id, s.index, role)]  # for each role v holds that s's shape asks for
attend[(v.id, s.index)]          # sum(role_vars) == attend — one Seat at most
```

Equating the sum with a boolean `attend`, rather than `AddMaxEquality`, is what
enforces one Role per person per Shift: no separate exclusion constraint exists
or is needed.

`constraints/base.py`'s `AssignmentVars = dict[tuple[str, int], IntVar]` becomes
a `Vars` dataclass with `.attend` and `.role`; both the `Constraint` and
`Preference` protocols take it. Six constraints are pure attendance rules and
need only the swap: `grouping`, `availability`, `closed_shifts`,
`max_frequency`, `no_back_to_back`, `one_shift_per_month`. `preallocations.py`
forces the group's `attend` and, additionally, the pinned person's specific
role var. `no_duplicate_allocation.py`'s key-set invariant is rewritten for the
new var space.

`shift_capacity`, `at_most_one_team_lead`, `male_required` and `even_fill` stay
on the temporary `seat_cost`/`is_team_lead` derivation, now reading `attend`.
`solution.py` still designates a lead post-hoc. **The commit-2 golden must still
pass unchanged** — that is the whole point of doing this separately.

### 7. Per-Role Seat capacity replaces `seat_cost` and `at_most_one_team_lead`

New `constraints/seat_capacity.py`: per open shift, per Role in the Shape,
`sum(role_var[v, s, role]) <= count`, less any custom pins on that Role. A
second, unconditional ceiling `<= role.max` regardless of Shape (belt and
braces, and what makes S2's editable Shapes safe). Delete
`constraints/shift_capacity.py`, `constraints/at_most_one_team_lead.py`,
`VolunteerView.seat_cost` and `VolunteerView.is_team_lead`, and their tests —
replacing them with per-Role equivalents.

`preferences/even_fill.py` becomes per-Role. Preserve today's numbers exactly
for the uncapped Role: harmonic `EVEN_FILL_WEIGHT // seat_number` = 60 // n,
with customs on that Role occupying the first levels. Capped Roles get a flat
weight *above* the top uncapped seat weight, ordered by priority, so a Team lead
Seat is always filled before an ordinary one. This is what makes the solver
prefer putting an attending team lead in the lead Seat rather than an ordinary
one — today's behaviour, which was previously a tie CP-SAT happened to break the
right way. Document the weight scheme in the module docstring next to the
existing one.

**This is the commit where the accepted divergence lands.** Expect the commit-2
golden to change for shifts where two team-lead holders can now share; inspect
every diff line by hand before re-recording it, and put the before/after in the
PR body.

### 8. Male cover generalises over Seats

`constraints/male_required.py` today builds three reified BoolVars per shift —
`has_male`, `tl_slot_open`, `seat_open` — the last two being hardcoded escapes.
Rewrite as: when `requiresMale` is set, every open shift must have a male
allocated *or* leave some Seat in its Shape open, whichever Role that Seat
belongs to. The two escapes collapse into one — a Team lead Seat and an
ordinary Seat are both just Seats — and behaviour is preserved. Skip the
constraint entirely when the flag is off.

`preferences/spread_males.py` counts males per shift via `attend` and needs no
change.

Rewrite `tests/constraints/test_male_required.py` (6 tests over the three
escapes) around the single open-Seat escape, plus a case for the flag off.

### 9. Role-tagged output; the designation logic dies

`OutputShift.team_lead_id` + `volunteer_ids` become
`assignments: [{volunteer_id, custom, role}]`. `solution.py` reads the role vars
directly — the whole post-hoc "first allocated team lead in canonical order"
paragraph goes, along with its docstring.

Go side, `CpsatOutputToShifts` (`cpsat_contract.go:190`) drops
`Shift.TeamLead *Volunteer` in favour of role-tagged assignments;
`types.go` loses `Volunteer.IsTeamLead`, `VolunteerGroup.HasTeamLead` and
`Shift.CurrentSize()` (check callers first — it may already be dead).
`convertToDBAllocations` (`allocationHelpers.go:102`) collapses from three loops
hardcoding roles into one loop writing each assignment's own role — the clearest
simplification in the ticket.

Update `pkg/core/services/cpsatContract_test.go` goldens,
`pkg/core/allocator/init_test.go`, `allocationHelpers_test.go:185`.

### 10. Services: preallocations and rota editing name Roles

- `preallocations.go`: `AddPreallocationParams.TeamLead bool` → `Role string`.
  Validation becomes generic — Role exists in config, volunteer holds it
  (error, as the team-lead check at `:124` is today), Role not already at its
  ceiling for that date (`:190`), config pin wins over manual (`:152`).
  `configPreallocationViews` (`:347`) renders config pins with their own Role;
  `sortPreallocationViews` (`:420`) sorts by Role priority.
  `buildManualPreallocationOverrides` (`allocationHelpers.go:350`) loses its
  three-way `switch` on team-lead-ness.
- `changeRota.go`: delete `inferRole` (`:456`) and `validateLeadNotTaken`
  (`:424`); the latter becomes a generic "this Role is at its ceiling" check
  covering every capped Role. Role becomes required on an add.
  **The swap path (`:188`) deliberately passes no role today and relies on
  inference** — give it a defined semantic instead: each leg inherits the role
  of the person it replaces. That is a rule, not a guess, so it survives
  `inferRole`'s deletion.
  Placing someone in a Role they do not hold warns via `slog` and proceeds
  (ADR: structure enforced, standing advisory).
- `utils/alterations.go:44`: keep defaulting a *blank* role to the uncapped
  Role's name. This is read-path back-compat — `004_alteration_role.sql` added
  the column late, so historical rows have NULL roles.
- `pkg/api/preallocations.go`: `createPreallocationRequest.TeamLead bool` →
  `Role string`. The wire is currently asymmetric (boolean in, role string out);
  this makes it a role name both ways.
- `pkg/api/volunteers.go:61`: `Role string` → `Roles []string`.

### 11. Read and output surfaces stop special-casing team leads

- `publishRota.go:185` + `sheetsclient/rotas.go:13`: `PublishedRotaRow` becomes
  a column per capped Role in priority order plus list cells for the uncapped
  one. `publishRota.go:162` currently smuggles the string `"CLOSED"` through
  `row.TeamLead` — give the row an explicit `Closed bool` and render it into the
  first capped column so the sheet looks the same. Keep emitting the hand-typed
  blank `Hot food` / `Collection` trailing columns until S3.
- `viewResponses.go:485-491` branches on the two constants and silently counts a
  third role as *neither*. Replace `ShiftAvailabilityInfo.HasTeamLead` with a
  per-Role availability tally; the ordinary count becomes the uncapped Role's.
- `listShifts.go:229`: sort assignees by Role priority, then alphabetically.
- `volunteerCalendar.go:40`: `" (team lead)"` becomes `" (<role name>)"` for any
  non-uncapped Role.
- `cmd/cli/commands/allocate_rota.go:91-160`: the table's dedicated
  `maxTeamLeadLen` / `teamLeadColWidth` column becomes a column per capped Role;
  `:19` and `:74` help/hint text drops "max one team lead".
  `publish_rota.go:58` and `view_responses.go:107` follow.

`publishRota_test.go` (12 tests over the row shape) and `viewResponses_test.go`
are the oracles here.

### 12. Frontend speaks role names

- `web/src/types.ts:3`: `Role = "lead" | "volunteer"` → `string`, carrying the
  configured name. `Volunteer.role` → `roles: string[]`.
- `web/src/api.ts:15-18`: delete `TEAM_LEAD_ROLE` / `SERVICE_VOLUNTEER_ROLE` and
  the five mapping sites (`:79`, `:96`, `:152`, `:195`, `:282`); `:195`'s
  `body.teamLead = true` becomes `body.role = role`.
- `RotaEditDialogs.tsx:263,418`: pickers keep their two hardcoded options with a
  comment pointing at S3; `:342`'s `canChooseRole` generalises to "this Role is
  not at its ceiling".
- `RotaViewer.tsx`: `role-${assignee.role}` class names break on arbitrary role
  names — move to a `data-role` attribute with a default chip colour and
  `--role-lead` kept as the override for `Team lead`. Same in
  `AdminVolunteers.css`.
- `AdminVolunteers.tsx:42`: the `activeTeamLeads` stat becomes a per-Role tally
  off `roles: string[]`; `:84`'s roster tag renders every held Role.

No frontend tests exist, so verify this one in the browser (below).

### 13. Delete the hardcoded constants

`model.RoleTeamLead`, `model.RoleVolunteer` and `Role.IsValid()` go, along with
`type Role string`. Every remaining reference should already read from config;
the compiler finds any that do not. Update `docs/configurable_roles_plan.md`'s
S1 entry to record what actually shipped, including the two accepted
divergences.

## Verification

- **`scripts/check.sh`** — now includes the pyallocator suite (commit 1). This
  is the gate.
- **The golden (commit 2)** is the behaviour-preservation oracle. Its only
  permitted change is in commit 7, hand-inspected, with the diff in the PR body.
- **Full allocation against the dev stack**: `scripts/dev-stack.sh start`, then
  `POST /api/rotations`, pin someone via the preallocations UI, run allocation,
  and read the rota page from the accessibility tree. Confirm a team lead
  appears as `Team lead` and pinning a non-holder to `Team lead` is refused.
- **Published output**: run publish against the test sheet and confirm the
  header and rows are unchanged from today.
- **Browser** (commit 12): rota page and admin volunteers page via
  `playwright-mcp`; screenshots into the PR per CLAUDE.md.

## PR

Branch `issue-89-roles-as-data` off up-to-date main; `Closes #89`. The body must
carry: the two accepted divergences from byte-identity, the before/after golden
diff, and the deploy note that the droplet's config needs the new `roles`,
`requiresMale` and `preallocations` keys. Request review from `jakec-github`
via the REST endpoint; block on `gh pr checks --watch --fail-fast`.
