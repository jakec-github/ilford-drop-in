# pyallocator

CP-SAT rota allocator. Called by the Go CLI's
`allocateRotaCpsat` command as a subprocess: JSON problem on stdin, JSON
rota on stdout. Motivation and design: `../docs/cpsat_allocator_plan.md`
and `../docs/allocator_issues.md`.

## Setup

```sh
python3 -m venv pyallocator/.venv          # Python >= 3.11
pyallocator/.venv/bin/pip install -e "pyallocator[dev]"
pyallocator/.venv/bin/pytest pyallocator/tests
```

The Go side looks for `pyallocator/.venv/bin/python` by default
(overridable with `--python` or `ILFORD_CPSAT_PYTHON`).

`scripts/check.sh` runs this suite too, and does the setup above itself when
the venv is missing — so the solver is covered by the one pre-push command and
by CI, not only by whoever remembers to run pytest.

## Usage

```sh
pyallocator/.venv/bin/python -m pyallocator < input.json > output.json
# or: python -m pyallocator --input input.json --output output.json
```

Exit codes: `0` for any well-formed run **including INFEASIBLE**
(`success: false` in the output); `1` for invalid input or crashes.

## JSON contract

Input (all snake_case; group composition and availability are resolved
in Go — Python enforces and counts):

```json
{
  "max_allocation_count": 4,
  "roles": [{"name": "Team lead", "max": 1, "priority": 1},
            {"name": "Service volunteer", "max": null, "priority": 2}],
  "requires_male": true,
  "shifts": [{"index": 0, "date": "2026-07-13", "closed": false,
              "shape": [{"role": "Team lead", "count": 1},
                        {"role": "Service volunteer", "count": 4}],
              "preallocations": [
                {"volunteer_id": "", "custom": "St John's team", "role": "Service volunteer"},
                {"volunteer_id": "vol-1", "custom": "", "role": "Service volunteer"},
                {"volunteer_id": "vol-9", "custom": "", "role": "Team lead"}]}],
  "groups": [{"group_key": "couple_alice_bob",
              "members": [{"id": "vol-1", "first_name": "Alice", "last_name": "Smith",
                           "display_name": "Alice S", "gender": "Female",
                           "roles": ["Service volunteer"]}],
              "available_shift_indices": [0, 2, 4],
              "historical_allocation_count": 3}],
  "historical_shifts": [{"date": "2026-06-29", "group_keys": ["couple_x"]}]
}
```

A **Role** is a job on a shift; volunteers hold the Roles they will do, and
only a holder may fill a Seat asking for that Role. `max` is the ceiling on a
Role's Seats per shift — `null` means uncapped, and exactly one Role is
uncapped. `shape` is the shift's Seats, resolved in Go. Every preallocation
names the Role it fills and sets exactly one of `volunteer_id` and `custom`.

A preallocation is the exception to both eligibility rules, because it records
a decision already taken rather than asking the solver to make one: the pinned
volunteer fills the named Role on that shift whether or not they hold it, and
attends whether or not their group said it was available. Both grants are
confined to the pinned shift. Pinning to a Role the shift's `shape` has no Seat
for is still an error — that is a statement about the shift, not the person.

Output:

```json
{
  "solver_status": "OPTIMAL", "success": true, "error": "", "objective_value": 23,
  "shifts": [{"index": 0, "date": "2026-07-13", "size": 4, "closed": false,
              "assignments": [
                {"volunteer_id": "vol-9", "custom": "", "role": "Team lead"},
                {"volunteer_id": "vol-1", "custom": "", "role": "Service volunteer"},
                {"volunteer_id": "", "custom": "St John's team", "role": "Service volunteer"}],
              "allocated_group_keys": ["couple_alice_bob", "Diana Green"]}],
  "diagnostics": {"solve_time_seconds": 0.12, "num_groups": 18,
                  "num_variables": 126, "constraints_applied": ["availability"]}
}
```

`assignments` are the Seats that ended up filled, mirroring the
preallocations going in: exactly one of `volunteer_id` and `custom` is
set. A Seat nobody filled is simply absent, which is how "this shift has
no team lead" is said — expected and common, and filled in manually
later.

## How the code is organised

The assignment unit is the **individual volunteer in a Role**: the model has
a BoolVar per (volunteer, shift, Role) the volunteer could fill, plus an
attendance BoolVar per (volunteer, shift) equal to their sum. That equality
is what holds a person to one Seat per shift — there is no separate
exclusion constraint. Group atomicity (couples/families move as one) is the
`grouping` constraint rather than the variable structure. Constraints about
*whether* someone works read `x.attend`; constraints about *what they do*
read `x.role`. Modularity is the point of this package:

- `constraints/` — one file per **hard rule** (something that can never
  be violated). Each module's docstring and `description` state exactly
  what rota feature it ensures. Production set: `DEFAULT_CONSTRAINTS` in
  `constraints/__init__.py`: grouping (members of a group work each
  shift together or not at all), availability, max_frequency,
  seat_capacity (a Role's Seats on a shift are never oversubscribed —
  where "at most one team lead" now comes from, that Role having one
  Seat), male_required (a shift without a male keeps a Seat open so one
  can be added manually), no_back_to_back,
  closed_shifts, preallocations, no_duplicate_allocation.
  `one_shift_per_month` also exists but sits in `STRICT_CONSTRAINTS`, out of
  the production set: it is regularly unsatisfiable at real volunteer numbers.
  Rules that are off must not be asserted in tests.
- `preferences/` — one file per **soft goal**, contributing weighted
  terms to a single maximised objective. Production set:
  `DEFAULT_PREFERENCES` in `preferences/__init__.py`. The shaping
  preferences use harmonic diminishing returns (the nth unit is worth
  `WEIGHT // n`), which makes marginal value fall as a shift/group
  accumulates — scarce resources spread evenly instead of stacking:
  - `even_fill` (uncapped Role: 60 // Seat; capped Role: a flat 61+) —
    get every shift to N volunteers before pushing any shift to N+1,
    and fill a capped Role's Seat before an ordinary one; custom
    preallocations occupy their Role's early Seats.
  - `spread_males` (30 // male) — distribute males one-per-shift first.
  - `fairness` (20 // lifetime allocation, historical + this rota) —
    reach for under-used groups before frequently-allocated ones.
  - `maximize_allocations` (1) — base reward so shifts fill where they
    can; the unit other weights are measured against.
- `problem.py` — normalised solver view; preallocation resolution and
  its error cases live here because several constraints need them.
- `model_builder.py` / `solver.py` / `solution.py` — model assembly,
  deterministic solve (fixed seed, single worker, 30s limit), extraction.
- `tests/` — one test file per constraint/preference, each solving with
  ONLY that module; `test_end_to_end.py` re-verifies every applied hard
  rule independently of CP-SAT via `verify_solution`, and pins the rota
  it produces against `tests/testdata/e2e_rota.json`. Legality and
  sameness are different questions: plenty of rotas are legal, so only
  the golden catches a refactor that quietly reshuffles who works when.
  A golden diff is a behaviour change — read it, then regenerate with
  `UPDATE_GOLDEN=1 pytest pyallocator/tests` once you mean it. The rota
  half is checked only on the CP-SAT build it was recorded against, since
  equally-optimal rotas differ between builds; the objective value is
  checked everywhere, CI included.

To add a rule: create a module in `constraints/` or `preferences/`
following `base.py`'s protocol, register it in the package's
`DEFAULT_*` list, and add a test file that solves with only that module.
