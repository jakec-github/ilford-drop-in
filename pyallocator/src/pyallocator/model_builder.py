"""Builds the CP-SAT model: one BoolVar per (volunteer, shift) pair,
then applies the constraint list and sums the preference terms into a
single Maximize objective. Group atomicity is not structural — the
grouping constraint ties members of a group together.

Constraint and preference lists are parameters so tests can solve with
exactly one module active.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Sequence

from ortools.sat.python import cp_model

from .constraints.base import AssignmentVars, Constraint
from .preferences.base import Preference
from .problem import Problem


@dataclass(frozen=True)
class BuiltModel:
    model: cp_model.CpModel
    x: AssignmentVars
    constraints_applied: tuple[str, ...]


def build(
    problem: Problem,
    constraints: Sequence[Constraint],
    preferences: Sequence[Preference],
) -> BuiltModel:
    model = cp_model.CpModel()
    x: AssignmentVars = {}
    for v in problem.volunteers:
        for shift in problem.shifts:
            x[(v.id, shift.index)] = model.NewBoolVar(
                f"x[{v.id},{shift.index}]"
            )

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
