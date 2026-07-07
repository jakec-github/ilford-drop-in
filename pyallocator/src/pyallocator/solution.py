"""Extracts the solved rota from variable values.

Team-lead designation per shift: the preallocated team lead if one was
set, otherwise the team-lead member of the first allocated TL group (by
group key order). Members of any additional TL group are reported in
volunteer_ids as ordinary volunteers — mirroring the Go side, where
non-designated team-lead members get Role: Volunteer rows.
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
        gv
        for gv in problem.groups
        if result.solver.Value(x[(gv.key, shift_index)]) == 1
    ]

    team_lead_id = problem.preallocated_team_lead.get(shift_index, "")
    volunteer_ids: list[str] = []
    for gv in allocated:
        for member in gv.group.members:
            if member.id == team_lead_id:
                continue
            if member.is_team_lead and not team_lead_id:
                team_lead_id = member.id
                continue
            volunteer_ids.append(member.id)

    return OutputShift(
        index=spec.index,
        date=spec.date,
        size=spec.size,
        closed=spec.closed,
        team_lead_id=team_lead_id,
        volunteer_ids=tuple(volunteer_ids),
        custom_preallocations=spec.custom_preallocations,
        allocated_group_keys=tuple(gv.key for gv in allocated),
    )
