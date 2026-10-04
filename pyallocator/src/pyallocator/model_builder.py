"""Builds the CP-SAT model: a BoolVar per (volunteer, shift, Role) the
volunteer could actually sit in there (Problem.seat_roles), plus an attendance
BoolVar per (volunteer, shift) equal to their sum. It then applies the
constraint list and sums the preference terms into a single Maximize
objective.

Where a pin splits a group it also states how many of the pinned
volunteers' group-mates are on alongside them (mates_joined), which the
solver keeps as high as the rules allow before it weighs any preference
(solver.py).

Equating the role vars with attendance is the model's one structural rule:
a person fills at most one Seat per shift. Group atomicity is not
structural — the grouping constraint ties members of a group together.

Constraint and preference lists are parameters so tests can solve with
exactly one module active.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Sequence

from ortools.sat.python import cp_model

from .constraints.base import Constraint, Vars
from .preferences.base import Preference
from .problem import Problem


@dataclass(frozen=True)
class BuiltModel:
    model: cp_model.CpModel
    x: Vars
    constraints_applied: tuple[str, ...]
    # The preferences' weighted sum, also set as the model's objective; None
    # when there are no preferences.
    objective: cp_model.LinearExpr | None
    # How many group-mates of pinned volunteers are on alongside them; None
    # when no pinned volunteer has a group-mate to keep.
    mates_joined: cp_model.LinearExpr | None


def build(
    problem: Problem,
    constraints: Sequence[Constraint],
    preferences: Sequence[Preference],
) -> BuiltModel:
    model = cp_model.CpModel()
    attend: dict[tuple[str, int], cp_model.IntVar] = {}
    role: dict[tuple[str, int, str], cp_model.IntVar] = {}

    for v in problem.volunteers:
        for shift in problem.shifts:
            attendance = model.NewBoolVar(f"attend[{v.id},{shift.index}]")
            attend[(v.id, shift.index)] = attendance

            # Only Roles this volunteer may fill on this shift and the Shape
            # asks for, plus the one a pin names: any other variable would be
            # a Seat nobody could fill.
            role_vars = []
            for role_name in problem.seat_roles(v, shift):
                role_var = model.NewBoolVar(f"role[{v.id},{shift.index},{role_name}]")
                role[(v.id, shift.index, role_name)] = role_var
                role_vars.append(role_var)

            # One Seat per person per shift, stated once. With no eligible
            # Seat this forces attendance to zero, which is right: there is
            # nothing on this shift for them to do.
            model.Add(sum(role_vars) == attendance)

    x = Vars(attend=attend, role=role)

    for constraint in constraints:
        constraint.apply(model, x, problem)

    terms = []
    for preference in preferences:
        terms.extend(preference.objective_terms(model, x, problem))
    objective = None
    if terms:
        objective = sum(expr * weight for expr, weight in terms)
        model.Maximize(objective)

    mates_joined = None
    if problem.pinned_mates:
        mates_joined = sum(x.attend[key] for key in problem.pinned_mates)

    return BuiltModel(
        model=model,
        x=x,
        constraints_applied=tuple(c.name for c in constraints),
        objective=objective,
        mates_joined=mates_joined,
    )
