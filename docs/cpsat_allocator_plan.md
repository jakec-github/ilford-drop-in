# Implementation Plan: CP-SAT Rota Allocator (Python, constraints-only v1)

> **Execution context for the implementing agent.** This is a self-contained plan; execute it top to bottom without needing prior conversation history. Facts you would otherwise have to rediscover:
>
> - This repo is a **Go** CLI (cobra) managing volunteer rotas: volunteers from Google Sheets, availability from Google Forms, state in Postgres, rota published to Sheets.
> - The existing allocator is a greedy heuristic in `pkg/core/allocator/` (design doc: `pkg/core/allocator/allocator.md`). **Do not modify it or any existing file**, with one flagged exception: a one-line command registration in `cmd/cli/main.go`.
> - There is no Python in the repo today. Python 3.13.5 is available at `/Users/jakechorley/miniconda3/bin/python3`; no uv/poetry — use venv + pip.
> - All line references below were verified against the repo at planning time (2026-07-07); re-verify if files have changed.
> - Motivation: `docs/allocator_issues.md` — the greedy allocator needs hand-tuned weights, has no backtracking, is opaque, and muddles constraints with preferences. CP-SAT addresses all of these.

## Scope decisions (agreed with the user — do not revisit)

1. v1 models **hard constraints only** — rules that forbid impossible allocations. "Requirements" (team lead per shift, ≥1 male per shift, fill to Size) are deferred to the preferences phase.
2. **Grouping ownership stays in Go.** Groups are used by other commands, so grouping logic will always live in Go. Go builds groups via the already-exported `allocator.InitVolunteerGroups` and sends **resolved group availability** to Python. No grouping logic duplicated in Python.
3. **≤1 team lead per shift is a preference, not a constraint** — and it IS included in v1 (the only real preference besides the placeholder). Rotas with missing team leads must be producible (happens a lot; they're filled in later), and stacking two TLs is discouraged, not forbidden.
4. One **placeholder objective** (maximise number of allocations) so solutions are non-empty; clearly marked, lives in `preferences/`.
5. **Wired into the Go CLI now**: new cobra command → new service (same `services` package, reusing unexported plumbing) → subprocess → Python. New files only, except the one registration line.
6. **Dedicated Python venv** at `pyallocator/.venv` (created in step 1; the Go side's default interpreter lookup).
7. **Modularity is the top requirement**: one Python file per constraint / per preference; file names and module docstrings state exactly what rota feature each ensures.

## Verified domain semantics (from Go source — reproduce exactly)

Allocation unit = **VolunteerGroup** (couples/families allocated together), not the individual. Group construction stays in Go (`allocator.InitVolunteerGroups`, `pkg/core/allocator/init.go:50` — exported): groups keyed by `GroupKey` (empty/"None" → individual keyed `FirstName LastName`); group "responded" if ANY member responded; unavailable indices = union over responding members; non-responding or zero-availability groups discarded; error if a group has >1 team lead.

- Capacity (`pkg/core/allocator/types.go:160-170`): `CurrentSize = len(CustomPreallocations) + non-team-lead members of allocated groups`. Team leads never count toward `Size`.
- Max allocations per group = `int(float64(len(shifts)) * MaxAllocationFrequency)` (`types.go:56`) — **computed in Go** and sent as a resolved integer (`max_allocation_count`), eliminating float-truncation parity risk.
- Preallocations (`init.go:363-449`): apply **regardless of availability**; group-atomic (partner comes too); preallocated TL must exist and have `IsTeamLead`; unknown IDs (incl. discarded non-responders) → error. Closed shifts strip preallocations (`init.go:323-329`).
- No double shifts: shift indices i and i±1 conflict (by **index**, not calendar distance); shift 0 also conflicts with the group's presence on the LAST historical shift (matched by GroupKey).
- **Custom preallocations flow**: YAML override → `allocator.InitShifts` resolves per-shift `CustomPreallocations` (free-text names) → sent to Python → Python counts them as occupied seats in the capacity constraint and echoes them back per shift → Go puts them on the rebuilt `allocator.Shift.CustomPreallocations` → existing `convertToDBAllocations` (`pkg/core/services/allocateRota.go:388-397`) persists them as `db.Allocation{Role: Volunteer, VolunteerID: "", CustomEntry: name}`.
- Gender is a free string; only `"Male"` has semantics (`allocator.GenderMale`, `types.go:7`).

## Python package: `pyallocator/` at repo root

Top-level sibling of `pkg/` (obvious language boundary; valid import name; `src/` layout so tests hit the installed package). Deps: `ortools`; dev: `pytest`. Setup (implementation step 1):
```
/Users/jakechorley/miniconda3/bin/python3 -m venv pyallocator/.venv
pyallocator/.venv/bin/pip install -e "pyallocator[dev]"
```

```
pyallocator/
├── pyproject.toml                 # setuptools, requires-python >=3.11, deps: ortools; [dev]: pytest
├── README.md                      # setup, JSON contract, how constraints/preferences are organised
├── src/pyallocator/
│   ├── __init__.py                # public API re-export (solve())
│   ├── __main__.py                # `python -m pyallocator` → cli.main()
│   ├── cli.py                     # JSON in (stdin/--input) → JSON out (stdout/--output); exit codes
│   ├── domain.py                  # frozen dataclasses mirroring JSON contract (in + out); GENDER_MALE
│   ├── serialization.py           # dict <-> dataclass, input validation
│   ├── problem.py                 # Problem: normalised solver view — groups (key, members,
│   │                              #   derived ordinary_size/has_team_lead/male_count, available
│   │                              #   indices, historical_allocation_count), shifts,
│   │                              #   max_allocation_count, preallocated_pairs {(g,s)},
│   │                              #   preallocated_team_lead {s: vol_id}, last_historical_group_keys.
│   │                              #   Preallocation resolution + errors live HERE (other constraints
│   │                              #   need them, e.g. availability exemption)
│   ├── model_builder.py           # CpModel + x[(g,s)] BoolVar per (group, shift); applies constraint
│   │                              #   list then preference list (weighted objective terms → Maximize)
│   ├── solver.py                  # CpSolver: max_time_in_seconds=30, random_seed=0,
│   │                              #   num_search_workers=1 (deterministic rotas); status mapping
│   ├── solution.py                # x values → per-shift team_lead_id + ordinary volunteer_ids
│   ├── constraints/
│   │   ├── __init__.py            # DEFAULT_CONSTRAINTS: explicit ordered list (greppable, no magic)
│   │   ├── base.py                # Constraint Protocol: name, description, apply(model, x, problem)
│   │   ├── availability.py        # "groups only on shifts they're available for (unless preallocated)"
│   │   ├── max_frequency.py       # "no group exceeds its allocation cap for the rota"
│   │   ├── shift_capacity.py      # "ordinary volunteers ≤ size − custom preallocations; TLs don't count"
│   │   ├── no_back_to_back.py     # "no consecutive shifts, incl. boundary from previous rota's last shift"
│   │   ├── closed_shifts.py       # "closed shifts get no allocations"
│   │   ├── preallocations.py      # "preallocated volunteers/team leads are always on their shift"
│   │   └── no_duplicate_allocation.py  # structural (one BoolVar per pair); validates invariant
│   └── preferences/
│       ├── __init__.py            # DEFAULT_PREFERENCES registry
│       ├── base.py                # Preference Protocol: objective_terms(model, x, problem) → [(expr, weight)]
│       ├── at_most_one_team_lead.py # "shifts should not have more than one team lead" — soft penalty
│       │                            #   per excess TL group on a shift (weight ≫ allocation reward so
│       │                            #   TLs only stack when preallocations force it)
│       └── maximize_allocations.py  # PLACEHOLDER: maximise Σx. Future: fill-to-size, TL coverage,
│                                    #   male coverage, fairness — as weighted soft terms here
└── tests/
    ├── conftest.py                # builders (make_group, make_input, solve_with(constraints=[...], preferences=[...]))
    ├── test_serialization.py      # contract round-trip
    ├── test_end_to_end.py         # scenario modeled on pkg/core/allocator/e2e/allocator_test.go
    │                              #   + verify_solution(input, output) helper re-checking every rule
    │                              #   independently of CP-SAT (future regression oracle)
    ├── constraints/               # one test file per constraint, solving with ONLY that constraint
    │   ├── test_availability.py … test_preallocations.py, test_structural.py
    └── preferences/
        ├── test_at_most_one_team_lead.py  # no TL required; 2 TL groups + 1 shift → only one placed;
        │                                  #   preallocated double-TL still solves
        └── test_maximize_allocations.py
```

## JSON contract (Go → Python stdin; Python stdout → Go)

Go owns grouping: it sends **resolved groups with group availability** (built by `allocator.InitVolunteerGroups`). Python derives per-group arithmetic (ordinary_size, has_team_lead, male_count) from members — counting, not grouping logic. RRules are resolved Go-side; Python never sees them.

Input (snake_case throughout):
```json
{
  "max_allocation_count": 4,
  "shifts": [{"index": 0, "date": "2026-07-13", "size": 4, "closed": false,
              "custom_preallocations": ["St John's team"],
              "preallocated_volunteer_ids": ["vol-1"], "preallocated_team_lead_id": "vol-9"}],
  "groups": [{
      "group_key": "couple_alice_bob",
      "members": [{"id": "vol-1", "first_name": "Alice", "last_name": "Smith",
                   "display_name": "Alice S", "gender": "Female", "is_team_lead": false}],
      "available_shift_indices": [0, 2, 4],
      "historical_allocation_count": 3}],
  "historical_shifts": [{"date": "2026-06-29", "group_keys": ["couple_x"]}]
}
```
`shifts[].size` is already override-resolved. `max_allocation_count` is the Go-computed integer cap. `historical_shifts` sorted ascending by date with Go-derived group keys; only the last matters in v1 (back-to-back boundary), full list future-proofs fairness preferences.

Output:
```json
{
  "solver_status": "OPTIMAL", "success": true, "error": "", "objective_value": 23,
  "shifts": [{"index": 0, "date": "2026-07-13", "size": 4, "closed": false,
              "team_lead_id": "vol-9", "volunteer_ids": ["vol-1", "vol-2"],
              "custom_preallocations": ["St John's team"],
              "allocated_group_keys": ["couple_alice_bob", "Diana Green"]}],
  "diagnostics": {"solve_time_seconds": 0.12, "num_groups": 18, "num_variables": 126,
                  "constraints_applied": ["availability", "..."]}
}
```
`volunteer_ids` = ordinary members (plus any non-designated TL, see preference below); `team_lead_id` separate ("" when the shift has no TL — expected and common). `success = status ∈ {OPTIMAL, FEASIBLE}`. Exit 0 for any well-formed run **including INFEASIBLE**; exit 1 for crashes/invalid input.

## CP-SAT mapping (x[g,s] BoolVar per group × shift)

Hard constraints:
1. **availability.py**: `Add(x[g,s] == 0)` for s ∉ g.available_shift_indices AND (g,s) ∉ preallocated_pairs (preallocations override availability — Go parity).
2. **no_duplicate_allocation.py**: structural (single BoolVar per pair); asserts `len(x) == n_groups × n_shifts`.
3. **max_frequency.py**: per g, `Add(Σ_s x[g,s] <= max_allocation_count)`. Preallocations count toward the cap.
4. **shift_capacity.py**: per open shift, `Add(Σ_g ordinary_size[g]·x[g,s] <= max(0, size − len(custom_preallocations)))`. TLs excluded from ordinary_size.
5. **no_back_to_back.py**: per g, s: `Add(x[g,s] + x[g,s+1] <= 1)`; if g.key ∈ last_historical_group_keys: `Add(x[g,0] == 0)`.
6. **closed_shifts.py**: `Add(x[g,s] == 0)` ∀g on closed shifts (Go already strips their preallocations, so no conflict with 7).
7. **preallocations.py**: `Add(x[g,s] == 1)` for resolved pairs. Resolution in `problem.py`: TL id → owning group, error if that member lacks `is_team_lead` (mirrors `init.go:396`); volunteer ids → owning groups, deduped per group (mirrors `init.go:427-439`); unknown id → error. Forcing the group brings partners along; their ordinary seats count toward capacity.

Preferences (weighted terms summed into one `Maximize`):
- **at_most_one_team_lead.py**: per open shift, `excess_s = IntVar(0, n_tl_groups)`, `Add(excess_s >= Σ_{g: has_team_lead} x[g,s] − 1)`, term `(−excess_s, W_TL)` with `W_TL` ≫ the +1 allocation reward (e.g. 10) so a second TL is only placed when preallocations force it. Shifts with **zero** TLs are always allowed.
- **maximize_allocations.py**: term `(Σ x, 1)` — placeholder until real preferences land.

`solution.py` TL designation: the preallocated TL if set, else the TL member of the first allocated TL group (by group key); members of any additional TL group are reported in `volunteer_ids` as ordinary volunteers — mirrors `convertToDBAllocations`, where non-designated TL members get `Role: Volunteer` rows.

## Constraint/preference protocol

`constraints/base.py`:
```python
class Constraint(Protocol):
    name: str         # e.g. "availability"
    description: str  # human sentence: what rota feature this ensures
    def apply(self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem) -> None: ...
```
`preferences/base.py` mirrors it with `objective_terms(model, x, problem) -> list[tuple[LinearExpr, int]]`. Explicit registry lists in each `__init__.py` (deterministic order, greppable). `model_builder.build(problem, constraints=..., preferences=...)` takes both lists as parameters so tests can inject a single module.

## Go side — new files only (+ one registration line)

- **`pkg/core/services/allocateRotaCpsatIO.go`**: JSON-tagged structs (`CpsatInput`, `CpsatGroup`, `CpsatOutput`, …); `buildCpsatInput(...)` — calls exported `allocator.InitVolunteerGroups` (`init.go:50`) for groups+availability+historical counts, exported `allocator.InitShifts` (`init.go:291`, with `&allocator.VolunteerState{VolunteerGroups: []*allocator.VolunteerGroup{}}`) for override-resolved shift specs, and computes `max_allocation_count` — zero duplicated logic; `cpsatOutputToAllocatorShifts(...)` — rebuilds `allocator.Shift`s (via a `volunteersByID` map + exported `allocator.BuildVolunteerGroup`, incl. `CustomPreallocations`) so persistence and printing reuse existing code.
- **`pkg/core/services/cpsatRunner.go`**: `runCpsatAllocator(ctx, pythonPath, input, logger)` — `exec.CommandContext(pythonPath, "-m", "pyallocator")`, JSON stdin/stdout, stderr → debug log, non-zero exit → error with stderr tail. `resolvePythonInterpreter`: `--python` flag → `ILFORD_CPSAT_PYTHON` env → `pyallocator/.venv/bin/python` if present → `python3`. No config-file change.
- **`pkg/core/services/allocateRotaCpsat.go`**: `AllocateRotaCpsat(...)` — copies `AllocateRota`'s orchestration (`allocateRota.go:98-197`), **directly reusing same-package unexported helpers**: `fetchAvailabilityResponses`, `convertToAllocatorVolunteers`, `buildHistoricalShifts`, `convertRotaOverrides`, plus `utils.FindLatestRotation` / `CalculateShiftDates` / `FilterSentRequestsByRotaID` / `FilterActiveVolunteers`. Then build input → subprocess → parse. **Persistence included** (~20 lines): save when `!dryRun && (output.Success || forceCommit)` via `convertToDBAllocations` + `InsertAllocationsAndSetAllocated` — same semantics as `AllocateRota`. (~80 duplicated orchestration lines accepted to avoid editing existing files.)
- **`cmd/cli/commands/allocate_rota_cpsat.go`**: `AllocateRotaCpsatCmd` — `Use: "allocateRotaCpsat"`, flags `--dry-run`, `--force-commit`, `--python`. Prints same table style as `allocate_rota.go` + solver status/objective/solve time; on INFEASIBLE explains which constraint families to check.
- **`cmd/cli/main.go`** (⚠️ only existing-file edit, unavoidable): one line after line 59: `rootCmd.AddCommand(newLazyCommand(commands.AllocateRotaCpsatCmd))`.

## Testing

- **Python**: per-constraint tests injecting a single constraint via the registry parameter (availability: unavailable shift never assigned; back-to-back: 2 shifts/1 group → exactly 1; historical boundary → never shift 0; preallocations: partner pulled along, availability overridden, unknown/non-TL id errors). Preference tests: TL penalty prevents stacking but allows zero-TL shifts and preallocation-forced stacking. `test_end_to_end.py` mirrors `pkg/core/allocator/e2e/allocator_test.go` scale (~24 volunteers, 7 shifts, couples, closed shift, preallocation, custom preallocation) and re-verifies every rule via a solver-independent `verify_solution` helper.
- **Go**: `allocateRotaCpsatIO_test.go` — golden JSON test pinning the contract field names; tests that `buildCpsatInput` groups correctly via `InitVolunteerGroups` and applies overrides via `InitShifts`.

## Implementation order

1. Venv: `/Users/jakechorley/miniconda3/bin/python3 -m venv pyallocator/.venv`; `pyproject.toml`; `pip install -e "pyallocator[dev]"` (verifies the ortools wheel installs on this machine); `domain.py`/`serialization.py` + tests.
2. Core: `problem.py` (incl. preallocation resolution + errors), `constraints/base.py`, `preferences/base.py`, `model_builder.py`, `solver.py`, `solution.py`, `maximize_allocations.py` — smoke-solvable with zero constraints.
3. Constraint modules one at a time, each with its test file; then `at_most_one_team_lead.py` + tests.
4. `cli.py`/`__main__.py` + end-to-end test; manual run: `pyallocator/.venv/bin/python -m pyallocator < fixture.json` (fixture in a scratch dir, not committed).
5. Go files in order IO → runner → service → command → register; `go build ./... && go test ./pkg/core/services/`.

## Verification

- `pyallocator/.venv/bin/pytest pyallocator/tests` — all constraint/preference + e2e tests green.
- `go build ./... && go test ./...` — existing suite untouched and green.
- End-to-end: `go run ./cmd/cli --env test allocateRotaCpsat --dry-run` (test DB via `scripts/test-db.sh`); compare with `allocateRota --dry-run` — expect identical hard-constraint compliance, different/fuller allocations (no fairness preferences yet), and shifts without team leads reported normally rather than blocking.

## Risks

- **INFEASIBLE vs Go's "invalid but produced"**: Go's preallocations can violate capacity/adjacency and yield a flagged rota; CP-SAT yields INFEASIBLE and no rota. v1 reports this clearly; future: assumption literals to name the unsatisfiable constraint family.
- **Placeholder-objective ties**: many optima; deterministic solver params give reproducibility, but spread may look arbitrary until preferences land — noted in command output/README.
- **TL-penalty weight** interacts with future preference weights; revisit when real preferences land (documented in `at_most_one_team_lead.py`).
- **Orchestration drift**: the ~80 copied fetch lines in `allocateRotaCpsat.go` can drift from `AllocateRota`; consolidate when the CP-SAT path becomes primary.
