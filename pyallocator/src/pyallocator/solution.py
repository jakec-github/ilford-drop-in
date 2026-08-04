"""Extracts the solved rota from variable values.

Who the team lead is is no longer guessed here: it is whoever the solver
put in the capped Role's Seat, read straight off that role variable.
A team-lead holder the solver placed in an ordinary Seat is reported in
volunteer_ids, because that is what they are doing.

Temporary shape, not a temporary rule: the output contract still names a
single team lead per shift, so this flattens the role variables back down
to team_lead_id + volunteer_ids. #89 commit 9 gives the contract
role-tagged assignments and this collapses to a straight read.
"""

from __future__ import annotations

from .constraints.base import Vars
from .domain import AllocationOutput, Diagnostics, OutputShift
from .problem import Problem
from .solver import SolveResult


def extract_solution(
    problem: Problem,
    x: Vars,
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
    problem: Problem, x: Vars, result: SolveResult, shift_index: int
) -> OutputShift:
    spec = problem.shifts[shift_index]
    allocated = [
        v
        for v in problem.volunteers
        if result.solver.Value(x.attend[(v.id, shift_index)]) == 1
    ]

    lead_name = problem.lead_role.name if problem.lead_role else None

    team_lead_id = ""
    volunteer_ids: list[str] = []
    group_keys: list[str] = []
    for v in allocated:
        if v.group_key not in group_keys:
            group_keys.append(v.group_key)
        lead_var = x.role.get((v.id, shift_index, lead_name))
        if lead_var is not None and result.solver.Value(lead_var) == 1:
            team_lead_id = v.id
            continue
        volunteer_ids.append(v.id)

    return OutputShift(
        index=spec.index,
        date=spec.date,
        size=problem.shift_size(spec),
        closed=spec.closed,
        team_lead_id=team_lead_id,
        volunteer_ids=tuple(volunteer_ids),
        custom_preallocations=problem.shift_customs(spec),
        allocated_group_keys=tuple(group_keys),
    )
