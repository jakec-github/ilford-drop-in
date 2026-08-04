"""Extracts the solved rota from variable values.

Every filled Seat is read straight off the role variable that filled it,
so nothing here decides anything: who the team lead is was settled by the
solver, not by a rule about canonical order applied afterwards.
"""

from __future__ import annotations

from .constraints.base import Vars
from .domain import AllocationOutput, Assignment, Diagnostics, OutputShift
from .problem import Problem, ProblemError
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

    assignments: list[Assignment] = []
    group_keys: list[str] = []
    for v in problem.volunteers:
        if result.solver.Value(x.attend[(v.id, shift_index)]) != 1:
            continue
        if v.group_key not in group_keys:
            group_keys.append(v.group_key)
        assignments.append(
            Assignment(
                volunteer_id=v.id,
                custom="",
                role=_seat_filled(problem, x, result, v.id, shift_index),
            )
        )

    # Custom entries are not solver decisions — they occupied their Role's
    # Seats on the way in, and come back out unchanged.
    assignments.extend(
        Assignment(volunteer_id="", custom=pin.custom, role=pin.role)
        for pin in spec.preallocations
        if pin.custom
    )

    return OutputShift(
        index=spec.index,
        date=spec.date,
        size=problem.shift_size(spec),
        closed=spec.closed,
        assignments=tuple(assignments),
        allocated_group_keys=tuple(group_keys),
    )


def _seat_filled(
    problem: Problem, x: Vars, result: SolveResult, volunteer_id: str, shift_index: int
) -> str:
    """The Role of the one Seat this attending volunteer took.

    model_builder equates attendance with the sum of a volunteer's role
    variables, so exactly one is set whenever they attend. Finding none is
    a broken model rather than an empty answer, and an assignment with no
    Role would be persisted as one.
    """
    for role in problem.roles:
        var = x.role.get((volunteer_id, shift_index, role.name))
        if var is not None and result.solver.Value(var) == 1:
            return role.name
    raise ProblemError(
        f"volunteer '{volunteer_id}' attends shift {shift_index} in no Role"
    )
