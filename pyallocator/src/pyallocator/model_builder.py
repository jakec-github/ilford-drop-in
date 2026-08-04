"""Builds the CP-SAT model: a BoolVar per (volunteer, shift, Role) the
volunteer could actually fill there (Problem.may_fill), plus an attendance
BoolVar per (volunteer, shift) equal to their sum. It then applies the
constraint list and sums the preference terms into a single Maximize
objective.

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
            # asks for: any other variable would be a Seat nobody could fill.
            role_vars = []
            for seat in shift.shape:
                if seat.count <= 0 or not problem.may_fill(v, shift.index, seat.role):
                    continue
                role_var = model.NewBoolVar(f"role[{v.id},{shift.index},{seat.role}]")
                role[(v.id, shift.index, seat.role)] = role_var
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
    if terms:
        model.Maximize(sum(expr * weight for expr, weight in terms))

    return BuiltModel(
        model=model,
        x=x,
        constraints_applied=tuple(c.name for c in constraints),
    )
