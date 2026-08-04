# Allocator

This package holds the Go side of rota allocation. The allocation itself is
solved by the Python CP-SAT solver in `pyallocator`; this package builds the
problem, runs the solver subprocess, and converts the result back into Go
domain types.

There is no longer a Go allocation algorithm — the previous greedy allocator
(criteria, ranking, validator) was removed in favour of CP-SAT. See ADR 0002
and issue #34.

## Responsibilities

1. **Domain types** (`types.go`) — `Volunteer`, `VolunteerGroup`,
   `VolunteerState`, `Shift`. Shared by the model builders, the CP-SAT
   contract, and the services layer.

2. **Model building** (`init.go`) — turns raw volunteers, per-group availability
   and shift overrides into the grouped model the solver needs:
   - `GroupKeyFor` is the one place the grouping rule lives: a volunteer's
     `GroupKey`, or their name when they have none (or the sheet's literal
     "None"). Callers use it to key availability the same way.
   - `InitVolunteerGroups` groups volunteers with `GroupKeyFor` (couples and
     families allocated together; individuals keyed by name) and attaches each
     group's availability from `GroupAvailability`, discarding groups nobody
     answered for and groups with no availability. It does **not** decide what a
     group's answer is — that rule belongs with the availability store (ADR
     0004), and the allocator keeping its own copy of it is what this replaced.
   - `InitShifts` applies date-matched overrides (size, closed, preallocations)
     and populates each shift's available groups.
   - `BuildVolunteerGroup` builds a single group with its derived metadata
     (`HasTeamLead`, `MaleCount`).

3. **CP-SAT contract** (`cpsat_contract.go`, `cpsat_runner.go`) — the JSON
   contract with `pyallocator` and the subprocess plumbing:
   - `BuildCpsatInput` assembles the solver input, reusing the model builders
     above so grouping and override resolution are never duplicated.
   - `RunCpsatAllocator` runs `<python> -m pyallocator`, sending the problem on
     stdin and parsing the rota from stdout. `ResolvePythonInterpreter` picks
     the interpreter (flag > `ILFORD_CPSAT_PYTHON` > pyallocator venv > python3).
   - `CpsatOutputToShifts` rebuilds `Shift` values from the solver output so
     persistence and printing reuse the existing code paths.

The solver's constraints and objective (hard constraints such as availability,
capacity, no back-to-back, at most one team lead, and the soft preferences that
shape the result) are documented in `pyallocator/README.md`.

## Core data structures

### VolunteerGroup

Volunteers allocated together (e.g. couples, families):

- `GroupKey` — identifies the group (empty for individuals)
- `Members` — the volunteers in the group
- `AvailableShiftIndices` — shifts this group is available for
- `AllocatedShiftIndices` — shifts this group has been allocated to
- `HistoricalAllocationCount` — historical allocations, for fairness
- `HasTeamLead` — whether any member is a team lead
- `MaleCount` — number of male volunteers in the group

### Shift

A single shift to be filled:

- `Date`, `Index`, `Size` (target ordinary-volunteer count; excludes team lead)
- `AllocatedGroups` — groups assigned to this shift
- `CustomPreallocations` — string IDs of manually pre-assigned volunteers
  (count toward `Size`)
- `TeamLead` — the assigned team lead (separate from `Size`, may be nil)
- `Closed` / `PreallocatedVolunteerIDs` / `PreallocatedTeamLeadID` — resolved
  from overrides during `InitShifts`

**Team leads and pre-allocated volunteers** are treated specially: team leads
are stored in `TeamLead` and do NOT count toward `Size`; pre-allocated
volunteers are string IDs that DO count toward `Size` but don't affect
`TeamLead` or `MaleCount`.
