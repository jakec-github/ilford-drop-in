"""Extracts the solved rota from variable values.

Team-lead designation per shift: the preallocated team lead if one was
set, otherwise the first allocated team-lead volunteer (in canonical
order: group input order, then member order). Any further team-lead
volunteers are reported in volunteer_ids as ordinary volunteers —
mirroring the Go side, where non-designated team-lead members get
Role: Volunteer rows.
"""

from __future__ import annotations

from .constraints.base import AssignmentVars
from .domain import AllocationOutput, Diagnostics, OutputShift
from .problem import Problem
from .solver import SolveResult


def extract_solution(
    problem: Problem,
    x: AssignmentVars,
    result: SolveResult,
    constraints_applied: tuple[str, ...],
) -> AllocationOutput:
    shifts = tuple(
        _extract_shift(problem, x, result, shift_index)
        for shift_index in range(len(problem.shifts))
    )
    return AllocationOutput(
        solver_status=result.status,
        success=result.success,
        error="",
        objective_value=result.objective_value,
        shifts=shifts,
        diagnostics=Diagnostics(
            solve_time_seconds=result.solve_time_seconds,
            num_groups=len(problem.groups),
            num_variables=len(x),
            constraints_applied=constraints_applied,
        ),
    )


def _extract_shift(
    problem: Problem, x: AssignmentVars, result: SolveResult, shift_index: int
) -> OutputShift:
    spec = problem.shifts[shift_index]
    allocated = [
        v
        for v in problem.volunteers
        if result.solver.Value(x[(v.id, shift_index)]) == 1
    ]

    team_lead_id = problem.preallocated_team_lead.get(shift_index, "")
    volunteer_ids: list[str] = []
    group_keys: list[str] = []
    for v in allocated:
        if v.group_key not in group_keys:
            group_keys.append(v.group_key)
        if v.id == team_lead_id:
            continue
        if v.is_team_lead and not team_lead_id:
            team_lead_id = v.id
            continue
        volunteer_ids.append(v.id)

    return OutputShift(
        index=spec.index,
        date=spec.date,
        size=spec.size,
        closed=spec.closed,
        team_lead_id=team_lead_id,
        volunteer_ids=tuple(volunteer_ids),
        custom_preallocations=spec.custom_preallocations,
        allocated_group_keys=tuple(group_keys),
    )
