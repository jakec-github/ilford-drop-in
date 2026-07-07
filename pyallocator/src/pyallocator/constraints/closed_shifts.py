"""Ensures closed shifts receive no allocations at all.

Go strips preallocations from closed shifts before sending the input
(and problem.py rejects any that slip through), so this never conflicts
with the preallocations constraint.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class ClosedShiftsConstraint:
    name = "closed_shifts"
    description = "closed shifts get no allocations"

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for shift in problem.shifts:
            if not shift.closed:
                continue
            for v in problem.volunteers:
                model.Add(x[(v.id, shift.index)] == 0)


CONSTRAINT = ClosedShiftsConstraint()
