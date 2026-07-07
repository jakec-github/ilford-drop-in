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
  "shifts": [{"index": 0, "date": "2026-07-13", "size": 4, "closed": false,
              "custom_preallocations": ["St John's team"],
              "preallocated_volunteer_ids": ["vol-1"],
              "preallocated_team_lead_id": "vol-9"}],
  "groups": [{"group_key": "couple_alice_bob",
              "members": [{"id": "vol-1", "first_name": "Alice", "last_name": "Smith",
                           "display_name": "Alice S", "gender": "Female",
                           "is_team_lead": false}],
              "available_shift_indices": [0, 2, 4],
              "historical_allocation_count": 3}],
  "historical_shifts": [{"date": "2026-06-29", "group_keys": ["couple_x"]}]
}
```

Output:

```json
{
  "solver_status": "OPTIMAL", "success": true, "error": "", "objective_value": 23,
  "shifts": [{"index": 0, "date": "2026-07-13", "size": 4, "closed": false,
              "team_lead_id": "vol-9", "volunteer_ids": ["vol-1", "vol-2"],
              "custom_preallocations": ["St John's team"],
              "allocated_group_keys": ["couple_alice_bob", "Diana Green"]}],
  "diagnostics": {"solve_time_seconds": 0.12, "num_groups": 18,
                  "num_variables": 126, "constraints_applied": ["availability"]}
}
```

`team_lead_id` is `""` when a shift has no team lead — expected and
common; missing team leads are filled in manually later.

## How the code is organised

The assignment unit is the **individual volunteer**: the model has one
BoolVar per (volunteer, shift) pair, and group atomicity
(couples/families move as one) is the `grouping` constraint rather
than the variable structure — so per-person roles can become solver
decisions later. Modularity is the point of this package:

- `constraints/` — one file per **hard rule** (something that can never
  be violated). Each module's docstring and `description` state exactly
  what rota feature it ensures. Production set: `DEFAULT_CONSTRAINTS` in
  `constraints/__init__.py`: grouping (members of a group work each
  shift together or not at all), availability, max_frequency,
  shift_capacity, at_most_one_team_lead (0 or 1 per shift),
  male_required (a shift without a male keeps a slot open — the TL slot
  or an ordinary seat — so one can be added manually), no_back_to_back,
  closed_shifts, preallocations, no_duplicate_allocation.
- `preferences/` — one file per **soft goal**, contributing weighted
  terms to a single maximised objective. Production set:
  `DEFAULT_PREFERENCES` in `preferences/__init__.py`. The shaping
  preferences use harmonic diminishing returns (the nth unit is worth
  `WEIGHT // n`), which makes marginal value fall as a shift/group
  accumulates — scarce resources spread evenly instead of stacking:
  - `even_fill` (60 // seat) — get every shift to N volunteers before
    pushing any shift to N+1; custom preallocations occupy early seats.
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
  ONLY that module; `test_end_to_end.py` re-verifies every hard rule
  independently of CP-SAT via `verify_solution`.

To add a rule: create a module in `constraints/` or `preferences/`
following `base.py`'s protocol, register it in the package's
`DEFAULT_*` list, and add a test file that solves with only that module.
