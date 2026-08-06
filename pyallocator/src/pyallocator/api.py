"""Public solve() entrypoint: input -> problem -> model -> solver -> output.

The constraint list comes from the input's enabled_constraints — the admin's
Allocation Settings, decided in the app and sent by Go — rather than from a
default list held here. Preferences still default to the production registry:
they are a weighted objective rather than a rule, and are not switchable.
Tests pass subsets of either to exercise one module at a time.
"""

from __future__ import annotations

from typing import Sequence

from .constraints import Constraint, constraints_for
from .domain import AllocationInput, AllocationOutput, Diagnostics
from .model_builder import build
from .preferences import DEFAULT_PREFERENCES, Preference
from .problem import Problem
from .solution import extract_solution
from .solver import solve_model


def solve(
    input_: AllocationInput,
    constraints: Sequence[Constraint] | None = None,
    preferences: Sequence[Preference] | None = None,
) -> AllocationOutput:
    if constraints is None:
        constraints = constraints_for(input_.enabled_constraints)
    if preferences is None:
        preferences = DEFAULT_PREFERENCES

    problem = Problem(input_)
    built = build(problem, constraints, preferences)
    result = solve_model(built)

    if result.success:
        return extract_solution(problem, built.x, result, built.constraints_applied)

    # INFEASIBLE (or UNKNOWN etc.) is a well-formed result: no rota, but
    # not a crash. The Go side reports the status to the operator.
    return AllocationOutput(
        solver_status=result.status,
        success=False,
        error="",
        objective_value=0,
        shifts=(),
        diagnostics=Diagnostics(
            solve_time_seconds=result.solve_time_seconds,
            num_groups=len(problem.groups),
            num_variables=len(built.x),
            constraints_applied=built.constraints_applied,
        ),
    )
